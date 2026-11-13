package infrastructure

import (
	"context"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// txContextKey 是本包私有的上下文键类型，避免与其他包写入同名键冲突。
type txContextKey struct{}

// panicError 标记事务回调发生 panic，用于让框架回滚后由本包重新抛出原始异常。
// 其错误文本固定，不包含 panic 原文，也不会传给日志或响应。
type panicError struct{}

// Error 返回固定的回调中断提示，不携带 panic 内容。
func (*panicError) Error() string {
	return "事务回调异常中断"
}

// Transactor 使用注入连接开启事务，并把事务对象放入上下文供仓储调用复用。
// 事务边界由 Service 决定，Repository 只复用上下文中的事务，不自行开启、提交或回滚事务。
// 连接由 Resources 持有并关闭，Transactor 只借用连接；可并发复用，不保存单次事务状态。
type Transactor struct {
	db gdb.DB
}

// NewTransactor 用已启用的数据库连接构造事务执行器，供应用装配层注入到业务服务。
// db 必须由应用统一管理生命周期，传入 nil 时 Transaction 返回配置错误而不是空指针异常。
func NewTransactor(db gdb.DB) *Transactor {
	return &Transactor{db: db}
}

// Transaction 在事务中执行 fn，并把含事务的上下文传给 fn，使仓储调用自动加入该事务。
// ctx 已含事务时直接执行 fn，加入现有事务而不嵌套开启，此时提交或回滚由外层事务决定；
// 未含事务时由注入连接开启新事务并在 fn 结束后提交或回滚。
// fn 返回错误时回滚并返回该错误；fn 发生 panic 时先回滚再重新抛出原始异常，交由最外层异常中间件处理，
// 回滚本身失败时仍以原始异常为准。未启用数据库连接时返回配置错误且不执行 fn；fn 必须非 nil。
func (t *Transactor) Transaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return gerror.NewCode(gcode.CodeInvalidParameter, "事务回调不可为空")
	}
	if _, ok := TxFromContext(ctx); ok {
		return fn(ctx)
	}
	if t == nil || t.db == nil {
		return gerror.NewCode(gcode.CodeInvalidConfiguration, "数据库未启用，无法开启事务")
	}
	// exception 暂存 fn 内部的 panic 值，回滚成功后由本函数重新抛出。
	var exception any
	// callback 把框架已注入事务的上下文交给 fn，并在 panic 时返回标记错误以触发回滚。
	callback := func(ctx context.Context, tx gdb.TX) (err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				exception = recovered
				err = &panicError{}
			}
		}()
		return fn(context.WithValue(ctx, txContextKey{}, tx))
	}
	err := t.db.Transaction(ctx, callback)
	if exception != nil {
		panic(exception)
	}
	return err
}

// TxFromContext 返回 ctx 中已开启的事务，未处于事务中时返回 nil 和 false。
// 仓储用它判断是否需要事务内的锁定读；调用方不应保存返回的事务，也不要自行提交或回滚。
func TxFromContext(ctx context.Context) (gdb.TX, bool) {
	if ctx == nil {
		return nil, false
	}
	tx, ok := ctx.Value(txContextKey{}).(gdb.TX)
	if !ok || tx == nil {
		return nil, false
	}
	return tx, true
}

// DBFromContext 返回当前上下文可用的数据库连接，供仓储统一取得执行入口。
// ctx 含事务时返回绑定该上下文的连接，GoFrame 按连接分组识别上下文中的事务并复用同一 tx；
// 不含事务时返回 fallback，此时操作走注入连接的自动提交。fallback 可为 nil，调用方需自行处理。
func DBFromContext(ctx context.Context, fallback gdb.DB) gdb.DB {
	if _, ok := TxFromContext(ctx); !ok || fallback == nil {
		return fallback
	}
	return fallback.Ctx(ctx)
}
