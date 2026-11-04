package security

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestPureRulesHaveNoAmbientCapabilityImports(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../internal/runtime/rules")
	entries, e := os.ReadDir(root)
	if e != nil {
		t.Fatal(e)
	}
	allowed := map[string]bool{"archive/zip": true, "bytes": true, "context": true, "crypto/sha256": true, "embed": true, "encoding/hex": true, "encoding/json": true, "errors": true, "io": true, "math/big": true, "reflect": true, "regexp": true, "sort": true, "strconv": true, "strings": true, "time": true, "unicode/utf8": true, "github.com/Karthikeyan49/Framwork/internal/erp": true}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		ast, e := parser.ParseFile(token.NewFileSet(), filepath.Join(root, entry.Name()), nil, parser.ImportsOnly)
		if e != nil {
			t.Fatal(e)
		}
		for _, spec := range ast.Imports {
			s, e := strconv.Unquote(spec.Path.Value)
			if e != nil || !allowed[s] {
				t.Errorf("Rule engine ambient import %s", s)
			}
		}
	}
}
