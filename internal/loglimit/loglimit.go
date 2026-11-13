// Package loglimit 对同类告警做时间窗口合并，避免故障期间日志洪泛。
// 依赖不可用时每个请求都可能产生相同告警，按告警键合并后只保留窗口内的首条，
// 并在窗口结束后的首条日志中追加上一窗口的重复次数，保留可观测性而不淹没日志。
package loglimit

import (
	"context"
	"sync"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const (
	// defaultWindow 是包级 Warn 使用的合并窗口。
	defaultWindow = 10 * time.Second
	// maxTrackedKeys 限制同时跟踪的告警键数量，防止动态键让进程内状态无界增长。
	maxTrackedKeys = 128
)

// Limiter 按告警键合并时间窗口内的重复日志，可并发使用。
// 同一窗口内同一键只输出首条，其余仅计数；窗口结束后的首条输出追加上一窗口的总次数。
// 状态只保存在进程内且不持久化，进程重启后重新计数；不要用用户可控的高基数值作为键。
type Limiter struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]*windowState
}

// windowState 记录单个告警键当前窗口的起点和已抑制的重复条数，由 Limiter 的互斥锁保护。
type windowState struct {
	start      time.Time
	suppressed int
}

// New 创建按 window 合并同类告警的限流器，可并发复用。
// window 必须为正，非正值回退到十秒默认窗口，避免退化成每条都输出或永不输出。
func New(window time.Duration) *Limiter {
	if window <= 0 {
		window = defaultWindow
	}
	return &Limiter{
		window:  window,
		entries: make(map[string]*windowState),
	}
}

// Warn 在时间窗口内合并同一 key 的重复告警，只输出首条。
// 窗口结束后的首条输出会追加"（本窗口共 N 次）"，N 为上一条已输出告警所在窗口的总次数。
// ctx 只用于日志关联追踪号；message 必须由调用方提供脱敏文本，不得包含凭据、SQL 或上游响应原文。
func (l *Limiter) Warn(ctx context.Context, key, message string) {
	if l == nil {
		g.Log().Warning(ctx, message)
		return
	}
	previous, output := l.record(key)
	if !output {
		return
	}
	if previous > 1 {
		g.Log().Warningf(ctx, "%s（本窗口共 %d 次）", message, previous)
		return
	}
	g.Log().Warning(ctx, message)
}

// record 更新 key 的窗口状态，返回上一窗口的总条数和本次是否应输出日志。
// 计数在同一把锁内完成，使并发告警只产生一条首条日志；被抑制的条数计入当前窗口。
func (l *Limiter) record(key string) (previous int, output bool) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.entries[key]
	if ok && now.Sub(state.start) < l.window {
		state.suppressed++
		return 0, false
	}
	if !ok {
		l.evict(now)
	}
	if ok {
		previous = state.suppressed + 1
	}
	l.entries[key] = &windowState{start: now}
	return previous, true
}

// evict 在跟踪键达到上限时先清理过期窗口，必要时淘汰最早开始的窗口。
// 调用方必须已持有 Limiter 的互斥锁；只有调用方使用动态键时才会触发淘汰。
func (l *Limiter) evict(now time.Time) {
	if len(l.entries) < maxTrackedKeys {
		return
	}
	var (
		oldestKey string
		oldest    time.Time
	)
	for key, state := range l.entries {
		if now.Sub(state.start) >= l.window {
			delete(l.entries, key)
			continue
		}
		if oldestKey == "" || state.start.Before(oldest) {
			oldestKey, oldest = key, state.start
		}
	}
	if len(l.entries) >= maxTrackedKeys && oldestKey != "" {
		delete(l.entries, oldestKey)
	}
}

// defaultLimiter 是包级 Warn 使用的进程级限流器，窗口为十秒。
var defaultLimiter = New(defaultWindow)

// Warn 使用进程级十秒窗口合并同类告警，供不持有 Limiter 的调用点直接使用。
// key 由调用方定义并保持稳定，message 须为脱敏文本；多个调用点共用同一份窗口状态。
func Warn(ctx context.Context, key, message string) {
	defaultLimiter.Warn(ctx, key, message)
}
