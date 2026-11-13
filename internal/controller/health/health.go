// Package health 提供不依赖业务模块的服务健康接口。
package health

import (
	"context"
	"runtime"
	"time"

	v1 "backend-template-go/api/health/v1"
	"backend-template-go/internal/infrastructure"
)

// New 注入依赖资源和生产环境标志，以当前时间记录控制器的启动基准。
// 非生产环境需要非 nil 的 resources；控制器不负责关闭这些资源。
func New(resources *infrastructure.Resources, production bool) *Controller {
	return &Controller{resources: resources, production: production, started: time.Now()}
}

// Check 查询服务健康状态，作为存活检查始终返回 ok 和 nil 错误。
// 生产环境只提供存活状态；非生产环境使用 ctx 探测依赖，并附加运行秒数和内存诊断。
// 依赖是否就绪由应用前缀下的 /readyz 判断，本接口不因依赖故障返回失败状态。
func (c *Controller) Check(ctx context.Context, _ *v1.CheckReq) (*v1.CheckRes, error) {
	res := &v1.CheckRes{Status: "ok"}
	if !c.production {
		res.Services = c.resources.Health(ctx)
		uptime := int64(time.Since(c.started).Seconds())
		res.UptimeSeconds = &uptime
		var memory runtime.MemStats
		runtime.ReadMemStats(&memory)
		res.MemoryUsage = &v1.MemoryUsage{HeapAllocBytes: memory.HeapAlloc, SysBytes: memory.Sys}
	}
	return res, nil
}
