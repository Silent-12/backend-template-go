// Package config 加载并校验应用配置，禁止输出包含密码的配置对象。
package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/joho/godotenv"
)

// 运行环境标识，Parse 只接受这三个值，其他取值会被拒绝。
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// 固定路由段，调用方通过 DocsPath 等方法拼出完整路径，避免在服务代码里重复字面量。
const (
	DocsPageSegment  = "doc.html"
	OpenAPISegment   = "api-json"
	LivenessSegment  = "livez"
	ReadinessSegment = "readyz"
)

// 包级编译一次的正则，避免每次解析配置都重复编译。
var (
	// appNamePattern 限制服务名可安全用于运行时名称、文档标题和默认 Redis 命名空间。
	appNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	// apiPrefixPattern 限制 API 前缀为由斜杠分隔的非空路径段。
	apiPrefixPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+(?:/[a-zA-Z0-9_-]+)*$`)
	// redisKeyPrefixPattern 限制 Redis 命名空间长度和字符集，并排除花括号以免覆盖 Cluster hash tag。
	redisKeyPrefixPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:_-]{0,127}$`)
)

// Load 从当前工作目录读取 .env 并解析启动配置，优先级为进程环境、.env、内置默认值。
// 不会修改进程环境；文件不存在时仅使用环境和默认值，读取、解析或配置校验失败时返回安全错误。
func Load() (Config, error) {
	return load(os.LookupEnv, ".env")
}

// load 从 path 读取环境文件，优先使用 lookup 提供的同名配置。
// lookup 必须非 nil；文件不存在可继续解析，其他文件错误不保留可能包含凭据的底层文本。
func load(lookup func(string) (string, bool), path string) (Config, error) {
	values, err := godotenv.Read(path)
	if err != nil && !os.IsNotExist(err) {
		// 解析错误可能包含凭据所在行，不把底层文本放进错误链。
		return Config{}, gerror.NewCode(gcode.CodeInvalidConfiguration, "无法读取或解析环境配置文件")
	}
	return Parse(func(key string) (string, bool) {
		if value, ok := lookup(key); ok {
			return value, true
		}
		value, ok := values[key]
		return value, ok
	})
}

// Parse 使用 lookup 提供的环境键值构造并校验启动配置，不读取文件或修改进程环境。
// lookup 必须非 nil；普通配置去除首尾空格，密码保持原值，未提供的键使用模板默认值。
// 解析按服务、数据库、Redis、安全域拆分，跨域依赖在装配后统一校验；
// 配置无效时汇总返回配置错误，此时返回的 Config 可能不完整，不得用于启动服务。
func Parse(lookup func(string) (string, bool)) (c Config, err error) {
	var (
		server, serverProblems     = parseServer(lookup)
		database, databaseProblems = parseDatabase(lookup)
		cache, cacheProblems       = parseRedis(lookup, server.AppName)
		security, securityProblems = parseSecurity(lookup)
	)
	problems := slices.Concat(serverProblems, databaseProblems, cacheProblems, securityProblems)

	c = combineConfig(server, database, cache, security)
	// 生产环境强制关闭文档入口，后续校验和启动都使用最终生效值。
	if c.IsProduction() {
		c.Docs = false
	}
	problems = append(problems, validate(c)...)
	if len(problems) > 0 {
		return c, gerror.NewCodef(gcode.CodeInvalidConfiguration, "配置校验失败: %s", strings.Join(problems, "; "))
	}
	return c, nil
}

// envReader 为单个配置域读取环境值，并把格式问题收集到 problems 供 Parse 汇总。
// 它只处理键的取值、默认值和格式，跨字段依赖由 validate 负责。
type envReader struct {
	lookup   func(string) (string, bool)
	problems []string
}

// newEnvReader 创建读取 lookup 的域内读取器；lookup 必须非 nil。
func newEnvReader(lookup func(string) (string, bool)) *envReader {
	return &envReader{lookup: lookup}
}

// text 读取普通配置并去除首尾空格，未提供的键返回 fallback。
func (r *envReader) text(key, fallback string) string {
	if value, ok := r.lookup(key); ok {
		return strings.TrimSpace(value)
	}
	return fallback
}

