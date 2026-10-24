package num

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

// refMulDiv computes the exact truncated result with big.Int and reports
// whether it fits in the symmetric Dec range.
func refMul(a, b int64) (int64, bool) {
	r := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	r.Quo(r, big.NewInt(Scale))
	return r.Int64(), r.IsInt64() && r.Int64() != math.MinInt64
}

func refDiv(a, b int64) (int64, bool) {
	r := new(big.Int).Mul(big.NewInt(a), big.NewInt(Scale))
	r.Quo(r, big.NewInt(b))
	return r.Int64(), r.IsInt64() && r.Int64() != math.MinInt64
}

// TestMulDivMatchBigInt checks the allocation-free math/bits implementation
// against a big.Int reference across random magnitudes and signs, including
// out-of-range results (which must saturate, never wrap).
func TestMulDivMatchBigInt(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	gen := func() int64 {
		v := rng.Int63() >> uint(rng.Intn(63))
		if rng.Intn(2) == 0 {
			v = -v
		}
		return v
	}
	for i := 0; i < 200_000; i++ {
		a, b := gen(), gen()
		want, fits := refMul(a, b)
		got, ok := FromRaw(a).MulChecked(FromRaw(b))
		if ok != fits || (ok && got.Raw() != want) {
			t.Fatalf("MulChecked(%d,%d) = %d,%v want %d,%v", a, b, got.Raw(), ok, want, fits)
		}
		if m := FromRaw(a).Mul(FromRaw(b)); fits && m.Raw() != want {
			t.Fatalf("Mul(%d,%d) = %d want %d", a, b, m.Raw(), want)
		} else if !fits && m != saturate((a < 0) != (b < 0)) {
			t.Fatalf("Mul(%d,%d) = %d, want saturation", a, b, m.Raw())
		}
		if b == 0 {
			continue
		}
		wantQ, fitsQ := refDiv(a, b)
		q := FromRaw(a).Div(FromRaw(b))
		if fitsQ && q.Raw() != wantQ {
			t.Fatalf("Div(%d,%d) = %d want %d", a, b, q.Raw(), wantQ)
		} else if !fitsQ && q != saturate((a < 0) != (b < 0)) {
			t.Fatalf("Div(%d,%d) = %d, want saturation", a, b, q.Raw())
		}
	}
}

// TestMulOverflowDoesNotWrap pins the order-notional overflow: before the fix
// this product wrapped to ~262.9, letting a 1.8e11 USDT order lock pennies.
func TestMulOverflowDoesNotWrap(t *testing.T) {
	p, q := MustParse("100000000"), MustParse("1844.67441")
	if _, ok := p.MulChecked(q); ok {
		t.Fatal("MulChecked should report overflow")
	}
	if got := p.Mul(q); got != MaxDec {
		t.Fatalf("Mul = %s, want saturation to MaxDec", got)
	}
	if got := p.Neg().Mul(q); got != MinDec {
		t.Fatalf("negative Mul = %s, want MinDec", got)
	}
}

func TestAddSubSaturate(t *testing.T) {
	big := MaxDec.Sub(FromInt(1))
	if got := big.Add(FromInt(2)); got != MaxDec {
		t.Errorf("Add overflow = %s, want MaxDec", got)
	}
	if got := MinDec.Add(FromInt(1)).Sub(FromInt(2)); got != MinDec {
		t.Errorf("Sub underflow = %s, want MinDec", got)
	}
	if got := MinDec.Sub(FromRaw(1)); got != MinDec {
		t.Errorf("MinDec-1 = %s, want MinDec", got)
	}
	if got := FromInt(5).Sub(FromInt(7)); got.String() != "-2" {
		t.Errorf("5-7 = %s", got)
	}
	if got := FromInt(-5).Add(FromInt(7)); got.String() != "2" {
		t.Errorf("-5+7 = %s", got)
	}
	if got := FromInt(math.MaxInt64 / 10); got != MaxDec {
		t.Errorf("FromInt overflow = %s, want MaxDec", got)
	}
	if got := FromInt(3).MulRaw(-4); got.String() != "-12" {
		t.Errorf("MulRaw = %s", got)
	}
}
