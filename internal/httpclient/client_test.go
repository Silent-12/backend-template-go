package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"go.opentelemetry.io/otel/trace"
)

// TestNewRejectsUnsafeBaseURL 验证基础地址校验按原因拒绝非法配置，
// 错误只说明原因，不回显地址原文，避免把凭据写进日志或响应。
func TestNewRejectsUnsafeBaseURL(t *testing.T) {
	for _, tc := range []struct{ name, baseURL, reason string }{
		{"missing scheme", "example.com/api", "http://"},
		{"unsupported scheme", "ftp://example.com", "http://"},
		{"missing host", "https://", "主机名"},
		{"contains credentials", "https://user:secret@example.com", "凭据"},
		{"contains query", "https://example.com/api?token=abc", "查询"},
		{"contains fragment", "https://example.com/api#section", "片段"},
	} {
		_, err := New(Options{BaseURL: tc.baseURL})
		if err == nil {
			t.Fatalf("%s: 非法基础地址未被拒绝", tc.name)
		}
		if code := gerror.Code(err).Code(); code != gcode.CodeInvalidConfiguration.Code() {
			t.Fatalf("%s: 错误码为 %d，期望配置错误", tc.name, code)
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Fatalf("%s: 错误信息未指明原因: %v", tc.name, err)
		}
		if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "token") {
			t.Fatalf("%s: 错误信息回显了地址原文: %v", tc.name, err)
		}
	}
	if _, err := New(Options{BaseURL: "https://example.com/api"}); err != nil {
		t.Fatalf("合法基础地址被拒绝: %v", err)
	}
}

func TestClientDoesNotRetryWritesOrFollowRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/target", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	registry := NewRegistry()
	defer registry.Close()
	client, err := registry.New(Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	res, err := client.R().SetContext(context.Background()).SetBody(map[string]string{"name": "example"}).Post("/write")
	if err != nil || res.StatusCode() != 503 || !res.IsError() || calls.Load() != 1 {
		t.Fatalf("unexpected write retry/status: calls=%d err=%v", calls.Load(), err)
	}
	res, err = client.R().SetContext(context.Background()).Get("/redirect")
	if err == nil || res == nil || res.StatusCode() != 302 || calls.Load() != 2 {
		t.Fatalf("redirect was followed: calls=%d err=%v", calls.Load(), err)
	}
}

func TestClientCancellationTimeoutAndBodyLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wait" {
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(strings.Repeat("x", 2048)))
	}))
	defer server.Close()
	registry := NewRegistry()
	defer registry.Close()
	client, err := registry.New(Options{BaseURL: server.URL, Timeout: 80 * time.Millisecond, ResponseBodyLimit: 1024})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = client.R().SetContext(ctx).Get("/cancel"); !errors.Is(err, context.Canceled) {
		t.Fatalf("context cancellation lost: %v", err)
	}
	if _, err = client.R().SetContext(context.Background()).Get("/wait"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout missing: %v", err)
	}
	if _, err = client.R().SetContext(context.Background()).Get("/large"); err == nil {
		t.Fatal("oversized upstream response accepted")
	}
}

func TestClientPropagatesTraceAndVerifiesTLS(t *testing.T) {
	type incomingHeaders struct{ traceID, parent, state string }
	observed := make(chan incomingHeaders, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- incomingHeaders{traceID: r.Header.Get("X-Trace-Id"), parent: r.Header.Get("Traceparent"), state: r.Header.Get("Tracestate")}
		w.WriteHeader(200)
	}))
	defer server.Close()
	registry := NewRegistry()
	defer registry.Close()
	client, err := registry.New(Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	client.SetHeaders(map[string]string{"X-Trace-Id": "stale-client", "Traceparent": "stale-client", "Tracestate": "old=client"})
	traceID, _ := trace.TraceIDFromHex("aabbccddeeff00112233445566778899")
	spanID, _ := trace.SpanIDFromHex("1122334455667788")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled}))
	if _, err = client.R().SetContext(ctx).SetHeader("Traceparent", "untrusted-request").Get("/trace"); err != nil {
		t.Fatal(err)
	}
	incoming := <-observed
	if incoming.traceID != traceID.String() || !strings.Contains(incoming.parent, traceID.String()) || incoming.state != "" {
		t.Fatalf("trace not propagated: %q %q", incoming.traceID, incoming.parent)
	}
	if _, err = client.R().SetContext(context.Background()).Get("/without-trace"); err != nil {
		t.Fatal(err)
	}
	incoming = <-observed
	if incoming.traceID != "" || incoming.parent != "" || incoming.state != "" {
		t.Fatal("request without a span inherited stale trace headers")
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer tlsServer.Close()
	if _, err = client.R().SetContext(context.Background()).Get(tlsServer.URL); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}

func TestClientDoesNotShareUpstreamCookies(t *testing.T) {
	observed := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed <- r.Header.Get("Cookie")
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "upstream-user"})
	}))
	defer server.Close()
	registry := NewRegistry()
	defer registry.Close()
	client, err := registry.New(Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = client.R().SetContext(context.Background()).Get("/"); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if cookie := <-observed; cookie != "" {
			t.Fatal("shared client persisted an upstream user session")
		}
	}
}
