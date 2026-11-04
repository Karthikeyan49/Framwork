package rules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Karthikeyan49/Framwork/internal/erp"
	"math/big"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"
)

func lit(kind string, x any) Expression {
	raw, _ := json.Marshal(x)
	return Expression{Op: "typed_literal", ValueType: kind, Value: raw}
}
func op(name string, args ...Expression) Expression { return Expression{Op: name, Args: args} }
func money(s, c string) Expression {
	return lit("money", map[string]string{"amount": s, "currency": c})
}
func field(id string) Expression { return Expression{Op: "field", FieldRef: id} }
func calendar() erp.BusinessCalendar {
	return erp.BusinessCalendar{ID: "calendar.office", Version: "1", Zone: "Asia/Kolkata", WorkingDays: []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}, Holidays: []string{"2026-10-14"}, OpenMinute: 540, CloseMinute: 1080, FiscalStartMonth: time.April}
}
func def(expr Expression, result string) Definition {
	return Definition{ID: "rule.test", EntityID: "entity.test", Version: Version, ResultType: result, Fields: []Field{{ID: "field.test.note", Type: Type{Kind: "string"}}}, Calendars: []erp.BusinessCalendar{calendar()}, Expression: expr}
}
func executable(t testing.TB, d Definition) Program {
	t.Helper()
	p, e := Compile(d)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func output(t testing.TB, v Value) any {
	t.Helper()
	raw, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var out struct {
		Value any `json:"value"`
	}
	if json.Unmarshal(raw, &out) != nil {
		t.Fatal("result JSON")
	}
	return out.Value
}

func TestEveryOperatorAndExactValues(t *testing.T) {
	one := lit("integer", 1)
	two := lit("integer", 2)
	str := func(s string) Expression { return lit("string", s) }
	cases := []struct {
		name, kind string
		expr       Expression
		want       any
	}{
		{"eq", "bool", op("eq", one, lit("decimal", "1.000")), true}, {"neq", "bool", op("neq", one, two), true},
		{"gt", "bool", op("gt", two, one), true}, {"gte", "bool", op("gte", one, one), true}, {"lt", "bool", op("lt", one, two), true}, {"lte", "bool", op("lte", two, two), true},
		{"and", "bool", op("and", lit("bool", true), lit("bool", false)), false}, {"or", "bool", op("or", lit("bool", false), lit("bool", true)), true}, {"not", "bool", op("not", lit("bool", false)), true},
		{"null", "bool", op("is_null", lit("null", nil)), true}, {"null equality", "bool", op("eq", lit("null", nil), lit("null", nil)), true},
		{"contains", "bool", op("contains", str("hello ERP"), str("ERP")), true}, {"concat", "string", op("concat", str("ERP"), str("CRM")), "ERPCRM"},
		{"lower", "string", op("lower", str("ÉRP")), "érp"}, {"upper", "string", op("upper", str("erp")), "ERP"}, {"trim", "string", op("trim", str("  ERP \n")), "ERP"},
		{"in", "bool", op("in", one, op("list", one, two)), true}, {"not in", "bool", op("not_in", lit("integer", 3), op("list", one, two)), true},
		{"add", "decimal", op("add", lit("decimal", "0.1"), lit("decimal", "0.2")), "0.3"}, {"sub", "integer", op("sub", two, one), "1"},
		{"mul", "decimal", op("mul", lit("decimal", "1.234567890123456789"), two), "2.469135780246913578"}, {"div", "decimal", op("div", one, lit("integer", 3)), "0.333333333333333333"},
		{"half even positive", "decimal", op("round", lit("decimal", "1.005"), two), "1"}, {"half even negative", "decimal", op("round", lit("decimal", "-1.015"), two), "-1.02"},
		{"if lazy", "decimal", op("if", lit("bool", true), lit("decimal", "5"), op("div", one, lit("integer", 0))), "5"},
		{"coalesce lazy", "decimal", op("coalesce", lit("decimal", "5"), op("div", one, lit("integer", 0))), "5"},
		{"date add", "date", op("date_add_days", lit("date", "2024-02-28"), two), "2024-03-01"},
		{"date difference full range", "integer", op("date_diff_days", lit("date", "9999-12-31"), lit("date", "0001-01-01")), "3652058"},
		{"instant add", "datetime", op("datetime_add_seconds", lit("datetime", "2026-03-08T06:59:59Z"), one), "2026-03-08T07:00:00Z"},
		{"explicit zone", "date", op("date_in_zone", lit("datetime", "2026-10-12T23:00:00Z"), str("Asia/Kolkata")), "2026-10-13"},
		{"holiday weekend", "date", op("business_add_days", lit("date", "2026-10-13"), two, str("calendar.office@1")), "2026-10-16"},
		{"negative business days", "date", op("business_add_days", lit("date", "2026-10-16"), lit("integer", -2), str("calendar.office@1")), "2026-10-13"},
		{"holiday", "bool", op("is_business_day", lit("date", "2026-10-14"), str("calendar.office@1")), false},
		{"open", "bool", op("business_is_open", lit("datetime", "2026-10-12T03:30:00Z"), str("calendar.office@1")), true},
		{"close exclusive", "bool", op("business_is_open", lit("datetime", "2026-10-12T12:30:00Z"), str("calendar.office@1")), false},
		{"fiscal year", "integer", op("fiscal_year", lit("date", "2026-03-31"), str("calendar.office@1")), "2025"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := executable(t, def(tc.expr, tc.kind))
			v, e := p.Evaluate(context.Background(), nil)
			if e != nil {
				t.Fatal(e)
			}
			if fmt.Sprint(output(t, v)) != fmt.Sprint(tc.want) {
				t.Fatal(output(t, v), tc.want)
			}
		})
	}
	for _, tc := range []struct {
		expr     Expression
		want     string
		currency string
	}{
		{op("add", money("0.1", "USD"), money("0.2", "USD")), "0.3", "USD"},
		{op("sub", money("1", "USD"), money("2", "USD")), "-1", "USD"},
		{op("mul", money("12.3", "USD"), lit("decimal", "2.5")), "30.75", "USD"},
		{op("div", money("1", "USD"), lit("integer", 3)), "0.333333333333333333", "USD"},
		{op("money_round", money("1.005", "USD")), "1", "USD"},
		{op("money_round", money("1.2355", "KWD")), "1.236", "KWD"},
		{op("money_round", money("2.5", "JPY")), "2", "JPY"},
	} {
		v, e := executable(t, def(tc.expr, "money")).Evaluate(context.Background(), nil)
		if e != nil {
			t.Fatal(e)
		}
		m := output(t, v).(map[string]any)
		if m["amount"] != tc.want || m["currency"] != tc.currency {
			t.Fatal(m)
		}
	}
}

