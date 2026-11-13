package accesslog

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
)

// createAccessLogsSQL 与 manifest/sql/V2等价，仅改用 SQLite 语法。
// 默认测试不依赖外部实例，因此按迁移字段在测试内建表。
const createAccessLogsSQL = `CREATE TABLE access_logs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	trace_id CHAR(32) NOT NULL,
	method VARCHAR(16) NOT NULL,
	path VARCHAR(512) NOT NULL,
	status_code SMALLINT NOT NULL,
	duration_ms BIGINT NOT NULL,
	client_ip VARCHAR(64) NOT NULL,
	created_at BIGINT NOT NULL
)`

// logCapture 在测试内收集全局 Logger 的输出，用于断言告警而不依赖控制台。
type logCapture struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (c *logCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.Write(data)
}

func (c *logCapture) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.buffer.String()
}

// captureLogs 把 GoFrame 全局 Logger 重定向到内存缓冲，测试结束后恢复原配置。
func captureLogs(t *testing.T) *logCapture {
	t.Helper()
	capture := &logCapture{}
	logger := g.Log()
	previous := logger.GetConfig()
	current := previous
	current.Writer = capture
	current.StdoutPrint = false
	if err := logger.SetConfig(current); err != nil {
		t.Fatalf("重定向测试日志失败: %v", err)
	}
	t.Cleanup(func() {
		if err := logger.SetConfig(previous); err != nil {
			t.Errorf("恢复测试日志失败: %v", err)
		}
	})
	return capture
}

// newTestDB 创建临时 SQLite 数据库；createTable 为 true 时建立 access_logs 表。
// 连接由测试 Cleanup 关闭，不使用进程级默认数据库组。
func newTestDB(t *testing.T, createTable bool) gdb.DB {
	t.Helper()
	ctx := context.Background()
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Name: filepath.Join(t.TempDir(), "accesslog.db")})
	if err != nil {
		t.Fatalf("初始化 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(ctx) })
	if createTable {
		if _, err = db.Exec(ctx, createAccessLogsSQL); err != nil {
			t.Fatalf("建立测试表失败: %v", err)
		}
	}
	return db
}

func TestTruncate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		size  int
		want  string
	}{
		{"空串", "", 512, ""},
		{"短于上限的 ASCII", "GET /health", 512, "GET /health"},
		{"正好等于上限", strings.Repeat("a", 512), 512, strings.Repeat("a", 512)},
		{"超过上限的 ASCII", strings.Repeat("a", 513), 512, strings.Repeat("a", 512)},
		{"中文字符按码点截断", strings.Repeat("中", 600), 512, strings.Repeat("中", 512)},
		{"emoji 不会被拆成半个字符", strings.Repeat("🙂", 3), 2, "🙂🙂"},
		{"零上限返回空串", "abc", 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.value, tc.size); got != tc.want {
				t.Fatalf("truncate 结果不符: got=%q want=%q", got, tc.want)
			}
		})
	}
}

// TestWriterPersistsRecord 验证写入的字段落库，且超长路径按码点截断为 512。
func TestWriterPersistsRecord(t *testing.T) {
	db := newTestDB(t, true)
	ctx := context.Background()
	writer := New(db)
	longPath := strings.Repeat("中", 600)
	writer.Write(Record{
		TraceID: "0123456789abcdef0123456789abcdef", Method: "GET", Path: longPath,
		StatusCode: 429, DurationMS: 12, ClientIP: "203.0.113.9",
	})
	writer.Close()

	record, err := db.Model("access_logs").Ctx(ctx).One()
	if err != nil {
		t.Fatalf("读取访问日志失败: %v", err)
	}
	if record.IsEmpty() {
		t.Fatal("关闭后仍未写入任何访问日志")
	}
	if record["trace_id"].String() != "0123456789abcdef0123456789abcdef" ||
		record["method"].String() != "GET" ||
		record["status_code"].Int() != 429 ||
		record["duration_ms"].Int64() != 12 ||
		record["client_ip"].String() != "203.0.113.9" {
		t.Fatalf("访问日志字段不符合写入值: %v", record)
	}
	stored := record["path"].String()
	if utf8.RuneCountInString(stored) != 512 || len(stored) != 512*3 {
		t.Fatalf("路径未按码点截断为 512: runes=%d bytes=%d", utf8.RuneCountInString(stored), len(stored))
	}
	if stored != strings.Repeat("中", 512) {
		t.Fatal("路径截断内容与预期不一致")
	}
	if record["created_at"].Int64() <= 0 {
		t.Fatalf("创建时间未写入: %v", record["created_at"])
	}
}

// TestWriterSurvivesInsertFailure 验证写入失败只记录告警，不 panic 不影响关闭。
// 用表结构缺失注入失败：实测 GoFrame SQLite 在 db.Close() 之后 Model 操作仍返回成功
// （INSERT 会静默生效），关闭连接无法作为写入失败面，因此这里不通过 Close 注入错误。
func TestWriterSurvivesInsertFailure(t *testing.T) {
	capture := captureLogs(t)
	db := newTestDB(t, false) // 不建表，模拟表结构缺失
	writer := New(db)
	writer.Write(Record{TraceID: "trace-failure", Method: "GET", Path: "/backend/v1/health", StatusCode: 200, ClientIP: "127.0.0.1"})
	writer.Close()
	if logged := capture.String(); !strings.Contains(logged, "访问日志写入失败") {
		t.Fatalf("写入失败未记录告警: %q", logged)
	}
}

// TestWriterCloseIsIdempotentAndIgnoresWrites 验证关闭幂等且关闭后丢弃新记录。
func TestWriterCloseIsIdempotentAndIgnoresWrites(t *testing.T) {
	db := newTestDB(t, true)
	ctx := context.Background()
	writer := New(db)
	writer.Write(Record{TraceID: "trace-before-close", Method: "GET", Path: "/backend/livez", StatusCode: 200, ClientIP: "127.0.0.1"})
	writer.Close()
	writer.Close() // 重复关闭不应 panic 或重复关闭 channel
	writer.Write(Record{TraceID: "trace-after-close", Method: "GET", Path: "/backend/readyz", StatusCode: 200, ClientIP: "127.0.0.1"})
	writer.Close()

	count, err := db.Model("access_logs").Ctx(ctx).Count()
	if err != nil {
		t.Fatalf("统计访问日志失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("关闭后应只保留关闭前的 1 条记录，实际 %d 条", count)
	}
	traceID, err := db.Model("access_logs").Ctx(ctx).Fields("trace_id").Value()
	if err != nil {
		t.Fatalf("读取 trace_id 失败: %v", err)
	}
	if traceID.String() != "trace-before-close" {
		t.Fatalf("关闭后写入的记录未被忽略: %s", traceID.String())
	}
}
