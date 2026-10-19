package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// The embedded zone archive is constant, but TZDataHash was recomputed (a SHA-256 over
// 408 KB) by every Restore, which the workflow engine calls once per transition condition.
// A release verification evaluates thousands of conditions, so the repeated hash was the
// largest single cost of restoring a candidate (finding M12-API-F9).
func TestTZDataHashIsStableAndComputedOnce(t *testing.T) {
	sum := sha256.Sum256(zoneArchive)
	if got := TZDataHash(); got != hex.EncodeToString(sum[:]) {
		t.Fatalf("TZDataHash %s does not hash the embedded archive", got)
	}
	if allocs := testing.AllocsPerRun(50, func() { _ = TZDataHash() }); allocs != 0 {
		t.Fatalf("TZDataHash recomputes the archive digest on every call (%v allocations per call)", allocs)
	}
}
