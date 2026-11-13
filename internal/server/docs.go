package server

import (
	"encoding/json"
	"fmt"
	"html"

	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/google/uuid"
)

// docsCSP 是文档页面使用的 Content-Security-Policy，%s 由每次请求生成的脚本 nonce 填充。
// 脚本只允许 unpkg 分发的 Swagger UI 与带 nonce 的内联初始化代码，页面不加载其他外部资源。
const docsCSP = "default-src 'none'; " +
	"script-src https://unpkg.com 'nonce-%s'; " +
	"style-src https://unpkg.com 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self' data:; " +
	"frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

// writeDocs 输出使用 specURL 加载 OpenAPI 的在线调试页面，并按 HTML 文本转义 title。
// 页面禁用缓存、外部规范校验和授权持久化，使用每次生成的 nonce 限制内联脚本。
// specURL 应为应用生成的同源路径；页面不经过统一业务 JSON 包装。
func writeDocs(r *ghttp.Request, specURL, title string) {
	nonce := uuid.NewString()
	spec, _ := json.Marshal(specURL)
	r.Response.Header().Set("Cache-Control", "no-store")
	r.Response.Header().Set("Content-Security-Policy", fmt.Sprintf(docsCSP, nonce))
	r.Response.Write(fmt.Sprintf(docsHTML, html.EscapeString(title), nonce, spec))
}

const docsHTML = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>%s</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.34.1/swagger-ui.css"></head>
<body><div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5.34.1/swagger-ui-bundle.js"></script>
<script nonce="%s">SwaggerUIBundle({url:%s,dom_id:"#swagger-ui",deepLinking:true,persistAuthorization:false,validatorUrl:null,queryConfigEnabled:false});</script>
</body></html>`
