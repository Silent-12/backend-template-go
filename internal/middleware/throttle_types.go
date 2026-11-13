package middleware

import (
	"github.com/gogf/gf/v2/database/gredis"
)

// limiter 使用 Redis 保存按客户端 IP 隔离的限流状态，连接由调用方管理。
// 由 Throttle 使用非 nil 连接和已校验前缀构造，零值不可直接执行 Redis 操作。
// 配置在请求期间保持不变；Redis 操作失败时记录告警并降级放行。
type limiter struct {
	redis  *gredis.Redis
	prefix string
}
