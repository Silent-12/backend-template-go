// Package model 定义跨传输层和业务服务复用的公共模型。
package model

// Pagination 描述页码分页位置、页容量和完整匹配范围的统计信息。
// 总页数按总条数除以正页容量向上取整，无记录时为零；本类型不自行校验或计算字段。
type Pagination struct {
	// Page 是从 1 开始的当前页码。
	Page int `json:"page" dc:"当前页码，从 1 开始"`
	// PageSize 是请求使用的每页条数。
	PageSize int `json:"pageSize" dc:"每页条数，1 到 100"`
	// Total 是筛选条件匹配的总记录数。
	Total int `json:"total" dc:"匹配条件的总记录数"`
	// TotalPages 是按总条数和页容量向上取整后的总页数。
	TotalPages int `json:"totalPages" dc:"总页数，按总数与页容量向上取整，无记录时为 0"`
}
