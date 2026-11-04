package ai

import "strings"

// allowedKinds are the definition kinds a proposal may add, update or delete
// (ADR104 section 6), sorted. The list is pinned from the payload fields of
// metadata.Definition: every kind outside it, notably "tool", is refused as a target,
// as a proposed definition and as an existing definition being replaced or deleted.
// TestAllowlistCoversEveryDefinitionKind fails when metadata gains a kind, so adding
// one is a conscious decision here and never an accident.
var allowedKinds = []string{"entity", "field", "page", "relationship", "rule", "workflow"}

// AllowedKinds returns a copy of the kind allowlist.
func AllowedKinds() []string { return append([]string(nil), allowedKinds...) }

func kindAllowed(kind string) bool {
	for _, k := range allowedKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// targetKind is the kind prefix of a definition id ("rule" for "rule.discount").
func targetKind(id string) string {
	kind, _, _ := strings.Cut(id, ".")
	return kind
}
