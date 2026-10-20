//go:build integration

package integration

// Fixtures of the fixes for the independent M12 API review
// (docs/verification/M12_API_INDEPENDENT_REVIEW.md). Everything here drives the HTTP
// API of the M12/M13 fixtures; nothing needs a type that the fixes introduce, so the
// regression tests built on it also run against the tree they were written for.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/audit"
	"github.com/Karthikeyan49/Framwork/internal/intent"
	"github.com/Karthikeyan49/Framwork/internal/metadata"
	"github.com/Karthikeyan49/Framwork/internal/platform/access"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
	"github.com/Karthikeyan49/Framwork/internal/policy"
	"github.com/Karthikeyan49/Framwork/internal/promotion"
)

// m12apiBigDocument is the purchase-order application plus bulk entities of 30 fields
// each. With 14 bulk entities it is the size class of the reviewer's cost probe (15
// entities x 30 fields): about 93 KB of source and 172 KB of IR, the largest class that
// fits the 256 KiB authoring envelope of the sandbox witness (finding F4).
func m12apiBigDocument(app, threshold string, bulk int) metadata.Document {
	doc := orderDocument(app, threshold)
	version, namespace := metadata.WorkflowSchemaVersion, "example.orders"
	for i := 0; i < bulk; i++ {
		entityID := fmt.Sprintf("entity.bulk%02d", i)
		entity := metadata.Definition{ID: entityID, SchemaVersion: version, Namespace: namespace, Label: entityID, Kind: "entity", Entity: &metadata.Entity{}}
		var fields []metadata.Definition
		for f := 0; f < 30; f++ {
			id := fmt.Sprintf("field.bulk%02d.f%02d", i, f)
			typ := "string"
			if f%3 == 1 {
				typ = "integer"
			}
			fields = append(fields, metadata.Definition{ID: id, SchemaVersion: version, Namespace: namespace, Label: id, Kind: "field", Field: &metadata.Field{EntityRef: entityID, Type: typ, Required: f%2 == 0}})
			entity.Entity.Fields = append(entity.Entity.Fields, id)
		}
		doc.Definitions = append(doc.Definitions, entity)
		doc.Definitions = append(doc.Definitions, fields...)
	}
	return doc
}

// m12apiBigAcceptance is a spec of n human cases above the threshold, each also checked
// against the oracle at authoring and re-run by every recomputation of the candidate.
func m12apiBigAcceptance(t testing.TB, set intent.RequirementSet, threshold string, n int) intent.AcceptanceSpec {
	t.Helper()
	base, err := strconv.Atoi(threshold)
	if err != nil {
		t.Fatal(err)
	}
	spec := intent.AcceptanceSpec{SchemaVersion: intent.AcceptanceSpecVersion, RequirementsHash: set.Hash(), Cases: []intent.AcceptanceCase{}}
	for i := 0; i < n; i++ {
		spec.Cases = append(spec.Cases, intent.AcceptanceCase{ID: fmt.Sprintf("above-%04d", i), RequirementID: "po-approval",
			Given:  map[string]json.RawMessage{"field.order.amount": json.RawMessage(fmt.Sprintf(`{"amount":"%d.01","currency":"USD"}`, base+i+1))},
			Expect: intent.Expectation{Outcome: intent.OutcomeApprovalRequired, Roles: []string{"admin"}}})
	}
	return spec
}

// m12apiPrepareBig registers the document as the next source revision (alice), freezes
// a sandbox for it (alice), appends the threshold requirement set (alice) and has bob
// author an acceptance spec of the given number of cases.
func (e *m13Env) m12apiPrepareBig(threshold string, bulk, cases int) m12Artifacts {
	t := e.t
	t.Helper()
	revision, requirements := e.heads()
	raw, _ := json.Marshal(m12apiBigDocument(e.app, threshold, bulk))
	if w := m3Call(e.h, "POST", "/v1/tenants/"+e.f.tenantA+"/metadata/"+e.app+"/versions", e.alice, revision, raw); w.Code != 201 {
		t.Fatal("register source", w.Code, w.Body.String())
	}
	revision++
	sandbox := e.sandbox(e.alice, revision)
	set := thresholdSet(e.app, threshold)
	w := e.requirements(e.alice, set, requirements)
	if w.Code != 201 {
		t.Fatal("requirements", w.Code, w.Body.String())
	}
	var stored promotion.StoredRequirements
	_ = json.Unmarshal(w.Body.Bytes(), &stored)
	w = e.acceptance(e.person("bob").token, stored.Version, m12apiBigAcceptance(t, set, threshold, cases))
	if w.Code != 201 {
		t.Fatal("acceptance", w.Code, w.Body.String())
	}
	var spec promotion.StoredAcceptance
	_ = json.Unmarshal(w.Body.Bytes(), &spec)
	return m12Artifacts{revision: revision, requirementsVersion: stored.Version, acceptanceID: spec.ID, sandboxID: sandbox}
}

