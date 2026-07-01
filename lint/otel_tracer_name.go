package main

import (
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

const (
	modulePath             = "github.com/docker/docker-agent"
	appTracerName          = "docker-agent"
	legacySharedTracerName = "cagent"
)

// OTelTracerName enforces package-scoped OpenTelemetry instrumentation names.
//
// OpenTelemetry's Tracer name identifies the instrumentation scope, not the
// service. Spans created directly by a package should therefore use that
// package's import path, so traces can be attributed to the code that emitted
// them. Runtime wiring is the exception: it intentionally passes the shared
// application tracer into runtime code. Prefer otel.Tracer(AppName) for that
// path; existing otel.Tracer("cagent") calls are kept as legacy shared-tracer
// exceptions.
//
// Per-line suppression: `//rubocop:disable Lint/OTelTracerName`.
var OTelTracerName = &cop.Func{
	Meta: cop.Meta{
		Name:        "Lint/OTelTracerName",
		Description: "otel.Tracer names must be AppName, cagent, or the current package import path",
		Severity:    cop.Error,
	},
	Types: true,
	Run: func(p *cop.Pass) {
		if p.Info == nil || p.Package == nil {
			return
		}
		expected := packageImportPath(p.Package.Path())
		p.ForEachCall(func(call *ast.CallExpr) {
			if !isOTelTracerCall(p.Info, call) || len(call.Args) == 0 {
				return
			}
			name, ok := tracerName(p, call.Args[0])
			if !ok || name == expected || isSharedTracerName(p, call.Args[0], name) {
				return
			}
			p.Reportf(call.Args[0], "otel.Tracer name must be %q for this package, AppName for the shared application tracer, or %q for legacy shared runtime tracing; got %q", expected, legacySharedTracerName, name)
		})
	},
}

func isOTelTracerCall(info *types.Info, call *ast.CallExpr) bool {
	if info != nil {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if fn, ok := info.Uses[sel.Sel].(*types.Func); ok {
				pkg := fn.Pkg()
				return pkg != nil && pkg.Path() == "go.opentelemetry.io/otel" && fn.Name() == "Tracer"
			}
		}
	}
	return cop.IsCallTo(call, "otel", "Tracer")
}

func packageImportPath(pkgPath string) string {
	if strings.HasPrefix(pkgPath, modulePath+"/") || pkgPath == modulePath {
		return pkgPath
	}
	return modulePath + "/" + strings.TrimPrefix(pkgPath, "./")
}

func isSharedTracerName(p *cop.Pass, expr ast.Expr, name string) bool {
	if name == legacySharedTracerName {
		return true
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "AppName" && name == appTracerName && packageImportPath(p.Package.Path()) == modulePath+"/cmd/root"
}

func stringConstValue(info *types.Info, ident *ast.Ident) (string, bool) {
	if info == nil {
		return "", false
	}
	c, ok := info.Uses[ident].(*types.Const)
	if !ok || c.Val().Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(c.Val()), true
}

func tracerName(p *cop.Pass, expr ast.Expr) (string, bool) {
	if name, ok := stringLit(expr); ok {
		return name, true
	}
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	if name, ok := stringConstValue(p.Info, ident); ok {
		return name, true
	}
	if val, ok := p.StringConsts()[ident.Name]; ok {
		return val, true
	}
	return "", false
}
