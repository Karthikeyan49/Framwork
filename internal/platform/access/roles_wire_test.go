package access

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/policy"
)

func TestRoleEditorHTTPHashFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/examples/role-editor-wire-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Definition json.RawMessage `json:"definition"`
		Hash       string          `json:"expected_http_hash"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("invalid fixture")
	}
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"definition":`+string(fixture.Definition)+`}`))
	request.Header.Set("Content-Type", "application/json")
	var body struct {
		Definition json.RawMessage `json:"definition"`
	}
	if readDataCommand(request, &body) != nil {
		t.Fatal("wire decoder rejected fixture")
	}
	if fmt.Sprintf("%x", sha256.Sum256(body.Definition)) != fixture.Hash {
		t.Fatal("HTTP serialization witness changed")
	}
	if _, err := policy.CompileDefinition(body.Definition, fixture.Hash); err != nil {
		t.Fatal("fixture does not compile")
	}
}
