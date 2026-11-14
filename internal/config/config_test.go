package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		env       map[string]string
		wantError string
	}{
		{"defaults", nil, ""},
		{"invalid app name", map[string]string{"APP_NAME": "bad/name"}, "APP_NAME"},
		{"mysql requires database", map[string]string{"MYSQL_ENABLED": "true"}, "DB_DATABASE"},
		{"invalid cache namespace", map[string]string{"REDIS_KEY_PREFIX": "same:{slot}"}, "REDIS_KEY_PREFIX"},
		{"empty cache namespace", map[string]string{"REDIS_KEY_PREFIX": ""}, "REDIS_KEY_PREFIX"},
		{"invalid throttle namespace", map[string]string{"THROTTLE_KEY_PREFIX": "same:{slot}"}, "THROTTLE_KEY_PREFIX"},
		{"invalid port", map[string]string{"PORT": "0"}, "PORT"},
		{"invalid boolean", map[string]string{"MYSQL_ENABLED": "yes"}, "MYSQL_ENABLED"},
		{"empty host", map[string]string{"HOST": ""}, "HOST"},
		{"invalid api prefix", map[string]string{"API_PREFIX": "backend/../x"}, "API_PREFIX"},
		{"redis requires host", map[string]string{"REDIS_ENABLED": "true", "REDIS_HOST": ""}, "REDIS_HOST"},
		{"throttle requires redis", map[string]string{"THROTTLE_ENABLED": "true"}, "Redis"},
		{"access log requires mysql", map[string]string{"HTTP_LOG_ENABLED": "true"}, "MySQL"},
		{"docs auth requires password", map[string]string{"DOCS_AUTH_ENABLED": "true"}, "文档认证"},
		{"invalid proxy", map[string]string{"TRUSTED_PROXY_CIDRS": "127.0.0.1"}, "CIDR"},
		{"origin contains path", map[string]string{"CORS_ORIGINS": "https://example.com/app"}, "CORS_ORIGINS"},
		{"origin contains credentials", map[string]string{"CORS_ORIGINS": "https://user:secret@example.com"}, "CORS_ORIGINS"},
		{"origin contains wildcard host", map[string]string{"CORS_ORIGINS": "https://*.example.com"}, "CORS_ORIGINS"},
		{"production wildcard", map[string]string{"APP_ENV": "production", "CORS_ORIGINS": "*"}, "通配符"},
		{"disabled optional passwords", map[string]string{"DB_PASSWORD": "", "REDIS_PASSWORD": "", "DOCS_PASSWORD": ""}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(func(key string) (string, bool) { value, ok := tc.env[key]; return value, ok })
			if tc.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("expected %q error, got %v", tc.wantError, err)
			}
		})
	}
	config, err := Parse(func(key string) (string, bool) {
		if key == "APP_ENV" {
			return "production", true
		}
		return "", false
	})
	if err != nil || config.Docs {
		t.Fatalf("production must disable docs: %+v, %v", config, err)
	}
}

