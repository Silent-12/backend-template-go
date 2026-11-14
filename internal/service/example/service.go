// Package example 实现示例模块的业务规则与事务边界，演示四层纵切中 Service 的职责。
package example

import (
	"context"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"backend-template-go/internal/infrastructure"
	"backend-template-go/internal/model"
)

// upstreamCacheKey 是示例上游探测结果的缓存业务键，由 Cache 自动附加服务命名空间。
const upstreamCacheKey = "example:upstream:status"

// upstreamCacheTTL 限制缓存结果的有效期，避免上游状态长期不刷新。
const upstreamCacheTTL = 5 * time.Second

// LogRepository 是 Service 需要的只读持久化能力，接口定义在使用方所在包以保持最小方法集。
type LogRepository interface {
	// CountAndPage 在同一上下文连接上返回总数和一页记录。
	CountAndPage(ctx context.Context, page, pageSize int) (int, []model.AccessLogEntry, error)
}

// Options 定义示例服务的全部依赖，由应用装配层注入并统一管理生命周期。
// UpstreamEnabled 为 false 时上游探测返回明确的功能未实现错误，客户端仍由注册表登记和关闭。
type Options struct {
	// Logs 是访问日志仓储。
	Logs LogRepository
	// Transactor 拥有事务边界，仓储只复用回调内的 ctx。
	Transactor *infrastructure.Transactor
	// Cache 是可选加速缓存，不可用时降级为直接查询或请求。
	Cache *infrastructure.Cache
	// Upstream 是通过应用注册表创建的 Resty 客户端。
	Upstream *resty.Client
	// UpstreamEnabled 表示已配置可信的上游来源。
	UpstreamEnabled bool
	// DatabaseEnabled 表示已启用数据库连接，未启用时分页查询直接返回依赖不可用。
	DatabaseEnabled bool
}

// Service 组合事务、缓存、出站客户端和仓储，自身不持有连接的关闭责任。
type Service struct {
	options Options
}

// New 使用已装配的依赖创建示例服务，调用方不得在构造后替换其中的连接。
func New(options Options) *Service {
	return &Service{options: options}
}

// ListLogs 由 Service 拥有事务边界，使总数与当页记录来自同一数据库快照。
// 事务回调内只执行数据库操作，不发起 Redis 或外部 HTTP 调用。
func (s *Service) ListLogs(ctx context.Context, page, pageSize int) (int, []model.AccessLogEntry, error) {
	if !s.options.DatabaseEnabled {
		return 0, nil, gerror.NewCode(gcode.CodeServerBusy, "访问日志查询需要启用 MySQL")
	}
	var (
		total   int
		entries []model.AccessLogEntry
	)
	err := s.options.Transactor.Transaction(ctx, func(txCtx context.Context) error {
		var queryErr error
		total, entries, queryErr = s.options.Logs.CountAndPage(txCtx, page, pageSize)
		return queryErr
	})
	if err != nil {
		return 0, nil, err
	}
	return total, entries, nil
}

// ProbeUpstream 请求可信配置中的示例上游来源，成功状态码在短窗口内复用缓存。
// 同时处理网络错误与非 2xx 响应；未配置来源时返回功能未实现错误，不接受调用方传入地址。
// 缓存读写在数据库事务之外执行，失败或被禁用时降级为直接请求，不影响探测结果。
func (s *Service) ProbeUpstream(ctx context.Context) (int, error) {
	if !s.options.UpstreamEnabled || s.options.Upstream == nil {
		return 0, gerror.NewCode(gcode.CodeNotImplemented, "示例上游未配置")
	}
	if s.options.Cache != nil {
		if cached, found, cacheErr := s.options.Cache.Get(ctx, upstreamCacheKey); cacheErr == nil && found {
			if status, convErr := strconv.Atoi(cached); convErr == nil {
				return status, nil
			}
		}
	}
	response, err := s.options.Upstream.R().SetContext(ctx).Get("/")
	if err != nil {
		return 0, gerror.WrapCode(gcode.CodeServerBusy, err, "示例上游请求失败")
	}
	if !response.IsSuccess() {
		return response.StatusCode(), gerror.NewCode(gcode.CodeServerBusy, "示例上游响应状态异常")
	}
	if s.options.Cache != nil {
		_ = s.options.Cache.Set(ctx, upstreamCacheKey, strconv.Itoa(response.StatusCode()), upstreamCacheTTL)
	}
	return response.StatusCode(), nil
}
