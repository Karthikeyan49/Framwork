//go:build integration

package integration

// Regression tests for the independent M12 database-layer review
// (docs/verification/M12_DB_INDEPENDENT_REVIEW.md), findings F2, F3, F4 and F5
// as they stand after migrations 49 and 50. F1 and F6 were closed by those
// migrations and keep their own tests (TestM13Ports*, TestM10R02*). Each test is
// built from the reviewer's probe and asserts the contract of migration 52 and
// the ADR038 amendment; the group is named TestM12R*.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/compiler"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/Karthikeyan49/Framwork/internal/tenant"
)

// m12rDropFixtureGrants revokes the worker grant that the M8 fixture adds for its consumer-effects table.
// That table lives outside the four guarded schemas, so the drift guard does not see it; the helper keeps
// the tests that assert the guard independent of the fixture's own grant.
func m12rDropFixtureGrants(t *testing.T, operator *sql.DB) {
	t.Helper()
	if _, err := operator.Exec(`REVOKE ALL ON m8_fixture.effects FROM framework_worker`); err != nil {
		t.Fatal(err)
	}
}

// m12rReplace replaces the single occurrence of old; the test fails when the
// anchor is missing or ambiguous, so a layout change cannot turn a variant into
// a no-op that would then pass for the wrong reason.
func m12rReplace(t *testing.T, raw []byte, old, replacement string) []byte {
	t.Helper()
	if n := bytes.Count(raw, []byte(old)); n != 1 {
		t.Fatalf("anchor %q occurs %d times in the body", old, n)
	}
	return bytes.Replace(raw, []byte(old), []byte(replacement), 1)
}

// m12rAppend adds members just before the final closing brace.
func m12rAppend(raw []byte, members string) []byte {
	out := append([]byte{}, raw[:len(raw)-1]...)
	return append(append(out, members...), '}')
}

// m12rPrepend adds members right after the opening brace.
func m12rPrepend(raw []byte, members string) []byte {
	return append(append([]byte{'{'}, members...), raw[1:]...)
}

// ---------------------------------------------------------------------- F2

