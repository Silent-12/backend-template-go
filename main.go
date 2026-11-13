// Package main 启动 GoFrame 后端模板服务。
package main

import (
	"github.com/gogf/gf/v2/os/gctx"

	"backend-template-go/internal/cmd"
)

// main 使用 GoFrame 初始化上下文执行默认服务命令。
// 配置加载、监听和停止信号处理由 cmd.Main 负责。
func main() {
	cmd.Main.Run(gctx.GetInitCtx())
}