// secret 原样读取凭据，首尾空格可能是密码的一部分，不参与常规去空格。
func (r *envReader) secret(key string) string {
	value, _ := r.lookup(key)
	return value
}

// boolean 读取布尔开关，取值不是 true 或 false 时记录问题并返回 false。
func (r *envReader) boolean(key string, fallback bool) bool {
	value := r.text(key, strconv.FormatBool(fallback))
	if value != "true" && value != "false" {
		r.problems = append(r.problems, key+" 必须是 true 或 false")
	}
	return value == "true"
}

// number 读取闭区间内的整数，非数字或越界时记录问题并返回解析到的值。
func (r *envReader) number(key string, fallback, minimum, maximum int) int {
	value, e := strconv.Atoi(r.text(key, strconv.Itoa(fallback)))
	if e != nil || value < minimum || value > maximum {
		r.problems = append(r.problems, fmt.Sprintf("%s 必须在 %d 到 %d 之间", key, minimum, maximum))
	}
	return value
}

// parseServer 解析运行环境、服务名、监听地址、API 前缀、日志开关和文档认证配置。
// 返回只填充服务域字段的部分配置和该域的格式问题，不做跨域校验。
func parseServer(lookup func(string) (string, bool)) (Config, []string) {
	var (
		c Config
		r = newEnvReader(lookup)
	)
	c.Env = r.text("APP_ENV", EnvDevelopment)
	c.AppName = r.text("APP_NAME", "backend-template-go")
	if !appNamePattern.MatchString(c.AppName) {
		r.problems = append(r.problems, "APP_NAME 必须以小写字母开头，仅包含小写字母、数字、下划线或连字符，最多 63 字符")
	}
	c.Host = r.text("HOST", "0.0.0.0")
	c.Port = r.number("PORT", 3000, 1, 65535)
	c.APIPrefix = strings.Trim(r.text("API_PREFIX", "backend"), "/")
	if c.APIPrefix != "" && !apiPrefixPattern.MatchString(c.APIPrefix) {
		r.problems = append(r.problems, "API_PREFIX 格式无效")
	}
	c.HTTPLog = r.boolean("HTTP_LOG_ENABLED", false)
	c.Docs = r.boolean("DOCS_ENABLED", true)
	c.DocsAuth = r.boolean("DOCS_AUTH_ENABLED", false)
	c.DocsUsername = r.text("DOCS_USERNAME", "")
	c.DocsPassword = r.secret("DOCS_PASSWORD")
	return c, r.problems
}

// parseDatabase 解析 MySQL 连接、开关、调试日志和服务独立库名。
// 返回只填充数据库域字段的部分配置和该域的格式问题。
func parseDatabase(lookup func(string) (string, bool)) (Config, []string) {
	var (
		c Config
		r = newEnvReader(lookup)
	)
	c.MySQL = r.boolean("MYSQL_ENABLED", false)
	c.DBLogging = r.boolean("DB_LOGGING", false)
	c.DBHost = r.text("DB_HOST", "127.0.0.1")
	c.DBUser = r.text("DB_USERNAME", "app")
	c.DBPassword = r.secret("DB_PASSWORD")
	c.DBName = r.text("DB_DATABASE", "")
	c.DBPort = r.number("DB_PORT", 3306, 1, 65535)
	return c, r.problems
}

// parseRedis 解析 Redis 连接、命名空间和限流配置。
// appName 取自服务域，用于未显式配置时推导 REDIS_KEY_PREFIX 的默认值；
// 限流前缀默认继承已解析的 Redis 前缀，旧的显式前缀继续优先。
func parseRedis(lookup func(string) (string, bool), appName string) (Config, []string) {
	var (
		c Config
		r = newEnvReader(lookup)
	)
	c.Redis = r.boolean("REDIS_ENABLED", false)
	c.RedisHost = r.text("REDIS_HOST", "127.0.0.1")
	c.RedisUser = r.text("REDIS_USERNAME", "")
	c.RedisPassword = r.secret("REDIS_PASSWORD")
	c.RedisPort = r.number("REDIS_PORT", 6379, 1, 65535)
	c.RedisDB = r.number("REDIS_DB", 0, 0, 15)
	c.RedisKeyPrefix = r.text("REDIS_KEY_PREFIX", appName)
	c.Throttle = r.boolean("THROTTLE_ENABLED", false)
	c.ThrottleKeyPrefix = r.text("THROTTLE_KEY_PREFIX", c.RedisKeyPrefix)
	if e := ValidateRedisKeyPrefix(c.RedisKeyPrefix); e != nil {
		r.problems = append(r.problems, "REDIS_KEY_PREFIX 格式无效")
	}
	if e := ValidateRedisKeyPrefix(c.ThrottleKeyPrefix); e != nil {
		r.problems = append(r.problems, "THROTTLE_KEY_PREFIX 格式无效")
	}
	return c, r.problems
}

