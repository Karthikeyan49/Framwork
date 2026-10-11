package security

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestModuleImportsUsePublicContracts(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	boundaries := []string{"identity", "tenant", "policy", "metadata", "compiler", "runtime/data", "runtime/rules", "runtime/workflow", "tools", "events", "sandbox", "promotion", "ai", "audit", "observability"}
	const prefix = "github.com/Karthikeyan49/Framwork/internal/"
	for _, tree := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, tree), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(filepath.Join(root, "internal"), path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			consumer := ""
			for _, boundary := range boundaries {
				if strings.HasPrefix(rel, boundary+"/") {
					consumer = boundary
					break
				}
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, spec := range parsed.Imports {
				dependency, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				for _, boundary := range boundaries {
					privatePrefix := prefix + boundary + "/"
					if strings.HasPrefix(dependency, privatePrefix) && consumer != boundary {
						t.Errorf("%s imports private implementation %s; use the %s contract", path, dependency, boundary)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
