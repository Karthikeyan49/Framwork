// Package identity authenticates tokens and owns users. Tenant claims are ignored.
package identity

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/Karthikeyan49/Framwork/internal/platform/postgres"
)

var ErrUnauthenticated = errors.New("authentication required")

// Principal can only be constructed by successful signature/claim verification.
type Principal struct{ issuer, subject string }

func (p Principal) Issuer() string  { return p.issuer }
func (p Principal) Subject() string { return p.subject }

type User struct {
	id, name string
}

func (u User) ID() string   { return u.id }
func (u User) Name() string { return u.name }
func (u User) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}{u.id, u.name})
}

type Authenticator interface {
	Verify(string) (Principal, error)
}
type Verifier struct {
	issuer, audience string
	keys             map[string]*rsa.PublicKey
	now              func() time.Time
	clockSkew        time.Duration
}

// NewVerifier pins an operator-controlled public JWKS. No token-controlled URLs,
// key discovery, private keys, or symmetric algorithms are accepted.
func NewVerifier(issuer, audience string, jwks []byte) (*Verifier, error) {
	return NewVerifierWithClockSkew(issuer, audience, jwks, 0)
}

// NewVerifierWithClockSkew permits only an explicit bounded clock tolerance.
// It never extends the signed token's maximum one-hour lifetime.
func NewVerifierWithClockSkew(issuer, audience string, jwks []byte, skew time.Duration) (*Verifier, error) {
	var set struct {
		Keys []struct{ Kty, Kid, Alg, Use, N, E string } `json:"keys"`
	}
	if issuer == "" || audience == "" || skew < 0 || skew > 2*time.Minute || skew%time.Second != 0 || len(jwks) > 65536 || json.Unmarshal(jwks, &set) != nil || len(set.Keys) == 0 || len(set.Keys) > 16 {
		return nil, errors.New("invalid authentication key configuration")
	}
	v := &Verifier{issuer: issuer, audience: audience, keys: map[string]*rsa.PublicKey{}, now: time.Now, clockSkew: skew}
	var raw struct {
		Keys []map[string]json.RawMessage `json:"keys"`
	}
	if json.Unmarshal(jwks, &raw) != nil {
		return nil, errors.New("invalid authentication key configuration")
	}
	for _, key := range raw.Keys {
		for name := range key {
			switch strings.ToLower(name) {
			case "d", "p", "q", "dp", "dq", "qi", "oth":
				return nil, errors.New("authentication key set must contain public keys only")
			}
		}
	}
	for _, key := range set.Keys {
		n, en := base64.RawURLEncoding.DecodeString(key.N)
		e, ee := base64.RawURLEncoding.DecodeString(key.E)
		if key.Kty != "RSA" || key.Alg != "RS256" || key.Use != "sig" || key.Kid == "" || len(key.Kid) > 128 || en != nil || ee != nil || len(e) > 4 || len(e) == 0 {
			return nil, errors.New("invalid authentication key configuration")
		}
		modulus := new(big.Int).SetBytes(n)
		exponent := new(big.Int).SetBytes(e).Int64()
		if modulus.BitLen() < 2048 || modulus.BitLen() > 8192 || exponent != 65537 || v.keys[key.Kid] != nil {
			return nil, errors.New("invalid authentication key configuration")
		}
		v.keys[key.Kid] = &rsa.PublicKey{N: modulus, E: int(exponent)}
	}
	return v, nil
}

func (v *Verifier) Verify(token string) (Principal, error) {
	fail := func() (Principal, error) { return Principal{}, ErrUnauthenticated }
	payload, err := v.signedPayload(token, false)
	if err != nil {
		return fail()
	}
	var c struct {
		Iss, Sub      string
		Aud           json.RawMessage
		Exp, Iat, Nbf int64
	}
	if json.Unmarshal(payload, &c) != nil || c.Iss != v.issuer || c.Sub == "" || len(c.Sub) > 255 {
		return fail()
	}
	var audiences []string
	var single string
	if json.Unmarshal(c.Aud, &single) == nil {
		audiences = []string{single}
	} else if json.Unmarshal(c.Aud, &audiences) != nil {
		return fail()
	}
	matched := false
	for _, a := range audiences {
		if a == v.audience {
			matched = true
		}
	}
	if !matched || !v.validTimes(c.Exp, c.Iat, c.Nbf) {
		return fail()
	}
	return Principal{c.Iss, c.Sub}, nil
}

func (v *Verifier) validTimes(exp, iat, nbf int64) bool {
	now, skew := v.now().Unix(), int64(v.clockSkew/time.Second)
	return exp > now-skew && iat > 0 && iat <= now+skew && nbf <= now+skew && exp > iat && exp-iat <= 3600
}

// Both token classes use pinned keys. They deliberately accept distinct typ
// values, so an ID token can never become an API bearer token.
func (v *Verifier) signedPayload(token string, idToken bool) ([]byte, error) {
	fail := func() ([]byte, error) { return nil, ErrUnauthenticated }
	if len(token) > 8192 {
		return fail()
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fail()
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fail()
	}
	var h struct {
		Alg, Kid, Typ string
		Crit          []string
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "RS256" || len(h.Crit) != 0 || v.keys[h.Kid] == nil {
		return fail()
	}
	if !idToken && h.Typ != "at+jwt" || idToken && h.Typ != "" && h.Typ != "JWT" {
		return fail()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fail()
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(v.keys[h.Kid], crypto.SHA256, hash[:], sig) != nil {
		return fail()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fail()
	}
	return payload, nil
}

type Store struct{}

func (Store) Ready(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `SELECT id,issuer,subject,name,active FROM iam.users LIMIT 0`)
	return err
}

func (Store) Resolve(ctx context.Context, tx postgres.Executor, p Principal) (User, error) {
	var u User
	// Rebinding identity invalidates any earlier actor/tenant scope, even on denial.
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.auth_issuer','',true),set_config('app.auth_subject','',true),set_config('app.actor_id','',true),set_config('app.tenant_id','',true)`); err != nil {
		return u, err
	}
	if p.issuer == "" || p.subject == "" {
		return u, ErrUnauthenticated
	}
	// Only verified identity claims become transaction-local database context.
	if _, err := tx.ExecContext(ctx, `SELECT set_config('app.auth_issuer',$1,true),set_config('app.auth_subject',$2,true)`, p.issuer, p.subject); err != nil {
		return u, err
	}
	err := tx.QueryRowContext(ctx, `SELECT id,name FROM iam.users WHERE issuer=$1 AND subject=$2 AND active`, p.issuer, p.subject).Scan(&u.id, &u.name)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrUnauthenticated
	}
	return u, err
}

// Provision is an operator-only port. The runtime database role cannot call it
// successfully. It never grants roles implicitly or accepts token tenant claims.
func (Store) Provision(ctx context.Context, tx postgres.Executor, issuer, subject, name string, active bool) (string, error) {
	var id string
	if issuer == "" || subject == "" || len(subject) > 255 || name == "" || len(name) > 200 {
		return id, errors.New("invalid user")
	}
	err := tx.QueryRowContext(ctx, `INSERT INTO iam.users(issuer,subject,name,active) VALUES($1,$2,$3,$4) ON CONFLICT(issuer,subject) DO UPDATE SET name=excluded.name,active=excluded.active RETURNING id`, issuer, subject, name, active).Scan(&id)
	return id, err
}
