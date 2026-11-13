// Package middleware 实现统一 HTTP 基础设施。
package middleware

import (
	"errors"
	"mime"
	"net/http"
	"reflect"
	"time"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
)

// responseHandledKey 标记统一响应中间件已接管当前请求的错误处理。
// 最外层 Recover 依据它判断响应是否已经安全改写，避免覆盖这里写入的状态码与提示。
type responseHandledKey struct{}

// JSON 清空响应缓冲并写入统一业务 JSON，使实际 HTTP 状态与 code 一致。
// message 和 data 必须由调用方提供安全内容，追踪号取自响应头；已提交响应不得调用此函数改写。
func JSON(r *ghttp.Request, status int, message string, data any) {
	r.Response.ClearBuffer()
	r.Response.WriteHeader(status)
	r.Response.WriteJson(Response{Code: status, Message: message, Data: data, Timestamp: time.Now().UnixMilli(), TraceID: r.Response.Header().Get("X-Trace-Id")})
}

// HandlerResponse 在后续处理器结束后包装标准业务结果，并将未处理错误映射为安全响应。
// 已提交、流式、下载和显式输出的协议响应按其状态保留，未提交错误优先转换为安全 JSON。
// 服务端错误仅记录追踪号、错误码和错误类型；空响应结构及 typed nil 统一输出 data: null。
// 规范化完成后才标记响应已接管，使最外层 Recover 能覆盖本中间件自身发生的异常。
func HandlerResponse(r *ghttp.Request) {
	r.Middleware.Next()
	handleResponse(r)
	r.SetCtxVar(responseHandledKey{}, true)
}

// handleResponse 按响应现状写入统一业务 JSON 或保留协议响应，不负责记录接管标记。
func handleResponse(r *ghttp.Request) {
	// 已提交的流或协议响应无法再安全改写。
	if r.Response.BytesWritten() > 0 || r.Response.IsHeaderWrote() || r.Response.IsHijacked() {
		return
	}
	if err := r.GetError(); err != nil {
		status, message := statusForError(err)
		if status >= 500 {
			// 底层 SQL / URL / panic 文本可能包含凭据，通用层只记录可安全关联的分类。
			g.Log().Errorf(r.Context(), "请求处理失败: traceId=%s code=%d errorType=%T", r.Response.Header().Get("X-Trace-Id"), gerror.Code(err).Code(), gerror.Cause(err))
		}
		// 错误发生于流或下载初始化阶段时，先清理协议头再返回安全 JSON。
		r.Response.Header().Del("Content-Disposition")
		r.Response.Header().Del("Content-Length")
		JSON(r, status, message, nil)
		return
	}
	// 显式输出的探针、Webhook 等协议内容保持原样；异常仍由上方统一脱敏。
	if r.Response.BufferLength() > 0 {
		return
	}
	status := r.Response.Status
	if status >= http.StatusBadRequest {
		JSON(r, status, errorMessage(status), nil)
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Response.Header().Get("Content-Type"))
	// 已提交的流或协议响应无法再安全改写。
	if isRawProtocolResponse(r, status, mediaType) {
		return
	}
	if r.GetServeHandler() == nil {
		JSON(r, http.StatusNotFound, "请求的接口不存在", nil)
		return
	}
	data := r.GetHandlerResponse()
	// 标准路由的空响应结构和 typed nil 都应序列化为 null。
	if data != nil {
		value := reflect.ValueOf(data)
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				data = nil
			} else if value.Elem().Kind() == reflect.Struct && value.Elem().NumField() == 0 {
				data = nil
			}
		}
	}
	message := successMessage(r.Method)
	if status == 0 {
		status = http.StatusOK
	}
	JSON(r, status, message, data)
}

// isRawProtocolResponse 判断响应是否为按协议自行输出的内容，这类响应不能再包装成业务 JSON。
// status 为当前响应状态，mediaType 为已解析的 Content-Type；空状态和普通 JSON 响应返回 false。
func isRawProtocolResponse(r *ghttp.Request, status int, mediaType string) bool {
	if status == http.StatusNoContent || status == http.StatusNotModified {
		return true
	}
	if status >= 300 && status < 400 {
		return true
	}
	if r.Response.Header().Get("Content-Disposition") != "" {
		return true
	}
	switch mediaType {
	case "text/event-stream", "application/octet-stream", "multipart/x-mixed-replace":
		return true
	}
	return false
}

// successMessage 按请求方法给出固定的成功提示，未列出的方法返回查询成功。
func successMessage(method string) string {
	switch method {
	case http.MethodPost:
		return "创建成功"
	case http.MethodPut, http.MethodPatch:
		return "更新成功"
	case http.MethodDelete:
		return "删除成功"
	default:
		return "查询成功"
	}
}

// statusForError 将错误分类映射为 HTTP 状态和固定的中文提示，不暴露错误明细。
// 优先识别请求体超限，再按 GoFrame 错误码分类；未实现与非法操作、非法配置分别映射为 501 与 500，
// 未知错误按 500 处理，err 应为非 nil。
func statusForError(err error) (int, string) {
	var bodyLimit *http.MaxBytesError
	if errors.As(err, &bodyLimit) {
		return http.StatusRequestEntityTooLarge, "请求体过大"
	}
	// Code 的 Detail 可能包含 map / slice，比较数值以避免接口值比较导致 panic。
	switch gerror.Code(err).Code() {
	case gcode.CodeValidationFailed.Code(), gcode.CodeInvalidParameter.Code(), gcode.CodeMissingParameter.Code(), gcode.CodeInvalidRequest.Code(), gcode.CodeBusinessValidationFailed.Code():
		return http.StatusBadRequest, "请求参数不合法"
	case gcode.CodeNotAuthorized.Code():
		return http.StatusUnauthorized, "身份认证失败"
	case gcode.CodeSecurityReason.Code():
		return http.StatusForbidden, "无权访问"
	case gcode.CodeNotFound.Code():
		return http.StatusNotFound, "请求的资源不存在"
	case gcode.CodeNotImplemented.Code():
		return http.StatusNotImplemented, "功能尚未实现"
	case gcode.CodeServerBusy.Code():
		return http.StatusServiceUnavailable, "服务暂时不可用"
	case gcode.CodeInvalidOperation.Code(), gcode.CodeInvalidConfiguration.Code():
		return http.StatusInternalServerError, "服务器内部错误"
	default:
		return http.StatusInternalServerError, "服务器内部错误"
	}
}

// errorMessage 为 HTTP 错误状态生成固定的中文提示，不使用底层异常文本。
// 已知状态返回对应提示，未列出的 5xx 返回服务端错误提示，其余返回通用请求失败提示。
func errorMessage(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "请求参数不合法"
	case http.StatusUnauthorized:
		return "身份认证失败"
	case http.StatusForbidden:
		return "无权访问"
	case http.StatusNotFound:
		return "请求的资源不存在"
	case http.StatusConflict:
		return "请求与当前资源状态冲突"
	case http.StatusRequestEntityTooLarge:
		return "请求体过大"
	case http.StatusTooManyRequests:
		return "当前操作频率过快，请稍后再试"
	case http.StatusNotImplemented:
		return "功能尚未实现"
	case http.StatusServiceUnavailable:
		return "服务暂时不可用"
	default:
		if status >= 500 {
			return "服务器内部错误"
		}
		return "请求处理失败"
	}
}
