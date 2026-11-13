package infrastructure

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gredis"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"backend-template-go/internal/config"
)

// NewCache 包装 Redis 连接并校验服务前缀，所有业务键自动附加 <前缀>:cache:。
// 前缀非法时返回配置错误；允许传入 nil 连接，但缓存操作会显式报告 Redis 未启用。
// 连接仍由调用方管理，构造后不得修改命名空间。
func NewCache(client *gredis.Redis, keyPrefix string) (*Cache, error) {
	if err := config.ValidateRedisKeyPrefix(keyPrefix); err != nil {
		return nil, err
	}
	return &Cache{client: client, prefix: keyPrefix + ":cache:"}, nil
}

// check 检查缓存实例、Redis 连接和业务键是否可用。
// 实例或连接为空时返回配置错误，业务键为空时返回参数错误；不会发起 Redis 请求。
func (c *Cache) check(key string) error {
	if c == nil || c.client == nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "Redis 未启用")
	}
	if key == "" {
		return gerror.NewCode(gcode.CodeInvalidParameter, "Redis key 不可为空")
	}
	return nil
}

// Set 使用 ctx 写入指定业务键的字符串值，覆盖已有值。
// ttl 为零时不设置过期时间，正值向上取整为毫秒，负值返回参数错误。
// 连接不可用、键为空或 Redis 写入失败时返回错误，不将失败视为写入成功。
func (c *Cache) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	if err := c.check(key); err != nil {
		return err
	}
	if ttl < 0 {
		return gerror.NewCode(gcode.CodeInvalidParameter, "缓存有效期不可为负数")
	}
	args := []any{c.prefix + key, value}
	if ttl > 0 {
		args = append(args, "PX", ttlMillis(ttl))
	}
	_, err := c.client.Do(ctx, "SET", args...)
	return err
}

// Get 使用 ctx 读取字符串缓存，自动附加当前服务的缓存命名空间。
// 键不存在时返回空字符串、false 和 nil；已缓存的空字符串返回空字符串、true 和 nil。
// 连接不可用、键为空或 Redis 读取失败时返回错误。
func (c *Cache) Get(ctx context.Context, key string) (value string, found bool, err error) {
	if err = c.check(key); err != nil {
		return "", false, err
	}
	result, err := c.client.Do(ctx, "GET", c.prefix+key)
	if err != nil {
		return "", false, err
	}
	if result.IsNil() {
		return "", false, nil
	}
	return result.String(), true, nil
}

// Delete 使用 ctx 删除业务键，返回该键是否实际存在并被删除。
// 键不存在时返回 false 和 nil；连接不可用、键为空或 Redis 执行失败时返回错误。
func (c *Cache) Delete(ctx context.Context, key string) (bool, error) {
	if err := c.check(key); err != nil {
		return false, err
	}
	result, err := c.client.Do(ctx, "DEL", c.prefix+key)
	if err != nil {
		return false, err
	}
	return result.Int() > 0, nil
}

// SetIfAbsent 在业务键不存在时原子执行 NX+PX 写入，返回是否成功写入。
// ttl 必须为正，执行前向上取整为毫秒；键已存在时返回 false 和 nil，执行失败时返回错误。
// 用于业务互斥时，调用方仍需实现基于唯一持有者令牌的续期和比较删除。
func (c *Cache) SetIfAbsent(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	if err := c.check(key); err != nil {
		return false, err
	}
	if ttl <= 0 {
		return false, gerror.NewCode(gcode.CodeInvalidParameter, "条件写入必须提供正有效期")
	}
	result, err := c.client.Do(ctx, "SET", c.prefix+key, value, "NX", "PX", ttlMillis(ttl))
	if err != nil {
		return false, err
	}
	return !result.IsNil(), nil
}

// ttlMillis 将调用方已校验的正 TTL 向上取整为毫秒。
// 这样既不会缩短指定有效期，也避免不足一毫秒被截断为 Redis 不接受的零值。
func ttlMillis(ttl time.Duration) int64 {
	value := ttl.Milliseconds()
	if ttl%time.Millisecond != 0 {
		value++
	}
	return value
}
