package rules

import (
	"math/big"
	"regexp"
	"strings"
)

// Every number is an integer count of 10^-18 units. No floating point conversion.
type decimal struct{ units *big.Int }

var fixed = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)

func scale() *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil) }
func checked(n *big.Int) (decimal, error) {
	if new(big.Int).Abs(n).Cmp(new(big.Int).Mul(scale(), scale())) > 0 {
		return decimal{}, ErrArithmetic
	}
	return decimal{new(big.Int).Set(n)}, nil
}
func number(s string) (decimal, error) {
	if len(s) > 64 || !fixed.MatchString(s) {
		return decimal{}, ErrInput
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	parts := strings.Split(s, ".")
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	n, ok := new(big.Int).SetString(parts[0]+frac+strings.Repeat("0", 18-len(frac)), 10)
	if !ok {
		return decimal{}, ErrInput
	}
	if neg {
		n.Neg(n)
	}
	return checked(n)
}
func (d decimal) String() string {
	s := new(big.Int).Abs(d.units).String()
	if len(s) < 19 {
		s = strings.Repeat("0", 19-len(s)) + s
	}
	s = strings.TrimRight(strings.TrimRight(s[:len(s)-18]+"."+s[len(s)-18:], "0"), ".")
	if d.units.Sign() < 0 {
		s = "-" + s
	}
	return s
}

// roundQuotient is symmetric round-half-to-even, including negative ties.
func roundQuotient(n, d *big.Int) (*big.Int, error) {
	if d.Sign() == 0 {
		return nil, ErrArithmetic
	}
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(n, d, r)
	cmp := new(big.Int).Mul(new(big.Int).Abs(r), big.NewInt(2)).Cmp(new(big.Int).Abs(d))
	if cmp > 0 || cmp == 0 && q.Bit(0) == 1 {
		if n.Sign()*d.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q, nil
}
func calculate(op string, a, b decimal) (decimal, error) {
	var n *big.Int
	var e error
	switch op {
	case "add":
		n = new(big.Int).Add(a.units, b.units)
	case "sub":
		n = new(big.Int).Sub(a.units, b.units)
	case "mul":
		n, e = roundQuotient(new(big.Int).Mul(a.units, b.units), scale())
	case "div":
		n, e = roundQuotient(new(big.Int).Mul(a.units, scale()), b.units)
	default:
		return decimal{}, ErrArithmetic
	}
	if e != nil {
		return decimal{}, e
	}
	return checked(n)
}
func rounded(d decimal, places int) (decimal, error) {
	if places < 0 || places > 18 {
		return decimal{}, ErrArithmetic
	}
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-places)), nil)
	q, e := roundQuotient(d.units, unit)
	if e != nil {
		return decimal{}, e
	}
	return checked(q.Mul(q, unit))
}
func (d decimal) integer() (int64, error) {
	q, r := new(big.Int), new(big.Int)
	q.QuoRem(d.units, scale(), r)
	if r.Sign() != 0 || !q.IsInt64() {
		return 0, ErrInput
	}
	return q.Int64(), nil
}
