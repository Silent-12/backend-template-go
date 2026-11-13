package accesslog

// Record 保存访问日志允许收集的请求元数据，以值传递给异步写入器。
// 不得包含实际路径参数、完整 URL 查询、认证信息或请求响应报文。
type Record struct {
	// TraceID 是由服务端追踪上下文确定的请求编号。
	TraceID string
	// Method 是 HTTP 请求方法。
	Method string
	// Path 是已匹配路由的模板，未匹配时使用固定标记。
	Path string
	// StatusCode 是最终 HTTP 响应状态码。
	StatusCode int
	// DurationMS 是请求处理耗时，单位为毫秒。
	DurationMS int64
	// ClientIP 是经过可信代理边界解析的客户端地址。
	ClientIP string
}