// parseSecurity 解析跨域来源白名单和可信代理网段。
// 逐项校验并保留原始值，非法项只记录问题；零前缀网段会让任意来源伪造转发头，必须拒绝。
func parseSecurity(lookup func(string) (string, bool)) (Config, []string) {
	var (
		c Config
		r = newEnvReader(lookup)
	)
	for _, value := range strings.Split(r.text("CORS_ORIGINS", ""), ",") {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, e := parseOrigin(value); e != nil {
			r.problems = append(r.problems, "CORS_ORIGINS "+e.Error())
		}
		c.CORSOrigins = append(c.CORSOrigins, value)
	}
	for _, value := range strings.Split(r.text("TRUSTED_PROXY_CIDRS", ""), ",") {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		prefix, e := netip.ParsePrefix(value)
		if e != nil {
			r.problems = append(r.problems, "TRUSTED_PROXY_CIDRS 包含无效 CIDR")
			continue
		}
		if prefix.Bits() == 0 {
			r.problems = append(r.problems, "TRUSTED_PROXY_CIDRS 不允许零前缀网段（0.0.0.0/0 或 ::/0），会允许任意来源伪造转发头，请填写入口代理的具体网段")
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix)
	}
	return c, r.problems
}

// validate 校验跨域字段依赖，一次返回全部问题便于调用方看到完整修复清单。
// 调用方须先应用生产环境降级（关闭文档入口）和域内格式校验，再调用本函数。
func validate(c Config) []string {
	var problems []string
	if c.Env != EnvDevelopment && c.Env != EnvTest && c.Env != EnvProduction {
		problems = append(problems, "APP_ENV 必须是 development、test 或 production")
	}
	if c.Host == "" {
		problems = append(problems, "HOST 不可为空")
	}
	if c.MySQL && (c.DBHost == "" || c.DBUser == "" || c.DBName == "") {
		problems = append(problems, "启用 MySQL 时必须配置 DB_HOST、DB_USERNAME、DB_DATABASE")
	}
	if c.Redis && c.RedisHost == "" {
		problems = append(problems, "启用 Redis 时 REDIS_HOST 不可为空")
	}
	if c.Throttle && (!c.Redis || c.ThrottleKeyPrefix == "") {
		problems = append(problems, "限流要求启用 Redis 且 THROTTLE_KEY_PREFIX 非空")
	}
	if c.HTTPLog && !c.MySQL {
		problems = append(problems, "数据库访问日志要求启用 MySQL")
	}
	if c.Docs && !c.IsProduction() && c.DocsAuth && (c.DocsUsername == "" || strings.TrimSpace(c.DocsPassword) == "") {
		problems = append(problems, "文档认证用户名和密码不可为空")
	}
	if c.IsProduction() {
		for _, origin := range c.CORSOrigins {
			if origin == "*" {
				problems = append(problems, "生产环境 CORS_ORIGINS 不允许通配符")
			}
		}
	}
	return problems
}

