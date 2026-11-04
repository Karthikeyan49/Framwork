package intent

import "testing"

func TestUnresolvedMandatoryIntentAndIndependentAcceptance(t *testing.T) {
	c := Contract{Version: "1.0.0", Requirements: []Requirement{{ID: "finance", Mandatory: true, Status: "AMBIGUOUS"}}}
	if c.Validate() == nil {
		t.Fatal("ambiguous mandatory requirement")
	}
	c.Requirements[0] = Requirement{ID: "finance", Mandatory: true, Status: "RESOLVED", EntityID: "entity.purchase", FieldID: "field.purchase.amount", Operator: "GT", Threshold: "500000", Currency: "INR", ApproverRole: "finance_manager", Trigger: "submit"}
	a := Acceptance{Version: "1.0.0", IntentHash: c.Hash(), AuthorActorID: "human", Provenance: "HUMAN_INDEPENDENT", Cases: []Case{{"finance", "499999 INR", "no approval"}, {"finance", "500000 INR", "no approval"}, {"finance", "500001 INR", "finance approval"}}}
	if a.Validate(c) != nil {
		t.Fatal("semantic boundary cases")
	}
	a.Provenance = "GENERATED_FROM_METADATA"
	if a.Validate(c) == nil {
		t.Fatal("circular acceptance")
	}
	a.Provenance = "HUMAN_INDEPENDENT"
	a.Cases = nil
	if a.Validate(c) == nil {
		t.Fatal("missing independent coverage")
	}
}
