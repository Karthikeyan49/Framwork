package access

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/identity"
)

// expiringAuth is an Authenticator that also says how long its token lasts, with a fixed answer.
type expiringAuth struct {
	remaining time.Duration
	err       error
}

func (expiringAuth) Verify(string) (identity.Principal, error) { return identity.Principal{}, nil }
func (a expiringAuth) VerifyRemaining(string) (identity.Principal, time.Duration, error) {
	return identity.Principal{}, a.remaining, a.err
}

// plainAuth cannot say how long its token lasts.
type plainAuth struct{}

func (plainAuth) Verify(string) (identity.Principal, error) { return identity.Principal{}, nil }

func requestWith(headers ...string) *http.Request {
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	for _, h := range headers {
		r.Header.Add("Authorization", h)
	}
	return r
}

// A creation whose bearer token ends before its run can finish is refused before anything is reserved (M14R-03), and a token that
// verified at the front door and no longer verifies is refused as unauthenticated at once, not admitted to a paid provider call
// whose result exchange would then fail (independent review PF-03).
func TestTokenCheckBeforeAdmission(t *testing.T) {
	const run = 70 * time.Second
	for _, c := range []struct {
		name    string
		auth    identity.Authenticator
		request *http.Request
		status  int // 0: served
		code    string
	}{
		{"plenty of time", expiringAuth{remaining: run + time.Second}, requestWith("Bearer t"), 0, ""},
		{"exactly the run", expiringAuth{remaining: run}, requestWith("Bearer t"), 0, ""},
		{"a second short", expiringAuth{remaining: run - time.Second}, requestWith("Bearer t"), 401, "token_expiring"},
		{"already over by the clock without skew", expiringAuth{remaining: -3 * time.Second}, requestWith("Bearer t"), 401, "token_expiring"},
		{"no longer verifies", expiringAuth{err: identity.ErrUnauthenticated}, requestWith("Bearer t"), 401, "unauthenticated"},
		{"fails for any other reason", expiringAuth{err: errors.New("keys unavailable")}, requestWith("Bearer t"), 401, "unauthenticated"},
		{"an authenticator that cannot say", plainAuth{}, requestWith("Bearer t"), 0, ""},
		{"no authorization header", expiringAuth{err: identity.ErrUnauthenticated}, requestWith(), 0, ""},
		{"two authorization headers", expiringAuth{err: identity.ErrUnauthenticated}, requestWith("Bearer a", "Bearer b"), 0, ""},
		{"not a bearer", expiringAuth{err: identity.ErrUnauthenticated}, requestWith("Basic abc"), 0, ""},
	} {
		got := API{Auth: c.auth}.aiTokenOutlivesTheRun(c.request, run)
		switch {
		case c.status == 0 && got != nil:
			t.Errorf("%s: refused %d %s", c.name, got.response.status, got.response.code)
		case c.status != 0 && got == nil:
			t.Errorf("%s: served, want %d %s", c.name, c.status, c.code)
		case c.status != 0 && (got.response.status != c.status || got.response.code != c.code):
			t.Errorf("%s: %d %s, want %d %s", c.name, got.response.status, got.response.code, c.status, c.code)
		}
	}
}
