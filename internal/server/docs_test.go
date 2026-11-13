package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gogf/gf/v2/database/gredis"

	"backend-template-go/internal/config"
	"backend-template-go/internal/infrastructure"
)

func TestKnife4xEmbeddedUI(t *testing.T) {
	for _, prefix := range []string{"backend", "", "services/example"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			c := testConfig(t)
			c.APIPrefix = prefix
			c.AppName = "example-service"
			app, err := Build(c, &infrastructure.Resources{})
			if err != nil {
				t.Fatal(err)
			}
			base := startApp(t, app)
			res, body := request(t, http.MethodGet, base+c.DocsPath(), "")
			page := string(body)
			if res.StatusCode != http.StatusOK || !strings.Contains(page, "window.__KNIFE4X_CONFIG__=") || !strings.Contains(page, "<title>example-service API</title>") {
				t.Fatalf("invalid Knife4x page: %d %s", res.StatusCode, page)
			}
			if res.Header.Get("Cache-Control") != "no-store" || res.ContentLength != int64(len(body)) {
				t.Fatalf("invalid cache or content length: %v", res.Header)
			}
			match := regexp.MustCompile(`window\.__KNIFE4X_CONFIG__=(\{[^<]+\});`).FindStringSubmatch(page)
			if len(match) != 2 {
				t.Fatal("missing embedded configuration")
			}
			var browserConfig map[string]string
			if err := json.Unmarshal([]byte(match[1]), &browserConfig); err != nil {
				t.Fatal(err)
			}
			if browserConfig["specUrl"] != c.OpenAPIPath() {
				t.Fatalf("incorrect spec URL: %v", browserConfig)
			}
			csp := res.Header.Get("Content-Security-Policy")
			nonce := regexp.MustCompile(`'nonce-([^']+)'`).FindStringSubmatch(csp)
			if len(nonce) != 2 || !strings.Contains(csp, "worker-src 'self'") || strings.Contains(csp, "unsafe-eval") {
				t.Fatalf("incorrect docs CSP: %s", csp)
			}
			for _, script := range regexp.MustCompile(`<script[^>]*>`).FindAllString(page, -1) {
				if !strings.Contains(script, `nonce="`+nonce[1]+`"`) {
					t.Fatalf("script blocked by CSP: %s", script)
				}
			}
			// 遍历入口引用，验证所有资源同源且返回真实资源而非业务 JSON 或 HTML 回退。
			for _, asset := range regexp.MustCompile(`(?:src|href)="([^"]+)"`).FindAllStringSubmatch(page, -1) {
				if !strings.HasPrefix(asset[1], "./") {
					t.Fatalf("unexpected external UI resource: %s", asset[1])
				}
				path := c.BasePath() + "/" + strings.TrimPrefix(asset[1], "./")
				assetRes, data := request(t, http.MethodGet, base+path, "")
				if assetRes.StatusCode != http.StatusOK || len(data) == 0 {
					t.Fatalf("missing UI resource %s: %d", path, assetRes.StatusCode)
				}
				if strings.HasSuffix(path, ".js") {
					assertJavaScript(t, assetRes)
				}
				if strings.HasSuffix(path, "/assets/index.js") {
					worker := regexp.MustCompile(`schemaDocumentWorker-[A-Za-z0-9_-]+\.js`).Find(data)
					if len(worker) == 0 {
						t.Fatal("Schema Worker reference is missing")
					}
					workerRes, workerBody := request(t, http.MethodGet, base+c.BasePath()+"/assets/"+string(worker), "")
					assertJavaScript(t, workerRes)
					if workerRes.StatusCode != http.StatusOK || len(workerBody) == 0 || !strings.Contains(workerRes.Header.Get("Content-Security-Policy"), "script-src 'self'") {
						t.Fatal("Schema Worker is unavailable or its module imports are blocked")
					}
				}
			}
			for _, path := range []string{c.DocsPath(), c.BasePath() + "/assets/index.js"} {
				head, data := request(t, http.MethodHead, base+path, "")
				if head.StatusCode != http.StatusOK || len(data) != 0 || head.Header.Get("Content-Length") == "" || strings.Contains(head.Header.Get("Content-Type"), "json") {
					t.Fatalf("invalid HEAD response for %s: %d %s", path, head.StatusCode, data)
				}
			}
			ranged, err := testClient.R().SetContext(context.Background()).SetHeader("Range", "bytes=0-31").Get(base + c.DocsPath())
			if err != nil || ranged.StatusCode() != http.StatusOK || !strings.Contains(ranged.String(), "window.__KNIFE4X_CONFIG__=") {
				t.Fatalf("ranged HTML lost embedded configuration: %v", err)
			}
			missing, _ := request(t, http.MethodGet, base+c.BasePath()+"/assets/missing.js", "")
			if missing.StatusCode != http.StatusNotFound {
				t.Fatalf("missing asset returned %d", missing.StatusCode)
			}
			envelope(t, http.MethodGet, base+c.BasePath()+"/v1/health", "", http.StatusOK)
		})
	}
}

