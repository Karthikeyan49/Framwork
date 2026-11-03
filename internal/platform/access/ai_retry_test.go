package access

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/ai"
	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
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

// ER-04: the result exchange of a paid answer is repeated after a known abort and after a full admission queue, and nothing else.
func TestAIResultExchangeRepeatsOnlyWhatLeavesNothingBehind(t *testing.T) {
	for name, err := range map[string]error{
		"serialization failure": &pgconn.PgError{Code: "40001"},
		"deadlock":              &pgconn.PgError{Code: "40P01"},
		"full queue":            postgres.ErrAdmissionFull,
		"full queue, wrapped":   fmt.Errorf("exchange: %w", postgres.ErrAdmissionFull),
	} {
		if !aiResultRetriable(err) {
			t.Errorf("%s is not repeated", name)
		}
	}
	for name, err := range map[string]error{
		"transport lost":      errors.New("transport lost"),
		"unknown outcome":     context.DeadlineExceeded,
		"another unique":      &pgconn.PgError{Code: "23505"},
		"a rejection":         &requestRejection{response: denied(403, "forbidden", "Access is denied")},
		"permission denied":   &pgconn.PgError{Code: "42501"},
		"no error at all (!)": nil,
	} {
		if aiResultRetriable(err) {
			t.Errorf("%s is repeated", name)
		}
	}
	// A conflict is over in milliseconds; a queue drains in seconds, and the six tries span about three of the six allowed.
	if d, ok := aiResultPause(1, &pgconn.PgError{Code: "40001"}); !ok || d < 5*time.Millisecond || d > 25*time.Millisecond {
		t.Errorf("pause after a conflict: %v %v", d, ok)
	}
	var total time.Duration
	for attempt := 1; attempt < aiResultAttempts; attempt++ {
		d, ok := aiResultPause(attempt, postgres.ErrAdmissionFull)
		if !ok || d < 100*time.Millisecond || d > 1500*time.Millisecond {
			t.Errorf("pause %d after a full queue: %v %v", attempt, d, ok)
		}
		total += d
	}
	if total < 2*time.Second || total > aiResultBudget {
		t.Errorf("the pauses of a full queue add up to %v; they must span seconds and fit the %v budget", total, aiResultBudget)
	}
	// The pause is a function of any try number: no shift can overflow or go negative.
	for _, attempt := range []int{-3, 0, 1, 5, 6, 37, 38, 64, 1 << 20} {
		if d, ok := aiResultPause(attempt, postgres.ErrAdmissionFull); !ok || d < 100*time.Millisecond || d > 1500*time.Millisecond {
			t.Errorf("pause for try %d after a full queue: %v %v", attempt, d, ok)
		}
	}
}
