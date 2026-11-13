// Package infrastructure 管理外部连接及其生命周期。
package infrastructure

import (
	"context"
	"net"
	"strconv"
	"time"

	_ "github.com/gogf/gf/contrib/drivers/mysql/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/errors/gerror"

	"backend-template-go/internal/config"
)

const (
	// probeTimeout 是单项依赖健康探测的超时，避免就绪检查无限等待。
	probeTimeout = 3 * time.Second
	// startTimeout 是应用启动时 MySQL 连接检查的超时。
	startTimeout = 5 * time.Second
	// redisStartTimeout 是应用启动时 Redis 连接检查的超时。
	redisStartTimeout = 3 * time.Second
)

// probeResult 在单次健康探测内部传递依赖名称和安全状态枚举，不保存底层错误。
type probeResult struct {
	name   string
	status string
}

// Open 按配置创建并探测已启用的 MySQL 和 Redis 连接，不创建关闭状态的依赖。
// c 应已经完成配置校验，ctx 传递给启动探测；任何初始化或探测失败均关闭已创建资源并返回安全错误。
// 成功后调用方拥有返回资源，必须在停止业务请求和后台任务后调用 Close。
func Open(ctx context.Context, c config.Config) (*Resources, error) {
	r := &Resources{}
	ok := false
	defer func() {
		if !ok {
			r.Close()
		}
	}()
	if c.MySQL {
		db, err := gdb.New(mysqlConfigNode(c))
		if err != nil {
			return nil, gerror.New("MySQL 初始化失败，请检查连接配置")
		}
		r.DB = db
		checkCtx, cancel := context.WithTimeout(ctx, startTimeout)
		_, err = db.GetValue(checkCtx, "SELECT 1")
		cancel()
		if err != nil {
			return nil, gerror.New("MySQL 连接检查失败，请检查服务与凭据")
		}
	}
	if c.Redis {
		client, err := gredis.New(redisConfig(c))
		if err != nil {
			return nil, gerror.New("Redis 初始化失败，请检查连接配置")
		}
		r.Redis = client
		checkCtx, cancel := context.WithTimeout(ctx, redisStartTimeout)
		_, err = client.Do(checkCtx, "PING")
		cancel()
		if err != nil {
			return nil, gerror.New("Redis 连接检查失败，请检查服务与凭据")
		}
	}
	ok = true
	return r, nil
}

// mysqlConfigNode 把已校验配置转换为 MySQL 连接参数。
// 连接池、查询和事务超时在此统一设上限；Debug 只在显式开启数据库日志时启用，避免输出完整 SQL。
func mysqlConfigNode(c config.Config) gdb.ConfigNode {
	return gdb.ConfigNode{
		Type:             "mysql",
		Host:             c.DBHost,
		Port:             strconv.Itoa(c.DBPort),
		User:             c.DBUser,
		Pass:             c.DBPassword,
		Name:             c.DBName,
		Charset:          "utf8mb4",
		Timezone:         "UTC",
		Debug:            c.DBLogging,
		MaxOpenConnCount: 10,
		MaxIdleConnCount: 5,
		MaxConnLifeTime:  30 * time.Minute,
		QueryTimeout:     5 * time.Second,
		ExecTimeout:      5 * time.Second,
		TranTimeout:      10 * time.Second,
		Extra:            "timeout=5s&readTimeout=5s&writeTimeout=5s",
	}
}

// redisConfig 把已校验配置转换为 Redis 连接参数，超时和连接数保持有界。
// 连接由 Resources 持有，缓存与限流共用同一份配置。
func redisConfig(c config.Config) *gredis.Config {
	return &gredis.Config{
		Address:      net.JoinHostPort(c.RedisHost, strconv.Itoa(c.RedisPort)),
		Db:           c.RedisDB,
		User:         c.RedisUser,
		Pass:         c.RedisPassword,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		MaxActive:    20,
	}
}

// Close 依次释放已启用的 Redis 和数据库连接，忽略底层关闭错误。
// 调用方必须先停止请求和使用连接的后台任务；关闭使用独立上下文，不受原请求取消影响。
func (r *Resources) Close() {
	if r.Redis != nil {
		_ = r.Redis.Close(context.Background())
	}
	if r.DB != nil {
		_ = r.DB.Close(context.Background())
	}
}

// Health 并行探测当前资源状态，分别返回 mysql 和 redis 的状态枚举。
// 未启用的依赖为 disabled，探测成功为 up，失败或超时为 down；每项探测使用派生自 ctx 的三秒超时。
// 等待两个探测结束后返回，不向调用方暴露底层错误文本。
func (r *Resources) Health(ctx context.Context) map[string]string {
	result := map[string]string{
		dependencyMySQL: StatusDisabled,
		dependencyRedis: StatusDisabled,
	}
	checks := r.checks()
	results := make(chan probeResult, len(checks))
	// 分别探测，避免 MySQL 耗尽共享超时后把健康的 Redis 也误报为 down。
	for name, check := range checks {
		go func() {
			results <- probe(ctx, name, check)
		}()
	}
	for range checks {
		status := <-results
		result[status.name] = status.status
	}
	return result
}

// checks 返回依赖名称到健康检查函数的映射，未启用的依赖对应 nil，由 probe 归类为 disabled。
func (r *Resources) checks() map[string]func(context.Context) error {
	checks := map[string]func(context.Context) error{
		dependencyMySQL: nil,
		dependencyRedis: nil,
	}
	if r.DB != nil {
		checks[dependencyMySQL] = r.checkMySQL
	}
	if r.Redis != nil {
		checks[dependencyRedis] = r.checkRedis
	}
	return checks
}

// checkMySQL 在给定上下文内执行一次 SELECT 1，只返回是否成功。
func (r *Resources) checkMySQL(ctx context.Context) error {
	_, err := r.DB.GetValue(ctx, "SELECT 1")
	return err
}

// checkRedis 在给定上下文内执行一次 PING，只返回是否成功。
func (r *Resources) checkRedis(ctx context.Context) error {
	_, err := r.Redis.Do(ctx, "PING")
	return err
}

// probe 探测单项依赖并返回依赖名称与安全状态枚举，供 Health 在独立协程中并行调用。
// check 为 nil 表示依赖未启用，不发起任何网络操作；否则使用派生自 ctx 的三秒超时，
// 失败或超时只记为 down，不保留底层错误文本。
func probe(ctx context.Context, name string, check func(context.Context) error) probeResult {
	if check == nil {
		return probeResult{name: name, status: StatusDisabled}
	}
	checkCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if check(checkCtx) != nil {
		return probeResult{name: name, status: StatusDown}
	}
	return probeResult{name: name, status: StatusUp}
}
