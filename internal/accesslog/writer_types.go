package accesslog

import (
	"context"
	"sync"

	"github.com/gogf/gf/v2/database/gdb"
)

// Writer 使用有界队列和单个后台协程隔离数据库写入与 HTTP 请求生命周期。
// 必须通过 New 创建，零值不可使用；可并发调用 Write 和 Close，关闭后忽略新记录。
// 消费协程由 Close 等待退出，数据库连接仍由应用资源管理器释放，实例不得复制。
type Writer struct {
	db     gdb.DB
	queue  chan Record
	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
}
