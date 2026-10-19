package ai_test

// Architecture test for ADR040 section 2 and risk 37: a pure go/parser walk over
// the whole module (no build, no database, no network). It is the builder-side
// adversarial boundary check written by m14-adv; it is not an independent review.
//
// The checker (archCheck) is a pure function over parsed files so that the
// negative tests below can feed it a synthetic tree that violates each rule and
// prove the rule fires. A rule that cannot fail is not a control.
//
// Rules (ids are quoted in docs/verification/M14_ADVERSARIAL_PLAN.md):
//
//	ARCH-01  only internal/platform/access/ai*.go and ONE wiring file under
//	         internal/platform/app import internal/ai (non-test files)
//	ARCH-02  test files import internal/ai only under internal/ai, internal/platform,
//	         cmd and tests
//	ARCH-03  no deterministic package reaches internal/ai through any import chain
//	ARCH-04  no model-vendor SDK is imported or required anywhere in the module
//	ARCH-05  ai.NewProvider and the concrete adapters are used outside internal/ai
//	         only by the wiring file (and tests)
//	ARCH-06  internal/ai imports no database/sql, pgx or internal/platform outside
//	         store*.go and service*.go
//	ARCH-07  net/http, net and crypto/tls are imported only by internal/ai/provider_http*.go;
//	         internal/platform/access/ai*.go creates no HTTP client
//	ARCH-08  internal/ai imports no os/exec, plugin, syscall, unsafe or math/rand
//	ARCH-09  internal/ai reads no environment variable
//	ARCH-10  internal/ai prints and logs nothing (prompts, responses, credentials)

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// ------------------------------------------------------------------ model

type archImport struct {
	Path string
	Name string // local name used in the file ("" for blank/dot imports)
	Line int
}

type archUse struct {
	ImportPath string // import path the selector's package resolves to
	Name       string // selected identifier
	Line       int
}

type archFile struct {
	Path    string // module-relative, slash separated
	Test    bool
	Imports []archImport
	Uses    []archUse
	Builtin []archUse // print/println calls (ImportPath "")
}

func (f archFile) dir() string  { return path.Dir(f.Path) }
func (f archFile) base() string { return path.Base(f.Path) }

type archTree struct {
	Module  string // module path from go.mod
	Files   []archFile
	Require []string // module paths required by go.mod
}

func (t archTree) own(importPath string) (string, bool) {
	if importPath == t.Module {
		return ".", true
	}
	rest, ok := strings.CutPrefix(importPath, t.Module+"/")
	return rest, ok
}

// ------------------------------------------------------------------ loading

var archSkipDirs = map[string]bool{".git": true, "node_modules": true, ".local": true, "testdata": true, "dist": true, "coverage": true, "bin": true}

func archModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func archLoad(t *testing.T) archTree {
	t.Helper()
	root := archModuleRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	tree := archTree{}
	inRequire := false
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "module "):
			tree.Module = strings.TrimSpace(strings.TrimPrefix(line, "module "))
		case line == "require (":
			inRequire = true
		case line == ")":
			inRequire = false
		case inRequire && line != "" && !strings.HasPrefix(line, "//"):
			tree.Require = append(tree.Require, strings.Fields(line)[0])
		case strings.HasPrefix(line, "require "):
			tree.Require = append(tree.Require, strings.Fields(strings.TrimPrefix(line, "require "))[0])
		}
	}
	if tree.Module == "" {
		t.Fatal("module path not found in go.mod")
	}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && (archSkipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		tree.Files = append(tree.Files, archDescribe(fset, filepath.ToSlash(rel), f))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func archDescribe(fset *token.FileSet, rel string, f *ast.File) archFile {
	out := archFile{Path: rel, Test: strings.HasSuffix(rel, "_test.go")}
	local := map[string]string{}
	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, "\"`")
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		if name == "_" || name == "." {
			name = ""
		} else {
			local[name] = p
		}
		out.Imports = append(out.Imports, archImport{Path: p, Name: name, Line: fset.Position(imp.Pos()).Line})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				if p, ok := local[id.Name]; ok {
					out.Uses = append(out.Uses, archUse{ImportPath: p, Name: x.Sel.Name, Line: fset.Position(x.Pos()).Line})
				}
			}
		case *ast.CallExpr:
			if id, ok := x.Fun.(*ast.Ident); ok && (id.Name == "print" || id.Name == "println") {
				out.Builtin = append(out.Builtin, archUse{Name: id.Name, Line: fset.Position(x.Pos()).Line})
			}
		}
		return true
	})
	return out
}

