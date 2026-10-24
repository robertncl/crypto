package earn

import (
	"testing"

	"cryptoex/internal/num"
)

// TestInterestForLongGapIsExact: once principal*apr*elapsed passes ~9.2e10
// (a large principal and/or a long accrual gap), the old chained Dec multiply
// wrapped and produced garbage interest (1M USDT at 8% over a year came out
// as -1891.94). The result must be the exact truncated simple interest.
func TestInterestForLongGapIsExact(t *testing.T) {
	cases := []struct {
		principal, apr string
		elapsed        int64
		wantRaw        int64
	}{
		// 10,000 USDT at 16.5% for one day = 4.52054794 USDT (no overflow; exactness).
		{"10000", "0.165", 86_400, 452054794},
		// 10M USDT at 16.5% after a day without accrual = 4520.54794520 USDT.
		{"10000000", "0.165", 86_400, 452054794520},
		// 1,000,000 USDT at 8% for a full year = 80,000 USDT.
		{"1000000", "0.08", secondsPerYear, 80_000 * num.Scale},
	}
	for _, c := range cases {
		got := interestFor(num.MustParse(c.principal), num.MustParse(c.apr), 0, c.elapsed)
		if got.Raw() != c.wantRaw {
			t.Errorf("interestFor(%s, %s, %ds) = %s, want %s",
				c.principal, c.apr, c.elapsed, got, num.FromRaw(c.wantRaw))
		}
	}
}