// F2: the stage port reads the jsonb view (duplicate keys: last wins, keys are
// case-sensitive, any number text is accepted) while Go decodes struct fields
// case-insensitively and integers only from integer literals. The index row, and
// every M13 port that reads the stored body, must never rest on bytes the two
// readers disagree about. Every variant below stages today; the port must refuse
// each with 22023 and write nothing.
func TestM12RStagePortRefusesBodiesTheGoDecoderReadsDifferently(t *testing.T) {
	e := m13Setup(t)
	tmpl := e.template()
	raw := tmpl.raw
	var b promotion.BundleV2
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	bob := e.person("bob").id

	// Control: the honest body is a plain replay for its requester.
	var staged bool
	if err := e.portIn(e.carol.subject, e.f.tenantA, &staged, `SELECT control.stage_candidate($1,$2,$3)`, e.app, tmpl.hash, raw); err != nil || staged {
		t.Fatalf("control: replaying the honest candidate returned %v, %v", staged, err)
	}
	count := func() int {
		var n int
		if err := e.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.release_candidates WHERE app_id=$1)+(SELECT count(*) FROM control.release_bundles WHERE app_id=$1)`, e.app).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := count()

	variants := []struct {
		name string
		body []byte
	}{
		{"leading duplicate HighRisk (jsonb: last wins)", m12rPrepend(raw, `"HighRisk":false,`)},
		{"trailing key differing only by case (Go: case-insensitive last wins)", m12rAppend(raw, `,"highrisk":false`)},
		{"trailing requesterid naming another member", m12rAppend(raw, fmt.Sprintf(`,"requesterid":%q`, bob))},
		{"F1 chain: trailing duplicate HighRisk and Risk that index as LOW", m12rAppend(raw, `,"HighRisk":false,"Risk":{"Level":"LOW","PolicyVersion":"release-risk-v1"}`)},
		{"duplicate key spelled with an escape", m12rPrepend(raw, `"HighRisk":true,`)},
		{"BaseGeneration written 0e0 (Go: unmarshal error)", m12rReplace(t, raw, `"BaseGeneration":0,"Source":`, `"BaseGeneration":0e0,"Source":`)},
		{"leading whitespace", append([]byte(" "), raw...)},
		{"trailing newline", append(append([]byte{}, raw...), '\n')},
		{"Source.Revision as a string", m12rReplace(t, raw, fmt.Sprintf(`"Revision":%d,"SchemaVersion"`, b.Source.Revision), fmt.Sprintf(`"Revision":"%d","SchemaVersion"`, b.Source.Revision))},
		{"Requirements.Version as a string", m12rReplace(t, raw, fmt.Sprintf(`"Requirements":{"Version":%d,`, b.Requirements.Version), fmt.Sprintf(`"Requirements":{"Version":"%d",`, b.Requirements.Version))},
		{"Risk.PolicyVersion as a number", m12rReplace(t, raw, fmt.Sprintf(`"Risk":{"PolicyVersion":%q`, b.Risk.PolicyVersion), `"Risk":{"PolicyVersion":1`)},
		{"Risk.PolicyVersion with a bidi override", m12rReplace(t, raw, fmt.Sprintf(`"Risk":{"PolicyVersion":%q`, b.Risk.PolicyVersion), `"Risk":{"PolicyVersion":"release‮risk-v1"`)},
		{"Risk.PolicyVersion with a newline", m12rReplace(t, raw, fmt.Sprintf(`"Risk":{"PolicyVersion":%q`, b.Risk.PolicyVersion), `"Risk":{"PolicyVersion":"release\nrisk-v1"`)},
		{"duplicate key inside Sandbox", m12rReplace(t, raw, fmt.Sprintf(`"Sandbox":{"DatasetID":%q,`, b.Sandbox.DatasetID), fmt.Sprintf(`"Sandbox":{"DatasetID":%q,"DatasetID":%q,`, b.Sandbox.DatasetID, b.Sandbox.DatasetID))},
		{"unknown key inside Source", m12rReplace(t, raw, `"Source":{"Hash":`, `"Source":{"Extra":1,"Hash":`)},
		{"duplicate Storage.class (read by the activation port)", m12rReplace(t, raw, fmt.Sprintf(`"Storage":{"class":%q,`, b.Storage.Class), fmt.Sprintf(`"Storage":{"class":%q,"class":"EQUAL",`, b.Storage.Class))},
		{"lower-case rollback key inside Risk (read by the rollback port)", m12rReplace(t, raw, fmt.Sprintf(`"Risk":{"PolicyVersion":%q,`, b.Risk.PolicyVersion), fmt.Sprintf(`"Risk":{"rollback":"POINTER_SAFE","PolicyVersion":%q,`, b.Risk.PolicyVersion))},
	}
	for _, v := range variants {
		if !json.Valid(v.body) {
			t.Fatalf("%s: the variant is not valid JSON, so it proves nothing", v.name)
		}
		hash, err := e.stageRaw(e.carol, v.body)
		if sqlState(err) != "22023" {
			t.Errorf("%s: SQLSTATE %q (%v), want 22023", v.name, sqlState(err), err)
			if err == nil {
				var level string
				var high bool
				if qerr := e.f.operator.QueryRow(`SELECT risk_level,high_risk FROM control.release_candidates WHERE app_id=$1 AND artifact_hash=$2`, e.app, hash).Scan(&level, &high); qerr == nil {
					t.Logf("%s: the port indexed it as %s high_risk=%v", v.name, level, high)
				}
			}
		}
	}
	if after := count(); after != before {
		t.Errorf("refused bodies wrote rows: %d before, %d after", before, after)
	}
}

// F2 (requirement sets and acceptance specs): the same ports store whatever
// parses as JSON. A duplicate-key body is stored although intent.ParseRequirementSet
// and ParseAcceptanceSpec reject it, poisoning that version for every later reader.
func TestM12RRequirementAndAcceptancePortsRefuseAmbiguousBodies(t *testing.T) {
	e := m12Setup(t)
	e.register(e.alice, "500", 0)
	set := thresholdSet(e.app, "500")
	setRaw, err := set.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	head := func() int64 {
		var v int64
		if err := e.f.operator.QueryRow(`SELECT coalesce(max(version),0) FROM control.requirement_sets WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	appendAs := func(body []byte) error {
		return e.run(e.f.aliceSubject, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
			var v int64
			return q.QueryRowContext(ctx, `SELECT control.append_requirements($1,$2,$3)`, e.app, head(), body).Scan(&v)
		})
	}
	if err = appendAs(setRaw); err != nil {
		t.Fatalf("control: the canonical requirement set was refused: %v", err)
	}
	other := "app.other"
	for _, v := range []struct {
		name string
		body []byte
	}{
		{"duplicate app_id", m12rPrepend(setRaw, fmt.Sprintf(`"app_id":%q,`, other))},
		{"app_id key differing only by case", m12rAppend(setRaw, fmt.Sprintf(`,"App_id":%q`, other))},
		{"duplicate key spelled with an escape", m12rPrepend(setRaw, fmt.Sprintf(`"app_id":%q,`, other))},
		{"unknown top-level key", m12rAppend(setRaw, `,"extra":1`)},
		{"leading whitespace", append([]byte(" "), setRaw...)},
		{"trailing newline", append(append([]byte{}, setRaw...), '\n')},
	} {
		if err := appendAs(v.body); sqlState(err) != "22023" {
			t.Errorf("requirement set, %s: SQLSTATE %q (%v), want 22023", v.name, sqlState(err), err)
		}
	}

	spec := acceptanceFor(set, "500.01")
	specRaw, err := spec.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	authorAs := func(body []byte) error {
		return e.run(e.f.bobSubject, func(ctx context.Context, q postgres.Executor, _ tenant.Access) error {
			var id string
			return q.QueryRowContext(ctx, `SELECT control.author_acceptance($1,$2,$3)::text`, e.app, set.Hash(), body).Scan(&id)
		})
	}
	if err = authorAs(specRaw); err != nil {
		t.Fatalf("control: the canonical acceptance spec was refused: %v", err)
	}
	wrong := strings.Repeat("e", 64)
	for _, v := range []struct {
		name string
		body []byte
	}{
		{"duplicate requirements_hash", m12rPrepend(specRaw, fmt.Sprintf(`"requirements_hash":%q,`, wrong))},
		{"requirements_hash key differing only by case", m12rAppend(specRaw, fmt.Sprintf(`,"Requirements_Hash":%q`, wrong))},
		{"unknown top-level key", m12rAppend(specRaw, `,"extra":1`)},
		{"leading whitespace", append([]byte(" "), specRaw...)},
		{"trailing newline", append(append([]byte{}, specRaw...), '\n')},
	} {
		if err := authorAs(v.body); sqlState(err) != "22023" {
			t.Errorf("acceptance spec, %s: SQLSTATE %q (%v), want 22023", v.name, sqlState(err), err)
		}
	}
}

// The allow-lists in migration 52 are the key sets of the Go types, in both
// directions: a field added to a 2.0.0 struct (which would also break Restore of
// every stored bundle) cannot silently make honest bodies unstageable, and a key
// the Go type does not have cannot be allowed.
func TestM12RBodyKeyListsMatchTheGoStructs(t *testing.T) {
	e := m12Setup(t)
	jsonKeys := func(typ reflect.Type) []string {
		var keys []string
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			keys = append(keys, name)
		}
		sort.Strings(keys)
		return keys
	}
	for _, c := range []struct {
		kind string
		typ  reflect.Type
	}{
		{"bundle", reflect.TypeOf(promotion.BundleV2{})},
		{"source", reflect.TypeOf(promotion.SourceV2{})},
		{"ir", reflect.TypeOf(promotion.IRV2{})},
		{"storage", reflect.TypeOf(compiler.StorageDelta{})},
		{"risk", reflect.TypeOf(promotion.RiskV2{})},
		{"requirements", reflect.TypeOf(promotion.RequirementsV2{})},
		{"acceptance", reflect.TypeOf(promotion.AcceptanceV2{})},
		{"sandbox", reflect.TypeOf(promotion.SandboxV2{})},
		{"verification", reflect.TypeOf(promotion.VerificationV2{})},
		{"requirement_set", reflect.TypeOf(intent.RequirementSet{})},
		{"acceptance_spec", reflect.TypeOf(intent.AcceptanceSpec{})},
	} {
		var got []string
		rows, err := e.f.operator.Query(`SELECT k FROM unnest(control.candidate_body_keys($1)) k ORDER BY k COLLATE "C"`, c.kind)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		for rows.Next() {
			var k string
			if err = rows.Scan(&k); err != nil {
				t.Fatal(err)
			}
			got = append(got, k)
		}
		rows.Close()
		want := jsonKeys(c.typ)
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: SQL key list %v, Go type keys %v", c.kind, got, want)
		}
	}
}

// ---------------------------------------------------------------------- F3

// F3: staging bytes that are already staged is a replay (ADR038 Amendment 8,
// release.candidate_replayed) however far the world has moved since: the row is
// immutable evidence and the staleness of a candidate is derived on read, never
// decided at write. Bytes that were never staged and name a base that is no
// longer current are still refused.
func TestM12RReplayAfterTheBaseMovedIsStillAReplay(t *testing.T) {
	e := m13Setup(t)
	tmpl := e.template()
	second, err := e.stageLevel("LOW")
	if err != nil {
		t.Fatal(err)
	}
	secondBody := e.bundle(second)
	if n, err := e.approvePort(e.dave.subject, tmpl.hash); err != nil || n != 1 {
		t.Fatalf("approve: %d %v", n, err)
	}
	if g, err := e.activatePort(e.dave.subject, tmpl.hash, "", 0); err != nil || g != 1 {
		t.Fatalf("activate: %d %v", g, err)
	}
	rows := func() int {
		var n int
		if err := e.f.operator.QueryRow(`SELECT count(*) FROM control.release_candidates WHERE app_id=$1`, e.app).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := rows()
	for _, c := range []struct {
		name string
		body []byte
	}{
		{"the candidate that became active", tmpl.raw},
		{"a candidate the activation left behind", secondBody},
	} {
		var staged bool
		err := e.portIn(e.carol.subject, e.f.tenantA, &staged, `SELECT control.stage_candidate($1,$2,$3)`, e.app, promotion.Hash(c.body), c.body)
		if err != nil || staged {
			t.Errorf("replay of %s: staged=%v err=%v (SQLSTATE %q), want false and no error", c.name, staged, err, sqlState(err))
		}
	}
	if after := rows(); after != before {
		t.Errorf("replays changed the candidate index: %d rows before, %d after", before, after)
	}
	// A body that was never staged and claims the old base is still stale.
	_, err = e.stageDoctored(func(b *promotion.BundleV2) {
		b.BaseRelease, b.BaseGeneration = "", 0
		b.Risk.Reasons = append(append([]string{}, b.Risk.Reasons...), "m12r-fresh-and-stale")
	})
	if sqlState(err) != "55000" {
		t.Errorf("a never-staged body on a moved base: SQLSTATE %q (%v), want 55000", sqlState(err), err)
	}
	// A replay by someone other than the recorded requester is still refused.
	var staged bool
	err = e.portIn(e.person("bob").subject, e.f.tenantA, &staged, `SELECT control.stage_candidate($1,$2,$3)`, e.app, tmpl.hash, tmpl.raw)
	if sqlState(err) != "22023" {
		t.Errorf("replay by another member: SQLSTATE %q (%v), want 22023", sqlState(err), err)
	}
}

// ---------------------------------------------------------------------- F4

// F4: the stage port binds the hashes of the requirement set, acceptance spec and
// source revision to their stored rows, but not the documents the bundle embeds.
// A body that keeps the hashes and swaps the embedded document is a lie that only
// Restore catches. The embedded document must be the stored one (compared as JSON
// values: the M13 fixtures re-encode bodies through generic maps).
func TestM12REmbeddedDocumentsAreTheStoredRows(t *testing.T) {
	e := m13Setup(t)
	tmpl := e.template()
	var b promotion.BundleV2
	if err := json.Unmarshal(tmpl.raw, &b); err != nil {
		t.Fatal(err)
	}
	// Precondition on the honest fixture: each embedded document equals its row.
	var reqEq, accEq, srcEq bool
	if err := e.f.operator.QueryRow(`SELECT x.body->'Requirements'->'JSON'=convert_from(r.body,'UTF8')::jsonb,
 x.body->'Acceptance'->'JSON'=convert_from(a.body,'UTF8')::jsonb,
 x.body->'Source'->'JSON'=v.document
 FROM (SELECT convert_from(body,'UTF8')::jsonb body FROM control.release_bundles WHERE tenant_id=$1 AND app_id=$2 AND artifact_hash=$3) x,
 control.requirement_sets r, control.acceptance_specs a, control.metadata_versions v
 WHERE r.tenant_id=$1 AND r.app_id=$2 AND r.version=$4 AND a.tenant_id=$1 AND a.app_id=$2 AND a.id=$5::uuid AND v.tenant_id=$1 AND v.app_id=$2 AND v.revision=$6`,
		e.f.tenantA, e.app, tmpl.hash, b.Requirements.Version, b.Acceptance.ID, b.Source.Revision).Scan(&reqEq, &accEq, &srcEq); err != nil {
		t.Fatal(err)
	}
	if !reqEq || !accEq || !srcEq {
		t.Fatalf("precondition: an honest candidate must embed the stored rows (requirements %v, acceptance %v, source %v)", reqEq, accEq, srcEq)
	}

	otherSet, err := thresholdSet(e.app, "501").Canonical()
	if err != nil {
		t.Fatal(err)
	}
	otherSpec, err := acceptanceFor(thresholdSet(e.app, "500"), "700.01").Canonical()
	if err != nil {
		t.Fatal(err)
	}
	otherSource, err := json.Marshal(orderDocument(e.app, "999"))
	if err != nil {
		t.Fatal(err)
	}
	swap := func(mutate func(*promotion.BundleV2)) []byte {
		doctored := b
		mutate(&doctored)
		body, err := json.Marshal(doctored)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	for _, v := range []struct {
		name string
		body []byte
	}{
		{"another requirement set under the stored hash", swap(func(x *promotion.BundleV2) { x.Requirements.JSON = otherSet })},
		{"another acceptance spec under the stored hash", swap(func(x *promotion.BundleV2) { x.Acceptance.JSON = otherSpec })},
		{"another source document under the stored hash", swap(func(x *promotion.BundleV2) { x.Source.JSON = otherSource })},
	} {
		if _, err := e.stageRaw(e.carol, v.body); sqlState(err) != "55000" {
			t.Errorf("%s: SQLSTATE %q (%v), want 55000", v.name, sqlState(err), err)
		}
	}

	// The same documents re-encoded (sorted keys, different whitespace) are still
	// the stored rows and stage: the M13 fixtures rely on exactly this.
	reencode := func(raw json.RawMessage) json.RawMessage {
		var generic any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&generic); err != nil {
			t.Fatal(err)
		}
		out, err := json.MarshalIndent(generic, "", " ")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	same := swap(func(x *promotion.BundleV2) {
		x.Requirements.JSON, x.Acceptance.JSON, x.Source.JSON = reencode(x.Requirements.JSON), reencode(x.Acceptance.JSON), reencode(x.Source.JSON)
	})
	if bytes.Equal(same, tmpl.raw) {
		t.Fatal("the re-encoded control did not change a byte")
	}
	if _, err := e.stageRaw(e.carol, same); err != nil {
		t.Errorf("the stored documents re-encoded as JSON values must stage: %v", err)
	}
}

// ---------------------------------------------------------------------- F5

// F5: control.worker_grants_safe() must trip on any privilege the worker holds
// beyond what the migrations gave it, on every relation of the four schemas, in
// every form (table and column, including REFERENCES and TRIGGER), and on object
// creation in those schemas. Several of these relations were not listed at all.
func TestM12RWorkerGuardTripsOnEveryExtraGrant(t *testing.T) {
	e := m12Setup(t)
	m12rDropFixtureGrants(t, e.f.operator)
	worker := portsRole(t, "WORKER_DATABASE_URL")
	safe := func() bool {
		var ok bool
		if err := worker.QueryRow(`SELECT control.worker_grants_safe()`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !safe() {
		t.Fatal("the migrated worker must start safe")
	}
	// Holding a privilege the migrations already gave is not drift.
	for _, again := range []string{`GRANT INSERT(state) ON audit.entries TO framework_worker`, `GRANT SELECT ON runtime.workflow_bindings TO framework_worker`} {
		if _, err := e.f.operator.Exec(again); err != nil {
			t.Fatal(err)
		}
		if !safe() {
			t.Errorf("the guard tripped on a grant the worker already holds: %s", again)
		}
	}
	for _, c := range []struct{ grant, revoke string }{
		{`GRANT SELECT ON runtime.preview_records TO framework_worker`, `REVOKE SELECT ON runtime.preview_records FROM framework_worker`},
		{`GRANT SELECT(tenant_id) ON runtime.preview_datasets TO framework_worker`, `REVOKE SELECT(tenant_id) ON runtime.preview_datasets FROM framework_worker`},
		{`GRANT SELECT ON control.workflow_jobs TO framework_worker`, `REVOKE SELECT ON control.workflow_jobs FROM framework_worker`},
		{`GRANT UPDATE ON control.workflow_attempts TO framework_worker`, `REVOKE UPDATE ON control.workflow_attempts FROM framework_worker`},
		{`GRANT SELECT ON control.event_deliveries TO framework_worker`, `REVOKE SELECT ON control.event_deliveries FROM framework_worker`},
		{`GRANT INSERT ON runtime.event_sequences TO framework_worker`, `REVOKE INSERT ON runtime.event_sequences FROM framework_worker`},
		{`GRANT SELECT ON audit.entries TO framework_worker`, `REVOKE SELECT ON audit.entries FROM framework_worker`},
		{`GRANT UPDATE(state) ON audit.entries TO framework_worker`, `REVOKE UPDATE(state) ON audit.entries FROM framework_worker`},
		{`GRANT REFERENCES(tenant_id) ON audit.entries TO framework_worker`, `REVOKE REFERENCES(tenant_id) ON audit.entries FROM framework_worker`},
		{`GRANT TRIGGER ON runtime.workflow_instances TO framework_worker`, `REVOKE TRIGGER ON runtime.workflow_instances FROM framework_worker`},
		{`GRANT TRUNCATE ON runtime.workflow_history TO framework_worker`, `REVOKE TRUNCATE ON runtime.workflow_history FROM framework_worker`},
		{`GRANT DELETE ON runtime.event_inbox TO framework_worker`, `REVOKE DELETE ON runtime.event_inbox FROM framework_worker`},
		{`GRANT INSERT ON runtime.workflow_signals TO framework_worker`, `REVOKE INSERT ON runtime.workflow_signals FROM framework_worker`},
		{`GRANT UPDATE(attempts) ON runtime.tool_invocations TO framework_worker`, `REVOKE UPDATE(attempts) ON runtime.tool_invocations FROM framework_worker`},
		{`GRANT UPDATE(deadline) ON runtime.workflow_instances TO framework_worker`, `REVOKE UPDATE(deadline) ON runtime.workflow_instances FROM framework_worker`},
		{`GRANT SELECT ON iam.roles TO framework_worker`, `REVOKE SELECT ON iam.roles FROM framework_worker`},
		{`GRANT INSERT ON iam.memberships TO framework_worker`, `REVOKE INSERT ON iam.memberships FROM framework_worker`},
		{`GRANT CREATE ON SCHEMA runtime TO framework_worker`, `REVOKE CREATE ON SCHEMA runtime FROM framework_worker`},
		{`GRANT CREATE ON SCHEMA control TO framework_worker`, `REVOKE CREATE ON SCHEMA control FROM framework_worker`},
	} {
		if _, err := e.f.operator.Exec(c.grant); err != nil {
			t.Fatalf("%s: %v", c.grant, err)
		}
		tripped := !safe()
		if _, err := e.f.operator.Exec(c.revoke); err != nil {
			t.Fatalf("%s: %v", c.revoke, err)
		}
		if !tripped {
			t.Errorf("the worker guard did not trip on: %s", c.grant)
		}
		if !safe() {
			t.Fatalf("the worker guard stayed tripped after: %s", c.revoke)
		}
	}
}

// The guard classifies every relation of the four schemas: either the worker
// must hold nothing on it, or its exact grants are listed. A table added by a
// later migration without a classification fails this test, so the list cannot
// rot; and the listed grants equal what the migrations really gave the worker.
func TestM12RWorkerGuardClassifiesEveryRelation(t *testing.T) {
	e := m12Setup(t)
	m12rDropFixtureGrants(t, e.f.operator)
	rows, err := e.f.operator.Query(`SELECT n.nspname||'.'||c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname IN('control','runtime','iam','audit') AND c.relkind IN('r','p','v','m','f')
 AND NOT (n.nspname||'.'||c.relname)=ANY(control.worker_guard_denied())
 AND NOT EXISTS(SELECT 1 FROM control.worker_guard_granted() g WHERE g.rel=n.nspname||'.'||c.relname) ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	var unclassified []string
	for rows.Next() {
		var rel string
		if err = rows.Scan(&rel); err != nil {
			t.Fatal(err)
		}
		unclassified = append(unclassified, rel)
	}
	rows.Close()
	if len(unclassified) != 0 {
		t.Errorf("relations the worker guard does not classify (add each to worker_guard_denied or worker_guard_granted): %v", unclassified)
	}
	// Both lists name only relations that exist (a typo would silently guard nothing).
	rows, err = e.f.operator.Query(`SELECT r FROM (SELECT unnest(control.worker_guard_denied()) r UNION SELECT rel FROM control.worker_guard_granted()) x
 WHERE to_regclass(r) IS NULL ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var rel string
		if err = rows.Scan(&rel); err != nil {
			t.Fatal(err)
		}
		t.Errorf("the worker guard names %s, which does not exist", rel)
	}
	rows.Close()
	// The listed grants are exactly the worker's real direct grants.
	rows, err = e.f.operator.Query(`WITH actual AS (
  SELECT n.nspname||'.'||c.relname rel,a.privilege_type priv,NULL::text col FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace CROSS JOIN LATERAL aclexplode(c.relacl) a
   WHERE n.nspname IN('control','runtime','iam','audit') AND a.grantee='framework_worker'::regrole
  UNION ALL
  SELECT n.nspname||'.'||c.relname,a.privilege_type,t.attname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute t ON t.attrelid=c.oid AND t.attnum>0 AND NOT t.attisdropped
   CROSS JOIN LATERAL aclexplode(t.attacl) a WHERE n.nspname IN('control','runtime','iam','audit') AND a.grantee='framework_worker'::regrole),
 listed AS (SELECT rel,priv,col FROM control.worker_guard_granted())
 SELECT 'listed but not granted',rel,priv,coalesce(col,'') FROM (SELECT * FROM listed EXCEPT SELECT * FROM actual) x
 UNION ALL SELECT 'granted but not listed',rel,priv,coalesce(col,'') FROM (SELECT * FROM actual EXCEPT SELECT * FROM listed) y ORDER BY 1,2,3,4`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var kind, rel, priv, col string
		if err = rows.Scan(&kind, &rel, &priv, &col); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s: %s %s %s", kind, rel, priv, col)
	}
	rows.Close()
}
