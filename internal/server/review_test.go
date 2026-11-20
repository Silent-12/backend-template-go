package server

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"

	"backend-template-go/internal/config"
	"backend-template-go/internal/infrastructure"
)

type logCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *logCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.Write(data)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

func TestUnhandledErrorsDoNotLeakIntoLogs(t *testing.T) {
	c := testConfig(t)
	var capture logCapture
	logger := g.Log()
	previous := logger.GetConfig()
	logging := previous
	logging.Writer = &capture
	logging.StdoutPrint = false
	if err := logger.SetConfig(logging); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.SetConfig(previous); err != nil {
			t.Error(err)
		}
	})
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	app.Server.BindHandler("GET:/sensitive-error", func(r *ghttp.Request) {
		r.SetError(gerror.NewCode(gcode.WithCode(gcode.CodeInternalError, map[string]string{"token": "detail-secret"}), "password=error-secret https://upstream.invalid?token=query-secret"))
	})
	base := startApp(t, app)
	result := envelope(t, "GET", base+"/sensitive-error", "", 500)
	logged := capture.String()
	for _, secret := range []string{"error-secret", "query-secret", "detail-secret"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("unhandled error leaked sensitive content into logs: %s", secret)
		}
	}
	if !strings.Contains(logged, "traceId="+result["traceId"].(string)) || !strings.Contains(logged, "errorType=") {
		t.Fatal("safe diagnostics lost the trace ID or error classification")
	}
}

func TestIncomingRequestsLogURL(t *testing.T) {
	c := testConfig(t)
	var capture logCapture
	logger := g.Log()
	previous := logger.GetWriter()
	logger.SetWriter(&capture)
	t.Cleanup(func() { logger.SetWriter(previous) })

	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	for _, test := range []struct {
		method string
		path   string
		body   string
		status int
	}{
		{http.MethodGet, c.OpenAPIPath(), "", http.StatusOK},
		{http.MethodPost, "/missing%20route", `{"data":"body-marker"}`, http.StatusNotFound},
	} {
		res, _ := request(t, test.method, base+test.path+"?token=query-marker", test.body)
		if res.StatusCode != test.status {
			t.Fatalf("%s %s: status = %d, want %d", test.method, test.path, res.StatusCode, test.status)
		}
		entry := "[INFO] method=" + test.method + " url=\"" + test.path + "\""
		if count := strings.Count(capture.String(), entry); count != 1 {
			t.Fatalf("expected exactly one log for %s %s, got %d", test.method, test.path, count)
		}
	}
	if logged := capture.String(); strings.Contains(logged, "query-marker") || strings.Contains(logged, "body-marker") {
		t.Fatal("request log exposed query or body")
	}
}

type identityReq struct {
	g.Meta `path:"/identity/{itemNo}" method:"patch"`
	ItemNo string `json:"itemNo" in:"path"`
	Name   string `json:"name"`
}

type identityRes struct {
	ItemNo string `json:"itemNo"`
}

type identityController struct{}

func (*identityController) Update(_ context.Context, req *identityReq) (*identityRes, error) {
	return &identityRes{ItemNo: req.ItemNo}, nil
}