// ------------------------------------------------------------------ rules

type archViolation struct {
	Rule, File string
	Line       int
	Msg        string
}

func (v archViolation) String() string {
	return fmt.Sprintf("%s: %s:%d: %s", v.Rule, v.File, v.Line, v.Msg)
}

const (
	archAIDir       = "internal/ai"
	archAccessDir   = "internal/platform/access"
	archWiringDir   = "internal/platform/app"
	archAIImportEnd = "/internal/ai"
)

// archDeterministic are the package trees whose behaviour must never depend on a
// model: CRUD/records, rules, workflow, tools, promotion, verification and the
// compiler/metadata/policy/audit foundations they stand on.
var archDeterministic = []string{
	"internal/erp", "internal/runtime", "internal/compiler", "internal/metadata", "internal/tools",
	"internal/promotion", "internal/verification", "internal/risk", "internal/sandbox", "internal/policy",
	"internal/events", "internal/intent", "internal/audit", "internal/tenant", "internal/identity",
}

// archVendorWords are path elements of model-vendor SDKs. Real vendor adapters are
// not shipped and not claimed (ADR040 section 3); adding one is a contract change.
var archVendorWords = []string{"anthropic", "openai", "generative-ai", "genai", "cohere", "mistral", "ollama", "langchain", "langchaingo", "bedrock", "vertexai", "aiplatform", "huggingface", "replicate", "groq", "togetherai", "gemini", "llama", "claude"}

var archAdapterNames = map[string]bool{"NewProvider": true, "NewScriptedProvider": true, "ScriptedProvider": true, "NewHTTPJSONProvider": true, "HTTPJSONProvider": true}

func archUnder(dir, root string) bool { return dir == root || strings.HasPrefix(dir, root+"/") }

func (t archTree) importsAI(f archFile) (archImport, bool) {
	for _, imp := range f.Imports {
		if rest, ok := t.own(imp.Path); ok && rest == archAIDir {
			return imp, true
		}
	}
	return archImport{}, false
}

