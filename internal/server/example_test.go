package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"

	"backend-template-go/internal/infrastructure"
)

// createExampleAccessLogsSQL 建出与 V2 迁移同列的 SQLite 表，使纵切测试不依赖外部 MySQL。
const createExampleAccessLogsSQL = `CREATE TABLE access_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	trace_id TEXT NOT NULL,
	method TEXT NOT NULL,
	path TEXT NOT NULL,
	status_code INTEGER NOT NULL,
	duration_ms INTEGER NOT NULL,
	client_ip TEXT NOT NULL,
	created_at INTEGER NOT NULL
)`

// newExampleDB 建立三条按时间递增的访问日志，连接由测试和 App.Close 共同释放。
func newExampleDB(t *testing.T) gdb.DB {
	t.Helper()
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Name: filepath.Join(t.TempDir(), "example.db")})
	if err != nil {
		t.Fatalf("初始化 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	ctx := context.Background()
	if _, err = db.Exec(ctx, createExampleAccessLogsSQL); err != nil {
		t.Fatalf("建立访问日志表失败: %v", err)
	}
	for i := 1; i <= 3; i++ {
		_, err = db.Exec(ctx,
			"INSERT INTO access_logs(trace_id, method, path, status_code, duration_ms, client_ip, created_at) VALUES(?,?,?,?,?,?,?)",
			fmt.Sprintf("trace-%d", i), "GET", "/backend/v1/health", 200, i, "192.0.2.1", i*1000)
		if err != nil {
			t.Fatalf("写入访问日志失败: %v", err)
		}
	}
	return db
}

// TestExampleVerticalSlice 验证四层纵切：事务内一致性读、分页契约、响应字段最小化和出站追踪头。
func TestExampleVerticalSlice(t *testing.T) {
	var (
		mu              sync.Mutex
		upstreamTraceID string
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		upstreamTraceID = r.Header.Get("X-Trace-Id")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	c := testConfig(t)
	c.MySQL = true
	c.ExampleUpstreamOrigin = upstream.URL
	app, err := Build(c, &infrastructure.Resources{DB: newExampleDB(t)})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)

	first := envelope(t, "GET", base+"/backend/v1/example/logs?page=1&pageSize=2", "", 200)
	data := first["data"].(map[string]any)
	list := data["list"].([]any)
	if len(list) != 2 {
		t.Fatalf("first page length = %d, want 2", len(list))
	}
	pagination := data["pagination"].(map[string]any)
	if pagination["page"] != float64(1) || pagination["pageSize"] != float64(2) ||
		pagination["total"] != float64(3) || pagination["totalPages"] != float64(2) {
		t.Fatalf("unexpected pagination: %v", pagination)
	}
	item := list[0].(map[string]any)
	if item["traceId"] != "trace-3" {
		t.Fatalf("records are not ordered by created_at desc: %v", item)
	}
	for _, hidden := range []string{"id", "clientIp", "client_ip"} {
		if _, ok := item[hidden]; ok {
			t.Fatalf("response leaked persistence field %s", hidden)
		}
	}

	last := envelope(t, "GET", base+"/backend/v1/example/logs?page=3&pageSize=2", "", 200)
	empty, ok := last["data"].(map[string]any)["list"].([]any)
	if !ok || len(empty) != 0 {
		t.Fatalf("empty page must encode []: %v", last["data"])
	}
	envelope(t, "GET", base+"/backend/v1/example/logs?page=0", "", 400)
	envelope(t, "GET", base+"/backend/v1/example/logs?pageSize=101", "", 400)

	probe := envelope(t, "GET", base+"/backend/v1/example/upstream", "", 200)
	if probe["data"].(map[string]any)["statusCode"] != float64(200) {
		t.Fatalf("unexpected upstream status: %v", probe["data"])
	}
	mu.Lock()
	defer mu.Unlock()
	if upstreamTraceID == "" || upstreamTraceID != probe["traceId"] {
		t.Fatalf("outbound trace header %q disagrees with response %v", upstreamTraceID, probe["traceId"])
	}
}

// TestExampleUpstreamResultIsCached 验证 Cache 已接入服务，命中后不再发起第二次出站请求。
func TestExampleUpstreamResultIsCached(t *testing.T) {
	var (
		mu   sync.Mutex
		hits int
	)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	mini := miniredis.RunT(t)
	client, err := gredis.New(&gredis.Config{
		Address: mini.Addr(), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t)
	c.Redis = true
	c.ExampleUpstreamOrigin = upstream.URL
	app, err := Build(c, &infrastructure.Resources{Redis: client})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)

	for i := 0; i < 2; i++ {
		envelope(t, "GET", base+"/backend/v1/example/upstream", "", 200)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("upstream was called %d times, want 1 with cache", hits)
	}
}

// TestExampleDegradesWhenDependenciesDisabled 验证默认零外部依赖启动时示例接口返回安全提示而非 panic。
func TestExampleDegradesWhenDependenciesDisabled(t *testing.T) {
	app, err := Build(testConfig(t), &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	envelope(t, "GET", base+"/backend/v1/example/logs", "", 503)
	envelope(t, "GET", base+"/backend/v1/example/upstream", "", 501)
}
