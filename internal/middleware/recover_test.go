package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"

	"backend-template-go/internal/httpclient"
)

// syncBuffer 是并发安全的日志缓冲，用于断言异常只记录安全分类信息。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog 把默认日志器的输出重定向到内存缓冲，返回读取函数。
// 断言依赖真实输出文本，因此测试不使用 t.Parallel，结束时恢复原日志配置。
func captureLog(t *testing.T) func() string {
	t.Helper()
	logger := g.Log()
	original := logger.GetConfig()
	buffer := &syncBuffer{}
	config := original
	config.Handlers = nil
	config.Path = ""
	config.StdoutPrint = false
	config.Writer = buffer
	if err := logger.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.SetConfig(original); err != nil {
			t.Error(err)
		}
	})
	return buffer.String
}

// startRecoverServer 启动一台按生产装配顺序注册中间件的测试服务器，返回访问地址。
// 顺序为 Recover、给定中间件、HandlerResponse，最后绑定探针路由；probe 为 nil 时返回正常 JSON。
// 服务器由测试 Cleanup 关闭，使用独立实例名避免与仓库中其他 ghttp 实例互相影响。
func startRecoverServer(t *testing.T, middlewares []ghttp.HandlerFunc, probe ghttp.HandlerFunc) string {
	t.Helper()
	if probe == nil {
		probe = func(r *ghttp.Request) {
			r.Response.WriteJson(map[string]string{"status": "ok"})
		}
	}
	server := ghttp.GetServer("recover-test-" + uuid.NewString())
	server.SetAddr("127.0.0.1:0")
	server.SetDumpRouterMap(false)
	server.SetAccessLogEnabled(false)
	server.SetErrorLogEnabled(false)
	handlers := append([]ghttp.HandlerFunc{Recover()}, middlewares...)
	server.Use(append(handlers, HandlerResponse)...)
	server.BindHandler("GET:/probe", probe)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Shutdown()
	})
	return fmt.Sprintf("http://127.0.0.1:%d", server.GetListenedPort())
}

// requestProbe 通过统一 Resty 客户端请求探针地址，返回状态码、响应体和追踪头。
// 客户端由 Registry 创建并在测试结束时统一关闭，符合业务不得自行创建或关闭客户端的约定。
func requestProbe(t *testing.T, base string) (int, []byte, string) {
	t.Helper()
	registry := httpclient.NewRegistry()
	t.Cleanup(registry.Close)
	client, err := registry.New(httpclient.Options{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.R().SetContext(context.Background()).Get(base + "/probe")
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode(), res.Body(), res.Header().Get("X-Trace-Id")
}

// TestRecoverHidesPanicBeforeResponseMiddleware 验证 panic 发生在统一响应中间件之前时，
// 最外层 Recover 丢弃框架写入的异常文本，返回 500 安全 JSON，且日志不含 panic 原文。
// 该场景对应入口与限流中间件自身的缺陷：框架此时只设置请求错误并直接写入响应缓冲。
func TestRecoverHidesPanicBeforeResponseMiddleware(t *testing.T) {
	output := captureLog(t)
	base := startRecoverServer(t, []ghttp.HandlerFunc{
		func(r *ghttp.Request) {
			r.Response.Header().Set("X-Trace-Id", "trace-panic")
			panic("secret-token=abc123")
		},
	}, nil)
	status, body, traceID := requestProbe(t, base)
	if status != 500 {
		t.Fatalf("panic 响应状态为 %d，期望 500: %s", status, body)
	}
	if strings.Contains(string(body), "secret-token") {
		t.Fatalf("响应体泄露 panic 原文: %s", body)
	}
	var envelope Response
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("panic 响应不是标准业务 JSON: %s", body)
	}
	if envelope.Code != 500 || envelope.Message != "服务器内部错误" || envelope.TraceID != "trace-panic" || traceID != "trace-panic" {
		t.Fatalf("panic 响应内容不安全: %+v traceId=%s", envelope, traceID)
	}
	logs := output()
	if strings.Contains(logs, "secret-token") {
		t.Fatalf("日志泄露 panic 原文: %s", logs)
	}
	if !strings.Contains(logs, "traceId=trace-panic") {
		t.Fatalf("日志缺少追踪号: %s", logs)
	}
}

// TestRecoverKeepsHandlerPanicSanitized 验证处理器 panic 先由统一响应中间件脱敏，
// 最外层 Recover 不再重复改写状态码与响应体。
func TestRecoverKeepsHandlerPanicSanitized(t *testing.T) {
	output := captureLog(t)
	base := startRecoverServer(t, []ghttp.HandlerFunc{
		func(r *ghttp.Request) {
			r.Response.Header().Set("X-Trace-Id", "trace-handler")
			r.Middleware.Next()
		},
	}, func(*ghttp.Request) {
		panic("sql password=leaked")
	})
	status, body, _ := requestProbe(t, base)
	if status != 500 || strings.Contains(string(body), "leaked") {
		t.Fatalf("处理器 panic 响应未脱敏: %d %s", status, body)
	}
	if strings.Contains(output(), "leaked") {
		t.Fatalf("日志泄露 panic 原文: %s", output())
	}
}

// TestRecoverLeavesBusinessErrorUntouched 验证普通业务错误仍由统一响应中间件映射状态码，
// 最外层 Recover 不会把它改写成 500。
func TestRecoverLeavesBusinessErrorUntouched(t *testing.T) {
	captureLog(t)
	base := startRecoverServer(t, []ghttp.HandlerFunc{
		func(r *ghttp.Request) {
			r.Response.Header().Set("X-Trace-Id", "trace-business")
			r.SetError(gerror.NewCode(gcode.CodeInvalidParameter, "测试参数错误"))
			r.Middleware.Next()
		},
	}, nil)
	status, body, _ := requestProbe(t, base)
	if status != 400 {
		t.Fatalf("业务错误状态为 %d，期望 400: %s", status, body)
	}
}
