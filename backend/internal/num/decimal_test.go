package num

import (
	"encoding/json"
	"testing"
)

func TestParseString(t *testing.T) {
	cases := []struct {
		in  string
		out string
	}{
		{"0", "0"},
		{"1", "1"},
		{"123.45", "123.45"},
		{"0.00000001", "0.00000001"},
		{"-5.5", "-5.5"},
		{"1000000.123456789", "1000000.12345678"}, // truncates excess precision
		{"50000.50000000", "50000.5"},
	}
	for _, c := range cases {
		d, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", c.in, err)
		}
		if got := d.String(); got != c.out {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, got, c.out)
		}
	}
}

func TestMulDivNoOverflow(t *testing.T) {
	// 90,000 USDT price * 0.5 BTC = 45,000 notional, with large scaled operands.
	price := MustParse("90000.12345678")
	qty := MustParse("0.5")
	got := price.Mul(qty).String()
	if got != "45000.06172839" {
		t.Errorf("price*qty = %q, want %q", got, "45000.06172839")
	}
	// division round-trip
	back := price.Mul(qty).Div(qty)
	if back.Sub(price).String() != "0" && back.Sub(price).Cmp(MustParse("0.00000001")) > 0 {
		t.Errorf("div round trip drifted: %s vs %s", back, price)
	}
}

func TestJSON(t *testing.T) {
	d := MustParse("12345.6789")
	b, _ := d.MarshalJSON()
	if string(b) != `"12345.6789"` {
		t.Errorf("MarshalJSON = %s", b)
	}
	var back Dec
	if err := back.UnmarshalJSON(b); err != nil || !back.Eq(d) {
		t.Errorf("round trip failed: %v %s", err, back)
	}
}

func TestFromRaw(t *testing.T) {
	tests := []struct {
		name   string
		raw    int64
		expect float64
	}{
		{"zero", 0, 0},
		{"one", Scale, 1},
		{"one point five", 150_000_000, 1.5},
		{"negative", -Scale, -1},
		{"small fraction", 1, 0.00000001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := FromRaw(tt.raw)
			if d.Float64() != tt.expect {
				t.Errorf("got %v, want %v", d.Float64(), tt.expect)
			}
		})
	}
}

func TestFromInt(t *testing.T) {
	tests := []struct {
		name   string
		n      int64
		expect float64
	}{
		{"zero", 0, 0},
		{"one", 1, 1},
		{"hundred", 100, 100},
		{"negative", -50, -50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := FromInt(tt.n)
			if d.Float64() != tt.expect {
				t.Errorf("got %v, want %v", d.Float64(), tt.expect)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		wantErr bool
	}{
		{"empty", "", true},
		{"invalid chars", "abc", true},
		{"double dot", "1.2.3", true},
		{"too long", "1" + string(make([]byte, 50)), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.s)
			if (err != nil) != tt.wantErr {
				t.Errorf("Parse(%q) error = %v, wantErr %v", tt.s, err, tt.wantErr)
			}
		})
	}
}

func TestMustParseValid(t *testing.T) {
	d := MustParse("123.45")
	if d.Float64() != 123.45 {
		t.Errorf("MustParse got %v, want 123.45", d.Float64())
	}
}

func TestMustParsePanic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParse should panic on invalid input")
		}
	}()
	MustParse("invalid")
}

func TestRaw(t *testing.T) {
	d := FromRaw(123456789)
	if d.Raw() != 123456789 {
		t.Errorf("Raw() = %d, want 123456789", d.Raw())
	}
}

func TestIsZero(t *testing.T) {
	if !Zero.IsZero() {
		t.Error("Zero.IsZero() should be true")
	}
	if FromInt(1).IsZero() {
		t.Error("FromInt(1).IsZero() should be false")
	}
}

func TestSign(t *testing.T) {
	tests := []struct {
		d    Dec
		want int
	}{
		{FromInt(-5), -1},
		{Zero, 0},
		{FromInt(5), 1},
	}
	for i, tt := range tests {
		if got := tt.d.Sign(); got != tt.want {
			t.Errorf("test %d: Sign() = %d, want %d", i, got, tt.want)
		}
	}
}

