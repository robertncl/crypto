package num

import "testing"

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
