package loglimit

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

// syncBuffer 是并发安全的日志缓冲，用于断言告警实际输出内容。
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLog 把默认日志器的输出重定向到内存缓冲，返回读取函数。
// 断言依赖真实输出文本，因此测试不使用 t.Parallel，且结束时恢复原日志配置，避免影响同包其他用例。
func captureLog(t *testing.T) func() string {
	t.Helper()
	logger := g.Log()
	original := logger.GetConfig()
	buffer := &syncBuffer{}
	config := original
	config.Handlers = nil
	config.Path = ""
	config.StdoutPrint = false
	config.Writer = buffer
	if err := logger.SetConfig(config); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := logger.SetConfig(original); err != nil {
			t.Error(err)
		}
	})
	return buffer.String
}

// TestWarnSuppressesRepeatedAlertsInWindow 验证同一告警键在窗口内只输出首条，
// 且不同告警键各自独立计数。
func TestWarnSuppressesRepeatedAlertsInWindow(t *testing.T) {
	output := captureLog(t)
	limiter := New(time.Minute)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		limiter.Warn(ctx, "throttle-redis", "Redis 限流状态查询失败，降级放行")
	}
	limiter.Warn(ctx, "accesslog-queue-full", "访问日志队列已满，本条记录被丢弃")
	text := output()
	if count := strings.Count(text, "Redis 限流状态查询失败"); count != 1 {
		t.Fatalf("same key logged %d times in one window, want 1: %s", count, text)
	}
	if count := strings.Count(text, "访问日志队列已满"); count != 1 {
		t.Fatalf("other key logged %d times, want 1: %s", count, text)
	}
	if strings.Contains(text, "本窗口共") {
		t.Fatalf("in-window alerts must not report suppressed count: %s", text)
	}
}

// TestWarnReportsSuppressedCountAfterWindow 验证窗口结束后的首条输出追加被抑制次数，
// 使运维仍能判断故障期的真实告警规模。
func TestWarnReportsSuppressedCountAfterWindow(t *testing.T) {
	output := captureLog(t)
	limiter := New(60 * time.Millisecond)
	ctx := context.Background()
	limiter.Warn(ctx, "throttle-redis", "Redis 限流计数失败，降级放行")
	limiter.Warn(ctx, "throttle-redis", "Redis 限流计数失败，降级放行")
	limiter.Warn(ctx, "throttle-redis", "Redis 限流计数失败，降级放行")
	time.Sleep(120 * time.Millisecond)
	limiter.Warn(ctx, "throttle-redis", "Redis 限流计数失败，降级放行")
	text := output()
	if count := strings.Count(text, "Redis 限流计数失败"); count != 2 {
		t.Fatalf("window boundary logged %d lines, want 2: %s", count, text)
	}
	if !strings.Contains(text, "（本窗口共 3 次）") {
		t.Fatalf("suppressed count missing: %s", text)
	}
}

// TestWarnIsConcurrentSafe 验证并发调用只产生一条首条告警，
// 且内部计数状态不因并发访问而损坏。
func TestWarnIsConcurrentSafe(t *testing.T) {
	output := captureLog(t)
	limiter := New(time.Minute)
	ctx := context.Background()
	var wait sync.WaitGroup
	for i := 0; i < 50; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			limiter.Warn(ctx, "throttle-redis", "并发告警")
		}()
	}
	wait.Wait()
	if count := strings.Count(output(), "并发告警"); count != 1 {
		t.Fatalf("concurrent alerts logged %d times, want 1", count)
	}
}

// TestPackageWarnUsesSharedLimiter 验证包级 Warn 共用同一个进程级窗口，
// 使不同调用点针对同一告警键也只会输出首条。
func TestPackageWarnUsesSharedLimiter(t *testing.T) {
	output := captureLog(t)
	ctx := context.Background()
	Warn(ctx, "shared-key-test", "共享窗口告警")
	Warn(ctx, "shared-key-test", "共享窗口告警")
	if count := strings.Count(output(), "共享窗口告警"); count != 1 {
		t.Fatalf("package level limiter logged %d times, want 1", count)
	}
}
