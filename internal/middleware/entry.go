package middleware

import (
	"crypto/sha256"
	"crypto/subtle"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/net/gtrace"
	"github.com/google/uuid"

	"backend-template-go/internal/accesslog"
	"backend-template-go/internal/config"
)

// Entry 创建追踪、安全响应头、CORS、文档认证和可选访问日志中间件。
// c 应为已校验的启动配置；logs 为 nil 时关闭访问日志，非 nil 时只写入路由模板和请求元数据。
// 中间件固定路径参数优先级，跨域预检或文档认证已完成响应时不再调用后续处理器。
func Entry(c config.Config, logs *accesslog.Writer) ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		start := time.Now()
		traceID := gtrace.GetTraceID(r.Context())
		if traceID == "" {
			traceID = fallbackTraceID()
		}
		r.Response.Header().Set("X-Trace-Id", traceID)
		setSecurityHeaders(r, c.IsProduction())
		// 路径标识优先于同名 query/body，防止请求悄然转向另一个资源。
		if handler := r.GetServeHandler(); handler != nil {
			for name, value := range handler.Values {
				r.SetParam(name, value)
			}
		}
		if logs != nil {
			defer recordAccessLog(r, logs, start, traceID, c.TrustedProxies)
		}
		if !applyCORS(r, c) || !allowDocumentation(r, c) {
			return
		}
		r.Middleware.Next()
	}
}

// fallbackTraceID 生成 32 位十六进制的兜底追踪号，仅在框架上下文没有追踪编号时使用。
// 长度与服务端追踪号一致，并匹配 access_logs.trace_id 的列宽，避免访问日志写入失败或被截断。
func fallbackTraceID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

// setSecurityHeaders 为响应设置默认浏览器安全策略，生产环境额外启用 HSTS。
// 文档等特殊协议处理器可在后续按其实际用途覆盖相关响应头。
func setSecurityHeaders(r *ghttp.Request, production bool) {
	for name, value := range map[string]string{
		"X-Content-Type-Options":            "nosniff",
		"Referrer-Policy":                   "no-referrer",
		"X-Frame-Options":                   "SAMEORIGIN",
		"Content-Security-Policy":           "default-src 'none'; frame-ancestors 'self'; base-uri 'none'",
		"X-DNS-Prefetch-Control":            "off",
		"X-Download-Options":                "noopen",
		"X-Permitted-Cross-Domain-Policies": "none",
		"X-XSS-Protection":                  "0",
		"Cross-Origin-Resource-Policy":      "same-origin",
	} {
		r.Response.Header().Set(name, value)
	}
	if production {
		r.Response.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
	}
}

// recordAccessLog 在请求结束时写入安全访问日志。
// panic 展开阶段响应状态尚未写入，此时按 500 记录并把异常重新抛出，
// 由最外层 Recover 统一脱敏，避免把异常请求记录成成功响应。
func recordAccessLog(r *ghttp.Request, logs *accesslog.Writer, start time.Time, traceID string, proxies []netip.Prefix) {
	if exception := recover(); exception != nil {
		logs.Write(accessRecord(r, start, traceID, http.StatusInternalServerError, proxies))
		panic(exception)
	}
	logs.Write(accessRecord(r, start, traceID, 0, proxies))
}

// accessRecord 组装访问日志记录，status 为零时读取响应状态或按 200 记录。
// 只包含追踪号、方法、路由模板、状态、耗时和客户端 IP，不记录查询参数与请求内容。
func accessRecord(r *ghttp.Request, start time.Time, traceID string, status int, proxies []netip.Prefix) accesslog.Record {
	if status == 0 {
		status = r.Response.Status
	}
	if status == 0 {
		status = http.StatusOK
	}
	return accesslog.Record{
		TraceID:    traceID,
		Method:     r.Method,
		Path:       routeTemplate(r),
		StatusCode: status,
		DurationMS: time.Since(start).Milliseconds(),
		ClientIP:   ClientIP(r.Request, proxies),
	}
}

// routeTemplate 返回当前请求的路由模板，未匹配到业务路由时返回固定标记。
// 使用模板而不是实际路径，避免把用户路径参数写入访问日志。
func routeTemplate(r *ghttp.Request) string {
	if handler := r.GetServeHandler(); handler != nil {
		return handler.Handler.Router.Uri
	}
	return "[unmatched]"
}

