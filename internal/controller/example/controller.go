// Package example 把示例模块的业务结果适配为 v1 传输协议，不直接访问数据库或上游客户端。
package example

import (
	"context"

	v1 "backend-template-go/api/example/v1"
	"backend-template-go/internal/model"
)

// Service 定义 Controller 需要的业务能力，接口放在使用方所在包以保持最小方法集。
type Service interface {
	// ListLogs 返回访问日志总数和当页记录。
	ListLogs(ctx context.Context, page, pageSize int) (int, []model.AccessLogEntry, error)
	// ProbeUpstream 返回示例上游的成功状态码。
	ProbeUpstream(ctx context.Context) (int, error)
}

// Controller 只做协议适配，不承载业务规则，也不持有需要关闭的资源。
type Controller struct {
	service Service
}

// New 注入业务服务，服务及其依赖的生命周期由应用装配层管理。
func New(service Service) *Controller {
	return &Controller{service: service}
}

// ListLogs 查询访问日志并组装标准页码分页响应，空列表返回空数组。
func (c *Controller) ListLogs(ctx context.Context, req *v1.ListLogsReq) (*v1.ListLogsRes, error) {
	total, entries, err := c.service.ListLogs(ctx, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	list := make([]v1.LogItem, 0, len(entries))
	for _, entry := range entries {
		list = append(list, v1.LogItem{
			TraceID:    entry.TraceID,
			Method:     entry.Method,
			Path:       entry.Path,
			StatusCode: entry.StatusCode,
			DurationMS: entry.DurationMS,
			CreatedAt:  entry.CreatedAt,
		})
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + req.PageSize - 1) / req.PageSize
	}
	return &v1.ListLogsRes{
		List: list,
		Pagination: model.Pagination{
			Page:       req.Page,
			PageSize:   req.PageSize,
			Total:      total,
			TotalPages: totalPages,
		},
	}, nil
}

// ProbeUpstream 返回示例上游响应状态，失败由统一响应中间件映射为安全提示。
func (c *Controller) ProbeUpstream(ctx context.Context, _ *v1.ProbeUpstreamReq) (*v1.ProbeUpstreamRes, error) {
	status, err := c.service.ProbeUpstream(ctx)
	if err != nil {
		return nil, err
	}
	return &v1.ProbeUpstreamRes{StatusCode: status}, nil
}
