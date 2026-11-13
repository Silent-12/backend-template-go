package server

import (
	"sync"

	"github.com/gogf/gf/v2/net/ghttp"

	"backend-template-go/internal/accesslog"
	"backend-template-go/internal/httpclient"
	"backend-template-go/internal/infrastructure"
)

// App 持有 HTTP 服务、访问日志写入器、上游客户端注册表和外部资源，按依赖顺序执行关闭。
// 通过 New 或 Build 创建后显式启动 Server，使用结束时调用 Close；零值不可用，实例不得复制。
// Close 可重复或并发调用，应用关闭后不应继续接受业务请求。
type App struct {
	// Server 是已完成路由和中间件配置的 HTTP 服务，由调用方显式启动监听。
	Server    *ghttp.Server
	resources *infrastructure.Resources
	logs      *accesslog.Writer
	clients   *httpclient.Registry
	once      sync.Once
}

// HTTPClients 返回本进程的上游 HTTP 客户端注册表，派生服务应通过它创建上游客户端。
// 注册表归应用所有，调用方不得自行关闭；应用关闭后再创建客户端会返回错误。
func (a *App) HTTPClients() *httpclient.Registry {
	return a.clients
}
