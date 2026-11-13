package infrastructure

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gogf/gf/v2/container/gvar"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/database/gredis"
)

func testRedis(t *testing.T) (*miniredis.Miniredis, *gredis.Redis) {
	t.Helper()
	server := miniredis.RunT(t)
	client, err := gredis.New(&gredis.Config{Address: server.Addr(), DialTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })
	return server, client
}

func TestCacheOperations(t *testing.T) {
	server, client := testRedis(t)
	cache, err := NewCache(client, "example")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if value, found, err := cache.Get(ctx, "missing"); err != nil || found || value != "" {
		t.Fatalf("missing cache: %q %v %v", value, found, err)
	}
	if err := cache.Set(ctx, "empty", "", 0); err != nil {
		t.Fatal(err)
	}
	if value, found, err := cache.Get(ctx, "empty"); err != nil || !found || value != "" {
		t.Fatalf("empty cache: %q %v %v", value, found, err)
	}
	if err := cache.Set(ctx, "temporary", "value", 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	server.FastForward(2 * time.Second)
	if _, found, err := cache.Get(ctx, "temporary"); err != nil || found {
		t.Fatalf("expired key: %v %v", found, err)
	}
	if ok, err := cache.SetIfAbsent(ctx, "once", "first", time.Second); err != nil || !ok {
		t.Fatalf("NX create: %v %v", ok, err)
	}
	if ok, err := cache.SetIfAbsent(ctx, "once", "second", time.Minute); err != nil || ok {
		t.Fatalf("NX overwrite: %v %v", ok, err)
	}
	if server.TTL("example:cache:once") != time.Second {
		t.Fatal("failed NX must not extend TTL")
	}
	if ok, err := cache.Delete(ctx, "empty"); err != nil || !ok {
		t.Fatalf("delete existing: %v %v", ok, err)
	}
	if ok, err := cache.Delete(ctx, "empty"); err != nil || ok {
		t.Fatalf("delete missing: %v %v", ok, err)
	}
	if err := cache.Set(ctx, "negative", "value", -time.Second); err == nil {
		t.Fatal("negative TTL accepted")
	}
	if ok, err := cache.SetIfAbsent(ctx, "forever", "value", 0); err == nil || ok {
		t.Fatal("non-expiring NX accepted")
	}
	disabled, err := NewCache(nil, "example")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := disabled.SetIfAbsent(ctx, "disabled", "value", time.Second); err == nil || ok {
		t.Fatal("disabled Redis must not grant NX success")
	}
	server.Close()
	if ok, err := cache.SetIfAbsent(ctx, "offline", "value", time.Second); err == nil || ok {
		t.Fatal("unavailable Redis must not grant NX success")
	}
}

func TestCacheNamespacesIsolateServices(t *testing.T) {
	_, client := testRedis(t)
	first, err := NewCache(client, "orders")
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewCache(client, "billing")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = first.Set(ctx, "same-key", "first", time.Minute); err != nil {
		t.Fatal(err)
	}
	if ok, err := second.SetIfAbsent(ctx, "same-key", "second", time.Minute); err != nil || !ok {
		t.Fatalf("another service's key blocked NX: %v %v", ok, err)
	}
	if _, err = second.Delete(ctx, "same-key"); err != nil {
		t.Fatal(err)
	}
	if value, found, err := first.Get(ctx, "same-key"); err != nil || !found || value != "first" {
		t.Fatalf("another service changed cached value: %q %v %v", value, found, err)
	}
	for _, prefix := range []string{"", "white space", "service:{fixed}"} {
		if _, err = NewCache(client, prefix); err == nil {
			t.Fatalf("invalid namespace accepted: %q", prefix)
		}
	}
}

type stalledDB struct{ gdb.DB }

func (stalledDB) GetValue(ctx context.Context, _ string, _ ...any) (gdb.Value, error) {
	<-ctx.Done()
	return gvar.New(nil), ctx.Err()
}

func TestHealthDependenciesAreIndependent(t *testing.T) {
	_, client := testRedis(t)
	resources := &Resources{DB: stalledDB{}, Redis: client}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	status := resources.Health(ctx)
	if status[dependencyMySQL] != StatusDown || status[dependencyRedis] != StatusUp {
		t.Fatalf("unhealthy MySQL contaminated Redis: %v", status)
	}
}