func assertJavaScript(t *testing.T, res *http.Response) {
	t.Helper()
	mediaType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || (mediaType != "text/javascript" && mediaType != "application/javascript") {
		t.Fatalf("invalid JavaScript MIME: %q", res.Header.Get("Content-Type"))
	}
}

func TestKnife4xAuthAndDisabledResources(t *testing.T) {
	c := testConfig(t)
	c.DocsAuth = true
	c.DocsUsername = "reader"
	c.DocsPassword = "local-test"
	app, err := Build(c, &infrastructure.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	base := startApp(t, app)
	paths := []string{c.DocsPath(), c.OpenAPIPath(), c.BasePath() + "/assets/index.js", c.BasePath() + "/oauth2-redirect.html", c.BasePath() + "/knife4j-next-mark.svg"}
	for _, path := range paths {
		envelope(t, http.MethodGet, base+path, "", http.StatusUnauthorized)
		res, err := testClient.R().SetContext(context.Background()).SetBasicAuth(c.DocsUsername, c.DocsPassword).Get(base + path)
		if err != nil || res.StatusCode() != http.StatusOK {
			t.Fatalf("authenticated resource %s failed: %v", path, err)
		}
	}
	for _, path := range []string{"/backend/ASSETS/index.js", "/BACKEND/assets/index.js", "/backend/assets/index.js/", "/backend/oauth2-redirect.html/"} {
		res, _ := request(t, http.MethodGet, base+path, "")
		if res.StatusCode != http.StatusUnauthorized && res.StatusCode != http.StatusNotFound {
			t.Errorf("unprotected UI alias %s: %d", path, res.StatusCode)
		}
	}
	for _, production := range []bool{false, true} {
		t.Run("production="+strconv.FormatBool(production), func(t *testing.T) {
			disabled := testConfig(t)
			if production {
				// 即使绕过 Parse 手工保留 Docs=true，生产环境仍不得挂载资源。
				disabled.Env = config.EnvProduction
			} else {
				disabled.Docs = false
			}
			app, err := Build(disabled, &infrastructure.Resources{})
			if err != nil {
				t.Fatal(err)
			}
			base := startApp(t, app)
			for _, path := range paths {
				envelope(t, http.MethodGet, base+path, "", http.StatusNotFound)
			}
		})
	}
}

func TestKnife4xResourcesSkipBusinessThrottle(t *testing.T) {
	redisServer := miniredis.RunT(t)
	client, err := gredis.New(&gredis.Config{Address: redisServer.Addr(), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	c := testConfig(t)
	c.Redis, c.Throttle = true, true
	app, err := Build(c, &infrastructure.Resources{Redis: client})
	if err != nil {
		_ = client.Close(context.Background())
		t.Fatal(err)
	}
	base := startApp(t, app)
	// UI 会并行加载大量分块，缺失资源也不应触发业务未知路由封禁。
	for i := 0; i < 35; i++ {
		res, _ := request(t, http.MethodGet, base+c.BasePath()+"/knife4j-next-mark.svg", "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("UI resource was throttled: %d", res.StatusCode)
		}
	}
	for i := 0; i < 5; i++ {
		res, _ := request(t, http.MethodGet, base+c.BasePath()+"/assets/missing.js", "")
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("missing UI resource was throttled: %d", res.StatusCode)
		}
	}
	if len(redisServer.Keys()) != 0 {
		t.Fatal("UI resources modified business throttle state")
	}
	for i := 0; i < 5; i++ {
		envelope(t, http.MethodGet, base+c.BasePath()+"/v1/missing", "", http.StatusNotFound)
	}
	// 确认业务封禁仍生效，而文档页面和资源继续可用。
	envelope(t, http.MethodGet, base+c.BasePath()+"/v1/health", "", http.StatusTooManyRequests)
	for _, path := range []string{c.DocsPath(), c.OpenAPIPath(), c.BasePath() + "/assets/index.js"} {
		res, _ := request(t, http.MethodGet, base+path, "")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("business ban blocked docs %s: %d", path, res.StatusCode)
		}
	}
}
