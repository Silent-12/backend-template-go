// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
)

// AccessLogs is the golang structure of table access_logs for DAO operations like Where/Data.
type AccessLogs struct {
	g.Meta     `orm:"table:access_logs, do:true"`
	Id         any //
	TraceId    any //
	Method     any //
	Path       any //
	StatusCode any //
	DurationMs any //
	ClientIp   any //
	CreatedAt  any //
}
