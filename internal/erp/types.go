// Package erp owns foundational value contracts; no accounting/inventory engine.
package erp

import (
	"errors"
	"math/big"
	"regexp"
	"time"
	_ "time/tzdata" // Consistent IANA resolution on minimal production images.
)

type Currency string

var currencies = map[Currency]int{"USD": 2, "INR": 2, "EUR": 2, "GBP": 2, "JPY": 0, "KWD": 3, "AUD": 2, "CAD": 2, "CHF": 2, "CNY": 2}

func (c Currency) MinorUnits() (int, error) {
	n, ok := currencies[c]
	if !ok {
		return 0, errors.New("currency requires reviewed ISO4217 catalog entry")
	}
	return n, nil
}

type Money struct {
	Amount   string
	Currency Currency
}

var fixed = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)

func (m Money) Validate() error {
	if _, e := m.Currency.MinorUnits(); e != nil {
		return e
	}
	if len(m.Amount) > 64 || !fixed.MatchString(m.Amount) {
		return errors.New("invalid fixed decimal")
	}
	n, ok := new(big.Rat).SetString(m.Amount)
	if !ok || new(big.Rat).Abs(n).Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))) > 0 {
		return errors.New("money bound")
	}
	return nil
}

type FXSnapshot struct {
	SourceCurrency, TargetCurrency Currency
	Rate, Source                   string
	EffectiveAt                    time.Time
}

func (f FXSnapshot) Validate() error {
	if _, e := f.SourceCurrency.MinorUnits(); e != nil {
		return e
	}
	if _, e := f.TargetCurrency.MinorUnits(); e != nil {
		return e
	}
	if len(f.Rate) > 64 || !fixed.MatchString(f.Rate) {
		return errors.New("invalid FX rate")
	}
	n, ok := new(big.Rat).SetString(f.Rate)
	if !ok || n.Sign() <= 0 || n.Cmp(new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))) > 0 || f.Source == "" || f.EffectiveAt.IsZero() || f.EffectiveAt.Location() != time.UTC {
		return errors.New("invalid FX snapshot")
	}
	return nil
}

type TimeZone string

func (z TimeZone) Location() (*time.Location, error) {
	if z == "Local" || z == "" {
		return nil, errors.New("IANA zone required")
	}
	return time.LoadLocation(string(z))
}

type BusinessCalendar struct {
	ID               string         `json:"id"`
	Version          string         `json:"version"`
	Zone             TimeZone       `json:"zone"`
	WorkingDays      []time.Weekday `json:"working_days"`
	Holidays         []string       `json:"holidays"`
	OpenMinute       int            `json:"open_minute"`
	CloseMinute      int            `json:"close_minute"`
	FiscalStartMonth time.Month     `json:"fiscal_start_month"`
}

func (c BusinessCalendar) Validate() error {
	if c.ID == "" || c.Version == "" || len(c.WorkingDays) == 0 || c.OpenMinute < 0 || c.CloseMinute > 1440 || c.OpenMinute >= c.CloseMinute || c.FiscalStartMonth < 1 || c.FiscalStartMonth > 12 {
		return errors.New("invalid calendar")
	}
	if _, e := c.Zone.Location(); e != nil {
		return e
	}
	for _, d := range c.WorkingDays {
		if d < 0 || d > 6 {
			return errors.New("invalid weekday")
		}
	}
	for _, day := range c.Holidays {
		if _, e := time.Parse("2006-01-02", day); e != nil {
			return e
		}
	}
	return nil
}

type DocumentNumber struct {
	DraftID, Series, FinalNumber string
	Posted                       bool
	GaplessRequired              bool
}

func (n DocumentNumber) Validate() error {
	if n.DraftID == "" || (n.Posted && (n.Series == "" || n.FinalNumber == "")) || (!n.Posted && n.FinalNumber != "") {
		return errors.New("draft/final numbering mismatch")
	}
	return nil
}

// Allocators must use the same posting transaction; no standalone counter execution.
type NumberAllocator interface {
	AllocateInPostingTransaction(PostingTransaction, string) (DocumentNumber, error)
}
type PostingTransaction interface{ PostingTransactionMarker() }