// applyCORS 按环境和来源白名单设置跨域响应头，返回是否继续处理请求。
// OPTIONS 预检在此返回 204 或安全的 403 响应并返回 false，普通请求继续由后续处理器执行；
// 无论是否放行都声明 Vary: Origin，避免缓存把不同来源的响应混用。
func applyCORS(r *ghttp.Request, c config.Config) bool {
	r.Response.Header().Add("Vary", "Origin")
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	allowed, credentials := corsPolicy(origin, c)
	if allowed {
		writeCORSHeaders(r, origin, credentials)
	}
	if r.Method != http.MethodOptions {
		return true
	}
	r.Response.Header().Add("Vary", "Access-Control-Request-Method")
	r.Response.Header().Add("Vary", "Access-Control-Request-Headers")
	if !allowed {
		JSON(r, http.StatusForbidden, "不允许的跨域来源", nil)
		return false
	}
	r.Response.WriteHeader(http.StatusNoContent)
	return false
}

// corsPolicy 判断来源是否允许跨域访问，以及此时是否允许携带凭据。
// 只有显式列出的来源才允许携带凭据；非生产环境中空白名单和通配符放行任意来源但不带凭据，
// 避免任意站点借助用户 Cookie 或认证头读取响应。生产环境只匹配明确来源。
func corsPolicy(origin string, c config.Config) (allowed, credentials bool) {
	var wildcard bool
	for _, item := range c.CORSOrigins {
		if origin == item {
			return true, true
		}
		if item == "*" && !c.IsProduction() {
			wildcard = true
		}
	}
	if wildcard || (len(c.CORSOrigins) == 0 && !c.IsProduction()) {
		return true, false
	}
	return false, false
}

// writeCORSHeaders 写入允许来源、允许方法与暴露的响应头。
// credentials 为 true 时额外声明允许凭据，只在显式白名单命中时由调用方开启。
func writeCORSHeaders(r *ghttp.Request, origin string, credentials bool) {
	header := r.Response.Header()
	header.Set("Access-Control-Allow-Origin", origin)
	if credentials {
		header.Set("Access-Control-Allow-Credentials", "true")
	}
	header.Set("Access-Control-Allow-Methods", "GET,HEAD,POST,PUT,PATCH,DELETE,OPTIONS")
	header.Set("Access-Control-Allow-Headers", "Content-Type,Authorization,X-Trace-Id")
	header.Set("Access-Control-Expose-Headers", "X-Trace-Id,Retry-After")
}

// allowDocumentation 在启用文档认证时校验两个文档入口的 Basic Auth。
// 非文档请求、生产环境或无需认证时返回 true；认证失败时写入安全的 401 响应和认证挑战头并返回 false。
func allowDocumentation(r *ghttp.Request, c config.Config) bool {
	isDoc := r.URL.Path == c.DocsPath() || r.URL.Path == c.OpenAPIPath()
	if !isDoc || !c.Docs || c.IsProduction() || !c.DocsAuth {
		return true
	}
	var (
		user, password, ok = r.Request.BasicAuth()
		userHash           = sha256.Sum256([]byte(user))
		expectedUser       = sha256.Sum256([]byte(c.DocsUsername))
		passHash           = sha256.Sum256([]byte(password))
		expectedPass       = sha256.Sum256([]byte(c.DocsPassword))
		validUser          = subtle.ConstantTimeCompare(userHash[:], expectedUser[:])
		validPass          = subtle.ConstantTimeCompare(passHash[:], expectedPass[:])
	)
	if ok && validUser&validPass == 1 {
		return true
	}
	r.Response.Header().Set("WWW-Authenticate", `Basic realm="API Documentation"`)
	JSON(r, http.StatusUnauthorized, "文档认证失败", nil)
	return false
}

// ClientIP 解析直接对端地址，仅在其属于 trusted CIDR 时自右向左解析 X-Forwarded-For。
// 返回第一个不可信地址；遇到无效转发项时返回当前已确认地址，全部可信时返回最左侧地址。
// IPv4 映射地址会被规范化，直接对端无法解析时返回固定的 unknown-ip，不信任其他客户端 IP 头。
func ClientIP(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return "unknown-ip"
	}
	peer = peer.Unmap()
	isTrusted := func(addr netip.Addr) bool {
		for _, prefix := range trusted {
			if prefix.Contains(addr) {
				return true
			}
		}
		return false
	}
	if !isTrusted(peer) {
		return peer.String()
	}
	chain := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(chain) - 1; i >= 0; i-- {
		addr, e := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if e != nil {
			return peer.String()
		}
		peer = addr.Unmap()
		if !isTrusted(peer) {
			break
		}
	}
	return peer.String()
}
