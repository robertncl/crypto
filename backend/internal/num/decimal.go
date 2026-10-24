// Package num provides a fixed-point decimal type for representing money and
// quantities. Floating point must never be used for balances, prices, or order
// quantities because of rounding error; everything is stored as an int64 scaled
// by Scale (8 decimal places), which mirrors how most exchanges represent funds.
package num

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"strings"
)

// Decimals is the number of fractional digits represented by a Dec.
const Decimals = 8

// Scale is 10^Decimals: the integer factor by which real values are multiplied
// to obtain the internal representation.
const Scale int64 = 100_000_000

// maxDecimalLen bounds the length of a parseable decimal string. The largest
// in-range value (int64 max, ~9.2e18, scaled) needs ~19 integer + 8 fractional
// digits; 40 is generous. Rejecting longer inputs prevents a malicious caller
// from forcing huge big.Int allocations/CPU via a million-digit number.
const maxDecimalLen = 40

var (
	bigScale = big.NewInt(Scale)

	// ErrParse is returned when a string cannot be parsed into a Dec.
	ErrParse = errors.New("num: invalid decimal")
	// ErrDivZero is returned on division by zero.
	ErrDivZero = errors.New("num: division by zero")
)

// Dec is a fixed-point decimal. The zero value is 0. The real value it
// represents is i / Scale.
type Dec struct {
	i int64
}

// Zero is the additive identity.
var Zero = Dec{}

// MaxDec and MinDec bound the representable range. Arithmetic whose exact
// result falls outside it saturates to these values instead of silently
// wrapping around, so an oversized operand can never turn into a small (or
// negative) amount. The range is symmetric so Neg never overflows.
var (
	MaxDec = Dec{i: math.MaxInt64}
	MinDec = Dec{i: -math.MaxInt64}
)

// saturate returns the bound in the direction of an overflowed result.
func saturate(negative bool) Dec {
	if negative {
		return MinDec
	}
	return MaxDec
}

// uabs returns |x| as a uint64 (well defined for math.MinInt64 too).
func uabs(x int64) uint64 {
	u := uint64(x)
	if x < 0 {
		u = -u
	}
	return u
}

// signed applies a sign to a magnitude, reporting false if it does not fit.
func signed(mag uint64, negative bool) (int64, bool) {
	if mag > math.MaxInt64 {
		return 0, false
	}
	if negative {
		return -int64(mag), true
	}
	return int64(mag), true
}

// FromRaw builds a Dec from an already-scaled integer (i.e. value * Scale). This
// is the representation persisted in the database.
func FromRaw(raw int64) Dec { return Dec{i: raw} }

// FromInt builds a Dec from a whole number (saturating if out of range).
func FromInt(n int64) Dec { return Dec{i: n}.MulRaw(Scale) }

// Raw returns the underlying scaled integer, suitable for storage.
func (d Dec) Raw() int64 { return d.i }

// IsZero reports whether d == 0.
func (d Dec) IsZero() bool { return d.i == 0 }

// Sign returns -1, 0 or +1.
func (d Dec) Sign() int {
	switch {
	case d.i < 0:
		return -1
	case d.i > 0:
		return 1
	default:
		return 0
	}
}

// Neg returns -d.
func (d Dec) Neg() Dec { return Dec{i: -d.i} }

// Add returns d + e, saturating at MaxDec/MinDec on overflow.
func (d Dec) Add(e Dec) Dec {
	s := d.i + e.i
	if (s > d.i) != (e.i > 0) || s == math.MinInt64 {
		return saturate(e.i < 0)
	}
	return Dec{i: s}
}

// Sub returns d - e, saturating at MaxDec/MinDec on overflow.
func (d Dec) Sub(e Dec) Dec {
	s := d.i - e.i
	if (s < d.i) != (e.i > 0) || s == math.MinInt64 {
		return saturate(e.i > 0)
	}
	return Dec{i: s}
}

// Cmp compares d and e, returning -1, 0 or +1.
func (d Dec) Cmp(e Dec) int {
	switch {
	case d.i < e.i:
		return -1
	case d.i > e.i:
		return 1
	default:
		return 0
	}
}

// Lt, Lte, Gt, Gte, Eq are comparison helpers.
func (d Dec) Lt(e Dec) bool  { return d.i < e.i }
func (d Dec) Lte(e Dec) bool { return d.i <= e.i }
func (d Dec) Gt(e Dec) bool  { return d.i > e.i }
func (d Dec) Gte(e Dec) bool { return d.i >= e.i }
func (d Dec) Eq(e Dec) bool  { return d.i == e.i }

// Min returns the smaller of d and e.
func Min(d, e Dec) Dec {
	if d.i < e.i {
		return d
	}
	return e
}

// Max returns the larger of d and e.
func Max(d, e Dec) Dec {
	if d.i > e.i {
		return d
	}
	return e
}

