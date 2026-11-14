package model

// AccessLogEntry 是访问日志在仓储、业务服务和传输层之间复用的只读投影。
// 刻意不包含自增主键、客户端 IP 与审计字段，避免持久化结构直接流向接口。
type AccessLogEntry struct {
	// TraceID 是服务端生成的请求追踪编号。
	TraceID string `orm:"trace_id"`
	// Method 是 HTTP 请求方法。
	Method string `orm:"method"`
	// Path 是匹配到的路由模板。
	Path string `orm:"path"`
	// StatusCode 是响应状态码。
	StatusCode int `orm:"status_code"`
	// DurationMS 是请求处理耗时。
	DurationMS int64 `orm:"duration_ms"`
	// CreatedAt 是记录写入时间，Unix 毫秒。
	CreatedAt int64 `orm:"created_at"`
}
