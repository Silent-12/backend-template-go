// Package v1 定义示例模块的请求和响应协议，供派生服务照此扩展业务接口。
package v1

import (
	"github.com/gogf/gf/v2/frame/g"

	"backend-template-go/internal/model"
)

// ListLogsReq 定义分页查询访问日志的协议，页码和页容量由入口校验。
// 示例模块保持模块根目录；派生服务应按实际端别把路由迁移到 admin 或 user 前缀下。
type ListLogsReq struct {
	g.Meta `path:"/example/logs" method:"get" tags:"示例模块" summary:"分页查询访问日志"`
	// Page 从 1 开始，默认第一页。
	Page int `json:"page" in:"query" v:"min:1#页码最小为 1" d:"1" dc:"当前页码，从 1 开始"`
	// PageSize 限制单次查询的记录数。
	PageSize int `json:"pageSize" in:"query" v:"min:1|max:100#每页条数需在 1 到 100 之间" d:"20" dc:"每页条数，1 到 100"`
}

// ListLogsRes 是访问日志分页结果，空列表编码为 []。
type ListLogsRes struct {
	// List 是当前页的日志记录，按创建时间倒序。
	List []LogItem `json:"list" dc:"访问日志列表，无记录时为空数组"`
	// Pagination 是页码分页统计。
	Pagination model.Pagination `json:"pagination" dc:"分页统计"`
}

// LogItem 是访问日志的对外投影，不暴露自增主键和客户端 IP。
type LogItem struct {
	// TraceID 是服务端生成的请求追踪编号。
	TraceID string `json:"traceId" dc:"请求追踪编号"`
	// Method 是 HTTP 请求方法。
	Method string `json:"method" dc:"HTTP 请求方法"`
	// Path 是匹配到的路由模板，不含查询参数。
	Path string `json:"path" dc:"匹配的路由模板"`
	// StatusCode 是响应状态码。
	StatusCode int `json:"statusCode" dc:"HTTP 响应状态码"`
	// DurationMS 是请求处理耗时。
	DurationMS int64 `json:"durationMs" dc:"处理耗时，毫秒"`
	// CreatedAt 是记录写入时间。
	CreatedAt int64 `json:"createdAt" dc:"记录创建时间，Unix 毫秒"`
}

// ProbeUpstreamReq 定义示例上游探测协议，不接受调用方指定地址。
type ProbeUpstreamReq struct {
	g.Meta `path:"/example/upstream" method:"get" tags:"示例模块" summary:"探测示例上游可达性"`
}

// ProbeUpstreamRes 返回示例上游的响应状态，演示同时处理网络错误与 HTTP 状态。
type ProbeUpstreamRes struct {
	// StatusCode 是上游返回的 HTTP 状态码。
	StatusCode int `json:"statusCode" dc:"示例上游返回的 HTTP 状态码"`
}
