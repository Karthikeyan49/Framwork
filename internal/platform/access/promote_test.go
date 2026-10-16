package access

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// concrete fills the wildcards of a registered release pattern.
func concrete(pattern string) (method, path string) {
	method, path, _ = strings.Cut(pattern, " ")
	return method, strings.NewReplacer(
		"{tenant}", "0f8c9a52-6a3e-4c1b-9d7e-2b5f1c3a4d6e",
		"{app}", "app.orders",
		"{candidate}", strings.Repeat("ab", 32),
		"{version}", "3",
		"{spec}", "5d1a2c3b-4e5f-4a6b-8c7d-9e0f1a2b3c4d",
	).Replace(path)
}

// TestReleasePatternsRegistrationAndPOSTAllowlistAgree is the drift guard of
// ADR039 section 7: every release pattern is registered exactly as listed (so
// the action map and the dispatch see it), and the body allowlist accepts the
// path of a POST pattern and nothing else.
func TestReleasePatternsRegistrationAndPOSTAllowlistAgree(t *testing.T) {
	a := API{Registry: &metadata.Registry{}, PreviewEnabled: true}
	mux := http.NewServeMux()
	a.Register(mux)
	posts := map[string]bool{}
	for pattern := range releasePatterns {
		if method, path := concrete(pattern); method == http.MethodPost {
			posts[path] = true
		}
	}
	for pattern := range releasePatterns {
		method, path := concrete(pattern)
		if _, registered := mux.Handler(httptest.NewRequest(method, path, nil)); registered != pattern {
			t.Errorf("%s routes to %q", pattern, registered)
		}
		if got := a.AcceptsPOST(path); got != posts[path] {
			t.Errorf("AcceptsPOST(%s) = %v, want %v", path, got, posts[path])
		}
	}
	root := "/v1/tenants/0f8c9a52-6a3e-4c1b-9d7e-2b5f1c3a4d6e/preview/app.orders"
	hash := strings.Repeat("ab", 32)
	for _, path := range []string{
		root + "/releases", root + "/releases/active", root + "/releases/rollback/x", root + "/releases/activate",
		root + "/candidates/" + hash, root + "/candidates/" + hash + "/promote", root + "/candidates/" + hash + "/approve/",
		root + "/candidates/" + strings.ToUpper(hash) + "/approve", root + "/candidates/" + hash[:63] + "/activate",
		root + "/candidates/" + hash + "/approve/x", root + "/candidates/" + hash + "x/reject",
	} {
		if a.AcceptsPOST(path) {
			t.Errorf("AcceptsPOST(%s) admits a body for a route that takes none", path)
		}
	}
	// The M13 routes and their actions are exactly ADR039 section 7.
	want := map[string]string{approvePattern: "releases.approve", rejectPattern: "releases.approve", activatePattern: "releases.activate",
		rollbackPattern: "releases.rollback", historyPattern: "releases.read", activePattern: "releases.read"}
	for pattern, action := range want {
		if releasePatterns[pattern] != action {
			t.Errorf("%s maps to %q, want %q", pattern, releasePatterns[pattern], action)
		}
	}
	// Production-like configuration: no preview, no release route, no body.
	for _, off := range []API{{Registry: &metadata.Registry{}}, {PreviewEnabled: true}} {
		mux := http.NewServeMux()
		off.Register(mux)
		for pattern := range releasePatterns {
			method, path := concrete(pattern)
			if _, registered := mux.Handler(httptest.NewRequest(method, path, nil)); registered != "" {
				t.Errorf("preview=%v registry=%v registers %s", off.PreviewEnabled, off.Registry != nil, registered)
			}
			if off.AcceptsPOST(path) {
				t.Errorf("preview=%v registry=%v admits a body on %s", off.PreviewEnabled, off.Registry != nil, path)
			}
		}
	}
}

// forbiddenCalls finds, in the functions reachable from roots within this
// package, any call of the compiler's Compile entry points, any use of the
// metadata registry (reading or parsing source) and any construction of a
// source validator. Same-package calls are followed through plain function
// calls and methods called on the API receiver.
func forbiddenCalls(t *testing.T, roots func(file string, fn *ast.FuncDecl) bool) []string {
	t.Helper()
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	funcs := map[string][]*ast.FuncDecl{}
	compilerName := map[*ast.FuncDecl]string{}
	metadataName := map[*ast.FuncDecl]string{}
	var queue []*ast.FuncDecl
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		local := map[string]string{}
		for _, spec := range f.Imports {
			p, _ := strconv.Unquote(spec.Path.Value)
			alias := filepath.Base(p)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			local[p] = alias
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			funcs[fn.Name.Name] = append(funcs[fn.Name.Name], fn)
			compilerName[fn] = local["github.com/Karthikeyan49/Framwork/internal/compiler"]
			metadataName[fn] = local["github.com/Karthikeyan49/Framwork/internal/metadata"]
			if roots(name, fn) {
				queue = append(queue, fn)
			}
		}
	}
	if len(queue) == 0 {
		t.Fatal("no root functions found")
	}
	seen := map[*ast.FuncDecl]bool{}
	var found []string
	for len(queue) > 0 {
		fn := queue[0]
		queue = queue[1:]
		if seen[fn] {
			continue
		}
		seen[fn] = true
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok {
					if id.Name == compilerName[fn] && compilerName[fn] != "" && strings.HasPrefix(x.Sel.Name, "Compile") ||
						id.Name == metadataName[fn] && metadataName[fn] != "" && x.Sel.Name == "NewValidator" {
						found = append(found, fn.Name.Name+": "+id.Name+"."+x.Sel.Name)
					}
				}
				if x.Sel.Name == "Registry" {
					found = append(found, fn.Name.Name+": Registry")
				}
			case *ast.CallExpr:
				switch f := x.Fun.(type) {
				case *ast.Ident:
					queue = append(queue, funcs[f.Name]...)
				case *ast.SelectorExpr:
					if id, ok := f.X.(*ast.Ident); ok && id.Name == "a" {
						queue = append(queue, funcs[f.Sel.Name]...)
					}
				}
			}
			return true
		})
	}
	sort.Strings(found)
	return found
}

// TestPromotionHandlersNeverCompile protects "no recompile after approval"
// (ADR039 section 1): nothing reachable from promote.go compiles source or reads
// or parses a source revision. Only stored bytes are restored.
func TestPromotionHandlersNeverCompile(t *testing.T) {
	if found := forbiddenCalls(t, func(file string, _ *ast.FuncDecl) bool { return file == "promote.go" }); len(found) > 0 {
		t.Fatalf("promotion handlers reach compile or source parsing: %v", found)
	}
	// The guard is not vacuous: the M12 builder does compile, and is caught.
	found := forbiddenCalls(t, func(_ string, fn *ast.FuncDecl) bool { return fn.Name.Name == "postCandidate" })
	if !strings.Contains(strings.Join(found, " "), "compiler.Compile") || !strings.Contains(strings.Join(found, " "), "Registry") {
		t.Fatalf("the static guard misses a known compile path: %v", found)
	}
}
