package app

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeProcessRefusesOperatorCredentials(t *testing.T) {
	env := func(pairs ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(pairs); i += 2 {
			m[pairs[i]] = pairs[i+1]
		}
		return func(k string) string { return m[k] }
	}
	// The configuration a backend legitimately holds, each credential bound to its own role, is never refused.
	runtime := []string{
		"APP_ENV", "test", "HTTP_ADDR", "127.0.0.1:8080", "DATABASE_URL", "postgres://app:x@db/app", "WORKER_DATABASE_URL", "postgres://w:x@db/app",
		"DISPATCHER_DATABASE_URL", "postgres://d:x@db/app", "REDIS_URL", "redis://u:x@cache", "EVENT_REDIS_URL", "redis://e:x@cache",
		"SIGNIN_REDIS_URL", "redis://s:x@cache", "AUTH_ISSUER", "https://id.example", "AI_PROVIDER_FILE", "/etc/ai.json",
	}
	if held := heldOperatorCredentials(env(runtime...)); len(held) != 0 {
		t.Fatalf("a runtime environment was refused: %v", held)
	}
	// Each operator credential alone is refused, by name; an empty value is not a credential.
	for _, name := range operatorCredentials {
		if held := heldOperatorCredentials(env(append([]string{name, "synthetic-secret-value"}, runtime...)...)); !reflect.DeepEqual(held, []string{name}) {
			t.Errorf("%s: held %v", name, held)
		}
		if held := heldOperatorCredentials(env(append([]string{name, ""}, runtime...)...)); len(held) != 0 {
			t.Errorf("%s set empty: held %v", name, held)
		}
	}
	// The whole environment of an operator, as the launcher hands it to a command it does not recognise.
	all := env("MIGRATION_DATABASE_URL", "postgres://m:x@db/app", "MIGRATOR_PASSWORD", "x", "POSTGRES_PASSWORD", "x", "DATABASE_URL", "postgres://app:x@db/app")
	if held := heldOperatorCredentials(all); !reflect.DeepEqual(held, []string{"MIGRATION_DATABASE_URL", "MIGRATOR_PASSWORD", "POSTGRES_PASSWORD"}) {
		t.Fatalf("held %v", held)
	}
}

func TestOperatorCredentialRefusalNamesAndNeverShows(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	const secret = "postgres://migrator:do-not-print@db.internal/app"
	getenv := func(k string) string {
		if k == "MIGRATION_DATABASE_URL" {
			return secret
		}
		return ""
	}
	if !refuseOperatorCredentials(getenv, logger) {
		t.Fatal("not refused")
	}
	text := out.String()
	if !strings.Contains(text, "MIGRATION_DATABASE_URL") || strings.Contains(text, "do-not-print") || strings.Contains(text, "db.internal") {
		t.Fatalf("the log must name the variable and never show it: %s", text)
	}
	out.Reset()
	if refuseOperatorCredentials(func(string) string { return "" }, logger) || out.Len() != 0 {
		t.Fatalf("a clean environment was refused or logged: %s", out.String())
	}
}

// The refusal is the control of IR-07 only if the daemon entrypoint makes it before it runs anything.
func TestMainRefusesOperatorCredentialsBeforeRunning(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "app.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	refuse, run := token.NoPos, token.NoPos
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "Main" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if id, ok := call.Fun.(*ast.Ident); ok {
					switch id.Name {
					case "refuseOperatorCredentials":
						if refuse == token.NoPos {
							refuse = call.Pos()
						}
					case "Run":
						if run == token.NoPos {
							run = call.Pos()
						}
					}
				}
			}
			return true
		})
	}
	if refuse == token.NoPos || run == token.NoPos || refuse > run {
		t.Fatalf("Main must call refuseOperatorCredentials before Run (refuse at %d, run at %d)", refuse, run)
	}
}