// m12apiRetrying repeats a request that the API refused with 429 or 503, up to attempts times in all, pausing for the
// Retry-After it was given (at most 2 s). Bodies are buffered so that the repetition sends the same bytes.
type m12apiRetrying struct {
	inner    http.Handler
	attempts int
}

func (h m12apiRetrying) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	for attempt := 1; ; attempt++ {
		clone := r.Clone(r.Context())
		clone.Body = io.NopCloser(bytes.NewReader(body))
		rec := httptest.NewRecorder()
		h.inner.ServeHTTP(rec, clone)
		if (rec.Code != 429 && rec.Code != 503) || attempt >= h.attempts {
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
		pause := 500 * time.Millisecond
		if seconds, err := strconv.Atoi(rec.Header().Get("Retry-After")); err == nil && seconds > 0 {
			pause = min(time.Duration(seconds)*time.Second, 2*time.Second)
		}
		time.Sleep(pause)
	}
}

// m12apiBigRelease builds a large candidate requested by carol. With activate it is
// approved by dave and activated by erin as well (so the next one has an active base).
func (e *m13Env) m12apiBigRelease(threshold string, activate bool) string {
	t := e.t
	t.Helper()
	// Building the large candidate is the set-up, not the claim: on a slow host it can exceed the 3 s transaction budget of
	// one request, which the API answers with a typed 503 ("nothing was changed, retry"). The set-up repeats such requests
	// like a client that honours Retry-After; the claims are measured afterwards on the unwrapped handler (review IR-03).
	direct := e.h
	e.h = m12apiRetrying{inner: direct, attempts: 6}
	defer func() { e.h = direct }()
	bulk, cases := 14, 1000
	if raceBuild {
		// The race detector multiplies the cost of building the candidate itself past the 3 s envelope; the
		// non-race run keeps the full size that finding F9 was about.

		bulk, cases = 5, 250
	}
	art := e.m12apiPrepareBig(threshold, bulk, cases)
	hash := viewOf(t, e.candidate(e.carol.token, art), 201).ArtifactHash
	if !activate {
		return hash
	}
	viewOf(t, e.httpApprove(e.dave.token, hash), 201)
	base, generation, _ := e.activePointer()
	m13apiRelease(t, e.httpActivate(e.erin.token, hash, base, generation), 200)
	return hash
}

// m12apiRead is one reader's outcome: the final status, the number of 503 answers it
// retried through and how long it took in all.
type m12apiRead struct {
	status     int
	attempts   int
	refusals   int
	elapsed    time.Duration
	retryAfter string
	body       string
}

// m12apiParallelReads has n readers GET the same path at once. A reader that is told 503
// or 429 retries after a short pause, as a client honouring Retry-After would, up to the
// given number of attempts; it stops at the first answer that is neither.
func (e *m13Env) m12apiParallelReads(n, attempts int, token, path string, pause time.Duration) []m12apiRead {
	out := make([]m12apiRead, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start := time.Now()
			for out[i].attempts < attempts {
				out[i].attempts++
				w := e.get(path, token)
				out[i].status, out[i].body, out[i].retryAfter = w.Code, w.Body.String(), w.Header().Get("Retry-After")
				if w.Code != 503 && w.Code != 429 {
					break
				}
				out[i].refusals++
				time.Sleep(pause)
			}
			out[i].elapsed = time.Since(start)
		}(i)
	}
	wg.Wait()
	return out
}

// useAPI rebuilds the HTTP handler of the fixture with the production-shaped API of
// m12Setup changed by mutate (a different Recompute, a PolicyLoader, ...).
func (e *m12Env) useAPI(mutate func(*access.API)) {
	api := access.API{DB: e.f.db, Auth: e.f.verifier, Policy: policy.Native{}, Audit: audit.Postgres{}, Registry: m3Registry(e.t), PreviewEnabled: true, WorkflowsEnabled: true}
	mutate(&api)
	e.h = httpserver.Handler(httpserver.Options{Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Register: api.Register, AllowPOST: api.AcceptsPOST})
}
