package server

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
)

// TestMySQLRedisIntegration 只连接 compose.test.yaml 的专用本地实例。
// 设置 INTEGRATION_TEST=true 显式启用，避免测试误操作业务数据库。
func TestMySQLRedisIntegration(t *testing.T) {
	if os.Getenv("INTEGRATION_TEST") != "true" {
		t.Skip("requires local disposable MySQL/Redis: manifest/docker/compose.test.yaml")
	}
	c := testConfig(t)
	c.MySQL, c.Redis, c.HTTPLog, c.Throttle = true, true, true, true
	c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName = "127.0.0.1", 13306, "app", "local-test-password", "backend_template_test"
	c.RedisHost, c.RedisPort, c.RedisDB = "127.0.0.1", 16379, 15
	// 每次测试隔离 Redis key，不使用 FLUSHDB。
	c.ThrottleKeyPrefix = "backend-template-test-" + uuid.NewString()
	app, err := New(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	app.Server.BindHandler("GET:/backend/v1/user/integration", func(r *ghttp.Request) { r.Response.Write("ok") })
	base := startApp(t, app)
	ctx := context.Background()
	// 使用测试路由验证限流和访问日志。
	created, err := testClient.R().SetContext(ctx).Get(base + "/backend/v1/user/integration?private=value")
	if err != nil || created.StatusCode() != http.StatusOK {
		t.Fatalf("test route failed: %v", err)
	}
	traceID := created.Header().Get("X-Trace-Id")
	rateLimited := false
	for i := 0; i < 31; i++ {
		res, _ := request(t, "GET", base+"/backend/v1/user/integration", "")
		if res.StatusCode == http.StatusTooManyRequests {
			if res.Header.Get("Retry-After") == "" {
				t.Fatal("rate limit missing Retry-After")
			}
			rateLimited = true
			break
		}
		if res.StatusCode != http.StatusOK {
			t.Fatalf("unexpected rate-limit response: %d", res.StatusCode)
		}
	}
	if !rateLimited {
		t.Fatal("user API rate limit did not activate")
	}
	// 关闭时等待访问日志入库，并验证不存储用户传入路径和请求体。
	_ = app.Server.Shutdown()
	app.logs.Close()
	count, err := app.resources.DB.Model("access_logs").Ctx(ctx).Where("trace_id", traceID).Where("path", "/backend/v1/user/integration").Count()
	if err != nil || count == 0 {
		t.Fatalf("access log missing: count=%d err=%v", count, err)
	}
	// 使用独立命名空间验证未知路由封禁，避免前面的频率封禁干扰。
	c.ThrottleKeyPrefix = "backend-template-test-" + uuid.NewString()
	second, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	base = startApp(t, second)
	for i := 0; i < 5; i++ {
		envelope(t, "GET", base+"/backend/v1/not-found", "", 404)
	}
	res, _ := request(t, "GET", base+"/backend/v1/health", "")
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("missing-route ban failed: %d", res.StatusCode)
	}

}
