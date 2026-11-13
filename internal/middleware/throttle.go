package middleware

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/net/ghttp"

	"backend-template-go/internal/config"
	"backend-template-go/internal/loglimit"
)

// incrementScript 原子维护固定窗口计数和封禁状态，返回向上取整的剩余封禁秒数，零表示放行。
// KEYS 依次为封禁键和计数键，必须使用相同 hash tag；ARGV 依次为窗口毫秒数、次数上限和封禁毫秒数。
const incrementScript = `
local remaining = redis.call('PTTL', KEYS[1])
if remaining > 0 then return math.ceil(remaining / 1000) end
local hits = redis.call('INCR', KEYS[2])
if hits == 1 then redis.call('PEXPIRE', KEYS[2], ARGV[1]) end
if hits > tonumber(ARGV[2]) then
  redis.call('SET', KEYS[1], '1', 'PX', ARGV[3])
  redis.call('DEL', KEYS[2])
  return math.ceil(tonumber(ARGV[3]) / 1000)
end
return 0`

// throttleRedisKey 是限流 Redis 故障告警的合并键。
// 故障期间每个请求都会失败，按同一键合并后只保留窗口内首条日志。
const throttleRedisKey = "throttle-redis"

// key 将服务前缀、IP 摘要和策略类别组合为限流基础键。
// 同一 IP 使用相同的 Cluster hash tag，使后续封禁键和计数键位于同一槽，避免保存原始 IP。
func (l limiter) key(kind, ip string) string {
	digest := sha256.Sum256([]byte(ip))
	return fmt.Sprintf("%s:throttle:{%x}:%s", l.prefix, digest[:16], kind)
}

// remaining 查询基础键对应的封禁状态，返回向上取整的剩余秒数。
// 查询使用派生自 ctx 的一秒超时；未封禁、键无有效期或查询失败时返回零，失败时只合并告警并放行。
func (l limiter) remaining(ctx context.Context, key string) int {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	value, err := l.redis.Do(ctx, "PTTL", key+":block")
	if err != nil {
		loglimit.Warn(ctx, throttleRedisKey, "Redis 限流状态查询失败，降级放行")
		return 0
	}
	return max(0, (value.Int()+999)/1000)
}

// hit 原子计数并在超过窗口次数上限时封禁，返回向上取整的剩余封禁秒数。
// window 和 block 须为正的整毫秒时长，limit 须为正数；Redis 调用使用派生自 ctx 的一秒超时。
// 未封禁或执行失败时返回零，失败时只合并告警并降级放行。
func (l limiter) hit(ctx context.Context, key string, window time.Duration, limit int, block time.Duration) int {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	value, err := l.redis.Do(ctx, "EVAL", incrementScript, 2, key+":block", key+":counter", window.Milliseconds(), limit, block.Milliseconds())
	if err != nil {
		loglimit.Warn(ctx, throttleRedisKey, "Redis 限流计数失败，降级放行")
		return 0
	}
	return value.Int()
}

// Throttle 按客户端 IP 限制业务 API 频率，并对反复访问未知业务路由的 IP 封禁。
// 已匹配路由中除 health、admin、webhooks 外，十秒内超过三十次封禁十分钟；
// 未匹配且返回 404 的业务请求在三十秒内超过四次后封禁二十四小时，后续业务请求均受此封禁约束。
// OPTIONS、文档和探针路径跳过检查；限流关闭、Redis 连接为空或操作失败时降级放行。
// 触发封禁时返回安全的 429 JSON 和 Retry-After，Redis 连接由调用方管理。
func Throttle(c config.Config, redis *gredis.Redis) ghttp.HandlerFunc {
	l := limiter{redis: redis, prefix: c.ThrottleKeyPrefix}
	return func(r *ghttp.Request) {
		if !c.Throttle || redis == nil || r.Method == http.MethodOptions {
			r.Middleware.Next()
			return
		}
		business, relevant := businessPath(r.URL.Path, c.BasePath())
		if !relevant {
			r.Middleware.Next()
			return
		}
		ip := ClientIP(r.Request, c.TrustedProxies)
		missingKey := l.key("missing-route", ip)
		blocked := l.remaining(r.Context(), missingKey)
		if blocked == 0 && isUserAPIRoute(r, business) {
			blocked = l.hit(r.Context(), l.key("user-api", ip), 10*time.Second, 30, 10*time.Minute)
		}
		if blocked > 0 {
			r.Response.Header().Set("Retry-After", strconv.Itoa(blocked))
			JSON(r, http.StatusTooManyRequests, "当前操作频率过快，请稍后再试", nil)
			return
		}
		r.Middleware.Next()
		if r.Response.Status == http.StatusNotFound && r.GetServeHandler() == nil {
			l.hit(r.Context(), missingKey, 30*time.Second, 4, 24*time.Hour)
		}
	}
}

// isUserAPIRoute 判断已匹配路由是否需要按用户接口频率限制。
// 未匹配路由和 health、admin、webhooks 顶层路由另有策略，不在此计数。
func isUserAPIRoute(r *ghttp.Request, business string) bool {
	if r.GetServeHandler() == nil {
		return false
	}
	switch strings.SplitN(business, "/", 2)[0] {
	case "health", "admin", "webhooks":
		return false
	}
	return true
}

// businessPath 移除应用前缀和可选数字版本段，返回业务相对路径及是否参与限流。
// 前缀外路径、空路径、文档和探针不参与限流；此函数只做路径分类，不判断路由是否已注册。
func businessPath(path, prefix string) (string, bool) {
	if prefix != "" && !strings.HasPrefix(path, prefix+"/") {
		return "", false
	}
	path = trimVersionSegment(strings.Trim(strings.TrimPrefix(path, prefix), "/"))
	if isFixedPath(path) {
		return "", false
	}
	return path, true
}

// trimVersionSegment 移除业务路径开头的 v<数字> 版本段，其余路径原样返回。
// 只有完整的数字版本段才被移除，避免把 v2beta 之类的业务名称当成版本号。
func trimVersionSegment(path string) string {
	parts := strings.SplitN(path, "/", 2)
	segment := parts[0]
	if len(segment) < 2 || segment[0] != 'v' {
		return path
	}
	if _, err := strconv.Atoi(segment[1:]); err != nil {
		return path
	}
	if len(parts) == 2 {
		return parts[1]
	}
	return ""
}

// isFixedPath 判断业务相对路径的首段是否为固定协议路径（文档、探针），这些路径不参与限流。
func isFixedPath(path string) bool {
	switch strings.SplitN(path, "/", 2)[0] {
	case "", config.DocsPageSegment, config.OpenAPISegment, config.LivenessSegment, config.ReadinessSegment:
		return true
	}
	return false
}
