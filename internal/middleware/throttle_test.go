package middleware

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	_ "github.com/gogf/gf/contrib/nosql/redis/v2"
	"github.com/gogf/gf/v2/database/gredis"
)

func TestRedisLimiterAtomicityAndExpiry(t *testing.T) {
	server := miniredis.RunT(t)
	client, err := gredis.New(&gredis.Config{Address: server.Addr(), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	l := limiter{redis: client, prefix: "unit"}
	ctx := context.Background()
	key := l.key("user-api", "192.0.2.1")
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.hit(ctx, key, 10*time.Second, 30, 10*time.Minute) == 0 {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 30 {
		t.Fatalf("concurrent limiter allowed %d, want 30", allowed.Load())
	}
	server.FastForward(time.Minute)
	if remaining := l.hit(ctx, key, 10*time.Second, 30, 10*time.Minute); remaining != 540 {
		t.Fatalf("block duration reset: %d", remaining)
	}
	server.FastForward(10 * time.Minute)
	if remaining := l.hit(ctx, key, 10*time.Second, 30, 10*time.Minute); remaining != 0 {
		t.Fatalf("expired block still active: %d", remaining)
	}
	missingKey := l.key("missing-route", "192.0.2.2")
	for i := 0; i < 4; i++ {
		if l.hit(ctx, missingKey, 30*time.Second, 4, 24*time.Hour) != 0 {
			t.Fatal("missing route blocked too early")
		}
	}
	if l.hit(ctx, missingKey, 30*time.Second, 4, 24*time.Hour) != 86400 {
		t.Fatal("fifth missing route did not create ban")
	}
}
