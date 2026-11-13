// Package cmd 管理应用命令、启动流程和停止信号。
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gcmd"

	"backend-template-go/internal/config"
	"backend-template-go/internal/server"
)

// Main 是后端服务的默认命令入口。
// 按配置初始化资源并开始监听，收到上下文取消或停止信号后按依赖顺序关闭应用。
// 配置、资源初始化和监听失败时返回错误，不继续执行后续启动步骤。
// 命令名在包初始化阶段取自固定模板名，派生服务应同步修改或改由 main 覆盖。
var Main = gcmd.Command{
	Name:  "backend-template-go",
	Usage: "backend-template-go",
	Brief: "GoFrame 后端基础模板",
	Func: func(ctx context.Context, _ *gcmd.Parser) error {
		c, err := config.Load()
		if err != nil {
			return err
		}
		app, err := server.New(ctx, c)
		if err != nil {
			return err
		}
		defer app.Close()
		stop, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err = app.Server.Start(); err != nil {
			return err
		}
		g.Log().Infof(ctx, "服务已启动: app=%s env=%s http://localhost:%d%s", c.AppName, c.Env, c.Port, c.BasePath())
		if c.Docs && !c.IsProduction() {
			g.Log().Infof(ctx, "接口文档: http://localhost:%d%s", c.Port, c.DocsPath())
		}
		// 停止信号只触发关闭流程，实际释放顺序由 App.Close 保证。
		<-stop.Done()
		g.Log().Info(ctx, "收到停止信号，开始按依赖顺序关闭")
		return nil
	},
}
