package sandbox

import "testing"

func TestIsolationAdmission(t *testing.T) {
	c := Context{ID: "sandbox.fixture", DataMode: "REDACTED_SNAPSHOT", ToolMode: "BLOCK"}
	if c.Validate() != nil {
		t.Fatal("valid")
	}
	c.ProductionCredentialsMounted = true
	if c.Validate() == nil {
		t.Fatal("prod secrets")
	}
	c.ProductionCredentialsMounted = false
	c.ToolMode = "PAYMENT_LIVE"
	if c.Validate() == nil {
		t.Fatal("live effects")
	}
}