func TestLoadDoesNotPersistFileValues(t *testing.T) {
	dir := t.TempDir()
	var (
		path   = filepath.Join(dir, ".env")
		env    = map[string]string{"PORT": "4321", "DOCS_PASSWORD": ""}
		lookup = func(key string) (string, bool) { value, ok := env[key]; return value, ok }
		write  = func(path, body string) {
			t.Helper()
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
	)
	// 旧配置文件即使存在且格式损坏，也不能影响只读取 .env 的加载流程。
	write(filepath.Join(dir, ".env.local"), "TOKEN!ignored-secret=value\n")
	write(path, "APP_NAME=local-service\nPORT=3001\nHOST=127.0.0.1\nDOCS_PASSWORD=file-secret\n")
	loaded, err := load(lookup, path)
	if err != nil || loaded.AppName != "local-service" || loaded.Port != 4321 || loaded.Host != "127.0.0.1" || loaded.DocsPassword != "" {
		t.Fatalf("configuration precedence failed: %v", err)
	}
	write(path, "APP_NAME=updated-service\n")
	loaded, err = load(lookup, path)
	if err != nil || loaded.AppName != "updated-service" {
		t.Fatalf("repeated load retained old file values: %v", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	loaded, err = load(lookup, path)
	if err != nil || loaded.AppName != "backend-template-go" || loaded.Port != 4321 {
		t.Fatalf("missing .env did not use process environment and defaults: %v", err)
	}
	// 配置文件解析错误可能包含原始行，错误消息和错误链都不能暴露它。
	write(path, "TOKEN!private-secret=value\n")
	if _, err = load(lookup, path); err == nil || strings.Contains(err.Error(), "private-secret") {
		t.Fatal("malformed file was accepted or leaked its contents")
	}
}

func TestServiceIdentityAndRedisPrefixDefaults(t *testing.T) {
	for _, tc := range []struct{ app, redis, throttle string }{
		{"orders", "orders", "orders"},
		{"billing", "billing", "billing"},
		{"orders", "env:orders", "env:orders"},
	} {
		values := map[string]string{"APP_NAME": tc.app}
		if tc.redis != tc.app {
			values["REDIS_KEY_PREFIX"] = tc.redis
		}
		loaded, err := Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
		if err != nil || loaded.RedisKeyPrefix != tc.redis || loaded.ThrottleKeyPrefix != tc.throttle {
			t.Fatalf("service namespaces were not inherited: %v", err)
		}
		values["THROTTLE_KEY_PREFIX"] = "legacy-throttle"
		loaded, err = Parse(func(key string) (string, bool) { value, ok := values[key]; return value, ok })
		if err != nil || loaded.ThrottleKeyPrefix != "legacy-throttle" {
			t.Fatalf("explicit throttle namespace was not retained: %v", err)
		}
	}
}

func TestPasswordsPreserveWhitespace(t *testing.T) {
	config, err := Parse(func(key string) (string, bool) {
		switch key {
		case "DB_PASSWORD", "REDIS_PASSWORD", "DOCS_PASSWORD":
			return "  opaque secret  ", true
		}
		return "", false
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.DBPassword != "  opaque secret  " || config.RedisPassword != config.DBPassword || config.DocsPassword != config.DBPassword {
		t.Fatal("credential whitespace was changed")
	}
}

// 复制模板后若沿用示例库名，多个派生服务会连到同一个库；示例文件不能提供可直接使用的值。
func TestEnvExampleDoesNotShipDatabaseName(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	for index, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if found && key == "DB_DATABASE" && strings.TrimSpace(value) != "" {
			t.Fatalf(".env.example line %d ships a usable DB_DATABASE: %q", index+1, line)
		}
	}
	if !strings.Contains(string(body), "禁止沿用模板示例值") {
		t.Fatal(".env.example does not explain that derived services must not reuse the sample database name")
	}
}

// Parse 拆分为分域解析后仍须一次汇总全部问题，避免调用方逐项修复。
func TestParseAggregatesProblems(t *testing.T) {
	_, err := Parse(func(key string) (string, bool) {
		switch key {
		case "APP_NAME":
			return "Bad/Name", true
		case "PORT":
			return "0", true
		case "MYSQL_ENABLED":
			return "true", true
		}
		return "", false
	})
	if err == nil {
		t.Fatal("invalid configuration was accepted")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "配置校验失败: ") {
		t.Fatalf("unexpected aggregate prefix: %q", message)
	}
	if count := strings.Count(message, "; "); count < 2 {
		t.Fatalf("expected problems from several domains, got %q", message)
	}
	for _, want := range []string{"APP_NAME", "PORT", "DB_DATABASE"} {
		if !strings.Contains(message, want) {
			t.Fatalf("aggregate message lost %q: %q", want, message)
		}
	}
}

// 零前缀网段会把任意客户端当作可信代理，使其伪造 X-Forwarded-For 绕过限流。
func TestTrustedProxyRejectsZeroPrefix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     string
		wantError string
	}{
		{"ipv4 zero prefix", "0.0.0.0/0", "零前缀"},
		{"ipv6 zero prefix", "::/0", "零前缀"},
		{"zero prefix among valid", "10.0.0.0/8,0.0.0.0/0", "零前缀"},
		{"missing mask", "10.0.0.1", "CIDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse(func(key string) (string, bool) {
				if key == "TRUSTED_PROXY_CIDRS" {
					return tc.value, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q error, got %v", tc.wantError, err)
			}
			// 整份配置无效，但已解析的合法网段仍保留在返回值中供排查，不得用于启动。
			if tc.name == "zero prefix among valid" && len(c.TrustedProxies) != 1 {
				t.Fatalf("valid prefix was dropped: %+v", c.TrustedProxies)
			}
		})
	}
	for _, tc := range []struct{ name, value, want string }{
		{"private ipv4", "10.0.0.0/8", "10.0.0.0/8"},
		{"unique local ipv6", "fd00::/8", "fd00::/8"},
		{"loopback", "127.0.0.1/32", "127.0.0.1/32"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse(func(key string) (string, bool) {
				if key == "TRUSTED_PROXY_CIDRS" {
					return tc.value, true
				}
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(c.TrustedProxies) != 1 || c.TrustedProxies[0].String() != tc.want {
				t.Fatalf("trusted proxy was not parsed: %+v", c.TrustedProxies)
			}
		})
	}
}

// 浏览器按 scheme、host 和 port 判定来源，带路径、凭据或通配符的来源会被静默忽略或放宽。
func TestCORSOriginsRequireBareOrigin(t *testing.T) {
	for _, tc := range []struct {
		name      string
		value     string
		wantError string
	}{
		{"path", "https://example.com/app", "路径"},
		{"query", "https://example.com?tenant=1", "查询"},
		{"empty query", "https://example.com?", "查询"},
		{"userinfo", "https://user:secret@example.com", "凭据"},
		{"wildcard host", "https://*.example.com", "通配符"},
		{"non http scheme", "ftp://example.com", "http 或 https"},
		{"missing scheme", "example.com", "http 或 https"},
		{"malformed url", "https://exa mple.com", "格式无效"},
		{"fragment", "https://example.com#docs", "片段"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(func(key string) (string, bool) {
				if key == "CORS_ORIGINS" {
					return tc.value, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q error, got %v", tc.wantError, err)
			}
		})
	}
	for _, tc := range []struct{ name, value string }{
		{"https origin", "https://example.com"},
		{"http origin with port", "http://localhost:8080"},
		{"explicit port", "https://example.com:8443"},
		{"development wildcard", "*"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse(func(key string) (string, bool) {
				if key == "CORS_ORIGINS" {
					return tc.value, true
				}
				return "", false
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(c.CORSOrigins) != 1 || c.CORSOrigins[0] != tc.value {
				t.Fatalf("origin was not retained: %+v", c.CORSOrigins)
			}
		})
	}
}

func TestProductionEnvironment(t *testing.T) {
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
	}
	c, err := Parse(lookup(map[string]string{"APP_ENV": EnvProduction, "DOCS_ENABLED": "true", "DOCS_AUTH_ENABLED": "true"}))
	if err != nil {
		// 生产环境关闭文档入口后，不应再要求文档认证凭据。
		t.Fatal(err)
	}
	if !c.IsProduction() || c.Docs || c.Env != EnvProduction {
		t.Fatalf("production did not disable docs: %+v", c)
	}
	if _, err = Parse(lookup(map[string]string{"APP_ENV": EnvProduction, "CORS_ORIGINS": "*"})); err == nil || !strings.Contains(err.Error(), "通配符") {
		t.Fatalf("production accepted wildcard CORS, got %v", err)
	}
	for _, env := range []string{EnvDevelopment, EnvTest} {
		if c, err := Parse(lookup(map[string]string{"APP_ENV": env})); err != nil || c.IsProduction() {
			t.Fatalf("environment %q was treated as production: %v", env, err)
		}
	}
}

func TestConfigPathsFollowAPIPrefix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		prefix    string
		base      string
		docs      string
		openAPI   string
		liveness  string
		readiness string
	}{
		{"empty prefix", "", "", "/doc.html", "/api-json", "/livez", "/readyz"},
		{"default prefix", "backend", "/backend", "/backend/doc.html", "/backend/api-json", "/backend/livez", "/backend/readyz"},
		{"nested prefix", "api/v1", "/api/v1", "/api/v1/doc.html", "/api/v1/api-json", "/api/v1/livez", "/api/v1/readyz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{APIPrefix: tc.prefix}
			if c.BasePath() != tc.base || c.DocsPath() != tc.docs || c.OpenAPIPath() != tc.openAPI || c.LivenessPath() != tc.liveness || c.ReadinessPath() != tc.readiness {
				t.Fatalf("paths do not follow API prefix %q: %q %q %q %q %q",
					tc.prefix, c.BasePath(), c.DocsPath(), c.OpenAPIPath(), c.LivenessPath(), c.ReadinessPath())
			}
		})
	}
	// 首尾斜杠和空格由 Parse 统一规范化，路径方法不重复清理。
	c, err := Parse(func(key string) (string, bool) {
		if key == "API_PREFIX" {
			return "  /backend/  ", true
		}
		return "", false
	})
	if err != nil || c.DocsPath() != "/backend/doc.html" {
		t.Fatalf("API prefix was not normalized: %q, %v", c.DocsPath(), err)
	}
}

