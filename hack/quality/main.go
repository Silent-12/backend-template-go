// Package main 检查模板约定，防止后续服务误用外部 HTTP 客户端、绕过仓储直连数据库或使用 map 写入 ORM。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// main 只读扫描仓库中的 Go 文件并输出模板约定问题。
// 隐藏目录、构建产物和测试数据目录不参与扫描；解析或约定检查失败时以非零状态退出。
func main() {
	failed := false
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != "." && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "bin" || entry.Name() == "logs" || entry.Name() == "vendor" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		issues, err := checkFile(path)
		if err != nil {
			return err
		}
		for _, issue := range issues {
			fmt.Fprintln(os.Stderr, issue)
			failed = true
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}

// checkFile 解析指定 Go 文件并返回模板约定问题，不修改文件。
// 读取或语法解析失败时返回错误；路径会统一为斜杠形式供约定匹配。
func checkFile(path string) ([]string, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	return checkAST(files, file, filepath.ToSlash(path)), nil
}

// checkAST 根据当前文件的 AST 和导入来源检查 HTTP 客户端、数据库入口、ORM 写入及目录约定。
// files 必须对应 file 的位置集合，path 使用仓库相对路径；返回包含位置的问题列表。
// 检查只使用当前文件可确定的信息，无法替代跨文件的类型分析和代码审查。
func checkAST(files *token.FileSet, file *ast.File, path string) []string {
	var issues []string
	report := func(node ast.Node, message string) {
		issues = append(issues, fmt.Sprintf("%s: %s", files.Position(node.Pos()), message))
	}
	imports := map[string]string{}
	for _, item := range file.Imports {
		name, err := strconv.Unquote(item.Path.Value)
		if err != nil {
			continue
		}
		alias := filepath.Base(name)
		// 带 /v2 的模块路径与实际包名不同，不能把 Resty 的默认导入识别为 v2。
		if name == "github.com/go-resty/resty/v2" {
			alias = "resty"
		}
		if item.Name != nil {
			alias = item.Name.Name
		}
		if alias == "." {
			report(item, "禁止点导入，以免隐藏 HTTP 客户端和其他依赖的来源")
		}
		imports[alias] = name
	}
	if strings.Contains(path, "/logic/") {
		report(file, "业务实现必须位于 internal/service，不能新增 logic 目录")
	}
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if entry := databaseEntryName(call, imports); entry != "" && !allowsDatabaseAccess(path) {
				report(call, "禁止在 internal/dao、internal/repository、internal/accesslog 之外直接使用数据库入口 "+entry+"；请通过 infrastructure.Resources 注入连接并在 internal/repository 中访问数据，业务代码只允许使用 dao 的 Table / Columns / Group 元数据")
			}
			if method, ok := call.Fun.(*ast.SelectorExpr); ok && method.Sel.Name == "Data" && ormReceiver(method.X, imports, make(map[ast.Node]bool)) {
				for _, argument := range call.Args {
					literal, ok := argument.(*ast.CompositeLit)
					if !ok {
						continue
					}
					if _, ok = literal.Type.(*ast.MapType); ok {
						report(argument, "ORM 写入必须使用生成的 DO 对象")
					}
					if kind, ok := literal.Type.(*ast.SelectorExpr); ok && kind.Sel.Name == "Map" {
						if alias, ok := kind.X.(*ast.Ident); ok && (imports[alias.Name] == "github.com/gogf/gf/v2/database/gdb" || imports[alias.Name] == "github.com/gogf/gf/v2/frame/g") {
							report(argument, "ORM 写入必须使用生成的 DO 对象")
						}
					}
				}
			}
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if !ok || identifier.Obj != nil {
			return true
		}
		pkg, name := imports[identifier.Name], selector.Sel.Name
		if pkg == "net/http" {
			switch name {
			case "Client", "DefaultClient", "DefaultTransport", "Get", "Head", "Post", "PostForm", "NewRequest", "NewRequestWithContext":
				report(node, "外部 HTTP 请求必须使用 internal/httpclient 创建的 Resty 客户端")
			}
		}
		if pkg == "github.com/gogf/gf/v2/net/gclient" || (pkg == "github.com/gogf/gf/v2/frame/g" && name == "Client") {
			report(node, "禁止通过 GoFrame HTTP 客户端绕过 Resty")
		}
		if pkg == "github.com/go-resty/resty/v2" && strings.HasPrefix(name, "New") && path != "internal/httpclient/client.go" {
			report(node, "Resty 必须通过 internal/httpclient 统一创建")
		}
		return true
	})
	return issues
}

