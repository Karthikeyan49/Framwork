//go:build integration && trafficreview

package integration

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// This explicit diagnostic tag requires a checksum-pinned artifact and has no
// default SKIP. It never replaces the unrelated retained pre-extension binary.
func TestM9RTrafficReviewPinnedBinaryInvalidBurst(t *testing.T) {
	binary, expected := os.Getenv("R7A_REVIEW_API_BINARY"), os.Getenv("R7A_REVIEW_API_SHA256")
	raw, err := os.ReadFile(binary)
	if err != nil || len(expected) != 64 {
		t.Fatal("R7a review requires an explicit checksum-pinned API artifact")
	}
	hash := sha256.Sum256(raw)
	if hex.EncodeToString(hash[:]) != expected {
		t.Fatal("R7a review artifact checksum mismatch")
	}
	m8Database(t)
	f := newIAM(t)
	scalarReviewTarget(t, f)
	public, _ := json.Marshal(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "kid": "test", "alg": "RS256", "use": "sig", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	keyFile := filepath.Join(t.TempDir(), "review-public-jwks.json")
	if os.WriteFile(keyFile, public, 0600) != nil {
		t.Fatal("review public-key fixture unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal("review loopback port unavailable")
	}
	address := listener.Addr().String()
	listener.Close()
	command := exec.Command(binary)
	// Allowlist only API runtime bindings; never pass parent operator, worker,
	// dispatcher, private signing key or legacy-artifact environment settings.
	command.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "APP_ENV=test", "HTTP_ADDR=" + address, "DATABASE_URL=" + os.Getenv("DATABASE_URL"), "REDIS_URL=" + os.Getenv("REDIS_URL"), "AUTH_ISSUER=https://test-issuer.example", "AUTH_AUDIENCE=test-api", "AUTH_JWKS_FILE=" + keyFile, "EVENTS_ENABLED=false", "WORKFLOWS_ENABLED=false"}
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if command.Start() != nil {
		t.Fatal("review API process failed to start")
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = command.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = command.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(12 * time.Second):
			_ = command.Process.Kill()
			<-done
			t.Error("review API shutdown timed out")
		}
		if exitErr != nil {
			t.Error("review API exit was unsuccessful")
		}
	})
	client := &http.Client{Timeout: 3 * time.Second}
	ready := false
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		select {
		case <-done:
			t.Fatal("review API exited before readiness")
		default:
		}
		response, e := client.Get("http://" + address + "/readyz")
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				ready = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("review API did not become ready")
	}
	var admitted, rejected []string
	seen := make(map[string]bool)
	idPattern := regexp.MustCompile(`^[0-9a-f]{32}$`)
	start := time.Now()
	for i := range 40 {
		request, e := http.NewRequest("GET", "http://"+address+"/v1/session", nil)
		if e != nil {
			t.Fatal("review request construction failed")
		}
		request.Header.Set("Authorization", "Bearer review-invalid-bearer")
		request.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(i+1))
		request.Header.Set("Forwarded", "for=untrusted-rotated-peer")
		response, e := client.Do(request)
		if e != nil {
			t.Fatal("review request failed")
		}
		var body struct {
			Code, Message string
			RequestID     string `json:"request_id"`
		}
		e = json.NewDecoder(io.LimitReader(response.Body, 2048)).Decode(&body)
		response.Body.Close()
		id := response.Header.Get("X-Request-ID")
		if e != nil || !idPattern.MatchString(id) || seen[id] || body.RequestID != id {
			t.Fatal("review response lacks a unique server-generated request witness")
		}
		seen[id] = true
		switch response.StatusCode {
		case 401:
			admitted = append(admitted, id)
		case 429:
			if response.Header.Get("Retry-After") != "1" || response.Header.Get("Cache-Control") != "no-store" || body.Code != "rate_limited" {
				t.Fatal("review rate response contract mismatch")
			}
			rejected = append(rejected, id)
		default:
			t.Fatal("review unexpected response status", response.StatusCode)
		}
	}
	var admittedAudits, rejectedAudits int
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=ANY($1::text[]) AND action='authentication.denied'`, admitted).Scan(&admittedAudits))
	scalarSQL(t, f.operator.QueryRow(`SELECT count(*) FROM audit.entries WHERE request_id=ANY($1::text[])`, rejected).Scan(&rejectedAudits))
	t.Logf("40 actual HTTP requests: 401=%d 429=%d matching denied audits=%d rejected audits=%d elapsed=%s artifact_sha256=%s", len(admitted), len(rejected), admittedAudits, rejectedAudits, time.Since(start).Round(time.Millisecond), expected)
	if len(admitted) > 22 || len(rejected) < 18 || admittedAudits != len(admitted) || rejectedAudits != 0 {
		t.Fatal("raw acceptance: excess malformed bearers reached PostgreSQL/audit instead of pre-database 429")
	}
}