// Mul returns d * e, truncated toward zero. The full 128-bit product is
// computed with math/bits (no allocation), so intermediates never overflow; a
// result outside the representable range saturates to MaxDec/MinDec. Use
// MulChecked where an out-of-range product must be rejected rather than capped.
func (d Dec) Mul(e Dec) Dec {
	v, ok := d.MulChecked(e)
	if !ok {
		return saturate((d.i < 0) != (e.i < 0))
	}
	return v
}

// MulChecked returns d * e and false if the result does not fit in a Dec.
func (d Dec) MulChecked(e Dec) (Dec, bool) {
	hi, lo := bits.Mul64(uabs(d.i), uabs(e.i))
	if hi >= uint64(Scale) { // quotient would need more than 64 bits
		return Zero, false
	}
	q, _ := bits.Div64(hi, lo, uint64(Scale))
	v, ok := signed(q, (d.i < 0) != (e.i < 0))
	if !ok || v == math.MinInt64 {
		return Zero, false
	}
	return Dec{i: v}, true
}

// Div returns d / e, truncated toward zero, saturating to MaxDec/MinDec if the
// quotient is out of range. It panics on division by zero; callers that take
// untrusted divisors should guard with IsZero first.
func (d Dec) Div(e Dec) Dec {
	if e.i == 0 {
		panic(ErrDivZero)
	}
	neg := (d.i < 0) != (e.i < 0)
	divisor := uabs(e.i)
	hi, lo := bits.Mul64(uabs(d.i), uint64(Scale))
	if hi >= divisor {
		return saturate(neg)
	}
	q, _ := bits.Div64(hi, lo, divisor)
	v, ok := signed(q, neg)
	if !ok || v == math.MinInt64 {
		return saturate(neg)
	}
	return Dec{i: v}
}

// MulRaw multiplies by a plain integer (no scaling involved), saturating on
// overflow.
func (d Dec) MulRaw(n int64) Dec {
	neg := (d.i < 0) != (n < 0)
	hi, lo := bits.Mul64(uabs(d.i), uabs(n))
	if hi != 0 {
		return saturate(neg)
	}
	v, ok := signed(lo, neg)
	if !ok || v == math.MinInt64 {
		return saturate(neg)
	}
	return Dec{i: v}
}

// Float64 returns an approximate float representation. Use only for charts,
// logging, or other display — never for settlement math.
func (d Dec) Float64() float64 { return float64(d.i) / float64(Scale) }

// String renders the decimal with up to Decimals fractional digits and trailing
// zeros trimmed.
func (d Dec) String() string {
	neg := d.i < 0
	v := d.i
	if neg {
		v = -v
	}
	intPart := v / Scale
	frac := v % Scale
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	fmt.Fprintf(&b, "%d", intPart)
	if frac != 0 {
		// zero-pad fractional part to Decimals digits, then trim trailing zeros
		fs := fmt.Sprintf("%0*d", Decimals, frac)
		fs = strings.TrimRight(fs, "0")
		b.WriteByte('.')
		b.WriteString(fs)
	}
	return b.String()
}

// Parse converts a decimal string (e.g. "123.45") into a Dec. Excess fractional
// digits beyond Decimals are truncated.
func Parse(s string) (Dec, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxDecimalLen {
		return Zero, ErrParse
	}
	neg := false
	switch s[0] {
	case '-':
		neg = true
		s = s[1:]
	case '+':
		s = s[1:]
	}
	if s == "" {
		return Zero, ErrParse
	}
	intStr, fracStr := s, ""
	if dot := strings.IndexByte(s, '.'); dot >= 0 {
		intStr = s[:dot]
		fracStr = s[dot+1:]
	}
	if intStr == "" {
		intStr = "0"
	}
	// Build the scaled integer using big.Int to tolerate large inputs, then
	// range-check against int64.
	whole := new(big.Int)
	if _, ok := whole.SetString(intStr, 10); !ok {
		return Zero, ErrParse
	}
	whole.Mul(whole, bigScale)

	if fracStr != "" {
		if len(fracStr) > Decimals {
			fracStr = fracStr[:Decimals] // truncate excess precision
		}
		// right-pad to Decimals digits
		padded := fracStr + strings.Repeat("0", Decimals-len(fracStr))
		fracVal := new(big.Int)
		if _, ok := fracVal.SetString(padded, 10); !ok {
			return Zero, ErrParse
		}
		whole.Add(whole, fracVal)
	}
	if !whole.IsInt64() {
		return Zero, ErrParse
	}
	out := whole.Int64()
	if neg {
		out = -out
	}
	return Dec{i: out}, nil
}

// MustParse is Parse but panics on error; intended for trusted literals such as
// seed data.
func MustParse(s string) Dec {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// MarshalJSON encodes the decimal as a JSON string to preserve full precision
// across the wire (JSON numbers are float64 and would lose precision).
func (d Dec) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON accepts either a JSON string or a bare number.
func (d *Dec) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*d = Zero
		return nil
	}
	v, err := Parse(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}