func TestNeg(t *testing.T) {
	d := FromInt(123)
	if d.Neg().Float64() != -123 {
		t.Error("Neg() failed")
	}
	if d.Neg().Neg().Float64() != 123 {
		t.Error("Double Neg() failed")
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b   Dec
		expect float64
	}{
		{FromInt(1), FromInt(2), 3},
		{MustParse("1.5"), MustParse("2.5"), 4},
		{FromInt(100), MustParse("-50"), 50},
	}
	for i, tt := range tests {
		got := tt.a.Add(tt.b).Float64()
		if got != tt.expect {
			t.Errorf("test %d: Add() = %v, want %v", i, got, tt.expect)
		}
	}
}

func TestSub(t *testing.T) {
	tests := []struct {
		a, b   Dec
		expect float64
	}{
		{FromInt(5), FromInt(3), 2},
		{MustParse("10.5"), MustParse("0.5"), 10},
		{FromInt(0), FromInt(5), -5},
	}
	for i, tt := range tests {
		got := tt.a.Sub(tt.b).Float64()
		if got != tt.expect {
			t.Errorf("test %d: Sub() = %v, want %v", i, got, tt.expect)
		}
	}
}

func TestMul(t *testing.T) {
	tests := []struct {
		a, b   Dec
		expect float64
	}{
		{FromInt(2), FromInt(3), 6},
		{MustParse("1.5"), MustParse("2"), 3},
		{MustParse("0.5"), MustParse("0.5"), 0.25},
		{FromInt(0), FromInt(100), 0},
	}
	for i, tt := range tests {
		got := tt.a.Mul(tt.b).Float64()
		if got != tt.expect {
			t.Errorf("test %d: Mul() = %v, want %v", i, got, tt.expect)
		}
	}
}

func TestDiv(t *testing.T) {
	tests := []struct {
		a, b   Dec
		expect float64
	}{
		{FromInt(10), FromInt(2), 5},
		{FromInt(1), FromInt(2), 0.5},
		{MustParse("100"), MustParse("4"), 25},
	}
	for i, tt := range tests {
		got := tt.a.Div(tt.b).Float64()
		if got != tt.expect {
			t.Errorf("test %d: Div() = %v, want %v", i, got, tt.expect)
		}
	}
}

func TestDivZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Div(Zero) should panic")
		}
	}()
	FromInt(1).Div(Zero)
}

func TestMulRaw(t *testing.T) {
	d := FromInt(5)
	if d.MulRaw(3).Float64() != 15 {
		t.Error("MulRaw() failed")
	}
}

func TestCmp(t *testing.T) {
	tests := []struct {
		a, b   Dec
		expect int
	}{
		{FromInt(1), FromInt(2), -1},
		{FromInt(2), FromInt(1), 1},
		{FromInt(2), FromInt(2), 0},
	}
	for i, tt := range tests {
		got := tt.a.Cmp(tt.b)
		if got != tt.expect {
			t.Errorf("test %d: Cmp() = %d, want %d", i, got, tt.expect)
		}
	}
}

func TestComparisons(t *testing.T) {
	a, b := FromInt(5), FromInt(10)
	if !a.Lt(b) {
		t.Error("Lt() failed")
	}
	if !a.Lte(b) {
		t.Error("Lte() failed")
	}
	if !b.Gt(a) {
		t.Error("Gt() failed")
	}
	if !b.Gte(a) {
		t.Error("Gte() failed")
	}
	if !a.Eq(FromInt(5)) {
		t.Error("Eq() failed")
	}
}

func TestMin(t *testing.T) {
	a, b := FromInt(5), FromInt(10)
	if Min(a, b) != a {
		t.Error("Min() failed")
	}
}

func TestMax(t *testing.T) {
	a, b := FromInt(5), FromInt(10)
	if Max(a, b) != b {
		t.Error("Max() failed")
	}
}

func TestUnmarshalJSONVariations(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		expect  float64
		wantErr bool
	}{
		{"string", []byte(`"123.45"`), 123.45, false},
		{"null", []byte("null"), 0, false},
		{"empty string", []byte(`""`), 0, false},
		{"invalid", []byte(`"abc"`), 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Dec
			err := json.Unmarshal(tt.input, &d)
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && d.Float64() != tt.expect {
				t.Errorf("UnmarshalJSON() = %v, want %v", d.Float64(), tt.expect)
			}
		})
	}
}
