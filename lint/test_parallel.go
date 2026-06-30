package main

import (
	"go/ast"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// TestParallel flags top-level `func TestXxx(t *testing.T)` tests that neither
// call t.Parallel() nor opt out for a concrete reason.
//
// The project runs its suite with parallelism enabled and the overwhelming
// majority of tests already call t.Parallel() as their first statement. A test
// that silently omits it serialises that slice of the suite and, more subtly,
// hides ordering assumptions: a test that only passes because it runs alone is
// a latent flake the moment a sibling is parallelised next to it.
//
// The cop is deliberately conservative — it never flags a test that *cannot*
// be parallel:
//
//   - t.Setenv / t.Chdir mutate process-global state and panic if the test or
//     any parent test is parallel, so their presence anywhere in the function
//     (including subtests) exempts it.
//   - a test whose *testing.T parameter is unnamed (`_`) has no handle on which
//     to call Parallel(), so it is skipped.
//   - TestMain is the suite entry point, not a parallelisable test.
//
// Detection is syntactic. The t.Parallel() check looks only at the test's own
// body and does not descend into subtest closures, so a parent that merely
// parallelises its subtests is still flagged for its own missing call.
//
// Per-line suppression: `//rubocop:disable Lint/TestParallel`.
var TestParallel = &cop.Func{
	Meta: cop.Meta{
		Name:        "Lint/TestParallel",
		Description: "top-level tests should call t.Parallel() unless they mutate process-global state",
		Severity:    cop.Convention,
	},
	Scope: func(p *cop.Pass) bool { return p.IsTestFile() },
	Run: func(p *cop.Pass) {
		p.ForEachFunc(func(fn *ast.FuncDecl) {
			name, ok := testParam(fn)
			if !ok || name == "_" || fn.Body == nil {
				return
			}
			if callsParallel(fn.Body, name) {
				return
			}
			if usesNonParallelizableAPI(fn.Body) {
				return
			}
			p.Reportf(fn.Name,
				"%s does not call %s.Parallel(); add it as the first statement "+
					"(or annotate with //rubocop:disable Lint/TestParallel if it must run serially)",
				fn.Name.Name, name)
		})
	},
}

// testParam returns the bound name of fn's `*testing.T` parameter when fn is a
// top-level test function: no receiver, named TestXxx (but not TestMain), and a
// single parameter of syntactic type *testing.T. The second result is false for
// anything else.
func testParam(fn *ast.FuncDecl) (string, bool) {
	if fn.Recv != nil || fn.Name == nil {
		return "", false
	}
	name := fn.Name.Name
	if name == "TestMain" || !strings.HasPrefix(name, "Test") {
		return "", false
	}
	params := fn.Type.Params
	if params == nil || len(params.List) != 1 {
		return "", false
	}
	field := params.List[0]
	if !isTestingTPtr(field.Type) || len(field.Names) != 1 {
		return "", false
	}
	return field.Names[0].Name, true
}

// isTestingTPtr reports whether expr is the syntactic type *testing.T.
func isTestingTPtr(expr ast.Expr) bool {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return false
	}
	return cop.IsSelector(star.X, "testing", "T")
}

// callsParallel reports whether body calls <recv>.Parallel() in its own scope.
// Subtest closures are not descended into: a parent test that only parallelises
// its subtests still needs its own t.Parallel() call.
func callsParallel(body *ast.BlockStmt, recv string) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false // don't look inside subtest closures
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if cop.IsSelector(call.Fun, recv, "Parallel") {
			found = true
		}
		return !found
	})
	return found
}

// usesNonParallelizableAPI reports whether body calls t.Setenv or t.Chdir
// anywhere — including inside subtest closures. Both panic when the test or any
// parent test is parallel, so their presence rules a test out of parallelism.
// (os.Setenv / os.Chdir are already forbidden in tests by golangci-lint, so a
// bare Setenv/Chdir selector call is necessarily on a *testing.T.)
func usesNonParallelizableAPI(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if sel.Sel.Name == "Setenv" || sel.Sel.Name == "Chdir" {
			found = true
		}
		return !found
	})
	return found
}