// TestPathIdentityAndBodyLimit 使用测试专用路由验证路径参数优先级和请求体上限。
func TestPathIdentityAndBodyLimit(t *testing.T) {
	app, err := Build(testConfig(t), &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	app.Server.BindObject("/", &identityController{})
	base := startApp(t, app)
	for _, name := range []string{"itemNo", "ItemNo", "item_no", "ITEMNO", "item-no"} {
		result := envelope(t, "PATCH", base+"/identity/first?"+name+"=second", `{"itemNo":"second","name":"ok"}`, 200)
		if result["data"].(map[string]any)["itemNo"] != "first" {
			t.Fatalf("query %s or body replaced path identity", name)
		}
	}
	envelope(t, "PATCH", base+"/identity/first", `{"name":"`+strings.Repeat("x", 2<<20)+`"}`, 413)
}

type scalarReq struct {
	g.Meta `path:"/review-scalars" method:"post"`
	Count  uint8   `json:"count"`
	Ratio  float32 `json:"ratio"`
}

type scalarController struct{}

func (*scalarController) Check(_ context.Context, _ *scalarReq) (*struct{}, error) {
	return nil, nil
}

func TestUnsignedAndFloatRequestTypes(t *testing.T) {
	app, err := Build(testConfig(t), &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	app.Server.BindObject("/", &scalarController{})
	base := startApp(t, app)
	for _, body := range []string{
		`{"count":-1,"ratio":1}`,
		`{"count":256,"ratio":1}`,
		`{"count":1.5,"ratio":1}`,
		`{"count":[],"ratio":1}`,
		`{"count":1,"ratio":{}}`,
		`{"count":1,"ratio":"NaN"}`,
		`{"count":1,"ratio":1e100}`,
	} {
		envelope(t, "POST", base+"/review-scalars", body, 400)
	}
	// 上界和小数均可正常通过，避免只拒绝非法值却破坏合法的派生服务 DTO。
	envelope(t, "POST", base+"/review-scalars", `{"count":255,"ratio":1.25}`, 200)
}

func TestServiceIdentityIsUsedInDocumentation(t *testing.T) {
	for _, name := range []string{"orders", "billing"} {
		c := testConfig(t)
		c.AppName = name
		app, err := Build(c, &infrastructure.Resources{})
		if err != nil {
			t.Fatal(err)
		}
		base := startApp(t, app)
		if app.Server.GetName() != name {
			t.Fatal("server name did not follow service identity")
		}
		for _, path := range []string{"doc.html", "api-json"} {
			res, body := request(t, "GET", base+"/backend/"+path, "")
			if res.StatusCode != 200 || !strings.Contains(string(body), name+" API") {
				t.Fatalf("documentation retained template identity: %s", path)
			}
		}
	}
}

func TestReviewErrorResponses(t *testing.T) {
	app, err := Build(testConfig(t), &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{401, 403, 404, 409, 429, 503} {
		app.Server.BindHandler("GET:/status/"+http.StatusText(status), func(r *ghttp.Request) { r.Response.WriteHeader(status) })
	}
	app.Server.BindHandler("GET:/coded-not-found", func(r *ghttp.Request) { r.SetError(gerror.NewCode(gcode.CodeNotFound, "private details")) })
	app.Server.BindHandler("GET:/coded-validation-detail", func(r *ghttp.Request) {
		r.SetError(gerror.NewCode(gcode.WithCode(gcode.CodeValidationFailed, map[string]string{"field": "private detail"}), "private validation message"))
	})
	app.Server.BindHandler("GET:/uncommitted-stream", func(r *ghttp.Request) {
		r.Response.Header().Set("Content-Type", "text/event-stream")
		panic("private SQL details")
	})
	app.Server.BindHandler("GET:/uncommitted-download", func(r *ghttp.Request) {
		r.Response.Header().Set("Content-Disposition", "attachment; filename=private.txt")
		panic("private SQL details")
	})
	app.Server.BindHandler("GET:/raw-probe", func(r *ghttp.Request) {
		r.Response.WriteHeader(503)
		r.Response.WriteJson(map[string]string{"status": "unavailable"})
	})
	base := startApp(t, app)
	for _, status := range []int{401, 403, 404, 409, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			envelope(t, "GET", base+"/status/"+strings.ReplaceAll(http.StatusText(status), " ", "%20"), "", status)
		})
	}
	t.Run("framework not found", func(t *testing.T) { envelope(t, "GET", base+"/coded-not-found", "", 404) })
	t.Run("non-comparable error detail", func(t *testing.T) { envelope(t, "GET", base+"/coded-validation-detail", "", 400) })
	t.Run("stream panic", func(t *testing.T) {
		res, body := request(t, "GET", base+"/uncommitted-stream", "")
		if res.StatusCode != 500 || strings.Contains(string(body), "private") {
			t.Fatalf("stream error leaked: %d %s", res.StatusCode, body)
		}
	})
	t.Run("download panic", func(t *testing.T) {
		res, body := request(t, "GET", base+"/uncommitted-download", "")
		if res.StatusCode != 500 || strings.Contains(string(body), "private") || res.Header.Get("Content-Disposition") != "" || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
			t.Fatalf("download error leaked: %d %s", res.StatusCode, body)
		}
	})
	t.Run("probe protocol", func(t *testing.T) {
		res, body := request(t, "GET", base+"/raw-probe", "")
		if res.StatusCode != 503 || string(body) != `{"status":"unavailable"}` {
			t.Fatalf("probe response wrapped: %d %s", res.StatusCode, body)
		}
	})
}

