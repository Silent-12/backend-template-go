// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

// AccessLogs is the golang structure for table access_logs.
type AccessLogs struct {
	Id         uint64 `json:"id"         orm:"id"          ` //
	TraceId    string `json:"traceId"    orm:"trace_id"    ` //
	Method     string `json:"method"     orm:"method"      ` //
	Path       string `json:"path"       orm:"path"        ` //
	StatusCode uint   `json:"statusCode" orm:"status_code" ` //
	DurationMs int64  `json:"durationMs" orm:"duration_ms" ` //
	ClientIp   string `json:"clientIp"   orm:"client_ip"   ` //
	CreatedAt  int64  `json:"createdAt"  orm:"created_at"  ` //
}
