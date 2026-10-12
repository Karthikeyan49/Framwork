package rules

import (
	"context"
	"encoding/json"
	"github.com/Karthikeyan49/Framwork/internal/erp"
	"math/big"
	"sort"
	"strings"
	"time"
)

// Evaluate accepts only a compiled Program and explicit field values. It cannot
// obtain data, time, identity, authorization, secrets or tool capabilities itself.
func (p Program) Evaluate(ctx context.Context, values map[string]json.RawMessage) (Value, error) {
	if !p.Valid() || ctx == nil {
		return Value{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return Value{}, e
	}
	plan := p.Plan()
	if len(values) > 32 {
		return Value{}, ErrBudget
	}
	fields := map[string]Field{}
	for _, f := range plan.Fields {
		fields[f.ID] = f
	}
	inputs := map[string]Value{}
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		raw := values[id]
		f, ok := fields[id]
		if !ok {
			return Value{}, ErrInput
		}
		v, e := input(f.Type, raw)
		if e != nil {
			return Value{}, e
		}
		if v.nilValue && f.Required {
			return Value{}, ErrInput
		}
		if f.Type.Kind == "enum" && !v.nilValue {
			ok := false
			for _, s := range f.EnumValues {
				if s == v.text {
					ok = true
				}
			}
			if !ok {
				return Value{}, ErrInput
			}
		}
		inputs[id] = v
	}
	steps := 0
	work := 0
	var eval func(Node) (Value, error)
	eval = func(n Node) (Value, error) {
		steps++
		work++
		if steps > MaxNodes || work > MaxWork {
			return Value{}, ErrBudget
		}
		if e := ctx.Err(); e != nil {
			return Value{}, e
		}
		if n.Op == "literal" || n.Op == "typed_literal" {
			return literal(Expression{Op: n.Op, Value: n.Value, ValueType: n.ValueType})
		}
		if n.Op == "field" {
			v, ok := inputs[n.FieldRef]
			if !ok {
				if fields[n.FieldRef].Required {
					return Value{}, ErrInput
				}
				return Value{typ: n.Type, nilValue: true}, nil
			}
			return v, nil
		}
		// Lazy branches and boolean operators prevent errors in unreachable operands.
		if n.Op == "and" || n.Op == "or" {
			for _, a := range n.Args {
				v, e := eval(a)
				if e != nil {
					return Value{}, e
				}
				b, e := v.Bool()
				if e != nil {
					return Value{}, ErrNull
				}
				if n.Op == "and" && !b || n.Op == "or" && b {
					return Value{typ: n.Type, flag: b}, nil
				}
			}
			return Value{typ: n.Type, flag: n.Op == "and"}, nil
		}
		if n.Op == "if" {
			v, e := eval(n.Args[0])
			if e != nil {
				return Value{}, e
			}
			b, e := v.Bool()
			if e != nil {
				return Value{}, ErrNull
			}
			i := 2
			if b {
				i = 1
			}
			v, e = eval(n.Args[i])
			v.typ = n.Type
			return v, e
		}
		if n.Op == "coalesce" {
			v, e := eval(n.Args[0])
			if e != nil {
				return Value{}, e
			}
			if v.nilValue {
				v, e = eval(n.Args[1])
			}
			v.typ = n.Type
			return v, e
		}
		args := []Value{}
		for _, a := range n.Args {
			v, e := eval(a)
			if e != nil {
				return Value{}, e
			}
			args = append(args, v)
		}
		if n.Op == "is_null" {
			return Value{typ: n.Type, flag: args[0].nilValue}, nil
		}
		if n.Op == "eq" || n.Op == "neq" {
			same := false
			if args[0].nilValue || args[1].nilValue {
				same = args[0].nilValue && args[1].nilValue
			} else {
				c, e := compare(args[0], args[1])
				if e != nil {
					return Value{}, e
				}
				same = c == 0
			}
			if n.Op == "neq" {
				same = !same
			}
			return Value{typ: n.Type, flag: same}, nil
		}
		for _, a := range args {
			if a.nilValue {
				return Value{}, ErrNull
			}
		}
		v := Value{typ: n.Type}
		switch n.Op {
		case "not":
			v.flag = !args[0].flag
		case "gt", "gte", "lt", "lte":
			c, e := compare(args[0], args[1])
			if e != nil {
				return Value{}, e
			}
			v.flag = n.Op == "gt" && c > 0 || n.Op == "gte" && c >= 0 || n.Op == "lt" && c < 0 || n.Op == "lte" && c <= 0
		case "contains":
			v.flag = strings.Contains(args[0].text, args[1].text)
		case "concat":
			v.text = args[0].text + args[1].text
		case "lower":
			v.text = strings.ToLower(args[0].text)
		case "upper":
			v.text = strings.ToUpper(args[0].text)
		case "trim":
			v.text = strings.TrimSpace(args[0].text)
		case "list":
			v.list = args
		case "in", "not_in":
			for _, item := range args[1].list {
				c, e := compare(args[0], item)
				if e != nil {
					return Value{}, e
				}
				if c == 0 {
					v.flag = true
					break
				}
			}
			if n.Op == "not_in" {
				v.flag = !v.flag
			}
		case "add", "sub", "mul", "div":
			num, e := calculate(n.Op, args[0].n, args[1].n)
			if e != nil {
				return Value{}, e
			}
			v.n = num
		case "round", "money_round":
			places := 0
			if n.Op == "money_round" {
				places, _ = erp.Currency(n.Type.Currency).MinorUnits()
			} else {
				x, e := args[1].n.integer()
				if e != nil || x < 0 || x > 18 {
					return Value{}, ErrArithmetic
				}
				places = int(x)
			}
			num, e := rounded(args[0].n, places)
			if e != nil {
				return Value{}, e
			}
			v.n = num
		case "date_add_days", "datetime_add_seconds":
			delta, e := args[1].n.integer()
			if e != nil {
				return Value{}, e
			}
			t, e := parseDate(args[0].text)
			if n.Op == "datetime_add_seconds" {
				t, e = parseInstant(args[0].text)
			}
			if e != nil {
				return Value{}, e
			}
			bound := int64(MaxDays)
			if n.Op == "datetime_add_seconds" {
				bound *= 86400
			}
			if delta < -bound || delta > bound {
				return Value{}, ErrBudget
			}
			if n.Op == "date_add_days" {
				t = t.AddDate(0, 0, int(delta))
				v.text = t.Format("2006-01-02")
			} else {
				t = t.Add(time.Duration(delta) * time.Second)
				v.text = t.UTC().Format(time.RFC3339Nano)
			}
			if t.Year() < 1 || t.Year() > 9999 {
				return Value{}, ErrDate
			}
		case "date_diff_days":
			a, e := parseDate(args[0].text)
			if e != nil {
				return Value{}, e
			}
			b, e := parseDate(args[1].text)
			if e != nil {
				return Value{}, e
			}
			days := (a.Unix() - b.Unix()) / 86400
			v.n, _ = checked(new(big.Int).Mul(big.NewInt(days), scale()))
		case "date_in_zone":
			t, e := parseInstant(args[0].text)
			if e != nil {
				return Value{}, e
			}
			z, e := location(args[1].text)
			if e != nil {
				return Value{}, e
			}
			t = t.In(z)
			if t.Year() < 1 || t.Year() > 9999 {
				return Value{}, ErrDate
			}
			v.text = t.Format("2006-01-02")
		case "is_business_day", "business_add_days", "business_is_open", "fiscal_year":
			idx := 1
			if n.Op == "business_add_days" {
				idx = 2
			}
			var cal erp.BusinessCalendar
			found := false
			for _, c := range plan.Calendars {
				if calendarKey(c) == args[idx].text {
					cal = c
					found = true
					break
				}
			}
			if !found {
				return Value{}, ErrDate
			}
			t, e := parseDate(args[0].text)
			if n.Op == "business_is_open" {
				t, e = parseInstant(args[0].text)
				if e == nil {
					z, err := location(string(cal.Zone))
					if err != nil {
						return Value{}, err
					}
					t = t.In(z)
				}
			}
			if e != nil || t.Year() < 1 || t.Year() > 9999 {
				return Value{}, ErrDate
			}
			switch n.Op {
			case "is_business_day":
				v.flag = businessDay(t, cal)
			case "business_is_open":
				minute := t.Hour()*60 + t.Minute()
				v.flag = businessDay(t, cal) && minute >= cal.OpenMinute && minute < cal.CloseMinute
			case "fiscal_year":
				year := t.Year()
				if t.Month() < cal.FiscalStartMonth {
					year--
				}
				if year < 1 {
					return Value{}, ErrDate
				}
				v.n, _ = checked(new(big.Int).Mul(big.NewInt(int64(year)), scale()))
			case "business_add_days":
				delta, e := args[1].n.integer()
				if e != nil {
					return Value{}, e
				}
				if delta < -MaxDays || delta > MaxDays {
					return Value{}, ErrBudget
				}
				direction := 1
				if delta < 0 {
					direction = -1
					delta = -delta
				}
				for attempts := 0; delta > 0; attempts++ {
					work++
					if work > MaxWork || attempts >= MaxDays*7 {
						return Value{}, ErrBudget
					}
					if e = ctx.Err(); e != nil {
						return Value{}, e
					}
					t = t.AddDate(0, 0, direction)
					if t.Year() < 1 || t.Year() > 9999 {
						return Value{}, ErrDate
					}
					if businessDay(t, cal) {
						delta--
					}
				}
				v.text = t.Format("2006-01-02")
			}
		default:
			return Value{}, ErrInvalid
		}
		if len(v.text) > MaxString {
			return Value{}, ErrBudget
		}
		return v, nil
	}
	result, e := eval(plan.Root)
	if e != nil {
		return Value{}, e
	}
	raw, e := json.Marshal(result)
	if e != nil || len(raw) > 256<<10 {
		return Value{}, ErrBudget
	}
	return result, nil
}
func compare(a, b Value) (int, error) {
	if a.nilValue || b.nilValue {
		return 0, ErrNull
	}
	if scalar(a.typ) && scalar(b.typ) || a.typ.Kind == "money" && equalType(a.typ, b.typ) {
		return a.n.units.Cmp(b.n.units), nil
	}
	if a.typ.Kind == "datetime" && b.typ.Kind == "datetime" {
		x, e := parseInstant(a.text)
		if e != nil {
			return 0, e
		}
		y, e := parseInstant(b.text)
		if e != nil {
			return 0, e
		}
		return x.Compare(y), nil
	}
	if a.typ.Kind == "bool" && b.typ.Kind == "bool" {
		if a.flag == b.flag {
			return 0, nil
		}
		if a.flag {
			return 1, nil
		}
		return -1, nil
	}
	if compatible(a.typ, b.typ) && a.typ.Kind != "list" {
		return strings.Compare(a.text, b.text), nil
	}
	return 0, ErrInput
}
func businessDay(t time.Time, c erp.BusinessCalendar) bool {
	day := t.Format("2006-01-02")
	i := sort.SearchStrings(c.Holidays, day)
	if i < len(c.Holidays) && c.Holidays[i] == day {
		return false
	}
	for _, d := range c.WorkingDays {
		if t.Weekday() == d {
			return true
		}
	}
	return false
}
