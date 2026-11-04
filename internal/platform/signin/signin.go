// Package signin exposes the origin-bound OIDC gateway. Its cookie binds a
// one-time login flow only; it never authenticates business API requests.
package signin

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"

	"github.com/Karthikeyan49/Framwork/internal/identity"
	"github.com/Karthikeyan49/Framwork/internal/platform/httpserver"
)

type API struct {
	Login       *identity.BrowserLogin
	Development bool
}

func (a API) AcceptsPOST(path string) bool {
	return a.Login != nil && (path == "/v1/auth/login/start" || path == "/v1/auth/login/complete")
}
func (a API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/auth/provider", func(w http.ResponseWriter, r *http.Request) {
		httpserver.JSON(w, 200, struct {
			Enabled bool `json:"enabled"`
			Manual  bool `json:"manual_token"`
		}{a.Login != nil, a.Development})
	})
	if a.Login == nil {
		return
	}
	mux.HandleFunc("POST /v1/auth/login/start", a.start)
	mux.HandleFunc("POST /v1/auth/login/complete", a.complete)
}
func (a API) cookieName() string {
	if a.Login.SecureCookie() {
		return "__Host-framework_login"
	}
	return "framework_login"
}
func (a API) cookie(value string, age int) *http.Cookie {
	return &http.Cookie{Name: a.cookieName(), Value: value, Path: "/", MaxAge: age, HttpOnly: true, Secure: a.Login.SecureCookie(), SameSite: http.SameSiteLaxMode}
}
func (a API) origin(r *http.Request) bool {
	return len(r.Header.Values("Origin")) == 1 && r.Header.Get("Origin") == a.Login.BrowserOrigin() && r.URL.RawQuery == ""
}
func rejection(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, identity.ErrLoginBusy):
		w.Header().Set("Retry-After", "60")
		httpserver.Error(w, r, 429, "sign_in_busy", "Sign-in is busy; wait before starting again")
	case errors.Is(err, identity.ErrLoginUnavailable):
		httpserver.Error(w, r, 503, "sign_in_unavailable", "Sign-in could not finish; start a new sign-in")
	default:
		httpserver.Error(w, r, 401, "sign_in_rejected", "Sign-in was not accepted; start a new sign-in")
	}
}
func (a API) start(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if !a.origin(r) {
		httpserver.Error(w, r, 403, "origin_denied", "Sign-in origin is not permitted")
		return
	}
	var empty struct{}
	if !readJSON(w, r, &empty) {
		return
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		rejection(w, r, identity.ErrUnauthenticated)
		return
	}
	location, binding, err := a.Login.Start(r.Context(), remote)
	if err != nil {
		rejection(w, r, err)
		return
	}
	http.SetCookie(w, a.cookie(binding, 300))
	httpserver.JSON(w, 200, struct {
		Location string `json:"authorization_url"`
	}{location})
}
func readJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	if content := r.Header.Get("Content-Type"); content != "application/json" {
		httpserver.Error(w, r, 400, "invalid_sign_in", "Invalid sign-in request")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(value) != nil || decoder.Decode(new(any)) != io.EOF {
		httpserver.Error(w, r, 400, "invalid_sign_in", "Invalid sign-in request")
		return false
	}
	return true
}
func (a API) complete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	if !a.origin(r) {
		httpserver.Error(w, r, 403, "origin_denied", "Sign-in origin is not permitted")
		return
	}
	var input struct {
		Code   string `json:"code"`
		State  string `json:"state"`
		Issuer string `json:"issuer,omitempty"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	bindings := r.CookiesNamed(a.cookieName())
	if len(bindings) != 1 || !identity.ValidLoginBinding(bindings[0].Value) {
		rejection(w, r, identity.ErrUnauthenticated)
		return
	}
	token, err := a.Login.Complete(r.Context(), bindings[0].Value, input.State, input.Code, input.Issuer)
	// A mismatched older state must not clear a newer browser binding. A consumed
	// flow is unusable even if its cookie remains until the next explicit start.
	if err != nil {
		rejection(w, r, err)
		return
	}
	// Do not delete the shared browser cookie here: another tab may have started
	// a newer flow while this exchange was in flight. This flow is already spent
	// by atomic Take; its binding expires in five minutes or is replaced at Start.
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Referrer-Policy", "no-referrer")
	httpserver.JSON(w, 200, struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}{token, "Bearer"})
}
