package metadata

import (
	"regexp"
	"sort"
	"strings"
)

// Authoring schema versions are named outside Go as well: in SQL function bodies, CHECK constraints, policies, trigger conditions,
// defaults, views and index predicates. 1.5.0 was added as a superset of 1.4.0 and one SQL function that selected a document with
// schema_version='1.4.0' was missed, so no unique claim could be registered under 1.5.0 (remediation review B-05, independent review
// IR-01). These helpers let a test read any piece of SQL (the embedded migrations, or the definitions of a live catalog) and say
// whether it gates on part of a family of versions, so the next superset contract cannot repeat the omission.

// sqlVersionToken finds a version spelled in SQL text in the forms a gate can take: a quoted literal ('1.4.0', "1.4.0", E'1.4.0',
// $v$1.4.0$v$, a member of a JSON document), a member of an array literal ('{1.4.0}', '{1.3.0,1.4.0}') or of a list in one literal
// ('1.4.0,1.3.0'), a prefix ('1.4%', '^1\.4\.') or the first half of a concatenation ('1.4.' || '0'). It needs the opening quote,
// dollar, caret, brace or comma in front so that numbers such as 1.5 or 11.4 are not taken for versions (a number right after a
// comma, as in round(x,1.3), is taken for one; none exists in the migrations, and a body that has one is reported for its author to
// look at). It does not see a version that is assembled ('1.'||4||'.0', format('1.%s.0', 4)), cut out of the column (split_part,
// substr, left) or matched by a pattern that does not spell it (LIKE '1_4%', '^1[.]4[.]', SIMILAR TO): an honest author writes one of
// the spellings above, and the behavioural test of a feature under every integrity-capable schema (TestM9RUniqueLifecycleIsTheSame
// UnderEveryIntegrityCapableSchema) is the control for the rest (independent review IR-08).
var sqlVersionToken = regexp.MustCompile(`['"$^{,]1\\?\.([0-9]+)`)

func knownSchemaVersions() []string {
	return []string{SchemaVersion, RuleSchemaVersion, WorkflowSchemaVersion, ApprovalSchemaVersion, IntegritySchemaVersion, ToolWorkflowSchemaVersion}
}

// schemaFamilies are the version sets that Go treats as one contract: each later member is a superset of the earlier ones.
func schemaFamilies() map[string]func(string) bool {
	return map[string]func(string) bool{"workflow (WorkflowSchema)": WorkflowSchema, "integrity (IntegritySchema)": IntegritySchema}
}

// AuthoringVersionsNamed returns, in release order, the authoring schema versions that the text names.
func AuthoringVersionsNamed(text string) []string {
	named := map[string]bool{}
	for _, m := range sqlVersionToken.FindAllStringSubmatch(text, -1) {
		named["1."+m[1]+".0"] = true
	}
	var out []string
	for _, v := range knownSchemaVersions() {
		if named[v] {
			out = append(out, v)
		}
	}
	return out
}

// AuthoringFamilyGaps reports, for the versions one SQL object names, every family in which it names a version but leaves out a
// LATER member of the same family. A gate on a feature may start at the version that introduced it (the earlier versions lack the
// feature), but every superset contract after that version carries the feature too and has to be named, or its documents fall
// through the gate.
func AuthoringFamilyGaps(named []string) []string {
	have := map[string]bool{}
	for _, v := range named {
		have[v] = true
	}
	var gaps []string
	for family, member := range schemaFamilies() {
		started := false
		var missing []string
		for _, v := range knownSchemaVersions() {
			if !member(v) {
				continue
			}
			if have[v] {
				started = true
			} else if started {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			gaps = append(gaps, family+": names a version of the family but not its later member(s) "+strings.Join(missing, ", "))
		}
	}
	sort.Strings(gaps)
	return gaps
}
