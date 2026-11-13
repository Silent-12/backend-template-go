// Package v1 定义健康检查的请求和响应协议。
package v1

import "github.com/gogf/gf/v2/frame/g"

// CheckReq 定义 v1 健康状态查询协议，不包含业务参数。
// 生产环境只查询存活状态，非生产环境同时返回依赖和运行时诊断。
type CheckReq struct {
	g.Meta `path:"/health" method:"get" tags:"健康检查" summary:"查询服务健康状态"`
}

// CheckRes 表示健康接口的业务数据，生产环境只设置 Status。
// 非生产环境补充依赖状态、运行时间和内存诊断，未设置的可选字段不参与 JSON 输出。
type CheckRes struct {
	// Status 是服务存活状态，当前固定为 ok，不代表全部外部依赖就绪。
	Status string `json:"status" dc:"服务存活状态，固定为 ok，不代表依赖就绪"`
	// UptimeSeconds 是自健康控制器创建以来的运行秒数，生产环境不设置。
	UptimeSeconds *int64 `json:"uptimeSeconds,omitempty" dc:"服务运行秒数，仅非生产环境返回"`
	// Services 包含 mysql 和 redis 的 up、down 或 disabled 状态，生产环境不设置。
	Services map[string]string `json:"services,omitempty" dc:"依赖健康状态，值域 up、down、disabled，仅非生产环境返回"`
	// MemoryUsage 是 Go 运行时内存诊断，生产环境不设置。
	MemoryUsage *MemoryUsage `json:"memoryUsage,omitempty" dc:"Go 运行时内存诊断，仅非生产环境返回"`
}

// MemoryUsage 提供 Go 运行时的字节数诊断，不等同于操作系统的进程内存占用。
// 仅用于非生产环境的健康响应，不对外提供完整运行时统计。
type MemoryUsage struct {
	// HeapAllocBytes 是当前已分配且尚未释放的堆对象字节数。
	HeapAllocBytes uint64 `json:"heapAllocBytes" dc:"当前堆分配字节数"`
	// SysBytes 是 Go 运行时向系统申请的总字节数，不等同于进程 RSS。
	SysBytes uint64 `json:"sysBytes" dc:"Go 运行时向系统申请的字节数，不等同于进程 RSS"`
}
