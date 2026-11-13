// Package accesslog 使用有界队列异步记录元数据，不收集凭据、查询参数和请求响应体。
package accesslog

import (
	"context"
	"time"

	"github.com/gogf/gf/v2/database/gdb"

	"backend-template-go/internal/dao"
	"backend-template-go/internal/loglimit"
	"backend-template-go/internal/model/do"
)

// New 使用已启用的数据库连接创建日志写入器并启动单个消费协程。
// db 必须非 nil，连接仍由调用方管理；调用方应在停止 HTTP 服务后调用 Close 等待协程退出。
func New(db gdb.DB) *Writer {
	w := &Writer{db: db, queue: make(chan Record, 256)}
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.wg.Add(1)
	go w.run()
	return w
}

// Write 尝试将请求元数据加入有界队列，不等待数据库写入完成。
// 队列满时丢弃当前记录并告警，关闭后忽略记录；可与其他 Write 和 Close 并发调用。
func (w *Writer) Write(record Record) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return
	}
	select {
	case w.queue <- record:
	default:
		// 故障期每条请求都可能命中该分支，按时间窗口合并告警避免日志洪泛。
		loglimit.Warn(context.Background(), "accesslog-queue-full", "访问日志队列已满，丢弃记录")
	}
}

// run 顺序消费日志队列，每次数据库写入使用两秒超时。
// 队列关闭后继续处理已入队记录；后台上下文取消时放弃剩余记录，退出时通知 Close。
func (w *Writer) run() {
	defer w.wg.Done()
	for record := range w.queue {
		if w.ctx.Err() != nil {
			return
		}
		ctx, cancel := context.WithTimeout(w.ctx, 2*time.Second)
		_, err := w.db.Model(dao.AccessLogs.Table()).Ctx(ctx).
			SoftTime(gdb.SoftTimeOption{SoftTimeType: gdb.SoftTimeTypeTimestampMilli}).Data(do.AccessLogs{
			TraceId: record.TraceID, Method: record.Method, Path: truncate(record.Path, 512),
			StatusCode: record.StatusCode, DurationMs: record.DurationMS, ClientIp: record.ClientIP,
		}).Insert()
		cancel()
		if err != nil {
			// 数据库持续不可用时每条记录都会失败，按窗口合并告警避免日志洪泛。
			loglimit.Warn(context.Background(), "accesslog-write-failed", "访问日志写入失败，请检查表结构和数据库状态")
		}
	}
}

// Close 停止接受记录并等待消费协程退出，调用前应先停止 HTTP 服务。
// 等待五秒后取消后台写入，未完成的日志可能丢弃；返回前仍等待协程响应取消。
// 重复或并发调用不会重复关闭队列，数据库连接由资源持有者另行释放。
func (w *Writer) Close() {
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	timer := time.AfterFunc(5*time.Second, w.cancel)
	defer timer.Stop()
	defer w.cancel()
	w.wg.Wait()
}

// truncate 将字符串限制为最多 size 个 Unicode 码点，保留完整的中文字符。
// size 由调用方提供非负值；未超过上限时返回原字符串。
func truncate(value string, size int) string {
	runes := []rune(value)
	if len(runes) > size {
		return string(runes[:size])
	}
	return value
}
