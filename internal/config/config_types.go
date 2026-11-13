package config

import (
	"net/netip"
)

// Config 是应用启动配置快照，由 Load 或 Parse 填充默认值并校验。
// 零值未经校验，不可直接用于启动；启动后按只读数据使用，包括其中的切片。
// 配置包含凭据，不得整体打印或交给日志格式化，JSON 忽略标签不能替代日志脱敏。
type Config struct {
	// Env 是运行环境，仅允许 development、test 或 production。
	Env string
	// AppName 是独立服务名称，同时作为运行时名称、文档标题和默认 Redis 命名空间来源。
	AppName string
	// Host 是 HTTP 服务监听地址。
	Host string
	// Port 是 HTTP 服务监听端口，范围为 1 到 65535。
	Port int
	// APIPrefix 是不含首尾斜杠的业务路由前缀，空值表示不增加前缀。
	APIPrefix string
	// CORSOrigins 是允许跨域访问的来源列表，生产环境禁止通配符。
	CORSOrigins []string
	// TrustedProxies 是允许解析转发 IP 链的直接代理 CIDR 白名单。
	TrustedProxies []netip.Prefix
	// HTTPLog 控制数据库访问日志，启用时必须同时启用 MySQL。
	HTTPLog bool
	// Docs 控制接口文档入口，生产环境解析配置时强制关闭。
	Docs bool
	// DocsAuth 控制非生产文档入口的 Basic Auth。
	DocsAuth bool
	// DocsUsername 是文档认证用户名。
	DocsUsername string
	// DocsPassword 是文档认证密码，保留原始首尾空格，不参与 JSON 编码。
	DocsPassword string `json:"-"`

	// MySQL 控制数据库连接及依赖数据库的示例业务接口。
	MySQL bool
	// DBLogging 控制数据库调试日志，使用时仍须遵循凭据和数据脱敏约定。
	DBLogging bool
	// DBHost 是 MySQL 主机地址。
	DBHost string
	// DBPort 是 MySQL 连接端口。
	DBPort int
	// DBUser 是 MySQL 连接用户名。
	DBUser string
	// DBPassword 是 MySQL 密码，原样读取，不参与 JSON 编码。
	DBPassword string `json:"-"`
	// DBName 是服务独立使用的数据库名，启用 MySQL 时必须显式配置。
	DBName string

	// Redis 控制 Redis 连接，限流启用时必须同时启用。
	Redis bool
	// RedisHost 是 Redis 主机地址。
	RedisHost string
	// RedisPort 是 Redis 连接端口。
	RedisPort int
	// RedisUser 是 Redis ACL 用户名，可为空。
	RedisUser string
	// RedisPassword 是 Redis 密码，原样读取，不参与 JSON 编码。
	RedisPassword string `json:"-"`
	// RedisDB 是 Redis 逻辑数据库编号，当前配置范围为 0 到 15。
	RedisDB int
	// RedisKeyPrefix 隔离共享 Redis 中的服务数据，未配置时继承 AppName。
	RedisKeyPrefix string
	// Throttle 控制基于 Redis 的 API 频率限制和未知路由封禁。
	Throttle bool
	// ThrottleKeyPrefix 是限流命名空间，未配置时继承 RedisKeyPrefix。
	ThrottleKeyPrefix string
}
