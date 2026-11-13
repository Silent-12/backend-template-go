package middleware

// Response 定义标准业务 JSON 响应外层，Code 必须与实际 HTTP 状态一致。
// 由 JSON 或 HandlerResponse 填充服务端时间和追踪信息，特殊协议响应不强制使用本结构。
type Response struct {
	// Code 是与实际响应一致的 HTTP 状态码。
	Code int `json:"code" dc:"HTTP 状态码"`
	// Message 是安全的中文结果提示，不包含底层异常文本。
	Message string `json:"message" dc:"结果提示"`
	// Data 是接口业务数据，写操作成功或失败时通常为 nil，编码为 null。
	Data any `json:"data" dc:"业务数据"`
	// Timestamp 是生成响应时的 Unix 毫秒时间戳。
	Timestamp int64 `json:"timestamp" dc:"Unix 毫秒时间戳"`
	// TraceID 是与响应追踪头一致的服务端请求编号。
	TraceID string `json:"traceId" dc:"请求追踪编号"`
}
