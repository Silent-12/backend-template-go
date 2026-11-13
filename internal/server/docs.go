package server

import (
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/gogf/gf/v2/errors/gcode"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
	knife4x "github.com/songxychn/knife4j-next/knife4x/go"

	"backend-template-go/internal/config"
	"backend-template-go/internal/middleware"
)

// docsCSP 仅允许同源 UI、Schema Worker 和带 nonce 的初始化脚本。
// 组件库需要内联样式；调试请求及规范加载限定同源，不允许任意脚本求值。
const docsCSP = "default-src 'none'; " +
	"script-src 'self' 'nonce-%s'; worker-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self' data:; " +
	"frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

// registerDocs 仅在非生产且启用文档时注册 GoFrame OpenAPI 和 Knife4x 内嵌 UI。
// 文档页面与资源均经过入口认证及安全中间件，不提供本地目录或静态文件托管。
func registerDocs(s *ghttp.Server, c config.Config) error {
	if !c.Docs || c.IsProduction() {
		return nil
	}
	handler, err := knife4x.NewHandler(knife4x.Config{
		SpecURL:  c.OpenAPIPath(),
		BasePath: c.BasePath(),
	}, nil)
	if err != nil {
		return gerror.WrapCode(gcode.CodeInvalidConfiguration, err, "无法初始化 Knife4x 文档")
	}
	s.SetOpenApiPath(c.OpenAPIPath())
	s.GetOpenApi().Info.Title = c.AppName + " API"
	s.GetOpenApi().Info.Version = "1.0.0"
	s.GetOpenApi().Config.CommonResponse = middleware.Response{}
	s.GetOpenApi().Config.CommonResponseDataField = "Data"
	serve := func(r *ghttp.Request) {
		serveDocs(r, handler, c.AppName+" API")
	}
	for _, route := range c.DocsUIRoutes() {
		s.BindHandler("GET:"+route, serve)
		s.BindHandler("HEAD:"+route, serve)
	}
	return nil
}

// serveDocs 将标准库 Handler 接入 GoFrame 缓冲响应，保留 MIME、状态码和资源路径。
// HTML 按完整 GET 内容生成 nonce 与服务标题，禁用分段读取和缓存，避免脚本配置被截断或复用。
// HEAD 仍生成相同内容，避免空缓冲被业务中间件包装；响应体由 HTTP 服务按协议省略。
func serveDocs(r *ghttp.Request, handler http.Handler, title string) {
	var (
		nonce   = uuid.NewString()
		request = r.Request
		isHTML  = strings.HasSuffix(r.URL.Path, ".html")
	)
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.Header().Set("Content-Security-Policy", fmt.Sprintf(docsCSP, nonce))
	if isHTML || request.Method == http.MethodHead {
		request = r.Request.Clone(r.Context())
		request.Method = http.MethodGet
	}
	if isHTML {
		request.Header.Del("Range")
		request.Header.Del("If-Range")
	}
	handler.ServeHTTP(r.Response.BufferWriter, request)
	if !isHTML || r.Response.Status != http.StatusOK {
		return
	}
	page := strings.ReplaceAll(r.Response.BufferString(), "<script", `<script nonce="`+nonce+`"`)
	page = strings.Replace(page, "<title>Knife4j Next</title>", "<title>"+html.EscapeString(title)+"</title>", 1)
	r.Response.SetBuffer([]byte(page))
	r.Response.Header().Set("Content-Length", strconv.Itoa(len(page)))
	r.Response.Header().Del("Accept-Ranges")
}
