package erp

import (
	"testing"
	"time"
)

func TestMoneyFXCalendarAndDocumentBoundaries(t *testing.T) {
	for _, m := range []Money{{"0.123456789012345678", "USD"}, {"100", "JPY"}, {"-12.001", "KWD"}} {
		if m.Validate() != nil {
			t.Fatal(m)
		}
	}
	for _, m := range []Money{{"1e2", "USD"}, {"NaN", "USD"}, {"1.1234567890123456789", "INR"}, {"1000000000000000001", "USD"}, {"1", "INVALID"}} {
		if m.Validate() == nil {
			t.Fatal("invalid money")
		}
	}
	f := FXSnapshot{SourceCurrency: "USD", TargetCurrency: "INR", Rate: "83.125", Source: "reviewed feed", EffectiveAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
	if f.Validate() != nil {
		t.Fatal("FX")
	}
	f.Rate = "0"
	if f.Validate() == nil {
		t.Fatal("FX zero")
	}
	c := BusinessCalendar{ID: "calendar.office", Version: "1", Zone: "Asia/Kolkata", WorkingDays: []time.Weekday{time.Monday}, OpenMinute: 540, CloseMinute: 1080, FiscalStartMonth: time.April, Holidays: []string{"2026-01-26"}}
	if c.Validate() != nil {
		t.Fatal("calendar")
	}
	c.Zone = "Local"
	if c.Validate() == nil {
		t.Fatal("ambient timezone")
	}
	if (DocumentNumber{DraftID: "draft", FinalNumber: "1"}).Validate() == nil {
		t.Fatal("draft final number")
	}
	if (DocumentNumber{DraftID: "draft", Posted: true, Series: "PO-2026", FinalNumber: "001", GaplessRequired: true}).Validate() != nil {
		t.Fatal("final number")
	}
}
