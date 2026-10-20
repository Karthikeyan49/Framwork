package metadata

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	migrations "github.com/Karthikeyan49/Framwork/db/migrations"
)

// An authoring schema version is named in three places: Go (WorkflowSchema, IntegritySchema, the compiler gates), the
// metadata_versions CHECK and SQL function bodies. 1.5.0 was added as a superset of 1.4.0 and one SQL function that selected a
// document with schema_version='1.4.0' was missed, so no unique claim could be registered under 1.5.0 (remediation review B-05,
// independent review IR-01). These tests read the embedded migrations and fail when a SQL gate names only part of a family of
// versions, or when the CHECK drops a version, so the next superset contract cannot repeat it.

var (
	sqlFunctionHeader = regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-z_]+\.[a-z_0-9]+)\s*\(([^)]*)\)`)
	sqlDollarTag      = regexp.MustCompile(`\$[a-z_]*\$`)
	sqlVersionLiteral = regexp.MustCompile(`'(1\.[0-9]+\.0)'`)
	sqlVersionsCheck  = regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+metadata_versions_schema_version_check\s+CHECK\s*\(\s*schema_version\s+IN\s*\(([^)]*)\)\s*\)`)
)

func knownSchemaVersions() []string {
	return []string{SchemaVersion, RuleSchemaVersion, WorkflowSchemaVersion, ApprovalSchemaVersion, IntegritySchemaVersion, ToolWorkflowSchemaVersion}
}

// schemaFamilies are the version sets that Go treats as one contract: each later member is a superset of the earlier ones.
func schemaFamilies() map[string]func(string) bool {
	return map[string]func(string) bool{"workflow (WorkflowSchema)": WorkflowSchema, "integrity (IntegritySchema)": IntegritySchema}
}

// familyGaps reports, for one SQL body, every family in which the body names a version but leaves out a LATER member of the same
// family. A gate on a feature may start at the version that introduced it (the earlier versions lack the feature), but every
// superset contract after that version carries the feature too and has to be named, or its documents fall through the gate.
func familyGaps(body string) []string {
	named := map[string]bool{}
	for _, m := range sqlVersionLiteral.FindAllStringSubmatch(body, -1) {
		named[m[1]] = true
	}
	var gaps []string
	for family, member := range schemaFamilies() {
		started := false
		var missing []string
		for _, v := range knownSchemaVersions() {
			if !member(v) {
				continue
			}
			if named[v] {
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

// latestFunctionBodies returns, for every function the migrations define, the dollar-quoted body of its LAST definition in
// migration order (a later CREATE OR REPLACE wins, exactly as in the database) and the file that holds it.
func latestFunctionBodies(t *testing.T) (map[string]string, map[string]string) {
	t.Helper()
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	bodies, files := map[string]string{}, map[string]string{}
	for _, name := range names {
		raw, err := migrations.Files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if i := strings.Index(text, "-- +goose Down"); i >= 0 {
			text = text[:i]
		}
		for pos := 0; ; {
			m := sqlFunctionHeader.FindStringSubmatchIndex(text[pos:])
			if m == nil {
				break
			}
			key := strings.ToLower(text[pos+m[2]:pos+m[3]]) + "(" + strings.Join(strings.Fields(text[pos+m[4]:pos+m[5]]), " ") + ")"
			rest := text[pos+m[1]:]
			open := sqlDollarTag.FindStringIndex(rest)
			if open == nil {
				pos += m[1]
				continue
			}
			tag := rest[open[0]:open[1]]
			closeAt := strings.Index(rest[open[1]:], tag)
			if closeAt < 0 {
				pos += m[1]
				continue
			}
			bodies[key], files[key] = rest[open[1]:open[1]+closeAt], name
			pos += m[1] + open[1] + closeAt + len(tag)
		}
	}
	return bodies, files
}

func TestSQLFunctionsNameWholeSchemaVersionFamilies(t *testing.T) {
	bodies, files := latestFunctionBodies(t)
	if len(bodies) < 100 {
		t.Fatalf("parsed only %d functions from the migrations", len(bodies))
	}
	checked := 0
	for key, body := range bodies {
		if !sqlVersionLiteral.MatchString(body) {
			continue
		}
		checked++
		for _, gap := range familyGaps(body) {
			t.Errorf("%s (%s) gates on an authoring schema version: %s", key, files[key], gap)
		}
	}
	// runtime.register_unique_constraint is the function that gated on 1.4.0 alone; it must be among the checked ones.
	if body := bodies["runtime.register_unique_constraint(t uuid,a text,v bigint,h text,e text,f text,schema text,sv bigint,sh text,expected bigint)"]; !strings.Contains(body, "'1.5.0'") {
		t.Fatal("runtime.register_unique_constraint does not accept schema 1.5.0")
	}
	if checked == 0 {
		t.Fatal("no function names an authoring schema version: the scan is vacuous")
	}
}

// The checker has to see the original defect: a body that selects the document with the single literal 1.4.0.
func TestFamilyGapsSeesTheOriginalDefect(t *testing.T) {
	gaps := familyGaps(`WHERE m.schema_version='1.4.0' AND item->>'kind'='field'`)
	if len(gaps) == 0 {
		t.Fatal("a body naming only 1.4.0 must be reported")
	}
	for _, g := range gaps {
		if !strings.Contains(g, "1.5.0") {
			t.Fatalf("the report must name the missing superset 1.5.0: %v", gaps)
		}
	}
	for name, body := range map[string]string{
		"the whole integrity family": `WHERE m.schema_version IN('1.4.0','1.5.0')`,
		"the whole workflow family":  `WHERE m.schema_version IN('1.2.0','1.3.0','1.4.0','1.5.0')`,
		"only the newest contract":   `WHERE m.schema_version='1.5.0'`,
		"an unrelated event version": `jsonb_build_object('schema_version','1.0.0')`,
	} {
		if gaps := familyGaps(body); len(gaps) != 0 {
			t.Fatalf("%s was reported: %v", name, gaps)
		}
	}
}

// Each migration that rewrites the CHECK installs a complete list; the last one wins, so it must list every known version.
func TestMetadataVersionsCheckListsEveryKnownVersion(t *testing.T) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var last []string
	var lastFile string
	for _, name := range names {
		raw, _ := migrations.Files.ReadFile(name)
		text := string(raw)
		if i := strings.Index(text, "-- +goose Down"); i >= 0 {
			text = text[:i]
		}
		if m := sqlVersionsCheck.FindStringSubmatch(text); m != nil {
			last, lastFile = nil, name
			for _, lit := range sqlVersionLiteral.FindAllStringSubmatch(m[1], -1) {
				last = append(last, lit[1])
			}
		}
	}
	want := knownSchemaVersions()
	sort.Strings(want)
	sort.Strings(last)
	if strings.Join(last, ",") != strings.Join(want, ",") {
		t.Fatalf("the last rewrite of metadata_versions_schema_version_check (%s) lists %v, the validator knows %v", lastFile, last, want)
	}
}
