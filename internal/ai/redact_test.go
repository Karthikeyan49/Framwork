package ai

import (
	"strings"
	"testing"
)

func TestRedactTextTable(t *testing.T) {
	secrets := map[string]string{
		"aws access key":   "AKIAIOSFODNN7EXAMPLE",
		"aws sts key":      "ASIAIOSFODNN7EXAMPLE",
		"github pat":       "ghp_0123456789abcdefghijklmnopqrstuvwxyz",
		"github fine":      "github_pat_11ABCDEFG0123456789_abcdefghijklmnop",
		"slack":            "xoxb-123456789012-abcdefghijklmnop",
		"stripe live":      "sk_live_0123456789abcdefABCDEF",
		"openai style":     "sk-abcdefghijklmnopqrstuvwxyz0123456789",
		"google api":       "AIzaSyA-0123456789abcdefghijklmnopqrstuv",
		"gitlab":           "glpat-0123456789abcdefghij",
		"jwt":              "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r_wW1gFWFOEjXk0",
		"bearer header":    "Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345",
		"basic header":     "authorization=Basic dXNlcjpwYXNzd29yZA==",
		"pem":              "-----BEGIN RSA PRIVATE KEY-----\nMIIBOgIBAAJBAKj34GkxFhD90vcNLYLInFEX6Ppy1tPf9Cnzj4p4WGeKLs1Pt8Qu\n-----END RSA PRIVATE KEY-----",
		"truncated pem":    "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSj",
		"postgres dsn":     "postgres://app:s3cr3tPassw0rd@db.internal:5432/prod",
		"https userinfo":   "https://user:hunter2hunter2@example.com/path?x=1",
		"hex 64":           strings.Repeat("9f86d081884c7d65", 4),
		"hex 64 letter":    "a" + strings.Repeat("f86d081884c7d65", 4) + "a",
		"base64 run":       "dGhpcyBpcyBhIHZlcnkgc2VjcmV0IHZhbHVlIHRoYXQgc2hvdWxkIG5vdCBsZWF2ZQ9xK2Vz",
		"assignment":       "password=correcthorsebatterystaple",
		"quoted assign":    `api_key = "A1b2C3d4E5f6G7h8"`,
		"colon credential": "client_secret: Zx9-Qm4_Lp7+Rt2@",
	}
	for name, secret := range secrets {
		out, n := redactText("before " + secret + " after")
		// A PEM block with no end marker swallows the rest of the text: fail closed.
		if n == 0 || !strings.Contains(out, redactionPrefix) || !strings.HasPrefix(out, "before ") || (name != "truncated pem" && !strings.HasSuffix(out, " after")) {
			t.Errorf("%s: %q", name, out)
		}
		core := strings.TrimSpace(secret[strings.LastIndexAny(secret, "=: ")+1:])
		if len(core) > 8 && strings.Contains(out, core) {
			t.Errorf("%s: the secret survived: %q", name, out)
		}
		again, m := redactText(out)
		if again != out || m != 0 {
			t.Errorf("%s: not idempotent: %q -> %q", name, out, again)
		}
	}
}

func TestRedactTextLeavesProseAlone(t *testing.T) {
	for _, s := range []string{
		"Rename the contact entity to Customer.",
		"Orders above 500 USD need approval by an admin.",
		"Password: required",
		"Token budget",
		"Use the secret santa page",
		"2026-10-18T12:00:00Z",
		"entity.order.amount",
		"https://example.com/docs/page",
		"user@example.com",
		"Total = quantity * unit_price",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ",
		"The quick brown fox jumps over the lazy dog, again and again and again.",
		"1234567890",
		"[REDACTED:secret]",
	} {
		if out, n := redactText(s); out != s || n != 0 {
			t.Errorf("false positive: %q -> %q", s, out)
		}
	}
}

func TestRedactStringKeepsIdentifiersButNotSecretsShapedLikeThem(t *testing.T) {
	for _, id := range []string{"entity.order", "field.order.amount", "app.orders_0123", "1.3.0", "USD", "workflow.order.approve"} {
		if redactString(id) != id {
			t.Errorf("identifier %q was changed", id)
		}
	}
	for _, secret := range []string{"ghp_0123456789abcdefghijklmnopqrstuvwxyz", "a" + strings.Repeat("f86d081884c7d65", 4) + "a", "akiaiosfodnn7example0123456789abcdef0123"} {
		out := redactString(secret)
		if out == secret || !strings.Contains(out, redactionPrefix) {
			// akia... lower-case is not an AWS id; the 40-char hex-like part is not >=32 hex, so only
			// the first two are required to be redacted.
			if !strings.HasPrefix(secret, "akia") {
				t.Errorf("identifier-shaped secret survived: %q -> %q", secret, out)
			}
		}
	}
}

func TestSanitizeTextDropsInvisibleAndControlRunes(t *testing.T) {
	in := "a\x00b\x1bc\u202ed\u2066e\u2028f\u200bg\ufeffh" + string(rune(0xE0064)) + "i\xffj\tk\nl"
	out := sanitizeText(in)
	if out != "abcdefghij\tk\nl" {
		t.Fatalf("%q", out)
	}
	if sanitizeText("clean text") != "clean text" {
		t.Fatal("clean text must be returned as is")
	}
}

func TestRedactTreeWalksNestedValues(t *testing.T) {
	out, n, err := redactedJSON(map[string]any{"label": "key AKIAIOSFODNN7EXAMPLE", "nested": []any{map[string]any{"v": "ghp_0123456789abcdefghijklmnopqrstuvwxyz"}, 7}, "ok": "fine"})
	if err != nil || n != 2 || strings.Contains(out, "AKIA") || strings.Contains(out, "ghp_") || !strings.Contains(out, `"ok":"fine"`) || !strings.Contains(out, "7") {
		t.Fatalf("%s %d %v", out, n, err)
	}
}