// combineConfig 按 Config 字段顺序装配各域解析结果；各域字段互不重叠，这里只搬运不校验。
func combineConfig(server, database, cache, security Config) Config {
	return Config{
		Env:               server.Env,
		AppName:           server.AppName,
		Host:              server.Host,
		Port:              server.Port,
		APIPrefix:         server.APIPrefix,
		CORSOrigins:       security.CORSOrigins,
		TrustedProxies:    security.TrustedProxies,
		HTTPLog:           server.HTTPLog,
		Docs:              server.Docs,
		DocsAuth:          server.DocsAuth,
		DocsUsername:      server.DocsUsername,
		DocsPassword:      server.DocsPassword,
		MySQL:             database.MySQL,
		DBLogging:         database.DBLogging,
		DBHost:            database.DBHost,
		DBPort:            database.DBPort,
		DBUser:            database.DBUser,
		DBPassword:        database.DBPassword,
		DBName:            database.DBName,
		Redis:             cache.Redis,
		RedisHost:         cache.RedisHost,
		RedisPort:         cache.RedisPort,
		RedisUser:         cache.RedisUser,
		RedisPassword:     cache.RedisPassword,
		RedisDB:           cache.RedisDB,
		RedisKeyPrefix:    cache.RedisKeyPrefix,
		Throttle:          cache.Throttle,
		ThrottleKeyPrefix: cache.ThrottleKeyPrefix,
	}
}

// parseOrigin 校验单个跨域来源，合法时返回原值，否则返回说明具体原因的错误。
// "*" 是唯一允许的通配符，生产环境由 validate 拒绝；其余来源必须是 http 或 https，
// 且不含路径、查询、片段、用户凭据和主机通配符，避免浏览器按不同来源放行。
func parseOrigin(value string) (string, error) {
	if value == "*" {
		return value, nil
	}
	origin, err := url.Parse(value)
	if err != nil {
		return "", gerror.New("格式无效")
	}
	switch {
	case origin.Scheme != "http" && origin.Scheme != "https":
		return "", gerror.New("必须使用 http 或 https 协议")
	case origin.Host == "":
		return "", gerror.New("缺少主机地址")
	case origin.User != nil:
		return "", gerror.New("不得包含用户凭据")
	case strings.Contains(origin.Host, "*"):
		return "", gerror.New("主机不得使用通配符")
	case origin.Path != "":
		return "", gerror.New("不得包含路径")
	case origin.RawQuery != "" || origin.ForceQuery:
		return "", gerror.New("不得包含查询参数")
	case origin.Fragment != "":
		return "", gerror.New("不得包含片段")
	}
	return value, nil
}

// ValidateRedisKeyPrefix 校验 Redis 服务命名空间，合法时返回 nil，否则返回配置错误。
// prefix 长度须为 1 到 128 字节，以字母或数字开头，仅允许字母、数字、冒号、下划线和连字符。
// 禁止花括号，避免覆盖限流键自身的 Redis Cluster hash tag。
func ValidateRedisKeyPrefix(prefix string) error {
	if !redisKeyPrefixPattern.MatchString(prefix) {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "Redis key 前缀必须为 1 到 128 位字母、数字、冒号、下划线或连字符，且以字母或数字开头")
	}
	return nil
}

// IsProduction 报告当前配置是否属于生产环境，用于关闭接口文档和收紧跨域等策略。
func (c Config) IsProduction() bool {
	return c.Env == EnvProduction
}

// BasePath 为配置中的非空 API 前缀补充开头的斜杠，空前缀返回空字符串。
// 调用方应使用 Load 或 Parse 已规范化的配置；此方法不再次清理或校验 APIPrefix。
func (c Config) BasePath() string {
	if c.APIPrefix == "" {
		return ""
	}
	return "/" + c.APIPrefix
}

// DocsPath 返回接口文档页面的完整路径，包含 API 前缀但不包含监听地址。
func (c Config) DocsPath() string {
	return c.BasePath() + "/" + DocsPageSegment
}

// OpenAPIPath 返回 OpenAPI 规范文档的完整路径，供文档页面同源加载。
func (c Config) OpenAPIPath() string {
	return c.BasePath() + "/" + OpenAPISegment
}

// LivenessPath 返回存活探针路径，仅表示进程可响应，不检查外部依赖。
func (c Config) LivenessPath() string {
	return c.BasePath() + "/" + LivenessSegment
}

// ReadinessPath 返回就绪探针路径，调用方需在依赖不可用时返回 503。
func (c Config) ReadinessPath() string {
	return c.BasePath() + "/" + ReadinessSegment
}
