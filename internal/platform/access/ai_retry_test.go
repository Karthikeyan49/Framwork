package access

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/jackc/pgx/v5/pgconn"
)

// The creation of an AI proposal and its result exchange are repeated after a known abort (independent review M14R-03). These tests
// pin the two places where that can silently stop working: the set of repeated routes, and the mapping that must not turn an abort into
// a 409 before the retry loop has seen it.

func TestAICreateRoutesAreExactlyTheProposeRoutes(t *testing.T) {
	for pattern := range aiCreatePOST {
		if aiPatterns[pattern] != "ai.propose" || !strings.HasPrefix(pattern, "POST ") {
			t.Errorf("%q is repeated after a known abort but is not a POST route of ai.propose", pattern)
		}
	}
	for pattern, action := range aiPatterns {
		if action == "ai.propose" && !aiCreatePOST[pattern] {
			t.Errorf("%q creates a proposal and is not repeated after a known abort", pattern)
		}
	}
	if len(aiCreatePOST) != 2 {
		t.Errorf("%d creation routes, want the two proposal routes", len(aiCreatePOST))
	}
}

func rejectionOf(t *testing.T, err error) reply {
	t.Helper()
	var rejection *requestRejection
	if !errors.As(err, &rejection) {
		t.Fatalf("error %v is not a rejection", err)
	}
	return rejection.response
}

func TestAIReplyRetryingLeavesKnownAbortsToTheRetryLoop(t *testing.T) {
	r := httptest.NewRequest("POST", "/", nil)
	abort := &pgconn.PgError{Code: "40001"}
	aborts := map[string]error{
		"serialization failure":           abort,
		"deadlock":                        &pgconn.PgError{Code: "40P01"},
		"port error over a serialization": &ai.PortError{Kind: ai.ErrProposalConflict, Cause: abort},
	}
	for name, err := range aborts {
		got, gotErr := aiReplyRetrying(r, err)
		if gotErr != err || got.status != 0 {
			t.Errorf("%s: the abort was mapped (%d, %v) before the retry loop saw it", name, got.status, gotErr)
		}
		// The exchanges that are not repeated keep the 409 they always answered.
		if _, mapped := aiReply(r, err); rejectionOf(t, mapped).code != "ai_conflict" {
			t.Errorf("%s: aiReply no longer maps the abort to ai_conflict", name)
		}
	}
	// Anything that is not a known abort keeps its mapping.
	if _, err := aiReplyRetrying(r, &ai.PortError{Kind: ai.ErrBusy}); rejectionOf(t, err).status != 429 {
		t.Error("a busy tenant is no longer refused with 429")
	}
	if _, err := aiReplyRetrying(r, &pgconn.PgError{Code: "23505"}); rejectionOf(t, err).code != "ai_conflict" {
		t.Error("another unique violation is no longer a conflict")
	}
	unknown := errors.New("transport lost")
	if _, err := aiReplyRetrying(r, unknown); err != unknown {
		t.Errorf("an unknown error became %v", err)
	}
}