// databaseAccessDirs 列出唯一允许直接持有数据库连接的目录。
// 仓储实现持久化，访问日志写入器只借用注入连接，生成 DAO 位于 internal/dao。
var databaseAccessDirs = []string{"internal/dao/", "internal/repository/", "internal/accesslog/"}

// allowsDatabaseAccess 判断文件是否位于允许直接访问数据库连接的目录。
// path 使用仓库相对路径和斜杠分隔；目录之外的业务代码必须使用注入连接。
func allowsDatabaseAccess(path string) bool {
	for _, dir := range databaseAccessDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}
	return false
}

// databaseEntryName 返回调用表达式使用的数据库入口名称，未命中禁止入口时返回空字符串。
// 识别 g.DB、gdb.Instance 以及 dao.<对象>.Ctx / DB / Model 三种绕过仓储的形式；
// dao.<对象>.Table / Columns / Group 只是表结构元数据，不在禁止范围。
// 与其它规则一致，无法确定来源的本地同名标识符不判违规，避免误拦业务自定义对象。
func databaseEntryName(call *ast.CallExpr, imports map[string]string) string {
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	// g.DB 与 gdb.Instance 直接由导入别名识别。
	if alias, ok := method.X.(*ast.Ident); ok && alias.Obj == nil {
		switch imports[alias.Name] {
		case "github.com/gogf/gf/v2/frame/g":
			if method.Sel.Name == "DB" {
				return alias.Name + ".DB"
			}
		case "github.com/gogf/gf/v2/database/gdb":
			if method.Sel.Name == "Instance" {
				return alias.Name + ".Instance"
			}
		}
		return ""
	}
	// dao.<对象> 是生成 DAO 的包级变量，其 Ctx / DB / Model 会回落到全局连接。
	object, ok := method.X.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	alias, ok := object.X.(*ast.Ident)
	if !ok || alias.Obj != nil || !strings.HasSuffix(imports[alias.Name], "/internal/dao") {
		return ""
	}
	switch method.Sel.Name {
	case "Ctx", "DB", "Model":
		return alias.Name + "." + object.Sel.Name + "." + method.Sel.Name
	}
	return ""
}

// ormReceiver 判断表达式能否在当前文件内追溯到 ORM 数据库、模型或 DAO。
// imports 提供导入别名映射，seen 防止沿声明追溯时重复访问；不能确定来源时返回 false，
// 避免误拦 gvalid.Data 或 SDK 同名方法。跨文件封装仍需代码审查确认使用 DO。
func ormReceiver(expr ast.Expr, imports map[string]string, seen map[ast.Node]bool) bool {
	if expr == nil || seen[expr] {
		return false
	}
	seen[expr] = true
	switch value := expr.(type) {
	case *ast.StarExpr:
		return ormReceiver(value.X, imports, seen)
	case *ast.ParenExpr:
		return ormReceiver(value.X, imports, seen)
	case *ast.SelectorExpr:
		if alias, ok := value.X.(*ast.Ident); ok && alias.Obj == nil {
			pkg := imports[alias.Name]
			return pkg == "github.com/gogf/gf/v2/database/gdb" ||
				(pkg == "github.com/gogf/gf/v2/frame/g" && value.Sel.Name == "DB") ||
				strings.HasSuffix(pkg, "/internal/dao")
		}
	case *ast.CallExpr:
		if method, ok := value.Fun.(*ast.SelectorExpr); ok {
			return ormReceiver(method, imports, seen) || ormReceiver(method.X, imports, seen)
		}
	case *ast.Ident:
		if value.Obj == nil {
			return false
		}
		switch declaration := value.Obj.Decl.(type) {
		case *ast.Field:
			return ormReceiver(declaration.Type, imports, seen)
		case *ast.ValueSpec:
			if ormReceiver(declaration.Type, imports, seen) {
				return true
			}
			for i, name := range declaration.Names {
				if name.Name == value.Name && i < len(declaration.Values) {
					return ormReceiver(declaration.Values[i], imports, seen)
				}
			}
		case *ast.AssignStmt:
			for i, left := range declaration.Lhs {
				if name, ok := left.(*ast.Ident); ok && name.Name == value.Name && i < len(declaration.Rhs) {
					return ormReceiver(declaration.Rhs[i], imports, seen)
				}
			}
		}
	}
	return false
}
