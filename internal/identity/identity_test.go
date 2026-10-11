package identity

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestVerificationRejectsUntrustedClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding.EncodeToString
	set, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "key1", "alg": "RS256", "use": "sig", "n": b64(key.N.Bytes()), "e": "AQAB"}}})
	v, err := NewVerifier("https://issuer.example", "api", set)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	privateSet := strings.Replace(string(set), `"kty":"RSA"`, `"kty":"RSA","d":"private"`, 1)
	if _, err := NewVerifier("https://issuer.example", "api", []byte(privateSet)); err == nil {
		t.Fatal("private signing material accepted")
	}
	sign := func(h, c map[string]any, signer *rsa.PrivateKey) string {
		hb, _ := json.Marshal(h)
		cb, _ := json.Marshal(c)
		input := b64(hb) + "." + b64(cb)
		hash := sha256.Sum256([]byte(input))
		sig, err := rsa.SignPKCS1v15(rand.Reader, signer, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		return input + "." + b64(sig)
	}
	for _, tc := range []struct {
		name           string
		header, claims map[string]any
		invalid        bool
	}{
		{name: "valid"}, {name: "audience array", claims: map[string]any{"aud": []string{"other", "api"}}},
		{name: "tenant and roles cannot enter principal", claims: map[string]any{"tenant_id": "forged", "roles": []string{"owner"}}},
		{name: "expired", claims: map[string]any{"exp": now}, invalid: true},
		{name: "future issued", claims: map[string]any{"iat": now + 10}, invalid: true},
		{name: "future not before", claims: map[string]any{"nbf": now + 10}, invalid: true},
		{name: "long lifetime", claims: map[string]any{"exp": now + 3601}, invalid: true},
		{name: "missing expiry", claims: map[string]any{"exp": nil}, invalid: true},
		{name: "wrong issuer", claims: map[string]any{"iss": "https://evil.example"}, invalid: true},
		{name: "wrong audience", claims: map[string]any{"aud": "other"}, invalid: true},
		{name: "empty subject", claims: map[string]any{"sub": ""}, invalid: true},
		{name: "none algorithm", header: map[string]any{"alg": "none"}, invalid: true},
		{name: "symmetric confusion", header: map[string]any{"alg": "HS256"}, invalid: true},
		{name: "unknown key", header: map[string]any{"kid": "unknown"}, invalid: true},
		{name: "identity token", header: map[string]any{"typ": "JWT"}, invalid: true},
		{name: "critical extension", header: map[string]any{"crit": []string{"b64"}}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := map[string]any{"alg": "RS256", "typ": "at+jwt", "kid": "key1"}
			c := map[string]any{"iss": "https://issuer.example", "aud": "api", "sub": "alice", "iat": now, "nbf": now, "exp": now + 900}
			for k, val := range tc.header {
				h[k] = val
			}
			for k, val := range tc.claims {
				c[k] = val
			}
			p, err := v.Verify(sign(h, c, key))
			if (err != nil) != tc.invalid {
				t.Fatalf("invalid=%v error=%v", tc.invalid, err)
			}
			if err == nil && (p.Issuer() != c["iss"] || p.Subject() != "alice") {
				t.Fatal("identity mismatch")
			}
		})
	}
	h := map[string]any{"alg": "RS256", "typ": "at+jwt", "kid": "key1"}
	c := map[string]any{"iss": "https://issuer.example", "aud": "api", "sub": "alice", "iat": now, "exp": now + 900}
	token := sign(h, c, key)
	parts := strings.Split(token, ".")
	parts[1] = b64([]byte(`{"sub":"mallory"}`))
	for _, bad := range []string{"", "a.b.c", strings.Repeat("x", 8193), strings.Join(parts, "."), token + ".extra"} {
		if _, err := v.Verify(bad); err == nil {
			t.Fatal("untrusted token accepted")
		}
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	if _, err := v.Verify(sign(h, c, other)); err == nil {
		t.Fatal("wrong signing key accepted")
	}
}

func TestInvalidKeySets(t *testing.T) {
	for _, raw := range []string{`{}`, `{"keys":[]}`, `{"keys":[{"kty":"oct","kid":"key1","alg":"RS256","use":"sig","n":"AQAB","e":"AQAB"}]}`, `{"keys":[{"kty":"RSA","kid":"key1","alg":"RS256","use":"sig","n":"AQAB","e":"AQAB"}]}`, strings.Repeat("x", 65537)} {
		if _, err := NewVerifier("issuer", "api", []byte(raw)); err == nil {
			t.Fatal("invalid key set accepted")
		}
	}
}
