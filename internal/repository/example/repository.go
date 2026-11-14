// Package example 提供访问日志的只读持久化示例，演示注入连接、上下文事务和生成列元数据的用法。
package example

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"

	"backend-template-go/internal/dao"
	"backend-template-go/internal/infrastructure"
	"backend-template-go/internal/model"
)

// Repository 复用注入的数据库连接读取访问日志，不自行开启、提交或回滚事务。
// db 由应用装配层注入并统一管理生命周期；nil 表示数据库未启用。
type Repository struct {
	db gdb.DB
}

// New 创建访问日志仓储，db 由调用方注入并负责关闭。
func New(db gdb.DB) *Repository {
	return &Repository{db: db}
}

// CountAndPage 在同一上下文连接上统计总数并读取一页记录，按创建时间倒序。
// ctx 含事务时通过 DBFromContext 复用事务连接；page 从 1 开始，pageSize 为正数。
// 数据库未启用时返回明确的功能不可用错误，不发起查询；列名只取自生成的列元数据。
func (r *Repository) CountAndPage(ctx context.Context, page, pageSize int) (int, []model.AccessLogEntry, error) {
	db := infrastructure.DBFromContext(ctx, r.db)
	if db == nil {
		return 0, nil, gerror.NewCode(gcode.CodeServerBusy, "访问日志查询需要启用 MySQL")
	}
	columns := dao.AccessLogs.Columns()
	table := dao.AccessLogs.Table()
	// Count 与 Scan 分别构造 Model：Fields 会参与 COUNT 的列列表，且链式方法共享模型状态。
	total, err := db.Model(table).Ctx(ctx).Count()
	if err != nil {
		return 0, nil, gerror.WrapCode(gcode.CodeInternalError, err, "统计访问日志失败")
	}
	entries := make([]model.AccessLogEntry, 0, pageSize)
	err = db.Model(table).Ctx(ctx).Fields(
		columns.TraceId, columns.Method, columns.Path, columns.StatusCode, columns.DurationMs, columns.CreatedAt,
	).OrderDesc(columns.CreatedAt).Page(page, pageSize).Scan(&entries)
	if err != nil {
		return 0, nil, gerror.WrapCode(gcode.CodeInternalError, err, "读取访问日志失败")
	}
	return total, entries, nil
}
