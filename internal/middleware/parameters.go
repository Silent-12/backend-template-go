package middleware

import (
	"encoding/json"
	"math"
	"reflect"
	"strconv"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/util/gutil"
)

// ParameterTypes 在标准路由转换参数前检查原始标量类型，防止隐式转换掩盖非法输入。
// 跳过 header、cookie、未传入或 nil 的值；类型不匹配时设置安全参数错误并停止后续处理。
// 范围、长度和枚举继续由 GoFrame 标签校验，非标准路由直接放行。
func ParameterTypes(r *ghttp.Request) {
	handler := r.GetServeHandler()
	if handler == nil || !handler.Handler.Info.IsStrictRoute {
		r.Middleware.Next()
		return
	}
	data := r.GetRequestMap()
	for _, field := range handler.Handler.Info.ReqStructFields {
		if field.TagIn() == "header" || field.TagIn() == "cookie" {
			continue
		}
		key, value := gutil.MapPossibleItemByKey(data, field.TagPriorityName())
		if key == "" || value == nil {
			continue
		}
		var target reflect.Type = field.Type()
		for target.Kind() == reflect.Pointer {
			target = target.Elem()
		}
		if !validScalar(value, target) {
			r.SetError(gerror.NewCode(gcode.CodeInvalidParameter, "请求参数类型不正确"))
			return
		}
	}
	r.Middleware.Next()
}

// validScalar 按目标标量种类和位宽检查原始值是否可安全转换。
// 拒绝字符串被对象或数字冒充、整数溢出及非有限浮点数；非标量类型交给后续框架处理。
// target 必须非 nil，指针应由调用方解引用至实际字段类型。
func validScalar(value any, target reflect.Type) bool {
	switch target.Kind() {
	case reflect.String:
		_, ok := value.(string)
		return ok
	case reflect.Bool:
		switch raw := value.(type) {
		case bool:
			return true
		case string:
			_, err := strconv.ParseBool(raw)
			return err == nil
		default:
			return false
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		raw, ok := numericText(value, false)
		_, err := strconv.ParseInt(raw, 10, target.Bits())
		return ok && err == nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		raw, ok := numericText(value, false)
		_, err := strconv.ParseUint(raw, 10, target.Bits())
		return ok && err == nil
	case reflect.Float32, reflect.Float64:
		raw, ok := numericText(value, true)
		number, err := strconv.ParseFloat(raw, target.Bits())
		return ok && err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return true
	}
}

// numericText 将字符串、JSON 数字或 Go 数值转换为待解析的数值文本。
// allowFloat 为 false 时不接受 Go 浮点值；不支持的类型返回空字符串和 false。
// 本函数不判断文本范围或有限性，这些约束由 validScalar 按目标位宽检查。
func numericText(value any, allowFloat bool) (string, bool) {
	switch raw := value.(type) {
	case string:
		return raw, true
	case json.Number:
		return raw.String(), true
	}
	raw := reflect.ValueOf(value)
	switch raw.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(raw.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(raw.Uint(), 10), true
	case reflect.Float32, reflect.Float64:
		if allowFloat {
			return strconv.FormatFloat(raw.Float(), 'g', -1, raw.Type().Bits()), true
		}
	}
	return "", false
}
