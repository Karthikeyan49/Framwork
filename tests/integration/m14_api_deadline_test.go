//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// TestM14APIProviderPhaseOutlivesTheServerWriteTimeout runs the AI route behind a real
// http.Server with the production timeouts (httpserver.New: 10 s read and write). A scripted
// answer that takes 11 s -- longer than the server's write timeout, shorter than the 60 s
// wall clock of the contract -- must still arrive as a stored READY proposal: the route moves
// the write deadline of its own connection for the provider phase (ADR104 A5). The httptest
// recorders of the other tests have no deadlines, so only this test proves the claim.
func TestM14APIProviderPhaseOutlivesTheServerWriteTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("takes 12 s of wall clock")
	}
	e := m14Setup(t, m14Options{})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Slow"), Delay: 11 * time.Second, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "scripted-model-1"})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httpserver.New(listener.Addr().String(), e.h)
	if server.WriteTimeout != 10*time.Second || server.ReadTimeout != 10*time.Second {
		t.Fatalf("the production server timeouts changed (read %v, write %v): re-derive the margins of ADR104 A5", server.ReadTimeout, server.WriteTimeout)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+listener.Addr().String()+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Call the order entity Slow."}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.alice)
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("the slow proposal did not complete behind the production server after %v: %v", time.Since(started), err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	elapsed := time.Since(started)
	if elapsed < 11*time.Second {
		t.Fatalf("answered in %v: the scripted delay was not honoured, so the test proves nothing", elapsed)
	}
	if resp.StatusCode != 201 {
		t.Fatalf("status %d after %v: %s", resp.StatusCode, elapsed, raw)
	}
	var p ai.Proposal
	if err = json.Unmarshal(raw, &p); err != nil || p.ID == "" || p.Status != ai.ProposalReady || p.Attempts != 1 {
		t.Fatalf("proposal %+v (%v) after %v: %s", p, err, elapsed, raw)
	}
	if e.provider.Calls() != 1 {
		t.Fatalf("the provider was called %d times for one request", e.provider.Calls())
	}
	if e.proposalRows() != 1 {
		t.Fatalf("%d proposal rows for one request", e.proposalRows())
	}
	// The connection is reusable afterwards: the next request gets the normal 10 s again.
	again, err := http.NewRequestWithContext(ctx, "GET", "http://"+listener.Addr().String()+e.root+"/ai/proposals/"+p.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	again.Header.Set("Authorization", "Bearer "+e.alice)
	read, err := http.DefaultClient.Do(again)
	if err != nil {
		t.Fatal(err)
	}
	read.Body.Close()
	if read.StatusCode != 200 {
		t.Fatalf("reading the stored proposal: %d", read.StatusCode)
	}
}

// TestM14APIClientGoneDuringProviderPhaseStillRecordsTheOutcome: a client (or a proxy in front of
// the API) that gives up while the provider phase runs must not leave the proposal PENDING or
// call the provider again. The result transaction is detached from the client's cancellation,
// so the row reaches a final state and frees its in-flight slot (ADR104 A5, A8).
func TestM14APIClientGoneDuringProviderPhaseStillRecordsTheOutcome(t *testing.T) {
	e := m14Setup(t, m14Options{limits: ai.ServiceLimits{InFlightPerTenant: 1}})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Abandoned"), Delay: 4 * time.Second, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "scripted-model-1"})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httpserver.New(listener.Addr().String(), e.h)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://"+listener.Addr().String()+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Call the order entity Abandoned."}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+e.alice)
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
		t.Fatalf("the client was expected to give up, got %d", resp.StatusCode)
	}

	var status, code string
	deadline := time.Now().Add(20 * time.Second)
	for {
		if err = e.f.operator.QueryRow(`SELECT status, coalesce(failure_code,'') FROM control.ai_proposals WHERE tenant_id=$1 AND app_id=$2`, e.f.tenantA, e.app).Scan(&status, &code); err != nil {
			t.Fatal(err)
		}
		if status != string(ai.ProposalPending) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the proposal is still PENDING 20 s after the client left; the slot is held until the lease lapses")
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("client gone during the provider phase: the proposal ended %s %s", status, code)
	if status != string(ai.ProposalReady) && status != string(ai.ProposalFailed) {
		t.Fatalf("unexpected final status %s", status)
	}
	if e.proposalRows() != 1 || e.provider.Calls() != 1 {
		t.Fatalf("%d proposal rows and %d provider calls for one request", e.proposalRows(), e.provider.Calls())
	}
	// The slot is free again (limit one in flight) and nothing was applied.
	e.provider.Push(e.answerFor(1, func(doc metadata.Document) []byte { return labelPatch(t, doc, "entity.order", "Next") }))
	if next := e.proposal(e.propose(e.alice, "Rename."), 201); next.Status != ai.ProposalReady {
		t.Fatalf("the next proposal is %s", next.Status)
	}
	if rev, _ := e.heads(); rev != 1 {
		t.Fatalf("the draft moved to revision %d", rev)
	}
}