func archCheck(tree archTree) []archViolation {
	var out []archViolation
	add := func(rule string, f archFile, line int, format string, args ...any) {
		out = append(out, archViolation{rule, f.Path, line, fmt.Sprintf(format, args...)})
	}
	wiring := map[string]bool{}
	for _, f := range tree.Files {
		imp, uses := tree.importsAI(f)
		inAI := archUnder(f.dir(), archAIDir)
		if uses && !inAI {
			isAccess := f.dir() == archAccessDir && strings.HasPrefix(f.base(), "ai")
			isWiring := f.dir() == archWiringDir
			switch {
			case f.Test:
				if !archUnder(f.dir(), "internal/platform") && !archUnder(f.dir(), "tests") && !archUnder(f.dir(), "cmd") {
					add("ARCH-02", f, imp.Line, "a test outside internal/ai, internal/platform, cmd and tests imports internal/ai")
				}
			case isAccess:
			case isWiring:
				wiring[f.Path] = true
			default:
				add("ARCH-01", f, imp.Line, "only internal/platform/access/ai*.go and one internal/platform/app wiring file may import internal/ai")
			}
		}
		// ARCH-05: constructing a provider is composition, not business logic.
		if !inAI && !f.Test && f.dir() != archWiringDir {
			for _, u := range f.Uses {
				if rest, ok := tree.own(u.ImportPath); ok && rest == archAIDir && archAdapterNames[u.Name] {
					add("ARCH-05", f, u.Line, "ai.%s is composition: only the app wiring file may use it", u.Name)
				}
			}
		}
		// ARCH-04: no vendor SDK anywhere (tests included: nothing may talk to a vendor).
		for _, i := range f.Imports {
			if w := archVendorWord(i.Path); w != "" && !strings.HasPrefix(i.Path, tree.Module+"/") {
				add("ARCH-04", f, i.Line, "import %q looks like a model-vendor SDK (%s)", i.Path, w)
			}
		}
		if !inAI || f.Test {
			continue
		}
		base := f.base()
		storage := strings.HasPrefix(base, "store") || strings.HasPrefix(base, "service")
		httpProvider := strings.HasPrefix(base, "provider_http")
		for _, i := range f.Imports {
			rest, own := tree.own(i.Path)
			switch {
			case !storage && (i.Path == "database/sql" || strings.HasPrefix(i.Path, "github.com/jackc/pgx") || own && archUnder(rest, "internal/platform")):
				add("ARCH-06", f, i.Line, "pure pipeline file imports %q (only store*.go and service*.go may)", i.Path)
			case !httpProvider && (i.Path == "net/http" || i.Path == "net" || i.Path == "crypto/tls" || i.Path == "net/http/httputil"):
				add("ARCH-07", f, i.Line, "%q outside the HTTP provider file", i.Path)
			case i.Path == "os/exec" || i.Path == "plugin" || i.Path == "syscall" || i.Path == "unsafe" || i.Path == "math/rand" || i.Path == "math/rand/v2":
				add("ARCH-08", f, i.Line, "forbidden import %q in internal/ai", i.Path)
			case i.Path == "log" || i.Path == "log/slog":
				add("ARCH-10", f, i.Line, "internal/ai must not log (%q)", i.Path)
			}
		}
		for _, u := range f.Uses {
			switch {
			case u.ImportPath == "os" && (u.Name == "Getenv" || u.Name == "LookupEnv" || u.Name == "Environ"):
				add("ARCH-09", f, u.Line, "os.%s: configuration comes from the operator file, not the environment", u.Name)
			case u.ImportPath == "os" && (u.Name == "Stdout" || u.Name == "Stderr"):
				add("ARCH-10", f, u.Line, "os.%s: internal/ai must not print", u.Name)
			case u.ImportPath == "fmt" && strings.HasPrefix(u.Name, "Print"):
				add("ARCH-10", f, u.Line, "fmt.%s: internal/ai must not print", u.Name)
			case !httpProvider && u.ImportPath == "net/http" && (u.Name == "DefaultClient" || u.Name == "Get" || u.Name == "Post" || u.Name == "Head" || u.Name == "PostForm" || u.Name == "Client"):
				add("ARCH-07", f, u.Line, "http.%s outside the HTTP provider file", u.Name)
			}
		}
		for _, b := range f.Builtin {
			add("ARCH-10", f, b.Line, "builtin %s: internal/ai must not print", b.Name)
		}
	}
	if len(wiring) > 1 {
		names := []string{}
		for p := range wiring {
			names = append(names, p)
		}
		sort.Strings(names)
		out = append(out, archViolation{"ARCH-01", names[0], 0, "more than one wiring file imports internal/ai: " + strings.Join(names, ", ")})
	}
	// ARCH-07 (HTTP surface): the route file creates no HTTP client of its own.
	for _, f := range tree.Files {
		if f.dir() == archAccessDir && strings.HasPrefix(f.base(), "ai") && !f.Test {
			for _, u := range f.Uses {
				if u.ImportPath == "net/http" && (u.Name == "Client" || u.Name == "DefaultClient" || u.Name == "Get" || u.Name == "Post" || u.Name == "Transport") {
					add("ARCH-07", f, u.Line, "http.%s: the AI route file must not call out", u.Name)
				}
			}
		}
	}
	// ARCH-03: transitive reachability from every deterministic package.
	out = append(out, archReach(tree)...)
	// ARCH-04 (go.mod).
	for _, r := range tree.Require {
		if w := archVendorWord(r); w != "" {
			out = append(out, archViolation{"ARCH-04", "go.mod", 0, fmt.Sprintf("go.mod requires %q, which looks like a model-vendor SDK (%s)", r, w)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rule != out[j].Rule {
			return out[i].Rule < out[j].Rule
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	return out
}

func archVendorWord(importPath string) string {
	for _, elem := range strings.Split(strings.ToLower(importPath), "/") {
		for _, w := range archVendorWords {
			if elem == w || strings.HasPrefix(elem, w+"-") || strings.HasSuffix(elem, "-"+w) || strings.HasPrefix(elem, w+"-go") || strings.HasPrefix(elem, "go-"+w) || strings.HasSuffix(elem, w+"-sdk-go") {
				return w
			}
		}
	}
	return ""
}

// archReach reports, for every package in a deterministic tree, an import chain
// that reaches internal/ai. Only non-test files count: a test may exercise a
// package, it does not ship in it.
func archReach(tree archTree) []archViolation {
	edges := map[string]map[string]archImport{}
	rep := map[string]string{} // package dir -> one representative file
	for _, f := range tree.Files {
		if f.Test {
			continue
		}
		dir := f.dir()
		if edges[dir] == nil {
			edges[dir] = map[string]archImport{}
			rep[dir] = f.Path
		}
		for _, imp := range f.Imports {
			if rest, ok := tree.own(imp.Path); ok {
				if _, seen := edges[dir][rest]; !seen {
					edges[dir][rest] = imp
				}
			}
		}
	}
	var out []archViolation
	dirs := make([]string, 0, len(edges))
	for d := range edges {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, root := range dirs {
		det := false
		for _, d := range archDeterministic {
			if archUnder(root, d) {
				det = true
			}
		}
		if !det {
			continue
		}
		// breadth-first search keeping the chain.
		type step struct {
			dir  string
			from string
		}
		parent := map[string]string{root: ""}
		queue := []string{root}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			next := make([]string, 0, len(edges[cur]))
			for to := range edges[cur] {
				next = append(next, to)
			}
			sort.Strings(next)
			for _, to := range next {
				if _, seen := parent[to]; seen {
					continue
				}
				parent[to] = cur
				if archUnder(to, archAIDir) {
					chain := []string{to}
					for p := cur; p != ""; p = parent[p] {
						chain = append([]string{p}, chain...)
					}
					out = append(out, archViolation{"ARCH-03", rep[root], 0, "deterministic package " + root + " reaches internal/ai through " + strings.Join(chain, " -> ")})
					queue = nil
					break
				}
				queue = append(queue, to)
			}
		}
	}
	return out
}

// ------------------------------------------------------------------ the real tree

func TestArchitectureAIBoundary(t *testing.T) {
	tree := archLoad(t)
	if len(tree.Files) < 100 {
		t.Fatalf("only %d Go files parsed: the walk is broken, the rules below would pass vacuously", len(tree.Files))
	}
	var seenAI, seenCompiler, seenAccess bool
	for _, f := range tree.Files {
		seenAI = seenAI || f.dir() == archAIDir
		seenCompiler = seenCompiler || f.dir() == "internal/compiler"
		seenAccess = seenAccess || f.dir() == archAccessDir
	}
	if !seenAI || !seenCompiler || !seenAccess {
		t.Fatalf("expected trees missing from the walk (ai=%v compiler=%v access=%v)", seenAI, seenCompiler, seenAccess)
	}
	for _, v := range archCheck(tree) {
		t.Error(v)
	}
}

// TestArchitectureAIImporters logs the exact set of non-test importers of
// internal/ai, so a new importer is a visible line in the test output and in
// review, not only a rule violation.
func TestArchitectureAIImporters(t *testing.T) {
	tree := archLoad(t)
	var got []string
	for _, f := range tree.Files {
		if _, ok := tree.importsAI(f); ok && !f.Test && !archUnder(f.dir(), archAIDir) {
			got = append(got, f.Path)
		}
	}
	sort.Strings(got)
	for _, p := range got {
		ok := (path.Dir(p) == archAccessDir && strings.HasPrefix(path.Base(p), "ai")) || path.Dir(p) == archWiringDir
		if !ok {
			t.Errorf("unexpected importer of internal/ai: %s", p)
		}
	}
	t.Logf("non-test importers of internal/ai: %v", got)
}

// ------------------------------------------------------------------ the checker is itself tested

func archSyn(module string, files ...archFile) archTree {
	return archTree{Module: module, Files: files}
}

func archSynFile(p string, imports ...string) archFile {
	f := archFile{Path: p, Test: strings.HasSuffix(p, "_test.go")}
	for i, imp := range imports {
		f.Imports = append(f.Imports, archImport{Path: imp, Name: path.Base(imp), Line: i + 3})
	}
	return f
}

func archRules(vs []archViolation) string {
	set := map[string]bool{}
	for _, v := range vs {
		set[v.Rule] = true
	}
	var rules []string
	for r := range set {
		rules = append(rules, r)
	}
	sort.Strings(rules)
	return strings.Join(rules, ",")
}

func TestArchitectureCheckerDetectsViolations(t *testing.T) {
	const m = "example.test/fw"
	ai := m + "/internal/ai"
	with := func(f archFile, uses ...archUse) archFile { f.Uses = append(f.Uses, uses...); return f }
	cases := []struct {
		name string
		tree archTree
		want string // comma-separated rules that must fire ("" = clean)
	}{
		{"clean tree", archSyn(m,
			archSynFile("internal/ai/gateway.go", m+"/internal/metadata", m+"/internal/compiler"),
			archSynFile("internal/ai/store.go", "database/sql", m+"/internal/platform/postgres"),
			archSynFile("internal/ai/provider_http.go", "net/http", "net", "crypto/tls"),
			archSynFile("internal/platform/access/ai.go", ai),
			archSynFile("internal/platform/access/records.go", m+"/internal/metadata"),
			archSynFile("internal/platform/app/app.go", ai, m+"/internal/platform/access"),
			archSynFile("internal/compiler/compiler.go", m+"/internal/metadata"),
			archSynFile("tests/integration/m14_test.go", ai)), ""},
		{"rules package imports ai", archSyn(m, archSynFile("internal/runtime/rules/eval.go", ai), archSynFile("internal/ai/gateway.go")), "ARCH-01,ARCH-03"},
		{"crud handler file imports ai", archSyn(m, archSynFile("internal/platform/access/records.go", ai), archSynFile("internal/ai/gateway.go")), "ARCH-01"},
		{"second wiring file", archSyn(m, archSynFile("internal/platform/app/app.go", ai), archSynFile("internal/platform/app/ai_wiring.go", ai)), "ARCH-01"},
		{"cmd imports ai", archSyn(m, archSynFile("cmd/api/main.go", ai)), "ARCH-01"},
		{"unit test in promotion imports ai", archSyn(m, archSynFile("internal/promotion/x_test.go", ai)), "ARCH-02"},
		{"transitive chain promotion -> access -> ai", archSyn(m,
			archSynFile("internal/promotion/promote.go", m+"/internal/platform/access"),
			archSynFile("internal/platform/access/ai.go", ai),
			archSynFile("internal/ai/gateway.go")), "ARCH-03"},
		{"vendor sdk import", archSyn(m, archSynFile("internal/ai/provider_vendor.go", "github.com/anthropics/anthropic-sdk-go")), "ARCH-04"},
		{"vendor sdk in tests too", archSyn(m, archSynFile("tests/integration/x_test.go", "github.com/sashabaranov/go-openai")), "ARCH-04"},
		{"vendor sdk in go.mod", archTree{Module: m, Require: []string{"github.com/tmc/langchaingo"}}, "ARCH-04"},
		{"provider built outside wiring", archSyn(m,
			with(archSynFile("internal/platform/access/ai.go", ai), archUse{ImportPath: ai, Name: "NewProvider", Line: 9})), "ARCH-05"},
		{"scripted provider in production code", archSyn(m,
			with(archSynFile("internal/platform/access/ai.go", ai), archUse{ImportPath: ai, Name: "NewScriptedProvider", Line: 9})), "ARCH-05"},
		{"wiring may build the provider", archSyn(m,
			with(archSynFile("internal/platform/app/app.go", ai), archUse{ImportPath: ai, Name: "NewProvider", Line: 9})), ""},
		{"pipeline imports database/sql", archSyn(m, archSynFile("internal/ai/gateway.go", "database/sql")), "ARCH-06"},
		{"pipeline imports platform", archSyn(m, archSynFile("internal/ai/context.go", m+"/internal/platform/postgres")), "ARCH-06"},
		{"pipeline imports pgx", archSyn(m, archSynFile("internal/ai/repair.go", "github.com/jackc/pgx/v5")), "ARCH-06"},
		{"service may import platform", archSyn(m, archSynFile("internal/ai/service_patch.go", m+"/internal/platform/postgres", "database/sql")), ""},
		{"net/http in gateway", archSyn(m, archSynFile("internal/ai/gateway.go", "net/http")), "ARCH-07"},
		{"net in context builder", archSyn(m, archSynFile("internal/ai/context.go", "net")), "ARCH-07"},
		{"http.Get in pipeline", archSyn(m,
			with(archSynFile("internal/ai/eval.go", "net/http"), archUse{ImportPath: "net/http", Name: "Get", Line: 20})), "ARCH-07"},
		{"route file calls out", archSyn(m,
			with(archSynFile("internal/platform/access/ai.go", "net/http"), archUse{ImportPath: "net/http", Name: "DefaultClient", Line: 30})), "ARCH-07"},
		{"os/exec in pipeline", archSyn(m, archSynFile("internal/ai/apply.go", "os/exec")), "ARCH-08"},
		{"math/rand in pipeline", archSyn(m, archSynFile("internal/ai/repair.go", "math/rand")), "ARCH-08"},
		{"getenv in pipeline", archSyn(m,
			with(archSynFile("internal/ai/config.go", "os"), archUse{ImportPath: "os", Name: "Getenv", Line: 12})), "ARCH-09"},
		{"log import", archSyn(m, archSynFile("internal/ai/gateway.go", "log/slog")), "ARCH-10"},
		{"fmt.Println", archSyn(m,
			with(archSynFile("internal/ai/gateway.go", "fmt"), archUse{ImportPath: "fmt", Name: "Println", Line: 14})), "ARCH-10"},
		{"println builtin", archSyn(m, archFile{Path: "internal/ai/gateway.go", Builtin: []archUse{{Name: "println", Line: 5}}}), "ARCH-10"},
		{"tests inside ai may do anything", archSyn(m, archSynFile("internal/ai/gateway_test.go", "net/http", "log", "database/sql", "os/exec")), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := archRules(archCheck(c.tree))
			if got != c.want {
				t.Fatalf("rules fired = %q, want %q: %v", got, c.want, archCheck(c.tree))
			}
		})
	}
}

func TestArchitectureVendorWords(t *testing.T) {
	for _, p := range []string{"github.com/anthropics/anthropic-sdk-go", "github.com/openai/openai-go", "github.com/sashabaranov/go-openai", "google.golang.org/genai", "github.com/google/generative-ai-go", "github.com/cohere-ai/cohere-go", "github.com/tmc/langchaingo", "github.com/ollama/ollama/api", "github.com/aws/aws-sdk-go-v2/service/bedrockruntime/bedrock"} {
		if archVendorWord(p) == "" {
			t.Errorf("%s is not recognised as a vendor SDK", p)
		}
	}
	// Dependencies the project really has must not be mistaken for vendors.
	for _, p := range []string{"github.com/jackc/pgx/v5", "go.opentelemetry.io/otel", "google.golang.org/genproto/googleapis/api", "google.golang.org/grpc", "github.com/santhosh-tekuri/jsonschema/v6", "go.opentelemetry.io/proto/otlp", "golang.org/x/net"} {
		if w := archVendorWord(p); w != "" {
			t.Errorf("%s is wrongly flagged as a vendor SDK (%s)", p, w)
		}
	}
}
