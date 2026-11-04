package compiler

import (
	"encoding/json"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// The 1.4.0 integrity IR restores only with its exact application/IR/compiler pairing. The 1.5.0 tool contract is a superset of it
// and used to bypass that check: a freshly hashed 1.5.0 artifact labelled with an unsupported compiler restored (remediation
// review B-02). Both contracts must refuse the same forgeries.
func TestIRRestoreKeepsTheExactCompilerIdentityOfEveryIntegrityCapableContract(t *testing.T) {
	for name, version := range map[string]string{"integrity 1.4.0": metadata.IntegritySchemaVersion, "tool 1.5.0": metadata.ToolWorkflowSchemaVersion} {
		t.Run(name, func(t *testing.T) {
			d := integrityDocument(t)
			d.SchemaVersion = version
			for i := range d.Definitions {
				d.Definitions[i].SchemaVersion = version
			}
			p := compileIntegrity(t, d)
			if !HasUniqueConstraints(p) {
				t.Fatal("the fixture declares no unique field")
			}
			if _, err := Restore(p.JSON(), p.Hash(), SupportedIRVersions()); err != nil {
				t.Fatal("control: the unmodified artifact must restore", err)
			}
			forge := map[string]func(*IR){
				"unsupported compiler": func(x *IR) { x.CompilerVersion = "unsupported-compiler" },
				"empty-ish compiler":   func(x *IR) { x.CompilerVersion = "0" },
				"another contract's compiler": func(x *IR) {
					if x.CompilerVersion == IntegrityCompilerVersion {
						x.CompilerVersion = ToolWorkflowCompilerVersion
					} else {
						x.CompilerVersion = IntegrityCompilerVersion
					}
				},
				"approval IR label": func(x *IR) { x.IRVersion = ApprovalIRVersion },
				"unique flag moved": func(x *IR) { x.Entities[0].Fields[1].Unique = !x.Entities[0].Fields[1].Unique },
			}
			for what, mutate := range forge {
				x := p.IR()
				mutate(&x)
				raw, _ := json.Marshal(x)
				if _, err := Restore(raw, digest(raw), SupportedIRVersions()); err == nil {
					t.Fatalf("%s: a freshly hashed forgery (%s) was restored", version, what)
				}
			}
		})
	}
}
