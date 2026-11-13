package httpclient

// safeLogger 适配 Resty 日志接口，只输出固定安全提示，不格式化第三方诊断信息。
// 零值即可使用且不保存请求数据，避免 URL 查询、认证头或报文进入应用日志。
type safeLogger struct{}
