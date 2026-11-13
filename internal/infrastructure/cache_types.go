package infrastructure

import (
	"github.com/gogf/gf/v2/database/gredis"
)

// Cache 提供带独立服务命名空间的 Redis 字符串缓存，不拥有底层连接。
// 通过 NewCache 创建，构造后连接和前缀保持不变；nil 实例或零值的操作显式返回配置错误。
// 业务只传场景内的键，TTL、故障处理和互斥令牌语义由具体操作及调用方约束。
type Cache struct {
	client *gredis.Redis
	prefix string
}
