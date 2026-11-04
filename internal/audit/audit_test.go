package audit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPromotionEvidenceIsBoundedAndClosed(t *testing.T) {
	h := strings.Repeat("a", 64)
	good := State{ArtifactHash: h, RiskLevel: "CRITICAL", ReleaseGeneration: 9007199254740991, ApprovalCount: 2, ApprovalsRequired: 2, RejectReason: "WITHDRAWN",
		PreviousHash: h, PlanHash: h, StorageClass: "ADDITIVE", RollbackClass: "DATA_NOT_REVERSIBLE", StaleReasons: []string{"BASE_CHANGED", "POLICY_CHANGED"},
		RefusalCode: "VERIFICATION_FAILED", AssessmentHash: h}
	args, err := encodeEntry(Entry{Action: "release.rolled_back", Resource: "release", Allow: true, State: good})
	if err != nil {
		t.Fatal("valid promotion evidence rejected", err)
	}
	var decoded map[string]any
	if json.Unmarshal(args[9].([]byte), &decoded) != nil || decoded["release_generation"] != float64(9007199254740991) || decoded["reject_reason"] != "WITHDRAWN" || len(decoded["stale_reasons"].([]any)) != 2 || decoded["refusal_code"] != "VERIFICATION_FAILED" || decoded["assessment_hash"] != h {
		t.Fatal("promotion evidence not encoded", string(args[9].([]byte)))
	}
	if _, err := encodeEntry(Entry{Action: "release.approved", State: State{}}); err != nil {
		t.Fatal("empty promotion evidence rejected", err)
	}
	for name, mutate := range map[string]func(*State){
		"negative generation":   func(s *State) { s.ReleaseGeneration = -1 },
		"generation overflow":   func(s *State) { s.ReleaseGeneration = 9007199254740992 },
		"negative approvals":    func(s *State) { s.ApprovalCount = -1 },
		"approvals overflow":    func(s *State) { s.ApprovalCount = 1000001 },
		"required overflow":     func(s *State) { s.ApprovalsRequired = 9 },
		"negative required":     func(s *State) { s.ApprovalsRequired = -1 },
		"free text reason":      func(s *State) { s.RejectReason = "the requester asked" },
		"lowercase reason":      func(s *State) { s.RejectReason = "withdrawn" },
		"unknown rollback":      func(s *State) { s.RollbackClass = "ANYTHING" },
		"storage free text":     func(s *State) { s.StorageClass = "additive; drop" },
		"storage too long":      func(s *State) { s.StorageClass = strings.Repeat("A", 17) },
		"short previous hash":   func(s *State) { s.PreviousHash = "abc" },
		"uppercase plan hash":   func(s *State) { s.PlanHash = strings.Repeat("A", 64) },
		"non-hex plan hash":     func(s *State) { s.PlanHash = strings.Repeat("z", 64) },
		"too many stale":        func(s *State) { s.StaleReasons = make([]string, 17) },
		"empty stale reason":    func(s *State) { s.StaleReasons = []string{""} },
		"stale reason too long": func(s *State) { s.StaleReasons = []string{strings.Repeat("A", 33)} },
		"stale free text":       func(s *State) { s.StaleReasons = []string{"note: secret"} },
		"refusal free text":     func(s *State) { s.RefusalCode = "storage breaking: see note" },
		"refusal lowercase":     func(s *State) { s.RefusalCode = "storage_breaking" },
		"refusal too long":      func(s *State) { s.RefusalCode = strings.Repeat("A", 41) },
		"short assessment hash": func(s *State) { s.AssessmentHash = "abc" },
		"upper assessment hash": func(s *State) { s.AssessmentHash = strings.Repeat("A", 64) },
	} {
		s := good
		s.StaleReasons = append([]string(nil), good.StaleReasons...)
		mutate(&s)
		if _, err := encodeEntry(Entry{Action: "release.approved", State: s}); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
