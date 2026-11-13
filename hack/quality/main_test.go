package main

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestClientPolicyRecognizesAliasesAndAllowsServers(t *testing.T) {
	for _, tc := range []struct {
		name, source, path string
		denied             bool
	}{
		{"raw client alias", `package p; import h "net/http"; var x = h.Client{}`, "internal/service/example.go", true},
		{"default client", `package p; import "net/http"; var x = http.DefaultClient`, "internal/service/example.go", true},
		{"server", `package p; import "net/http"; var x = http.Server{}`, "internal/server/example.go", false},
		{"factory", `package p; import "github.com/go-resty/resty/v2"; var x = resty.New()`, "internal/httpclient/client.go", false},
		{"factory bypass", `package p; import r "github.com/go-resty/resty/v2"; var x = r.New()`, "internal/service/example.go", true},
		{"default Resty import bypass", `package p; import "github.com/go-resty/resty/v2"; var x = resty.New()`, "internal/service/example.go", true},
		{"factory package bypass", `package p; import "github.com/go-resty/resty/v2"; var x = resty.New()`, "internal/httpclient/example.go", true},
		{"main bypass", `package main; import "net/http"; var x = http.Get`, "main.go", true},
		{"test dot import bypass", `package p; import . "net/http"; var x = Get`, "internal/service/example_test.go", true},
		{"local identifier", `package p; import "net/http"; func f(){ http := struct{ Get int }{}; _ = http.Get }`, "internal/service/example.go", false},
		{"goframe bypass", `package p; import "github.com/gogf/gf/v2/frame/g"; var x = g.Client()`, "internal/service/example.go", true},
		{"map ORM write", `package p; import "github.com/gogf/gf/v2/database/gdb"; func f(m *gdb.Model){ m.Data(gdb.Map{"name":"x"}) }`, "internal/repository/example.go", true},
		{"map ORM variable", `package p; import "github.com/gogf/gf/v2/database/gdb"; func f(db gdb.DB){ m := db.Model("items"); m.Data(map[string]any{"name":"x"}) }`, "internal/repository/example.go", true},
		{"DO ORM write", `package p; import "github.com/gogf/gf/v2/database/gdb"; func f(m *gdb.Model){ m.Data(do.Items{Name:"x"}) }`, "internal/repository/example.go", false},
		{"validation data is not ORM", `package p; import "github.com/gogf/gf/v2/util/gvalid"; func f(){ gvalid.New().Data(map[string]any{"name":"x"}) }`, "internal/service/example.go", false},
		{"unknown Data is not ORM", `package p; func f(){ builder.Data(map[string]any{"name":"x"}) }`, "internal/service/example.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := token.NewFileSet()
			file, err := parser.ParseFile(files, "example.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			issues := checkAST(files, file, tc.path)
			if (len(issues) > 0) != tc.denied {
				t.Fatalf("unexpected policy result: %v", issues)
			}
		})
	}
}

func TestDatabaseAccessPolicyOnlyAllowsPersistenceLayer(t *testing.T) {
	for _, tc := range []struct {
		name, source, path string
		denied             bool
	}{
		{"g.DB in service", `package p; import "github.com/gogf/gf/v2/frame/g"; func f() { _, _ = g.DB() }`, "internal/service/example.go", true},
		{"g.DB in main", `package main; import "github.com/gogf/gf/v2/frame/g"; func f() { _, _ = g.DB() }`, "main.go", true},
		{"g.DB in repository", `package p; import "github.com/gogf/gf/v2/frame/g"; func f() { _, _ = g.DB() }`, "internal/repository/example.go", false},
		{"g.DB in generated dao", `package p; import "github.com/gogf/gf/v2/frame/g"; func f() { _, _ = g.DB("group") }`, "internal/dao/access_logs.go", false},
		{"gdb.Instance in controller", `package p; import "github.com/gogf/gf/v2/database/gdb"; func f() { _, _ = gdb.Instance() }`, "internal/controller/example.go", true},
		{"gdb.Instance in repository", `package p; import "github.com/gogf/gf/v2/database/gdb"; func f() { _, _ = gdb.Instance() }`, "internal/repository/example.go", false},
		{"dao Ctx in service", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Ctx(nil) }`, "internal/service/example.go", true},
		{"dao DB in controller", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.DB() }`, "internal/controller/example.go", true},
		{"dao Model in middleware", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Model(nil) }`, "internal/middleware/example.go", true},
		{"aliased dao Ctx in service", `package p; import cols "backend-template-go/internal/dao"; func f() { _ = cols.AccessLogs.Ctx(nil) }`, "internal/service/example.go", true},
		{"dao Ctx in repository", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Ctx(nil) }`, "internal/repository/example.go", false},
		{"dao Ctx in accesslog", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Ctx(nil) }`, "internal/accesslog/example.go", false},
		{"dao Table metadata allowed", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Table() }`, "internal/service/example.go", false},
		{"dao Columns metadata allowed", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Columns() }`, "internal/service/example.go", false},
		{"dao Group metadata allowed", `package p; import "backend-template-go/internal/dao"; func f() { _ = dao.AccessLogs.Group() }`, "internal/service/example.go", false},
		{"local dao identifier", `package p; func f() { dao := struct{ Ctx func() int }{}; _ = dao.Ctx() }`, "internal/service/example.go", false},
		{"local g identifier", `package p; func f() { g := struct{ DB func() int }{}; _ = g.DB() }`, "internal/service/example.go", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := token.NewFileSet()
			file, err := parser.ParseFile(files, "example.go", tc.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			issues := checkAST(files, file, tc.path)
			if (len(issues) > 0) != tc.denied {
				t.Fatalf("unexpected database access policy result: %v", issues)
			}
		})
	}
}
