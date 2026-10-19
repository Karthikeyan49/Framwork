//go:build integration

package integration

// Integrity checks for the integrated M10..M13 tree. They exist because the
// branches were developed apart and merged: a candidate built before the DSL
// 2.2.0 runtime-components change must not stay promotable, and every SQL
// function that several migrations replace must keep the guards of the older
// definitions. Everything here is reusable after any further merge.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/db/migrations"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
	"github.com/pressly/goose/v3"
)

// m13StaleSummary prints the staleness fields of a candidate view, never the
// stored artifacts.
func m13StaleSummary(v access.CandidateView) string {
	if v.Stale == nil {
		return "stale=<absent>"
	}
	return fmt.Sprintf("stale=%v reasons=%v", *v.Stale, v.StaleReasons)
}

// TestM13IntegrationRuntimeContractChangeStalesOlderCandidates: a candidate
// whose recorded runtime components differ from the running binary (here the
// workflow DSL list recorded before DSL 2.2.0) is self-consistent, restores and
// verifies, yet must read STALE with a reason, be refused by approve and by
// activate, and a rebuilt candidate must go through approve and activate.
func TestM13IntegrationRuntimeContractChangeStalesOlderCandidates(t *testing.T) {
	e := m13Setup(t)
	m13RequireStack(t, e, true)
	tmpl := e.template()

	// Control: a candidate built by this binary is fresh.
	if v := viewOf(t, e.httpCandidate(e.viewer, tmpl.hash), 200); v.Stale == nil || *v.Stale || len(v.StaleReasons) != 0 {
		t.Fatalf("a candidate built by this binary is not fresh: %s", m13StaleSummary(v))
	}

	// The same candidate as the binary before DSL 2.2.0 recorded it: the
	// components document without "2.2.0", with its own consistent hash.
	var b promotion.BundleV2
	if err := json.Unmarshal(tmpl.raw, &b); err != nil {
		t.Fatal(err)
	}
	older := b.RuntimeContract.Components
	older.WorkflowDSL = append([]string{}, older.WorkflowDSL...)
	if n := len(older.WorkflowDSL); n < 2 || older.WorkflowDSL[n-1] != "2.2.0" {
		t.Fatalf("the current workflow DSL list does not end in 2.2.0: %v", older.WorkflowDSL)
	}
	older.WorkflowDSL = older.WorkflowDSL[:len(older.WorkflowDSL)-1]
	raw, _ := json.Marshal(older)
	sum := sha256.Sum256(raw)
	olderHash, err := e.stageDoctored(func(d *promotion.BundleV2) {
		d.RuntimeContract.Components, d.RuntimeContract.ComponentsHash = older, hex.EncodeToString(sum[:])
	})
	if err != nil {
		t.Fatal("stage the older candidate", err)
	}
	if olderHash == tmpl.hash {
		t.Fatal("the older candidate must be a different artifact")
	}

	// It restores (so it is not candidate_failed_verification) and reads stale.
	v := viewOf(t, e.httpCandidate(e.viewer, olderHash), 200)
	if v.Stale == nil || !*v.Stale || !reflect.DeepEqual(v.StaleReasons, []promotion.StaleReason{"RUNTIME_CHANGED"}) {
		t.Fatalf("an older runtime contract did not read stale: %s", m13StaleSummary(v))
	}

	// Approve is refused with the reason and writes nothing.
	before := e.state()
	r := m13apiExpect(t, "approval of an older runtime contract", e.httpApprove(e.dave.token, olderHash), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "RUNTIME_CHANGED" {
		t.Fatalf("stale reasons at approval: %v", r.Reasons)
	}
	e.unchanged("stale approval", before)

	// An approval recorded by a compromised caller through the raw port does not
	// make it activatable: activation re-evaluates staleness.
	if _, err = e.approvePort(e.dave.subject, olderHash); err != nil {
		t.Fatal("raw approval of the older candidate", err)
	}
	before = e.state()
	r = m13apiExpect(t, "activation of an older runtime contract", e.httpActivate(e.erin.token, olderHash, "", 0), 409, "candidate_stale")
	if strings.Join(r.Reasons, ",") != "RUNTIME_CHANGED" {
		t.Fatalf("stale reasons at activation: %v", r.Reasons)
	}
	e.unchanged("stale activation", before)
	if hash, _, ok := e.activePointer(); ok {
		t.Fatal("the older candidate became active", hash)
	}

	// A rebuilt candidate (new hash, this binary's components) is promotable.
	rebuilt, _ := e.build("520", nil, e.carol)
	if rebuilt == olderHash || rebuilt == tmpl.hash {
		t.Fatal("a rebuild must be a new artifact")
	}
	if v = viewOf(t, e.httpCandidate(e.viewer, rebuilt), 200); v.Stale == nil || *v.Stale {
		t.Fatalf("the rebuilt candidate is stale: %s", m13StaleSummary(v))
	}
	viewOf(t, e.httpApprove(e.dave.token, rebuilt), 201)
	if rel := m13apiRelease(t, e.httpActivate(e.erin.token, rebuilt, "", 0), 200); rel.Generation != 1 {
		t.Fatalf("activation of the rebuilt candidate: %+v", rel)
	}
	if hash, generation, ok := e.activePointer(); !ok || hash != rebuilt || generation != 1 {
		t.Fatal("the rebuilt candidate is not the active release", hash, generation, ok)
	}
	e.checkHistory()
}

// TestM13IntegrationUpgradeFrom48KeepsM12DataAndStillPromotes: the database is migrated to 00048 only, M12
// data is created through the HTTP API of this binary (source revision, frozen sandbox, requirement set,
// acceptance spec), then migrations 49 and later are applied on top of that data. The data must survive
// unchanged, and a candidate built afterwards from those very rows must go through approve and activate.
func TestM13IntegrationUpgradeFrom48KeepsM12DataAndStillPromotes(t *testing.T) {
	m8MigrateTo = 48
	e12 := m12Setup(t)
	m8MigrateTo = 0
	var applied int64
	if err := e12.f.operator.QueryRow(`SELECT max(version_id) FROM goose_db_version`).Scan(&applied); err != nil || applied != 48 {
		t.Fatal("the rehearsal must start at migration 48", applied, err)
	}
	art := e12.prepare("500", 0, 0, e12.alice, e12.bob)
	type snapshot struct{ sets, specs, evidence, versions int }
	take := func() (s snapshot) {
		if err := e12.f.operator.QueryRow(`SELECT (SELECT count(*) FROM control.requirement_sets WHERE tenant_id=$1),(SELECT count(*) FROM control.acceptance_specs WHERE tenant_id=$1),
 (SELECT count(*) FROM runtime.sandbox_evidence WHERE tenant_id=$1),(SELECT count(*) FROM control.metadata_versions WHERE tenant_id=$1)`, e12.f.tenantA).Scan(&s.sets, &s.specs, &s.evidence, &s.versions); err != nil {
			t.Fatal(err)
		}
		return s
	}
	before := take()
	if before.sets != 1 || before.specs != 1 || before.evidence != 1 || before.versions != 1 {
		t.Fatalf("the M12 data was not created: %+v", before)
	}
	var setHash, specHash, evidenceHash string
	if err := e12.f.operator.QueryRow(`SELECT (SELECT body_hash FROM control.requirement_sets WHERE tenant_id=$1),(SELECT body_hash FROM control.acceptance_specs WHERE tenant_id=$1),(SELECT body_hash FROM runtime.sandbox_evidence WHERE tenant_id=$1)`, e12.f.tenantA).Scan(&setHash, &specHash, &evidenceHash); err != nil {
		t.Fatal(err)
	}

	// The upgrade: every migration after 48, on top of the data.
	ctx := context.Background()
	migrator, err := postgres.Open(os.Getenv("MIGRATION_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, migrator, migrations.Files, goose.WithDisableGlobalRegistry(true))
	if err != nil {
		t.Fatal(err)
	}
	results, err := provider.Up(ctx)
	if err != nil || len(results) < 2 || results[0].Source.Version != 49 {
		t.Fatalf("upgrade from 48: %d migrations, first %v, %v", len(results), results, err)
	}
	if after := take(); after != before {
		t.Fatalf("the upgrade changed M12 data: %+v -> %+v", before, after)
	}
	var setHash2, specHash2, evidenceHash2 string
	if err = e12.f.operator.QueryRow(`SELECT (SELECT body_hash FROM control.requirement_sets WHERE tenant_id=$1),(SELECT body_hash FROM control.acceptance_specs WHERE tenant_id=$1),(SELECT body_hash FROM runtime.sandbox_evidence WHERE tenant_id=$1)`, e12.f.tenantA).Scan(&setHash2, &specHash2, &evidenceHash2); err != nil || setHash != setHash2 || specHash != specHash2 || evidenceHash != evidenceHash2 {
		t.Fatal("stored bodies changed", err)
	}

	// The upgraded database promotes: build from the pre-upgrade rows, approve (dave), activate (erin).
	e := &m13Env{m12Env: e12, people: map[string]m13Actor{}}
	e.people["alice"] = m13Actor{"alice", e.f.alice, e.f.aliceSubject, e.alice}
	e.people["bob"] = m13Actor{"bob", e.f.bob, e.f.bobSubject, e.bob}
	e.carol, e.dave, e.erin = e.member("carol", "admin"), e.member("dave", "admin"), e.member("erin", "admin")
	hash := viewOf(t, e.candidate(e.carol.token, art), 201).ArtifactHash
	if v := viewOf(t, e.httpCandidate(e.viewer, hash), 200); v.Stale == nil || *v.Stale {
		t.Fatalf("the candidate built on upgraded data reads stale: %s", m13StaleSummary(v))
	}
	viewOf(t, e.httpApprove(e.dave.token, hash), 201)
	if rel := m13apiRelease(t, e.httpActivate(e.erin.token, hash, "", 0), 200); rel.Generation != 1 {
		t.Fatalf("activation after the upgrade: %+v", rel)
	}
	e.checkHistory()
}

// ------------------------------------------------------------ replaced functions

var (
	functionHeader = regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-z_]+\.[a-z_0-9]+)\s*\(`)
	dollarTag      = regexp.MustCompile(`\$[a-z_]*\$`)
	raiseMessage   = regexp.MustCompile(`(?is)RAISE\s+EXCEPTION\s+'((?:[^']|'')*)'`)
)

