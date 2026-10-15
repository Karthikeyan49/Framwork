//go:build unix

package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestFileSecretsResolvesOwnerOnlyRegularFiles(t *testing.T) {
	dir := t.TempDir()
	writeSecret(t, dir, "crm_live", secretValue+"\n", 0o600)
	writeSecret(t, dir, "read_only", secretValue, 0o400)
	s := FileSecrets{Dir: dir}
	for _, ref := range []string{"crm_live", "read_only"} {
		got, err := s.Resolve(context.Background(), ref)
		if err != nil || got != secretValue {
			t.Fatalf("%s: %q %v", ref, got, err)
		}
	}
}

func TestFileSecretsRefusesUnsafeSources(t *testing.T) {
	dir := t.TempDir()
	writeSecret(t, dir, "target", secretValue, 0o600)
	writeSecret(t, dir, "group_readable", secretValue, 0o640)
	writeSecret(t, dir, "world_readable", secretValue, 0o604)
	writeSecret(t, dir, "group_writable", secretValue, 0o620)
	writeSecret(t, dir, "too_large", strings.Repeat("a", MaxSecretBytes+1), 0o600)
	writeSecret(t, dir, "max_size", strings.Repeat("a", MaxSecretBytes), 0o600)
	writeSecret(t, dir, "empty", "", 0o600)
	writeSecret(t, dir, "only_newline", "\n", 0o600)
	writeSecret(t, dir, "two_newlines", secretValue+"\n\n", 0o600)
	writeSecret(t, dir, "crlf", secretValue+"\r\n", 0o600)
	writeSecret(t, dir, "inner_newline", "abc\nAuthorization: x", 0o600)
	writeSecret(t, dir, "space", "abc def", 0o600)
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := FileSecrets{Dir: dir}
	if got, err := s.Resolve(context.Background(), "max_size"); err != nil || len(got) != MaxSecretBytes {
		t.Fatal("maximum size secret")
	}
	refs := []string{
		"group_readable", "world_readable", "group_writable", "too_large", "empty", "only_newline",
		"two_newlines", "crlf", "inner_newline", "space", "link", "directory", "fifo", "missing",
		"", "../target", "Target", "target/x", "1target", strings.Repeat("a", 65),
	}
	for _, ref := range refs {
		got, err := s.Resolve(context.Background(), ref)
		if err == nil || got != "" || !errors.Is(err, ErrSecretUnavailable) {
			t.Errorf("%q resolved", ref)
			continue
		}
		if strings.Contains(err.Error(), secretValue) || strings.Contains(err.Error(), "aaaa") || strings.Contains(err.Error(), dir) {
			t.Errorf("%q error leaks content or path: %v", ref, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Resolve(ctx, "target"); err == nil {
		t.Error("canceled context resolved")
	}
	if _, err := (FileSecrets{}).Resolve(context.Background(), "target"); err == nil {
		t.Error("empty directory resolved")
	}
}
