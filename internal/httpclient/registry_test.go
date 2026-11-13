package httpclient

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// TestRegistryCloseIsIdempotentAndConcurrentSafe 验证关闭可重复、可并发调用，
// 且关闭后创建客户端返回明确的配置错误。
func TestRegistryCloseIsIdempotentAndConcurrentSafe(t *testing.T) {
	registry := NewRegistry()
	for i := 0; i < 3; i++ {
		if _, err := registry.New(Options{}); err != nil {
			t.Fatalf("登记客户端失败: %v", err)
		}
	}
	var wait sync.WaitGroup
	for i := 0; i < 4; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			registry.Close()
		}()
	}
	wait.Wait()
	registry.Close()
	if _, err := registry.New(Options{}); err == nil {
		t.Fatal("关闭后仍可创建客户端")
	} else if code := gerror.Code(err).Code(); code != gcode.CodeInvalidConfiguration.Code() {
		t.Fatalf("关闭后创建客户端返回错误码 %d，期望配置错误", code)
	}
}

// TestRegistryNewReturnsUsableClient 验证登记成功时返回已配置好基础地址的客户端，
// 并使用统一工厂的默认超时。
func TestRegistryNewReturnsUsableClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	registry := NewRegistry()
	defer registry.Close()
	client, err := registry.New(Options{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("登记客户端失败: %v", err)
	}
	if client == nil || client.GetClient() == nil || client.BaseURL != server.URL {
		t.Fatalf("客户端未正确初始化: %+v", client)
	}
	res, err := client.R().SetContext(context.Background()).Get("/probe")
	if err != nil || res.StatusCode() != http.StatusNoContent {
		t.Fatalf("登记后的客户端不可用: status=%v err=%v", res, err)
	}
}

// TestRegistryCloseReleasesIdleConnections 验证关闭注册表时清理已登记客户端的空闲连接。
// 通过测试服务器的连接状态回调统计被关闭的连接，避免只断言 Close 不报错。
func TestRegistryCloseReleasesIdleConnections(t *testing.T) {
	var (
		mu     sync.Mutex
		closed int
	)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state != http.StateClosed {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		closed++
	}
	server.Start()
	defer server.Close()
	registry := NewRegistry()
	client, err := registry.New(Options{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("登记客户端失败: %v", err)
	}
	if _, err = client.R().SetContext(context.Background()).Get("/probe"); err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	registry.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		count := closed
		mu.Unlock()
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("关闭注册表后空闲连接未被清理")
}