func TestAdmissionAndRuntimeFailClosed(t *testing.T) {
	for name, expr := range map[string]Expression{
		"currency mismatch": op("add", money("1", "USD"), money("1", "INR")), "money scalar sum": op("add", money("1", "USD"), lit("integer", 1)),
		"unknown function": op("http", lit("string", "https://example.com")), "ambient clock": op("now"), "wrong branch": op("if", lit("bool", true), lit("integer", 1), lit("string", "x")),
		"wrong owner": field("field.other.amount"), "missing calendar": op("business_add_days", lit("date", "2026-01-01"), lit("integer", 1), lit("string", "calendar.office@2")),
		"dynamic timezone": op("date_in_zone", lit("datetime", "2026-01-01T00:00:00Z"), field("field.test.note")),
		"Local":            op("date_in_zone", lit("datetime", "2026-01-01T00:00:00Z"), lit("string", "Local")), "exponent": lit("decimal", "1e2"), "bad date": lit("date", "2026-02-30"), "bad precision": lit("decimal", "1.0000000000000000001"),
	} {
		t.Run(name, func(t *testing.T) {
			if p, e := Compile(def(expr, "money")); e == nil || p.Valid() {
				t.Fatal("unsafe plan admitted")
			}
		})
	}
	for _, expr := range []Expression{op("div", lit("integer", 1), lit("integer", 0)), op("add", lit("decimal", "1000000000000000000"), lit("integer", 1)), op("round", lit("decimal", "1"), lit("integer", 19)), op("date_add_days", lit("date", "9999-12-31"), lit("integer", 1)), op("datetime_add_seconds", lit("datetime", "0001-01-01T00:00:00Z"), lit("integer", -1))} {
		kind := "decimal"
		if expr.Op == "date_add_days" {
			kind = "date"
		}
		if expr.Op == "datetime_add_seconds" {
			kind = "datetime"
		}
		p := executable(t, def(expr, kind))
		if _, e := p.Evaluate(context.Background(), nil); e == nil {
			t.Fatal("bad operation succeeded")
		}
	}
	d := def(op("is_null", field("field.test.note")), "bool")
	p := executable(t, d)
	v, e := p.Evaluate(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if b, _ := v.Bool(); !b {
		t.Fatal("missing optional not null")
	}
	d.Fields[0].Required = true
	p = executable(t, d)
	if _, e = p.Evaluate(context.Background(), nil); !errors.Is(e, ErrInput) {
		t.Fatal(e)
	}
	for _, expr := range []Expression{op("and", lit("bool", false), op("gt", op("div", lit("integer", 1), lit("integer", 0)), lit("integer", 1))), op("or", lit("bool", true), op("gt", op("div", lit("integer", 1), lit("integer", 0)), lit("integer", 1)))} {
		if _, e := executable(t, def(expr, "bool")).Evaluate(context.Background(), nil); e != nil {
			t.Fatal("lost short circuit", e)
		}
	}
	p = executable(t, def(lit("string", "ok"), "string"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Evaluate(ctx, nil); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	if _, e := p.Evaluate(context.Background(), map[string]json.RawMessage{"unknown": json.RawMessage(`"x"`)}); e == nil {
		t.Fatal("unknown input")
	}
	for _, raw := range []string{`{"amount":"1","amount":"2","currency":"USD"}`, `{"amount":"1","currency":"USD","extra":1}`, `{"amount":"1e2","currency":"USD"}`} {
		if _, e := input(Type{Kind: "money", Currency: "USD"}, []byte(raw)); e == nil {
			t.Fatal("invalid money JSON")
		}
	}
	expr := lit("bool", true)
	for i := 0; i < MaxDepth+1; i++ {
		expr = op("not", expr)
	}
	if _, e := Compile(def(expr, "bool")); e == nil {
		t.Fatal("depth not bounded")
	}
	if _, e := Compile(def(op("concat", lit("string", strings.Repeat("x", MaxString)), lit("string", strings.Repeat("x", MaxString))), "string")); e != nil {
		t.Fatal(e)
	} else {
		p := executable(t, def(op("concat", lit("string", strings.Repeat("x", MaxString)), lit("string", "x")), "string"))
		if _, e = p.Evaluate(context.Background(), nil); !errors.Is(e, ErrBudget) {
			t.Fatal(e)
		}
	}
}

func TestPinnedTimezoneDSTCalendarsAndImmutability(t *testing.T) {
	t.Setenv("ZONEINFO", t.TempDir()+"/invalid.zip")
	t.Setenv("TZ", "Pacific/Apia")
	c := calendar()
	c.Zone = "America/New_York"
	c.WorkingDays = []time.Weekday{time.Sunday}
	c.OpenMinute = 60
	c.CloseMinute = 120
	for _, tc := range []struct {
		instant string
		open    bool
	}{{"2026-11-01T05:30:00Z", true}, {"2026-11-01T06:30:00Z", true}, {"2026-03-08T07:30:00Z", false}} {
		d := def(op("business_is_open", lit("datetime", tc.instant), lit("string", "calendar.office@1")), "bool")
		d.Calendars = []erp.BusinessCalendar{c}
		p := executable(t, d)
		v, e := p.Evaluate(context.Background(), nil)
		if e != nil {
			t.Fatal(e)
		}
		if b, _ := v.Bool(); b != tc.open {
			t.Fatal(tc)
		}
		before := p.Hash()
		copy := p.Plan()
		copy.Calendars[0].Zone = "Local"
		copy.Root.Type.Kind = "money"
		raw := p.JSON()
		raw[0] = '!'
		if p.Hash() != before {
			t.Fatal("mutable plan")
		}
		if _, e := Restore(p.JSON(), p.Hash()); e != nil {
			t.Fatal(e)
		}
	}
	d := def(op("date_in_zone", lit("datetime", "2026-04-04T15:00:00Z"), lit("string", "Australia/Lord_Howe")), "date")
	if _, e := executable(t, d).Evaluate(context.Background(), nil); e != nil {
		t.Fatal(e)
	}
	d = def(op("date_in_zone", lit("datetime", "0001-01-01T00:00:00Z"), lit("string", "America/New_York")), "date")
	if _, e := executable(t, d).Evaluate(context.Background(), nil); !errors.Is(e, ErrDate) {
		t.Fatal("year boundary", e)
	}
	d = def(lit("bool", true), "bool")
	d.Calendars[0].WorkingDays = append(d.Calendars[0].WorkingDays, time.Monday)
	if _, e := Compile(d); e == nil {
		t.Fatal("duplicate weekday")
	}
}

func TestRestoreTamperingRejectedAndConcurrentDeterminism(t *testing.T) {
	p := executable(t, def(op("add", lit("decimal", "0.1"), lit("decimal", "0.2")), "decimal"))
	for _, alter := range []func(*Plan){func(p *Plan) { p.Root.Type.Kind = "integer" }, func(p *Plan) { p.Root.Op = "execute_sql" }, func(p *Plan) { p.TZDataHash = strings.Repeat("0", 64) }, func(p *Plan) { p.Fields[0].Type.Currency = "USD" }, func(p *Plan) { p.Version = "3.0.0" }} {
		x := p.Plan()
		alter(&x)
		raw, _ := json.Marshal(x)
		if _, e := Restore(raw, Hash(raw)); e == nil {
			t.Fatal("tampered plan restored")
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Go(func() {
			for i := 0; i < 20; i++ {
				v, e := p.Evaluate(context.Background(), nil)
				if e != nil || output(t, v) != "0.3" {
					t.Error("nondeterministic", e)
				}
			}
		})
	}
	wg.Wait()
}

func TestMoneyArithmeticProperties(t *testing.T) {
	check := func(a, b int32) bool {
		as := new(big.Rat).SetFrac(big.NewInt(int64(a)), big.NewInt(100)).FloatString(2)
		bs := new(big.Rat).SetFrac(big.NewInt(int64(b)), big.NewInt(100)).FloatString(2)
		expr := op("sub", op("add", money(as, "USD"), money(bs, "USD")), money(bs, "USD"))
		p, e := Compile(def(expr, "money"))
		if e != nil {
			return false
		}
		v, e := p.Evaluate(context.Background(), nil)
		if e != nil {
			return false
		}
		want, _ := number(as)
		return v.n.String() == want.String()
	}
	if e := quick.Check(check, &quick.Config{MaxCount: 500, Rand: rand.New(rand.NewSource(7))}); e != nil {
		t.Fatal(e)
	}
}

func FuzzRuleRestoreAndInputs(f *testing.F) {
	d := def(op("add", field("field.test.note"), lit("decimal", "0.1")), "decimal")
	d.Fields[0].Type.Kind = "decimal"
	p, e := Compile(d)
	if e != nil {
		f.Fatal(e)
	}
	f.Add([]byte(`"0.2"`))
	f.Add([]byte(`1e1000`))
	f.Add([]byte(`{"amount":"2"}`))
	f.Add(p.JSON())
	f.Fuzz(func(t *testing.T, raw []byte) {
		a, ea := p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": raw})
		b, eb := p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": raw})
		if fmt.Sprint(ea) != fmt.Sprint(eb) {
			t.Fatal("error changed")
		}
		if ea == nil {
			ra, _ := json.Marshal(a)
			rb, _ := json.Marshal(b)
			if string(ra) != string(rb) {
				t.Fatal("value changed")
			}
		}
		restored, e := Restore(raw, Hash(raw))
		if e == nil {
			if _, e := restored.Evaluate(context.Background(), nil); e == nil && !restored.Valid() {
				t.Fatal("invalid program")
			}
		}
	})
}

func TestListsEnumsReferencesAndGlobalBudgets(t *testing.T) {
	for _, s := range []string{"2026-01-01T00:00:00+24:00", "2026-01-01T00:00:00+00:60", "2026-01-01T24:00:00Z", "2026-01-01T00:00:00.1234567890Z"} {
		if _, e := Compile(def(lit("datetime", s), "datetime")); e == nil {
			t.Fatal("invalid instant literal", s)
		}
		raw, _ := json.Marshal(s)
		if _, e := input(Type{Kind: "datetime"}, raw); e == nil {
			t.Fatal("invalid instant input", s)
		}
	}
	d := def(op("list", field("field.test.note"), field("field.test.note")), "list")
	p := executable(t, d)
	v, e := p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": json.RawMessage(`"hello"`)})
	if e != nil || v.Type().Element.Kind != "string" {
		t.Fatal(e)
	}
	d.Expression = op("eq", field("field.test.note"), lit("string", "active"))
	d.ResultType = "bool"
	d.Fields[0].Type = Type{Kind: "enum"}
	d.Fields[0].EnumValues = []string{"active", "inactive"}
	p = executable(t, d)
	if _, e = p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": json.RawMessage(`"unknown"`)}); e == nil {
		t.Fatal("unknown enum")
	}
	d.Fields[0].Type = Type{Kind: "reference", ReferenceTo: "entity.target"}
	d.Fields[0].EnumValues = nil
	d.Expression = op("eq", field("field.test.note"), lit("string", "11111111-1111-1111-1111-111111111111"))
	p = executable(t, d)
	if _, e = p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": json.RawMessage(`"not-a-reference"`)}); e == nil {
		t.Fatal("invalid reference")
	}
	d = def(lit("bool", true), "bool")
	d.Calendars[0].WorkingDays = []time.Weekday{time.Monday}
	d.Calendars[0].Holidays = nil
	slow := op("business_add_days", lit("date", "2026-10-12"), lit("integer", MaxDays), lit("string", "calendar.office@1"))
	d.Expression = op("eq", slow, slow)
	p = executable(t, d)
	if _, e = p.Evaluate(context.Background(), nil); !errors.Is(e, ErrBudget) {
		t.Fatal("global calendar fuel not enforced", e)
	}
	d = def(Expression{Op: "list"}, "list")
	for i := 0; i < MaxList; i++ {
		d.Expression.Args = append(d.Expression.Args, field("field.test.note"))
	}
	p = executable(t, d)
	raw, _ := json.Marshal(strings.Repeat("x", MaxString))
	if _, e = p.Evaluate(context.Background(), map[string]json.RawMessage{"field.test.note": raw}); !errors.Is(e, ErrBudget) {
		t.Fatal("output budget", e)
	}
}

func FuzzRuleCompile(f *testing.F) {
	raw, _ := json.Marshal(op("eq", lit("integer", 1), lit("integer", 1)))
	f.Add(raw)
	f.Add([]byte(`{"op":"list","args":[]}`))
	f.Add([]byte(`{"op":"http","args":[]}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 256<<10 {
			return
		}
		var expr Expression
		if json.Unmarshal(raw, &expr) != nil {
			return
		}
		p, e := Compile(def(expr, "bool"))
		if e != nil {
			return
		}
		a, ea := p.Evaluate(context.Background(), nil)
		b, eb := p.Evaluate(context.Background(), nil)
		if fmt.Sprint(ea) != fmt.Sprint(eb) {
			t.Fatal("error nondeterminism")
		}
		if ea == nil {
			ra, _ := json.Marshal(a)
			rb, _ := json.Marshal(b)
			if string(ra) != string(rb) {
				t.Fatal("value nondeterminism")
			}
		}
		if _, e = Restore(p.JSON(), p.Hash()); e != nil {
			t.Fatal("compiled plan did not restore", e)
		}
	})
}

func FuzzFixedDecimalMoney(f *testing.F) {
	f.Add("0.1", "0.2", uint8(0))
	f.Add("-1.005", "2", uint8(2))
	f.Add("1000000000000000000", "1", uint8(0))
	f.Add("1", "0", uint8(3))
	f.Fuzz(func(t *testing.T, a, b string, which uint8) {
		ops := []string{"add", "sub", "mul", "div"}
		expr := op(ops[which%4], money(a, "USD"), money(b, "USD"))
		if which%4 >= 2 {
			expr.Args[1] = lit("decimal", b)
		}
		p, e := Compile(def(expr, "money"))
		if e != nil {
			return
		}
		x, ex := p.Evaluate(context.Background(), nil)
		y, ey := p.Evaluate(context.Background(), nil)
		if fmt.Sprint(ex) != fmt.Sprint(ey) {
			t.Fatal("error changed")
		}
		if ex == nil {
			if x.n.String() != y.n.String() || x.typ.Currency != "USD" {
				t.Fatal("money determinism")
			}
			n, e := number(x.n.String())
			if e != nil || n.units.Cmp(x.n.units) != 0 {
				t.Fatal("precision lost")
			}
		}
	})
}

func TestIndependentM7MoneyRationalOracle(t *testing.T) {
	source := rand.New(rand.NewSource(47))
	unit := new(big.Int).SetUint64(1000000000000000000)
	for trial := 0; trial < 500; trial++ {
		a := new(big.Rat).SetFrac(big.NewInt(int64(int32(source.Uint32()))), big.NewInt(1000))
		b := new(big.Rat).SetFrac(big.NewInt(int64(int32(source.Uint32()))), big.NewInt(1000))
		for _, name := range []string{"add", "sub", "mul", "div"} {
			if name == "div" && b.Sign() == 0 {
				continue
			}
			want := new(big.Rat)
			switch name {
			case "add":
				want.Add(a, b)
			case "sub":
				want.Sub(a, b)
			case "mul":
				want.Mul(a, b)
			case "div":
				want.Quo(a, b)
			}
			scaled := new(big.Rat).Mul(want, new(big.Rat).SetInt(unit))
			positive := new(big.Int).Abs(scaled.Num())
			q, r := new(big.Int), new(big.Int)
			q.QuoRem(positive, scaled.Denom(), r)
			cmp := new(big.Int).Lsh(r, 1).Cmp(scaled.Denom())
			if cmp > 0 || cmp == 0 && q.Bit(0) == 1 {
				q.Add(q, big.NewInt(1))
			}
			if want.Sign() < 0 {
				q.Neg(q)
			}
			expected := new(big.Rat).SetFrac(q, unit)
			rhs := money(b.FloatString(3), "USD")
			if name == "mul" || name == "div" {
				rhs = lit("decimal", b.FloatString(3))
			}
			d := def(op(name, money(a.FloatString(3), "USD"), rhs), "money")
			d.Calendars = nil
			v, e := executable(t, d).Evaluate(context.Background(), nil)
			if e != nil {
				t.Fatalf("trial%d %s failed: %v", trial, name, e)
			}
			m := output(t, v).(map[string]any)
			actual, ok := new(big.Rat).SetString(m["amount"].(string))
			if !ok || actual.Cmp(expected) != 0 || m["currency"] != "USD" {
				t.Fatalf("trial%d %s differs from rational oracle", trial, name)
			}
		}
	}
	t.Log("2000 signed Money operations match independently quantized rational oracle at18 decimals")
}
