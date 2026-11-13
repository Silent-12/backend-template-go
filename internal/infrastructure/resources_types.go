package infrastructure

import (
	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"
)

// 依赖健康状态枚举，由 Resources.Health 返回，供就绪探针判断。
const (
	// StatusUp 表示依赖已启用且探测成功。
	StatusUp = "up"
	// StatusDown 表示依赖已启用但探测失败或超时。
	StatusDown = "down"
	// StatusDisabled 表示依赖未启用，不参与就绪判断。
	StatusDisabled = "disabled"
)

// 健康状态映射的依赖名称，探测与结果写入共用，避免出现拼写不一致的键。
const (
	dependencyMySQL = "mysql"
	dependencyRedis = "redis"
)

// Resources 持有当前应用启用的外部连接，由应用统一管理生命周期。
// 字段为 nil 表示依赖未启用；零值表示没有外部连接，可用于全部依赖关闭的应用。
// 关闭前必须停止全部使用者，不应与请求中的连接操作并发执行 Close。
type Resources struct {
	// DB 是注入业务仓储的数据库连接，启用后由 Resources.Close 释放。
	DB gdb.DB
	// Redis 是缓存及限流共享的 Redis 连接，启用后由 Resources.Close 释放。
	Redis *gredis.Redis
}
