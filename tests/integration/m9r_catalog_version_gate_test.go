//go:build integration

package integration

import (
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/metadata"
)

// TestM9RNoCatalogObjectGatesOnPartOfTheAuthoringFamily reads the definitions the migrated database actually holds, of every kind
// of object that can test a document's schema_version: functions, CHECK constraints, row-level policies, triggers (their WHEN
// conditions), column defaults and generated columns, views, and indexes (expressions and predicates). Each definition is read for
// the authoring versions it names, and one that starts a family of versions (1.2.0 onwards, 1.4.0 onwards) and leaves out a later
// member is a gate that the next superset contract falls through (remediation review B-05, independent review IR-01 and F-03).
// The static test over the migrations reads only function bodies; this one covers the rest, and the live text instead of the
// files, so a later migration that replaced an object is what is judged.
func TestM9RNoCatalogObjectGatesOnPartOfTheAuthoringFamily(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	const userSchemas = `n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg\_%'`
	rows, err := f.operator.Query(`
SELECT 'function', n.nspname||'.'||p.proname||'('||pg_get_function_identity_arguments(p.oid)||')', pg_get_functiondef(p.oid)
  FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE ` + userSchemas + ` AND p.prokind IN ('f','p')
UNION ALL
SELECT 'constraint', coalesce(NULLIF(c.conrelid::regclass::text,'-'),'domain '||c.contypid::regtype::text)||'.'||c.conname, pg_get_constraintdef(c.oid)
  FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE ` + userSchemas + ` AND c.contype IN ('c','x')
UNION ALL
SELECT 'policy', pol.polrelid::regclass::text||'.'||pol.polname, coalesce(pg_get_expr(pol.polqual,pol.polrelid),'')||' '||coalesce(pg_get_expr(pol.polwithcheck,pol.polrelid),'')
  FROM pg_policy pol JOIN pg_class c ON c.oid=pol.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas + `
UNION ALL
SELECT 'trigger', t.tgrelid::regclass::text||'.'||t.tgname, pg_get_triggerdef(t.oid)
  FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE NOT t.tgisinternal AND ` + userSchemas + `
UNION ALL
SELECT 'default', d.adrelid::regclass::text||'.'||a.attname, pg_get_expr(d.adbin,d.adrelid)
  FROM pg_attrdef d JOIN pg_attribute a ON a.attrelid=d.adrelid AND a.attnum=d.adnum JOIN pg_class c ON c.oid=d.adrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas + `
UNION ALL
SELECT 'view', c.oid::regclass::text, pg_get_viewdef(c.oid)
  FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('v','m') AND ` + userSchemas + `
UNION ALL
SELECT 'index', i.indexrelid::regclass::text, pg_get_indexdef(i.indexrelid)
  FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE ` + userSchemas)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	scanned, byKind := 0, map[string]int{}
	named := map[string][]string{}
	for rows.Next() {
		var kind, name, definition string
		if err = rows.Scan(&kind, &name, &definition); err != nil {
			t.Fatal(err)
		}
		scanned++
		byKind[kind]++
		versions := metadata.AuthoringVersionsNamed(definition)
		if len(versions) == 0 {
			continue
		}
		named[kind+" "+name] = versions
		for _, gap := range metadata.AuthoringFamilyGaps(versions) {
			t.Errorf("%s %s gates on an authoring schema version: %s (it names %s)", kind, name, gap, strings.Join(versions, ", "))
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("scanned %d definitions %v; %d name an authoring version", scanned, byKind, len(named))
	for object, versions := range named {
		t.Logf("  %s names %s", object, strings.Join(versions, ", "))
	}
	// The scan must see what it is for: the function that gated on 1.4.0 alone (now both), and the CHECK that lists every version.
	if v := named["function runtime.register_unique_constraint(t uuid, a text, v bigint, h text, e text, f text, schema text, sv bigint, sh text, expected bigint)"]; len(v) < 2 {
		t.Errorf("runtime.register_unique_constraint names %v: the scan is blind to it, or it lost 1.5.0", v)
	}
	if v := named["constraint control.metadata_versions.metadata_versions_schema_version_check"]; len(v) != 6 {
		t.Errorf("the metadata_versions CHECK names %v, want all six versions", v)
	}
	for _, kind := range []string{"function", "constraint", "policy", "trigger", "default", "view", "index"} {
		if byKind[kind] == 0 {
			t.Errorf("no %s definition was read: the scan of that kind is vacuous", kind)
		}
	}
}