// 分域解析后由 combineConfig 统一装配，本用例锁定每个域字段都被搬运，避免新增字段被静默丢弃。
func TestParseFillsEveryDomain(t *testing.T) {
	env := map[string]string{
		"APP_ENV":                 EnvTest,
		"APP_NAME":                "orders",
		"HOST":                    "127.0.0.1",
		"PORT":                    "8080",
		"API_PREFIX":              "/api/v1/",
		"CORS_ORIGINS":            "https://a.example.com",
		"TRUSTED_PROXY_CIDRS":     "10.0.0.0/8,fd00::/8",
		"HTTP_LOG_ENABLED":        "true",
		"DOCS_ENABLED":            "true",
		"DOCS_AUTH_ENABLED":       "true",
		"DOCS_USERNAME":           "docs",
		"DOCS_PASSWORD":           " docs-secret ",
		"EXAMPLE_UPSTREAM_ORIGIN": "https://upstream.example.com",
		"MYSQL_ENABLED":           "true",
		"DB_LOGGING":              "true",
		"DB_HOST":                 "db.internal",
		"DB_PORT":                 "3307",
		"DB_USERNAME":             "app",
		"DB_PASSWORD":             " db-secret ",
		"DB_DATABASE":             "demo",
		"REDIS_ENABLED":           "true",
		"REDIS_HOST":              "cache.internal",
		"REDIS_PORT":              "6380",
		"REDIS_USERNAME":          "cache-user",
		"REDIS_PASSWORD":          " redis-secret ",
		"REDIS_DB":                "3",
		"REDIS_KEY_PREFIX":        "test:demo",
		"THROTTLE_ENABLED":        "true",
		"THROTTLE_KEY_PREFIX":     "test:throttle",
	}
	c, err := Parse(func(key string) (string, bool) { value, ok := env[key]; return value, ok })
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Env:                   EnvTest,
		AppName:               "orders",
		Host:                  "127.0.0.1",
		Port:                  8080,
		APIPrefix:             "api/v1",
		CORSOrigins:           []string{"https://a.example.com"},
		TrustedProxies:        []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8")},
		HTTPLog:               true,
		Docs:                  true,
		DocsAuth:              true,
		DocsUsername:          "docs",
		ExampleUpstreamOrigin: "https://upstream.example.com",
		DocsPassword:          " docs-secret ",
		MySQL:                 true,
		DBLogging:             true,
		DBHost:                "db.internal",
		DBPort:                3307,
		DBUser:                "app",
		DBPassword:            " db-secret ",
		DBName:                "demo",
		Redis:                 true,
		RedisHost:             "cache.internal",
		RedisPort:             6380,
		RedisUser:             "cache-user",
		RedisPassword:         " redis-secret ",
		RedisDB:               3,
		RedisKeyPrefix:        "test:demo",
		Throttle:              true,
		ThrottleKeyPrefix:     "test:throttle",
	}
	var (
		got      = reflect.ValueOf(c)
		expected = reflect.ValueOf(want)
		missing  []string
	)
	for i := 0; i < got.NumField(); i++ {
		// 只报告字段名，不打印配置值，避免失败日志里出现凭据。
		if !reflect.DeepEqual(got.Field(i).Interface(), expected.Field(i).Interface()) {
			missing = append(missing, got.Type().Field(i).Name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("fields were not assembled: %s", strings.Join(missing, ", "))
	}
}

// 示例上游只接受可信的纯 origin；带路径、查询、凭据或通配符会让出站请求目标不确定。
func TestExampleUpstreamOriginRequiresBareOrigin(t *testing.T) {
	for _, tc := range []struct{ name, value, wantError string }{
		{"wildcard", "*", "通配符"},
		{"path", "https://upstream.example.com/health", "EXAMPLE_UPSTREAM_ORIGIN"},
		{"query", "https://upstream.example.com?token=secret", "EXAMPLE_UPSTREAM_ORIGIN"},
		{"credentials", "https://user:pass@upstream.example.com", "EXAMPLE_UPSTREAM_ORIGIN"},
		{"missing scheme", "upstream.example.com", "EXAMPLE_UPSTREAM_ORIGIN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(func(key string) (string, bool) {
				if key == "EXAMPLE_UPSTREAM_ORIGIN" {
					return tc.value, true
				}
				return "", false
			})
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q error, got %v", tc.wantError, err)
			}
		})
	}
	for _, value := range []string{"", "https://upstream.example.com", "http://127.0.0.1:8080"} {
		c, err := Parse(func(key string) (string, bool) {
			if key == "EXAMPLE_UPSTREAM_ORIGIN" {
				return value, true
			}
			return "", false
		})
		if err != nil || c.ExampleUpstreamOrigin != value {
			t.Fatalf("valid origin %q was rejected: %v", value, err)
		}
	}
}
