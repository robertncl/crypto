package num

import "testing"

var sink Dec

func BenchmarkMul(b *testing.B) {
	p, q := MustParse("64123.45"), MustParse("0.01234")
	for i := 0; i < b.N; i++ {
		sink = p.Mul(q)
	}
}

func BenchmarkDiv(b *testing.B) {
	p, q := MustParse("791.28"), MustParse("64123.45")
	for i := 0; i < b.N; i++ {
		sink = p.Div(q)
	}
}