func TestReviewDocsRouteAliases(t *testing.T) {
	c := testConfig(t)
	c.DocsAuth = true
	c.DocsUsername = "reader"
	c.DocsPassword = "secret"
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	for _, path := range []string{"/backend/API-JSON", "/BACKEND/doc.html", "/backend/doc.html/", "/backend/api-json/"} {
		res, _ := request(t, "GET", base+path, "")
		if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusNotFound {
			t.Errorf("unprotected alias %s: %d", path, res.StatusCode)
		}
	}
}

func TestSecurityHeadersAndCORS(t *testing.T) {
	c := testConfig(t)
	c.Env = config.EnvProduction
	c.CORSOrigins = []string{"https://app.example.com"}
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	for _, origin := range []string{"https://app.example.com", "https://untrusted.example.com"} {
		res, err := testClient.R().SetContext(context.Background()).SetHeader("Origin", origin).SetHeader("Access-Control-Request-Method", "GET").Options(base + "/backend/v1/health")
		if err != nil {
			t.Fatal(err)
		}
		body := res.Body()
		if origin == c.CORSOrigins[0] {
			if res.StatusCode() != 204 || len(body) != 0 || res.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatalf("allowed preflight failed: %d %s", res.StatusCode(), body)
			}
		} else if res.StatusCode() != 403 || res.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("untrusted CORS origin accepted")
		}
	}
	res, _ := request(t, "GET", base+"/backend/v1/health", "")
	for _, header := range []string{"Content-Security-Policy", "Strict-Transport-Security", "X-Content-Type-Options", "X-Frame-Options"} {
		if res.Header.Get(header) == "" {
			t.Errorf("missing security header %s", header)
		}
	}
	if res.Header.Get("X-Trace-Id") == "" || res.Header.Get("X-Trace-Id") != res.Header.Get("Trace-Id") {
		t.Fatal("framework trace and response trace disagree")
	}
}

type unavailableDB struct{ gdb.DB }

func (unavailableDB) Close(context.Context) error { return nil }

func (unavailableDB) GetValue(context.Context, string, ...any) (gdb.Value, error) {
	return nil, gerror.New("private connection error")
}

func TestReadinessFailureAndDependencyConfiguration(t *testing.T) {
	c := testConfig(t)
	if _, err := Build(c, nil); err == nil {
		t.Fatal("nil resources accepted")
	}
	c.HTTPLog = true
	if _, err := Build(c, &infrastructure.Resources{}); err == nil {
		t.Fatal("database log without DB accepted")
	}
	c.HTTPLog = false
	c.MySQL = true
	app, err := Build(c, &infrastructure.Resources{DB: unavailableDB{}})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	res, body := request(t, "GET", base+"/backend/readyz", "")
	if res.StatusCode != 503 || string(body) != `{"status":"unavailable"}` {
		t.Fatalf("bad failed readiness: %d %s", res.StatusCode, body)
	}
	res, body = request(t, "GET", base+"/backend/livez", "")
	if res.StatusCode != 200 || string(body) != `{"status":"ok"}` {
		t.Fatalf("dependency failure broke liveness: %d %s", res.StatusCode, body)
	}
}