// m13FunctionVersions parses the Up sections of the embedded migrations and returns, for every
// schema.name/arity, the RAISE EXCEPTION messages of each definition in migration order.
func m13FunctionVersions(t *testing.T) map[string][]map[string]bool {
	t.Helper()
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	out := map[string][]map[string]bool{}
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
			m := functionHeader.FindStringSubmatchIndex(text[pos:])
			if m == nil {
				break
			}
			start := pos + m[1] // just after the opening parenthesis
			depth, end, args, top := 1, start, 0, false
			for ; end < len(text) && depth > 0; end++ {
				switch text[end] {
				case '(':
					depth++
				case ')':
					depth--
				case ',':
					if depth == 1 {
						args++
					}
				}
				if depth >= 1 && !top && strings.TrimSpace(string(text[end])) != "" && text[end] != ')' {
					top = true
				}
			}
			if top {
				args++
			}
			key := fmt.Sprintf("%s/%d", strings.ToLower(text[pos+m[2]:pos+m[3]]), args)
			rest := text[end:]
			open := dollarTag.FindStringIndex(rest)
			if open == nil {
				pos = end
				continue
			}
			tag := rest[open[0]:open[1]]
			closeAt := strings.Index(rest[open[1]:], tag)
			if closeAt < 0 {
				pos = end
				continue
			}
			body := rest[open[1] : open[1]+closeAt]
			messages := map[string]bool{}
			for _, mm := range raiseMessage.FindAllStringSubmatch(body, -1) {
				messages[mm[1]] = true
			}
			out[key] = append(out[key], messages)
			pos = end + open[1] + closeAt + len(tag)
		}
	}
	return out
}

