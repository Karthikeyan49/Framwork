package promotion // zz: WorseRollbackClass and the bounds (kills D20, D21, D22, D23)

import (
	"testing"

	"github.com/Karthikeyan49/Framwork/internal/risk"
)

func TestWorseRollbackClassAndTheBoundsThatMirrorTheDatabase(t *testing.T) {
	for _, tc := range []struct{ a, b, want string }{
		{risk.RollbackPointerSafe, risk.RollbackDataNotReversible, risk.RollbackDataNotReversible},
		{risk.RollbackDataNotReversible, risk.RollbackPointerSafe, risk.RollbackDataNotReversible},
		{risk.RollbackDataNotReversible, risk.RollbackExternalEffects, risk.RollbackExternalEffects},
		{risk.RollbackExternalEffects, risk.RollbackDataNotReversible, risk.RollbackExternalEffects},
		{risk.RollbackPointerSafe, "", ""},
		{"", risk.RollbackPointerSafe, ""},
		{risk.RollbackDataNotReversible, "", risk.RollbackDataNotReversible},
		{risk.RollbackPointerSafe, "weird", "weird"},
	} {
		if got := WorseRollbackClass(tc.a, tc.b); got != tc.want {
			t.Errorf("WorseRollbackClass(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
	if MaxNoteRunes != 280 || MaxRollbackSpan != 32 { // the note bound is the database's char_length <= 280
		t.Errorf("MaxNoteRunes %d MaxRollbackSpan %d", MaxNoteRunes, MaxRollbackSpan)
	}
}
