// Package httpclient 统一创建 Resty 外部 HTTP 客户端。
// 每个上游复用独立实例，请求通过 SetContext 继承取消信号和追踪信息；
// 生产装配通过 Registry 创建并登记客户端，由应用退出时统一关闭空闲连接。
package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/gtrace"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// DefaultTimeout 是单次外部 HTTP 请求的默认超时。
	DefaultTimeout = 10 * time.Second
	// DefaultResponseBodyLimit 限制自动读取的响应大小，避免上游耗尽服务内存。
	DefaultResponseBodyLimit = 2 << 20
)

// New 根据启动配置创建可并发复用的 Resty 客户端，每个上游应独立创建并复用一份。
// 零超时和零响应体上限使用安全默认值；负值或非法基础地址返回配置错误。
// 默认校验证书，关闭重试、重定向、Cookie 持久化和报文调试，并按请求上下文重建追踪头。
// 配置在请求开始前确定；认证、参数和结果对象设置在 Request 上，每次请求通过 SetContext 传入上下文。
func New(options Options) (*resty.Client, error) {
	if options.Timeout < 0 || options.ResponseBodyLimit < 0 {
		return nil, gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP 超时和响应限制不可为负数")
	}
	if err := validateBaseURL(options.BaseURL); err != nil {
		return nil, err
	}
	if options.Timeout == 0 {
		options.Timeout = DefaultTimeout
	}
	if options.ResponseBodyLimit == 0 {
		options.ResponseBodyLimit = DefaultResponseBodyLimit
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: options.Timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	client := resty.New().
		SetTransport(transport).
		SetBaseURL(options.BaseURL).
		SetTimeout(options.Timeout).
		SetResponseBodyLimit(options.ResponseBodyLimit).
		SetRetryCount(0).
		SetCookieJar(nil).
		SetRedirectPolicy(resty.NoRedirectPolicy()).
		SetDebug(false).
		SetLogger(safeLogger{})
	// 在 Resty 合并客户端和请求头之后执行，避免共享默认头残留另一个请求的追踪信息。
	client.SetPreRequestHook(func(_ *resty.Client, request *http.Request) error {
		ctx := request.Context()
		request.Header.Del("X-Trace-Id")
		request.Header.Del("Traceparent")
		request.Header.Del("Tracestate")
		if traceID := gtrace.GetTraceID(ctx); traceID != "" {
			request.Header.Set("X-Trace-Id", traceID)
		}
		propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
		return nil
	})
	return client, nil
}

// validateBaseURL 校验可信配置中的上游基础地址，空值表示不设置基础地址。
// 只接受 http/https 且必须带主机名；凭据、查询和片段会让共享客户端把敏感信息带进每次请求，
// 因此逐项拒绝并返回指明原因的错误，不把地址原文写进错误或日志。
func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	base, err := url.Parse(raw)
	if err != nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 不是合法 URL")
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 必须以 http:// 或 https:// 开头")
	}
	if base.Hostname() == "" {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 缺少主机名")
	}
	if base.User != nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 不可包含凭据")
	}
	if base.RawQuery != "" || base.ForceQuery {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 不可包含查询参数")
	}
	if base.Fragment != "" {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP BaseURL 不可包含片段")
	}
	return nil
}

// Errorf 记录固定的客户端失败提示，忽略 Resty 提供的格式串和参数。
// Resty 日志接口不提供请求上下文，因此使用独立上下文，避免输出 URL、凭据或报文。
func (safeLogger) Errorf(string, ...any) {
	g.Log().Error(context.Background(), "外部 HTTP 客户端执行失败")
}

// Warnf 记录固定的客户端警告提示，不格式化第三方传入的参数。
// Resty 日志接口不提供请求上下文，因此使用独立上下文，避免泄露请求信息。
func (safeLogger) Warnf(string, ...any) {
	g.Log().Warning(context.Background(), "外部 HTTP 客户端产生警告")
}

// Debugf 忽略全部调试信息，不产生日志输出。
// 即使调用方触发底层调试日志，也不会通过此适配器写出认证头或报文。
func (safeLogger) Debugf(string, ...any) {}