// m13DroppedMessages are guards (RAISE EXCEPTION messages) that a later definition of the same
// function dropped or reworded ON PURPOSE. Everything else a replaced function ever raised must still be
// raised by its latest definition: a lost guard is a defect. Each entry names the migration that moved it.
var m13DroppedMessages = map[string][]string{
	// 00027 (M9R) consolidates the four checks of 00004/00010 into three reworded errors with the same conditions.
	"runtime.install_tenant_rls/1": {
		"tenant table needs nonnull UUID scope and tenant-inclusive primary/unique keys",
		"tenant foreign keys must include matching tenant scope",
		"existing policies require explicit security review",
		"tenant RLS requires a migration-owned tenant table",
	},
	// 00022 (M9R) moves claiming to the dispatcher role: the error is reworded to "dispatcher required".
	"control.claim_workflow/1": {"worker required"},
	// 00022/00023 (M9R) turn the worker check of the lease probe into a false result (control.worker_principal()).
	"control.check_workflow_lease/5": {"scoped worker required"},
}

// TestM13IntegrationReplacedFunctionsKeepTheirGuards: every SQL function that more than one migration
// defines is, in a freshly migrated database, exactly its highest-numbered definition (no older body
// resurrected), and that definition still raises every error the older ones raised unless the drop is listed.
func TestM13IntegrationReplacedFunctionsKeepTheirGuards(t *testing.T) {
	m8Database(t)
	f := newIAM(t)
	versions := m13FunctionVersions(t)
	checked := 0
	for key, defs := range versions {
		if len(defs) < 2 {
			continue
		}
		name, arity, _ := strings.Cut(key, "/")
		schema, fn, _ := strings.Cut(name, ".")
		latest := defs[len(defs)-1]
		union := map[string]bool{}
		for _, d := range defs {
			for msg := range d {
				union[msg] = true
			}
		}
		if len(union) == 0 {
			continue // SQL-language or guard-free functions: nothing to lose
		}
		rows, err := f.operator.Query(`SELECT pg_get_functiondef(p.oid) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=$1 AND p.proname=$2 AND p.pronargs=$3::int`, schema, fn, arity)
		if err != nil {
			t.Fatal(err)
		}
		var live []string
		for rows.Next() {
			var def string
			if err = rows.Scan(&def); err != nil {
				t.Fatal(err)
			}
			live = append(live, def)
		}
		rows.Close()
		if len(live) != 1 {
			t.Errorf("%s: %d live definitions", key, len(live))
			continue
		}
		checked++
		liveSet := map[string]bool{}
		for _, mm := range raiseMessage.FindAllStringSubmatch(live[0], -1) {
			liveSet[mm[1]] = true
		}
		if !reflect.DeepEqual(liveSet, latest) {
			t.Errorf("%s: the live function raises %v but the highest-numbered definition raises %v", key, sortedKeys(liveSet), sortedKeys(latest))
		}
		dropped := map[string]bool{}
		for _, msg := range m13DroppedMessages[key] {
			dropped[msg] = true
		}
		for msg := range union {
			if !latest[msg] && !dropped[msg] {
				t.Errorf("%s: an older definition raised %q and the latest does not (add it to m13DroppedMessages with the reason if intended)", key, msg)
			}
		}
	}
	if checked < 10 {
		t.Fatalf("only %d replaced functions were compared: the parser lost the migrations", checked)
	}
	t.Logf("compared %d functions that more than one migration defines", checked)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
