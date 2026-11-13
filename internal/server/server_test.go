package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/glog"

	"backend-template-go/internal/config"
	"backend-template-go/internal/httpclient"
	"backend-template-go/internal/infrastructure"
)

var (
	testClient *resty.Client
	// testLogDir 是包级测试日志目录，保证 go test 不向仓库写入任何文件。
	// GoFrame logger 在进程内持有日志文件句柄，Windows 上删除打开中的文件会失败，
	// 因此不用 t.TempDir() 承载日志，也不在 TestMain 结束时删除，交由操作系统清理临时目录。
	testLogDir string
)

func TestMain(m *testing.M) {
	client, err := httpclient.New(httpclient.Options{Timeout: 5 * time.Second})
	if err != nil {
		panic(err)
	}
	testClient = client
	if testLogDir, err = os.MkdirTemp("", "backend-template-go-test-*"); err != nil {
		panic(err)
	}
	if err = g.Log().SetConfig(testLogConfig()); err != nil {
		panic(err)
	}
	code := m.Run()
	client.GetClient().CloseIdleConnections()
	os.Exit(code)
}

// testLogConfig 返回写入包级临时目录且不打印到控制台的日志配置，供全局 logger 和框架服务复用。
func testLogConfig() glog.Config {
	logging := glog.DefaultConfig()
	logging.Path = testLogDir
	logging.StdoutPrint = false
	return logging
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	if err := g.Log().SetConfig(testLogConfig()); err != nil {
		t.Fatal(err)
	}
	c, err := config.Parse(func(string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	c.Port = 0
	c.Host = "127.0.0.1"
	return c
}

func startApp(t *testing.T, app *App) string {
	t.Helper()
	// 框架服务的 logger 独立于全局 logger，同样改写到包级临时目录，避免测试向仓库写入文件。
	if err := app.Server.Logger().SetConfig(testLogConfig()); err != nil {
		t.Fatal(err)
	}
	if err := app.Server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	return fmt.Sprintf("http://127.0.0.1:%d", app.Server.GetListenedPort())
}

func request(t *testing.T, method, url, body string) (*http.Response, []byte) {
	t.Helper()
	req := testClient.R().SetContext(context.Background())
	if body != "" {
		req.SetHeader("Content-Type", "application/json").SetBody(body)
	}
	res, err := req.Execute(method, url)
	if err != nil {
		t.Fatal(err)
	}
	return res.RawResponse, res.Body()
}

func envelope(t *testing.T, method, url, body string, status int) map[string]any {
	t.Helper()
	res, data := request(t, method, url, body)
	if res.StatusCode != status {
		t.Fatalf("%s %s: got %d want %d: %s", method, url, res.StatusCode, status, data)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid json: %s", data)
	}
	if result["code"] != float64(status) || result["traceId"] == "" || result["traceId"] != res.Header.Get("X-Trace-Id") {
		t.Fatalf("bad envelope: %s headers=%v", data, res.Header)
	}
	return result
}

func TestHTTPContract(t *testing.T) {
	c := testConfig(t)
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	app.Server.BindHandler("GET:/backend/v1/panic", func(r *ghttp.Request) { panic("secret SQL password=test-secret") })
	base := startApp(t, app)
	health := envelope(t, "GET", base+"/backend/v1/health", "", 200)["data"].(map[string]any)
	if _, ok := health["uptimeSeconds"]; !ok {
		t.Fatal("startup uptime is missing")
	}
	if health["memoryUsage"].(map[string]any)["sysBytes"].(float64) <= 0 {
		t.Fatal("runtime memory diagnostics are missing")
	}
	envelope(t, "GET", base+"/backend/v1/missing", "", 404)
	for _, probe := range []string{"livez", "readyz"} {
		res, data := request(t, "GET", base+"/backend/"+probe, "")
		if res.StatusCode != 200 || string(data) != `{"status":"ok"}` {
			t.Fatalf("bad probe: %d %s", res.StatusCode, data)
		}
	}
	result := envelope(t, "GET", base+"/backend/v1/panic", "", 500)
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "test-secret") || strings.Contains(string(encoded), "SQL") {
		t.Fatal("internal error leaked")
	}
	res, data := request(t, "GET", base+"/backend/api-json", "")
	if res.StatusCode != 200 || !strings.Contains(string(data), "/backend/v1/health") || !strings.Contains(string(data), "traceId") {
		t.Fatalf("bad OpenAPI: %d %s", res.StatusCode, data)
	}
}

func TestDocsAndProduction(t *testing.T) {
	c := testConfig(t)
	c.DocsAuth = true
	c.DocsUsername = "reader"
	c.DocsPassword = "local-test"
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	for _, path := range []string{"doc.html", "api-json"} {
		envelope(t, "GET", base+"/backend/"+path, "", 401)
		res, err := testClient.R().SetContext(context.Background()).SetBasicAuth("reader", "local-test").Get(base + "/backend/" + path)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode() != 200 {
			t.Fatalf("authenticated docs: %d", res.StatusCode())
		}
	}
	// 内部路由覆盖头不能绕过文档认证。
	overrideRes, err := testClient.R().SetContext(context.Background()).SetHeader(ghttp.HeaderXUrlPath, "/backend/api-json").Get(base + "/public")
	if err != nil {
		t.Fatal(err)
	}
	if overrideRes.StatusCode() != http.StatusNotFound {
		t.Fatalf("route override accepted: %d", overrideRes.StatusCode())
	}
	prod := testConfig(t)
	prod.Env = config.EnvProduction
	prodApp, err := Build(prod, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	prodBase := startApp(t, prodApp)
	result := envelope(t, "GET", prodBase+"/backend/v1/health", "", 200)
	if len(result["data"].(map[string]any)) != 1 {
		t.Fatalf("production health leaks details: %v", result)
	}
	envelope(t, "GET", prodBase+"/backend/api-json", "", 404)
	envelope(t, "GET", prodBase+"/backend/doc.html", "", 404)
}

// TestInboundTraceHeadersCannotChooseTraceID 验证客户端无法通过 W3C 追踪头指定服务端追踪号。
// 直接调用服务处理入口而不是仓库的 Resty 工厂：工厂会主动剥离入站追踪头，
// 无法复现“客户端直填 traceparent”的攻击场景。
func TestInboundTraceHeadersCannotChooseTraceID(t *testing.T) {
	app, err := Build(testConfig(t), &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	startApp(t, app)
	const clientTraceID = "4bf92f3577b34da6a3ce929d0e0e4736"
	req := httptest.NewRequest(http.MethodGet, "/backend/v1/health", nil)
	req.Header.Set(headerTraceparent, "00-"+clientTraceID+"-00f067aa0ba902b7-01")
	req.Header.Set(headerTracestate, "vendor=value")
	recorder := httptest.NewRecorder()
	app.Server.GetHandler()(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("handler status: %d %s", recorder.Code, recorder.Body.String())
	}
	traceID := recorder.Header().Get("X-Trace-Id")
	if traceID == "" || traceID == clientTraceID {
		t.Fatalf("client traceparent controlled trace id: %q", traceID)
	}
	if frameworkTraceID := recorder.Header().Get("Trace-Id"); frameworkTraceID != traceID {
		t.Fatalf("framework trace id %q disagrees with response trace id %q", frameworkTraceID, traceID)
	}
	var body map[string]any
	if err = json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %s", recorder.Body.String())
	}
	if body["traceId"] != traceID {
		t.Fatalf("body trace id %v disagrees with header %q", body["traceId"], traceID)
	}
}
