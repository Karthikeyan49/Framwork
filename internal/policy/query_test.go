package policy

import "testing"

type embeddedQueryPolicy struct{ Native }

func TestSQLFieldActionsProofCannotBeInherited(t *testing.T) {
	for _, e := range []Engine{Native{}, &Native{}, Defined{}, &Defined{}} {
		if !SQLFieldActions(e) {
			t.Fatal("closed kernel not recognized")
		}
	}
	for _, e := range []Engine{nil, (*Native)(nil), (*Defined)(nil), embeddedQueryPolicy{}, &embeddedQueryPolicy{}} {
		if SQLFieldActions(e) {
			t.Fatal("opaque/inherited kernel bypass")
		}
	}
}
