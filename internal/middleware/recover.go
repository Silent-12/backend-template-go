package middleware

import (
	"net/http"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// Recover 捕获处理链中的 panic，返回安全的 500 业务响应。
// 必须注册为最外层中间件：GoFrame 会把中间件和处理器内的 panic 转换为请求错误并直接写入响应缓冲，
// 因此除兜底的 recover 外，还要在 Next 返回后检查统一响应中间件是否已接管；
// 未接管说明 panic 发生在其之前，必须丢弃缓冲内容并以安全 JSON 响应。
// 日志只记录追踪号、错误码和错误类型，绝不输出 panic 原文与堆栈，避免凭据与内部结构泄露；
// 响应已提交（下载、SSE 或协议处理器）时不再改写。装配上应始终与 HandlerResponse 同时使用。
func Recover() ghttp.HandlerFunc {
	return func(r *ghttp.Request) {
		defer func() {
			if exception := recover(); exception != nil {
				logAbnormal(r, gcode.CodeInternalPanic.Code(), exception)
				writeSafeInternalError(r)
			}
		}()
		r.Middleware.Next()
		// 统一响应中间件已接管时，错误响应已经是安全 JSON，这里不再重复改写。
		if err := r.GetError(); err != nil && !r.GetCtxVar(responseHandledKey{}).Bool() {
			logAbnormal(r, gerror.Code(err).Code(), err)
			writeSafeInternalError(r)
		}
	}
}

// logAbnormal 记录请求异常的可安全关联信息，异常值只以类型输出，不展开其内容。
func logAbnormal(r *ghttp.Request, code int, cause any) {
	g.Log().Errorf(r.Context(), "请求处理异常中断: traceId=%s code=%d errorType=%T", r.Response.Header().Get("X-Trace-Id"), code, cause)
}

// writeSafeInternalError 在响应尚未提交时写入固定的 500 业务响应。
// 已写入网络、已提交头部或被劫持的响应无法安全改写，保持原样避免破坏流式协议。
func writeSafeInternalError(r *ghttp.Request) {
	if r.Response.BytesWritten() > 0 || r.Response.IsHeaderWrote() || r.Response.IsHijacked() {
		return
	}
	JSON(r, http.StatusInternalServerError, "服务器内部错误", nil)
}
