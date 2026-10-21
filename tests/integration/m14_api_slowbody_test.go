//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

// TestM14SlowRequestBodiesDoNotHoldDatabaseSlots is the regression test of independent review IR-02. The handlers read their
// request body inside the database admission slot, so a client that announces a body and sends one byte every 0.7 s held a
// slot for the server's whole read timeout (10 s): four such connections of a tenant owner stalled the API for everyone, and an
// AI proposal whose result transaction needs a slot again after the provider phase lost its result. The body is now read
// before the slot is taken. The test runs the real server of httpserver.New (the production read and write timeouts) with the
// production default of four slots, holds all four with trickling POSTs on three body-reading route families, and requires an
// honest read and an honest AI proposal to be served at their normal speed meanwhile.
func TestM14SlowRequestBodiesDoNotHoldDatabaseSlots(t *testing.T) {
	if testing.Short() {
		t.Skip("takes about 12 s of wall clock")
	}
	const slots = 4 // the default of API_DB_CONCURRENCY
	e := m14Setup(t, m14Options{admission: slots})
	doc := e.draft("500")
	e.provider.Push(ai.ScriptStep{Payload: labelPatch(t, doc, "entity.order", "Honest"), Delay: 1500 * time.Millisecond, Usage: ai.Usage{InputTokens: 100, OutputTokens: 100}, Model: "scripted-model-1"})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httpserver.New(listener.Addr().String(), e.h)
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("the production read timeout changed (%v): re-derive the attack below", server.ReadTimeout)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	addr := listener.Addr().String()

	// The attackers hold a valid token of the tenant owner (the policy check passes), so the unfixed server reached the
	// handler's body read: one connection per slot, over the AI, metadata and dataset routes.
	routes := []string{
		e.root + "/ai/patch-proposals",
		"/v1/tenants/" + e.f.tenantA + "/metadata/" + e.app + "/versions",
		e.root + "/datasets",
		e.root + "/ai/patch-proposals",
	}
	stop := make(chan struct{})
	var attackers sync.WaitGroup
	t.Cleanup(func() { close(stop); attackers.Wait() })
	for i := 0; i < slots; i++ {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 30000\r\n\r\n{", routes[i], addr, e.alice); err != nil {
			t.Fatal(err)
		}
		attackers.Add(1)
		go func() {
			defer attackers.Done()
			defer conn.Close()
			tick := time.NewTicker(700 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-stop:
					return
				case <-tick.C:
					if _, err := conn.Write([]byte(" ")); err != nil {
						return
					}
				}
			}
		}()
	}
	time.Sleep(1500 * time.Millisecond) // every attacker has reached its handler (or, fixed, its body read)

	client := &http.Client{Timeout: 15 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// An honest read: 10 ms normally; 8.5 s behind the four held slots before the fix. The bound sits between the two, far from both, so a
	// slow host does not fail the test and the defect still does.
	read, err := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions/1", nil)
	if err != nil {
		t.Fatal(err)
	}
	read.Header.Set("Authorization", "Bearer "+e.alice)
	started := time.Now()
	resp, err := client.Do(read)
	if err != nil {
		t.Fatalf("the honest read failed after %v: %v", time.Since(started), err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if elapsed := time.Since(started); resp.StatusCode != 200 || elapsed > 5*time.Second {
		t.Fatalf("honest read behind %d slow bodies: status %d after %v (it must not wait for them)", slots, resp.StatusCode, elapsed)
	}

	// An honest AI proposal: the provider answers in 1.5 s; the result transaction needs a slot after the provider phase.
	propose, err := http.NewRequestWithContext(ctx, "POST", "http://"+addr+e.root+"/ai/patch-proposals", strings.NewReader(`{"request":"Call the order entity Honest."}`))
	if err != nil {
		t.Fatal(err)
	}
	propose.Header.Set("Authorization", "Bearer "+e.alice)
	propose.Header.Set("Content-Type", "application/json")
	started = time.Now()
	resp, err = client.Do(propose)
	if err != nil {
		t.Fatalf("the honest proposal failed after %v: %v", time.Since(started), err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	elapsed := time.Since(started)
	if resp.StatusCode != 201 || elapsed > 7*time.Second {
		t.Fatalf("honest proposal behind %d slow bodies: status %d after %v: %s", slots, resp.StatusCode, elapsed, raw)
	}
	var p ai.Proposal
	if err = json.Unmarshal(raw, &p); err != nil || p.ID == "" || p.Status != ai.ProposalReady {
		t.Fatalf("proposal %+v (%v): %s", p, err, raw)
	}
	if e.provider.Calls() != 1 || e.proposalRows() != 1 {
		t.Fatalf("%d provider calls and %d proposal rows for one honest request", e.provider.Calls(), e.proposalRows())
	}
	t.Logf("honest requests behind %d trickling bodies: proposal 201 READY in %v", slots, elapsed)
}
