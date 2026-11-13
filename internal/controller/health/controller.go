package health

import (
	"time"

	"backend-template-go/internal/infrastructure"
)

// Controller 根据运行环境提供存活状态或运行时诊断，不承载业务规则。
// 必须通过 New 创建，以初始化启动基准和依赖资源；借用资源，不负责关闭连接。
type Controller struct {
	resources  *infrastructure.Resources
	production bool
	started    time.Time
}
