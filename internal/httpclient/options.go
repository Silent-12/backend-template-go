package httpclient

import (
	"time"
)

// Options 定义一个上游 HTTP 客户端的启动配置，零值使用有限超时和响应大小默认值。
// 基础地址来自可信配置；客户端构造后应完成配置，再开始并发请求。
type Options struct {
	// BaseURL 是可信配置中的 HTTP/HTTPS 基础地址，禁止包含凭据、查询或片段；空值不设置基础地址。
	BaseURL string
	// Timeout 是单次请求超时，零值使用 DefaultTimeout（十秒），负值被拒绝。
	Timeout time.Duration
	// ResponseBodyLimit 是自动读取的响应体字节数上限，零值使用 DefaultResponseBodyLimit（2 MiB），负值被拒绝。
	ResponseBodyLimit int
}
