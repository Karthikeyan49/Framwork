//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/runtime/data"
	"github.com/jackc/pgx/v5/pgconn"
)

func m11Create(t *testing.T, h http.Handler, root, token string, witness map[string]any, ceiling string, want int) data.Dataset {
	t.Helper()
	body := map[string]any{}
	for k, v := range witness {
		body[k] = v
	}
	body["workspace_id"], body["tool_ceiling"] = uuid(t), ceiling
	w := m5Call(h, root+"/sandboxes", token, body)
	if w.Code != want {
		t.Fatal("create sandbox", w.Code, w.Body.String())
	}
	var d data.Dataset
	if want == 200 && (json.Unmarshal(w.Body.Bytes(), &d) != nil || d.ID == "" || d.Environment != "sandbox" || d.Sandbox == nil) {
		t.Fatal("sandbox identity", w.Body.String())
	}
	return d
}

func TestM11SandboxIsolationPolicyFreezeAndEvidence(t *testing.T) {
	m8Database(t)
	f, h, base, alice, app := m5Fixture(t)
	root := "/v1/tenants/" + f.tenantA + "/preview/" + app
	viewer := f.token(t, f.viewerSubject, nil)
	bob := f.token(t, f.bobSubject, nil)
	witness := m9rDatasetWitness(t, h, root+"/versions/1", alice)

	// A preview record exists; a sandbox must never see or adopt it.
	preview := m5Create(t, h, base, alice, "Preview only")

	m11Create(t, h, root, viewer, witness, "BLOCK", 403)
	m11Create(t, h, "/v1/tenants/"+f.tenantB+"/preview/"+app, alice, witness, "BLOCK", 403)
	m11Create(t, h, root, alice, witness, "LIVE", 400)
	m11Create(t, h, root, alice, witness, "", 400)
	// A request cannot choose adoption, environment or credentials.
	bad := map[string]any{"workspace_id": uuid(t), "tool_ceiling": "BLOCK", "mode": "ADOPT", "environment": "preview", "production_credentials_mounted": true}
	for k, v := range witness {
		bad[k] = v
	}
	if w := m5Call(h, root+"/sandboxes", alice, bad); w.Code != 400 {
		t.Fatal("unknown request fields accepted", w.Code)
	}

	box := m11Create(t, h, root, alice, witness, "REROUTE", 200)
	if box.State != "OPEN" || box.Sandbox.DataMode != "SYNTHETIC_SEPARATE" || box.Sandbox.ToolCeiling != "REROUTE" {
		t.Fatal("sandbox posture", box)
	}
	// Exact retry of the workspace key returns the same sandbox; a different
	// ceiling for that key conflicts instead of silently changing the posture.
	again := map[string]any{"workspace_id": "", "tool_ceiling": "REROUTE"}
	for k, v := range witness {
		again[k] = v
	}
	var workspace string
	if err := f.operator.QueryRow(`SELECT workspace_id::text FROM runtime.datasets WHERE tenant_id=$1 AND id=$2`, f.tenantA, box.ID).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	again["workspace_id"] = workspace
	if w := m5Call(h, root+"/sandboxes", alice, again); w.Code != 200 {
		t.Fatal("create replay", w.Code, w.Body.String())
	}
	again["tool_ceiling"] = "BLOCK"
	if w := m5Call(h, root+"/sandboxes", alice, again); w.Code != 409 {
		t.Fatal("ceiling change accepted", w.Code)
	}

	// Reads: any member may read posture; other tenants and bad ids cannot.
	if w := m3Call(h, "GET", root+"/sandboxes/"+box.ID, viewer, 0, nil); w.Code != 200 {
		t.Fatal("sandbox read", w.Code)
	}
	if w := m3Call(h, "GET", "/v1/tenants/"+f.tenantB+"/preview/"+app+"/sandboxes/"+box.ID, bob, 0, nil); w.Code != 404 {
		t.Fatal("foreign tenant sandbox disclosed", w.Code)
	}
	// A preview dataset is not a sandbox, even on the sandbox route.
	preset := m9rDatasetCreate(t, h, root+"/datasets", alice, "EMPTY", witness)
	if w := m3Call(h, "GET", root+"/sandboxes/"+preset.ID, alice, 0, nil); w.Code != 404 {
		t.Fatal("preview dataset served as sandbox", w.Code)
	}

	// Data isolation: sandbox is empty, writable through the governed command
	// route, and its records never appear in the preview namespace.
	path := root + "/datasets/" + box.ID + "/bindings/1/entities/entity.contact"
	page := m5Page(t, m5Call(h, path+"/query", alice, m5Query()))
	if len(page.Records) != 0 {
		t.Fatal("sandbox adopted preview records", len(page.Records))
	}
	seeded := m5Call(h, path+"/commands", alice, data.Command{Operation: "create", Values: m5Values("Synthetic one")})
	if seeded.Code != 200 {
		t.Fatal("synthetic seed", seeded.Code, seeded.Body.String())
	}
	var record data.Record
	_ = json.Unmarshal(seeded.Body.Bytes(), &record)
	if page = m5Page(t, m5Call(h, base+"/query", alice, m5Query())); len(page.Records) != 1 || page.Records[0].ID != preview.ID {
		t.Fatal("sandbox write leaked into preview", len(page.Records))
	}
	if m5Call(h, path+"/commands", viewer, data.Command{Operation: "create", Values: m5Values("viewer")}).Code != 403 {
		t.Fatal("viewer seeded sandbox")
	}

	// Database invariants: no direct promotion paths exist for the runtime role.
	var pe *pgconn.PgError
	if _, err := f.db.Exec(`INSERT INTO runtime.sandboxes(tenant_id,app_id,dataset_id,data_mode,tool_ceiling) VALUES($1,$2,$3,'SYNTHETIC_SEPARATE','TEST_ACCOUNT')`, f.tenantA, app, preset.ID); err == nil {
		t.Fatal("runtime inserted a sandbox directly")
	}
	for _, bad := range []string{`UPDATE runtime.sandboxes SET tool_ceiling='TEST_ACCOUNT' WHERE dataset_id=$1`, `DELETE FROM runtime.sandboxes WHERE dataset_id=$1`} {
		if _, err := f.operator.Exec(bad, box.ID); err == nil || !errors.As(err, &pe) {
			t.Fatal("operator mutated sandbox identity", bad, err)
		}
	}
	// A sandbox dataset cannot be created non-empty or without registration.
	tx, err := f.operator.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`INSERT INTO runtime.datasets(tenant_id,app_id,id,workspace_id,anchor_revision,anchor_hash,schema_hash,state,environment) SELECT tenant_id,app_id,gen_random_uuid(),gen_random_uuid(),anchor_revision,anchor_hash,schema_hash,'ADOPTING','sandbox' FROM runtime.datasets WHERE id=$1`, preset.ID)
	_ = tx.Rollback()
	if err == nil {
		t.Fatal("adopting sandbox dataset admitted")
	}

	// Freeze: owner/admin only; immutable evidence; writes and workflows sealed.
	if w := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", viewer, struct{}{}); w.Code != 403 {
		t.Fatal("viewer froze sandbox", w.Code)
	}
	w := m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{})
	if w.Code != 200 {
		t.Fatal("freeze", w.Code, w.Body.String())
	}
	var ev data.SandboxEvidence
	if json.Unmarshal(w.Body.Bytes(), &ev) != nil || len(ev.Hash) != 64 {
		t.Fatal("evidence contract")
	}
	for _, want := range []string{`"tool_ceiling": "REROUTE"`, `"data_mode": "SYNTHETIC_SEPARATE"`, `"count": 1`, `"schema_version": "sandbox-evidence-v1"`} {
		if !strings.Contains(string(ev.Body), want) {
			t.Fatal("evidence body missing", want, string(ev.Body))
		}
	}
	if w = m5Call(h, root+"/sandboxes/"+box.ID+"/freeze", alice, struct{}{}); w.Code != 409 {
		t.Fatal("double freeze", w.Code)
	}
	if w = m5Call(h, path+"/commands", alice, data.Command{Operation: "create", Values: m5Values("after freeze")}); w.Code != 409 {
		t.Fatal("frozen sandbox accepted a write", w.Code)
	}
	if got := m5Page(t, m5Call(h, path+"/query", alice, m5Query())); len(got.Records) != 1 || got.Records[0].ID != record.ID {
		t.Fatal("frozen sandbox lost evidence data")
	}
	read := m3Call(h, "GET", root+"/sandboxes/"+box.ID+"/evidence", alice, 0, nil)
	var stored data.SandboxEvidence
	if read.Code != 200 || json.Unmarshal(read.Body.Bytes(), &stored) != nil || stored.Hash != ev.Hash {
		t.Fatal("stored evidence differs", read.Code)
	}
	if _, err := f.operator.Exec(`UPDATE runtime.sandbox_evidence SET frozen_by=frozen_by WHERE dataset_id=$1`, box.ID); err == nil {
		t.Fatal("evidence mutated")
	}
	if _, err := f.operator.Exec(`UPDATE runtime.datasets SET state='OPEN',epoch=epoch+1 WHERE id=$1`, box.ID); err == nil {
		t.Fatal("frozen sandbox reopened")
	}
	var frozen data.Dataset
	if g := m3Call(h, "GET", root+"/sandboxes/"+box.ID, alice, 0, nil); g.Code != 200 || json.Unmarshal(g.Body.Bytes(), &frozen) != nil || frozen.Sandbox == nil || frozen.Sandbox.State != "FROZEN" || frozen.Sandbox.EvidenceHash != ev.Hash || frozen.State != "FROZEN" {
		t.Fatal("frozen posture", g.Code, g.Body.String())
	}
}
