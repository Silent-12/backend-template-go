package infrastructure

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	_ "github.com/gogf/gf/contrib/drivers/sqlite/v2"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
)

// createTxProbeSQL 在临时 SQLite 库建立事务测试表，默认测试不依赖外部数据库实例。
const createTxProbeSQL = `CREATE TABLE tx_probe (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL
)`

// newTxTestDB 创建临时 SQLite 连接并建立测试表，连接由测试结束时关闭。
// 使用独立连接而不是默认数据库组，避免测试期间引入第二份进程级连接配置。
func newTxTestDB(t *testing.T) gdb.DB {
	t.Helper()
	db, err := gdb.New(gdb.ConfigNode{Type: "sqlite", Name: filepath.Join(t.TempDir(), "tx.db")})
	if err != nil {
		t.Fatalf("初始化 SQLite 失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close(context.Background()) })
	if _, err = db.Exec(context.Background(), createTxProbeSQL); err != nil {
		t.Fatalf("建立事务测试表失败: %v", err)
	}
	return db
}

// insertProbe 通过上下文连接写入一行，用于验证写入是否真的加入上下文中的事务。
// 返回错误而不是直接断言，便于在事务回调内部比较写入结果。
func insertProbe(ctx context.Context, db gdb.DB, name string) error {
	_, err := DBFromContext(ctx, db).Exec(ctx, "INSERT INTO tx_probe(name) VALUES(?)", name)
	return err
}

// countProbe 返回事务测试表的行数，用于断言提交与回滚结果。
func countProbe(t *testing.T, db gdb.DB) int {
	t.Helper()
	value, err := db.GetValue(context.Background(), "SELECT COUNT(*) FROM tx_probe")
	if err != nil {
		t.Fatalf("统计测试数据失败: %v", err)
	}
	return value.Int()
}

// TestTransactionCommitAndRollback 验证回调正常返回时提交、
// 返回错误时回滚并原样返回该错误。
func TestTransactionCommitAndRollback(t *testing.T) {
	var (
		ctx      = context.Background()
		db       = newTxTestDB(t)
		transact = NewTransactor(db)
	)
	if err := transact.Transaction(ctx, func(ctx context.Context) error {
		if err := insertProbe(ctx, db, "committed"); err != nil {
			t.Fatalf("写入测试数据失败: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("提交事务失败: %v", err)
	}
	if got := countProbe(t, db); got != 1 {
		t.Fatalf("提交后行数为 %d，期望 1", got)
	}
	sentinel := errors.New("业务校验失败")
	if err := transact.Transaction(ctx, func(ctx context.Context) error {
		if err := insertProbe(ctx, db, "rolled-back"); err != nil {
			t.Fatalf("写入测试数据失败: %v", err)
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("回调错误未被原样返回: %v", err)
	}
	if got := countProbe(t, db); got != 1 {
		t.Fatalf("回滚后行数为 %d，期望 1", got)
	}
}

// TestTransactionRollsBackOnPanicAndRethrows 验证回调 panic 时先回滚，
// 再把原始 panic 值重新抛出，交由最外层异常中间件统一脱敏。
func TestTransactionRollsBackOnPanicAndRethrows(t *testing.T) {
	var (
		ctx      = context.Background()
		db       = newTxTestDB(t)
		transact = NewTransactor(db)
	)
	recovered := func() (result any) {
		defer func() { result = recover() }()
		_ = transact.Transaction(ctx, func(ctx context.Context) error {
			if err := insertProbe(ctx, db, "panic"); err != nil {
				t.Fatalf("写入测试数据失败: %v", err)
			}
			panic("boom")
		})
		return nil
	}()
	if recovered != "boom" {
		t.Fatalf("原始 panic 未被重新抛出，得到 %v", recovered)
	}
	if got := countProbe(t, db); got != 0 {
		t.Fatalf("panic 后行数为 %d，期望 0", got)
	}
}

// TestTransactionJoinsExistingTransaction 验证上下文已含事务时直接加入，
// 不嵌套开启新事务，且外层回滚同时丢弃内层写入。
func TestTransactionJoinsExistingTransaction(t *testing.T) {
	var (
		ctx      = context.Background()
		db       = newTxTestDB(t)
		transact = NewTransactor(db)
	)
	sentinel := errors.New("外层回滚")
	err := transact.Transaction(ctx, func(ctx context.Context) error {
		outer, ok := TxFromContext(ctx)
		if !ok {
			t.Fatal("外层事务未写入上下文")
		}
		if err := insertProbe(ctx, db, "outer"); err != nil {
			t.Fatalf("写入测试数据失败: %v", err)
		}
		if err := transact.Transaction(ctx, func(ctx context.Context) error {
			inner, ok := TxFromContext(ctx)
			if !ok {
				t.Fatal("内层未发现已开启的事务")
			}
			if inner != outer {
				t.Fatal("内层嵌套开启了新事务")
			}
			if err := insertProbe(ctx, db, "inner"); err != nil {
				t.Fatalf("写入测试数据失败: %v", err)
			}
			return nil
		}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("外层错误未被原样返回: %v", err)
	}
	if got := countProbe(t, db); got != 0 {
		t.Fatalf("外层回滚后行数为 %d，期望 0", got)
	}
}

// TestTransactionAppliesToModelQueries 验证仓储通过 DBFromContext 取得连接后，
// 直接由该连接创建 Model 也会复用上下文事务，不需要再显式绑定一次上下文。
func TestTransactionAppliesToModelQueries(t *testing.T) {
	var (
		ctx      = context.Background()
		db       = newTxTestDB(t)
		transact = NewTransactor(db)
	)
	sentinel := errors.New("回滚未提交")
	err := transact.Transaction(ctx, func(ctx context.Context) error {
		if err := insertProbe(ctx, db, "model"); err != nil {
			return err
		}
		count, err := DBFromContext(ctx, db).Model("tx_probe").Count()
		if err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("事务内 Model 查询未复用事务，可见行数为 %d", count)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("事务错误未被原样返回: %v", err)
	}
	if got := countProbe(t, db); got != 0 {
		t.Fatalf("回滚后行数为 %d，期望 0", got)
	}
}

// TestDBFromContextFallsBackToInjectedConnection 验证没有事务时返回注入连接，
// 未启用连接时返回 nil 而不是空指针连接。
func TestDBFromContextFallsBackToInjectedConnection(t *testing.T) {
	db := newTxTestDB(t)
	if conn := DBFromContext(context.Background(), db); conn != db {
		t.Fatalf("无事务时未返回注入连接: %T", conn)
	}
	if conn := DBFromContext(context.Background(), nil); conn != nil {
		t.Fatalf("未启用连接时返回了非 nil 连接: %T", conn)
	}
}

// TestTransactionRejectsInvalidInput 验证未注入连接或回调为空时返回明确配置/参数错误，
// 且不会执行回调。
func TestTransactionRejectsInvalidInput(t *testing.T) {
	executed := false
	callback := func(context.Context) error {
		executed = true
		return nil
	}
	err := NewTransactor(nil).Transaction(context.Background(), callback)
	if err == nil || executed {
		t.Fatalf("未启用数据库时仍执行了回调: %v", err)
	}
	if code := gerror.Code(err).Code(); code != gcode.CodeInvalidConfiguration.Code() {
		t.Fatalf("未启用数据库返回错误码 %d，期望配置错误", code)
	}
	err = NewTransactor(nil).Transaction(context.Background(), nil)
	if err == nil || gerror.Code(err).Code() != gcode.CodeInvalidParameter.Code() {
		t.Fatalf("空回调未被拒绝: %v", err)
	}
	if executed {
		t.Fatal("空回调场景改动了回调状态")
	}
}
