package middleware

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"

	"backend-template-go/internal/config"
	"backend-template-go/internal/httpclient"
)

func TestClientIPTrustBoundary(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, tc := range []struct{ peer, forwarded, expected string }{
		{"192.0.2.1:10", "198.51.100.8", "192.0.2.1"},
		{"10.0.0.1:10", "198.51.100.8, 10.0.0.2", "198.51.100.8"},
		{"10.0.0.1:10", "203.0.113.3, 192.0.2.5", "192.0.2.5"},
		{"[::ffff:192.0.2.1]:10", "198.51.100.8", "192.0.2.1"},
		{"10.0.0.1:10", "bad-ip", "10.0.0.1"},
	} {
		r := httptest.NewRequest("GET", "http://example.com", nil)
		r.RemoteAddr = tc.peer
		r.Header.Set("X-Forwarded-For", tc.forwarded)
		if got := ClientIP(r, trusted); got != tc.expected {
			t.Errorf("peer=%s forwarded=%s got=%s want=%s", tc.peer, tc.forwarded, got, tc.expected)
		}
	}
}

// TestFallbackTraceIDFitsAccessLogColumn 验证兜底追踪号为 32 位十六进制，
// 与 access_logs.trace_id 的列宽一致，避免兜底分支触发后访问日志写入失败或被静默截断。
func TestFallbackTraceIDFitsAccessLogColumn(t *testing.T) {
	first := fallbackTraceID()
	if len(first) != 32 {
		t.Fatalf("兜底追踪号长度为 %d，期望 32", len(first))
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("兜底追踪号不是十六进制: %v", err)
	}
	if second := fallbackTraceID(); second == first {
		t.Fatal("兜底追踪号未随机生成")
	}
}

// TestCORSPolicyOnlyAllowsCredentialsForListedOrigins 验证跨域凭据只在显式白名单命中时开启，
// 非生产环境的空白名单与通配符放行来源但不允许携带凭据，避免任意站点借用户凭据读取响应。
func TestCORSPolicyOnlyAllowsCredentialsForListedOrigins(t *testing.T) {
	for _, tc := range []struct {
		name        string
		env         string
		origins     []string
		origin      string
		allowed     bool
		credentials bool
	}{
		{"listed origin", config.EnvProduction, []string{"https://app.example.com"}, "https://app.example.com", true, true},
		{"unlisted origin", config.EnvProduction, []string{"https://app.example.com"}, "https://evil.example.com", false, false},
		{"production empty list", config.EnvProduction, nil, "https://app.example.com", false, false},
		{"development empty list", config.EnvDevelopment, nil, "https://any.example.com", true, false},
		{"development wildcard", config.EnvDevelopment, []string{"*"}, "https://any.example.com", true, false},
		{"listed origin wins over wildcard", config.EnvDevelopment, []string{"*", "https://app.example.com"}, "https://app.example.com", true, true},
	} {
		allowed, credentials := corsPolicy(tc.origin, config.Config{Env: tc.env, CORSOrigins: tc.origins})
		if allowed != tc.allowed || credentials != tc.credentials {
			t.Errorf("%s: got allowed=%v credentials=%v, want %v/%v", tc.name, allowed, credentials, tc.allowed, tc.credentials)
		}
	}
}

// startCORSServer 启动一台只挂载 Entry 中间件的测试服务器，用于断言实际跨域响应头。
// 服务器由测试 Cleanup 关闭，使用独立实例名避免与其他 ghttp 实例互相影响。
func startCORSServer(t *testing.T, c config.Config) string {
	t.Helper()
	server := ghttp.GetServer("cors-test-" + uuid.NewString())
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.SetAccessLogEnabled(false)
	server.SetErrorLogEnabled(false)
	server.Use(Entry(c, nil))
	server.BindHandler("ALL:/probe", func(r *ghttp.Request) {
		r.Response.WriteJson(map[string]string{"status": "ok"})
	})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown() })
	return fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
}

// preflight 使用统一 Resty 客户端发送跨域预检请求，返回状态码与响应头。
func preflight(t *testing.T, base, origin string) (int, http.Header) {
	t.Helper()
	registry := httpclient.NewRegistry()
	t.Cleanup(registry.Close)
	client, err := registry.New(httpclient.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.R().SetContext(context.Background()).
		SetHeader("Origin", origin).
		SetHeader("Access-Control-Request-Method", "GET").
		Options(base + "/probe")
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode(), res.Header()
}

// TestCORSResponseHeaders 验证预检响应头：显式白名单命中返回 204 且允许凭据，
// 非生产空白名单放行但不声明凭据，未列出来源返回安全 403。
func TestCORSResponseHeaders(t *testing.T) {
	base := startCORSServer(t, config.Config{Env: config.EnvProduction, CORSOrigins: []string{"https://app.example.com"}})
	status, header := preflight(t, base, "https://app.example.com")
	if status != http.StatusNoContent || header.Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("白名单来源预检失败: %d %v", status, header)
	}
	if header.Get("Access-Control-Allow-Credentials") != "true" || header.Get("Access-Control-Expose-Headers") != "X-Trace-Id,Retry-After" {
		t.Fatalf("白名单来源缺少凭据或暴露头: %v", header)
	}
	status, header = preflight(t, base, "https://evil.example.com")
	if status != http.StatusForbidden || header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("未列出来源被放行: %d %v", status, header)
	}

	development := startCORSServer(t, config.Config{Env: config.EnvDevelopment})
	status, header = preflight(t, development, "https://any.example.com")
	if status != http.StatusNoContent || header.Get("Access-Control-Allow-Origin") != "https://any.example.com" {
		t.Fatalf("开发环境空白名单未放行来源: %d %v", status, header)
	}
	if header.Get("Access-Control-Allow-Credentials") != "" {
		t.Fatal("非生产空白名单不应声明允许凭据")
	}
}
