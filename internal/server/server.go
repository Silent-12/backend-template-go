// Package server 组装路由、基础设施和资源生命周期。
package server

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"

	"backend-template-go/internal/accesslog"
	"backend-template-go/internal/config"
	"backend-template-go/internal/controller/health"
	"backend-template-go/internal/httpclient"
	"backend-template-go/internal/infrastructure"
	"backend-template-go/internal/middleware"
)

// 入站追踪头由 W3C Trace Context 定义，服务端不接受客户端指定的追踪上下文。
const (
	headerTraceparent = "traceparent"
	headerTracestate  = "tracestate"
)

// New 初始化配置启用的外部资源并组装 HTTP 应用，不开始监听。
// c 应已完成校验，ctx 传递给资源启动探测；资源初始化或应用组装失败时释放已创建连接。
// 成功后调用方应通过返回应用的 Close 按顺序停止服务并释放资源。
func New(ctx context.Context, c config.Config) (*App, error) {
	resources, err := infrastructure.Open(ctx, c)
	if err != nil {
		return nil, err
	}
	app, err := Build(c, resources)
	if err != nil {
		resources.Close()
		return nil, err
	}
	return app, nil
}

// Build 使用已初始化的资源和默认依赖装配组装 HTTP 应用，供启动流程或测试显式注入资源。
// c 应已完成校验，resources 须非 nil 且与配置开关一致，否则返回错误；此函数不开始监听。
// 成功后由 App 接管资源并通过 App.Close 释放，失败时资源仍由调用方释放。
func Build(c config.Config, resources *infrastructure.Resources) (*App, error) {
	if err := checkResourceSwitches(c, resources); err != nil {
		return nil, err
	}
	s := ghttp.GetServer(c.AppName + "-" + uuid.NewString())
	s.SetAddr(net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	s.SetName(c.AppName)
	// 不接受客户端指定的追踪上下文：删除入站 traceparent 和 tracestate，追踪编号一律由服务端生成；
	// 同时禁用外部请求通过框架内部路由覆盖头改变目标接口。
	s.SetHandler(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Del(ghttp.HeaderXUrlPath)
		r.Header.Del(headerTraceparent)
		r.Header.Del(headerTracestate)
		s.ServeHTTP(w, r)
	})
	s.SetDumpRouterMap(false)
	s.SetAccessLogEnabled(false)
	// 框架内置错误日志可能记录完整请求，统一由业务响应中间件记录错误。
	s.SetErrorLogEnabled(false)
	s.SetReadTimeout(15 * time.Second)
	s.SetWriteTimeout(30 * time.Second)
	s.SetClientMaxBodySize(2 << 20)
	s.SetGracefulShutdownTimeout(10)
	app := &App{Server: s, resources: resources, clients: httpclient.NewRegistry()}
	if c.HTTPLog {
		app.logs = accesslog.New(resources.DB)
	}
	// Recover 必须位于最外层，捕获其余中间件自身的 panic 并输出安全的 500 响应。
	s.Use(
		middleware.Recover(),
		middleware.Entry(c, app.logs),
		middleware.Throttle(c, resources.Redis),
		middleware.HandlerResponse,
		middleware.ParameterTypes,
	)
	registerBusinessRoutes(s, c, resources)
	registerProbes(s, c, resources)
	registerDocs(s, c)
	return app, nil
}

// checkResourceSwitches 逐项校验配置开关与已初始化资源一致，返回首个不一致项的具体开关名。
// resources 为 nil 或开关与资源不匹配时返回配置错误，调用方不应继续组装应用。
func checkResourceSwitches(c config.Config, resources *infrastructure.Resources) error {
	if resources == nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "组装应用需要已初始化的资源")
	}
	if c.MySQL != (resources.DB != nil) {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "配置项 MYSQL_ENABLED 与已初始化的 MySQL 连接不一致")
	}
	if c.Redis != (resources.Redis != nil) {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "配置项 REDIS_ENABLED 与已初始化的 Redis 连接不一致")
	}
	if c.HTTPLog && resources.DB == nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "启用 HTTP_LOG_ENABLED 但未启用 MySQL")
	}
	if c.Throttle && resources.Redis == nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "启用 THROTTLE_ENABLED 但未启用 Redis")
	}
	return nil
}

// registerBusinessRoutes 在应用前缀下注册 v1 健康接口。
func registerBusinessRoutes(s *ghttp.Server, c config.Config, resources *infrastructure.Resources) {
	s.Group(c.BasePath()+"/v1", func(group *ghttp.RouterGroup) {
		group.Bind(health.New(resources, c.IsProduction()))
	})
}

// registerProbes 注册不经过业务响应包装的存活和就绪探针。
// 存活探针返回 ok；就绪探针等待依赖状态，任何启用依赖为 down 时返回安全的 503 响应。
// 响应体是探针协议自身的固定值，不使用业务 JSON 包装，也不复用依赖健康状态枚举。
func registerProbes(s *ghttp.Server, c config.Config, resources *infrastructure.Resources) {
	s.BindHandler("GET:"+c.LivenessPath(), func(r *ghttp.Request) {
		r.Response.WriteJson(map[string]string{"status": "ok"})
	})
	s.BindHandler("GET:"+c.ReadinessPath(), func(r *ghttp.Request) {
		for _, status := range resources.Health(r.Context()) {
			if status == infrastructure.StatusDown {
				r.Response.WriteHeader(http.StatusServiceUnavailable)
				r.Response.WriteJson(map[string]string{"status": "unavailable"})
				return
			}
		}
		r.Response.WriteJson(map[string]string{"status": "ok"})
	})
}

// registerDocs 仅在非生产且启用文档时注册 OpenAPI 和在线调试入口。
// 标题来自应用配置，业务响应使用统一包装模型；是否允许访问由入口中间件执行认证。
func registerDocs(s *ghttp.Server, c config.Config) {
	if !c.Docs || c.IsProduction() {
		return
	}
	s.SetOpenApiPath(c.OpenAPIPath())
	s.GetOpenApi().Info.Title = c.AppName + " API"
	s.GetOpenApi().Info.Version = "1.0.0"
	s.GetOpenApi().Config.CommonResponse = middleware.Response{}
	s.GetOpenApi().Config.CommonResponseDataField = "Data"
	s.BindHandler("GET:"+c.DocsPath(), func(r *ghttp.Request) {
		r.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
		writeDocs(r, c.OpenAPIPath(), c.AppName+" API")
	})
}

// Close 先停止 HTTP 请求，再等待访问日志消费协程退出，最后依次关闭上游客户端和外部连接。
// 日志等待五秒后取消未完成写入；底层关闭错误被忽略，重复或并发调用只执行一次关闭流程。
func (a *App) Close() {
	a.once.Do(func() {
		_ = a.Server.Shutdown()
		if a.logs != nil {
			a.logs.Close()
		}
		if a.clients != nil {
			a.clients.Close()
		}
		a.resources.Close()
	})
}
