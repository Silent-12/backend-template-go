package httpclient

import (
	"sync"

	"github.com/go-resty/resty/v2"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// Registry 持有本进程创建的上游 HTTP 客户端，由 App 持有并在退出时统一关闭空闲连接。
// 业务代码通过 Registry 取得客户端，不自行创建客户端，也不自行调用 CloseIdleConnections；
// 创建与关闭互斥且可并发调用，Close 之后 New 返回明确的配置错误。
type Registry struct {
	mu      sync.Mutex
	clients []*resty.Client
	closed  bool
}

// NewRegistry 创建空的上游客户端注册表，由应用装配层持有，退出时调用 Close。
func NewRegistry() *Registry {
	return &Registry{}
}

// New 按给定配置创建上游客户端并登记，参数语义与 New 相同。
// 注册表已关闭或客户端创建失败时返回配置错误，且不会登记；可并发调用，
// 但同一上游应在启动阶段创建一次并复用，避免运行期反复新建连接池。
func (r *Registry) New(options Options) (*resty.Client, error) {
	if r == nil {
		return nil, gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP 客户端注册表未初始化")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, gerror.NewCode(gcode.CodeInvalidConfiguration, "外部 HTTP 客户端注册表已关闭，不能再创建客户端")
	}
	client, err := New(options)
	if err != nil {
		return nil, err
	}
	r.clients = append(r.clients, client)
	return client, nil
}

// Close 关闭全部已登记客户端的空闲连接，并拒绝后续创建，并发和重复调用安全。
// 关闭只释放空闲连接，不中断正在执行的请求，调用方应先停止发起新请求；
// 已登记客户端清空后不再重复关闭，处理中的错误由调用方按普通请求错误处理。
func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for _, client := range r.clients {
		client.GetClient().CloseIdleConnections()
	}
	r.clients = nil
}
