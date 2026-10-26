package helpers

import (
	"fmt"
	"strings"
	"testing"
)

// Dense prefix candidates reproduce repeated rescans of an absent case variant.
// Sparse and absent candidates also check the cost of searching long inputs
// where IndexByte can skip efficiently.
func BenchmarkIndexStringIgnoreCaseASCII(b *testing.B) {
	for _, size := range []int{1024, 16384, 65536} {
		for _, tc := range []struct {
			name string
			text string
			want int
		}{
			{"DenseLowerMiss", strings.Repeat("a", size), -1},
			{"DenseUpperMiss", strings.Repeat("A", size), -1},
			{"SpacedLowerMiss", strings.Repeat("ax", size/2), -1},
			{"SpacedUpperMiss", strings.Repeat("Ax", size/2), -1},
			{"SparseLowerHit", strings.Repeat("x", size-2) + "ab", size - 2},
			{"SparseUpperHit", strings.Repeat("x", size-2) + "AB", size - 2},
			{"NoStartMiss", strings.Repeat("x", size), -1},
			{"EarlyLowerHit", "ab" + strings.Repeat("x", size-2), 0},
			{"EarlyUpperHit", "AB" + strings.Repeat("x", size-2), 0},
		} {
			b.Run(fmt.Sprintf("%s/%d", tc.name, size), func(b *testing.B) {
				b.ReportAllocs()
				if tc.want != 0 {
					b.SetBytes(int64(len(tc.text)))
				}
				for i := 0; i < b.N; i++ {
					if got := IndexStringIgnoreCaseASCII(tc.text, "ab"); got != tc.want {
						b.Fatalf("got %d, want %d", got, tc.want)
					}
				}
			})
		}
	}
	for _, text := range []string{"ab", "AB", "xAb", "0123456789TeStToken"} {
		prefix, want := "ab", 0
		if text == "xAb" {
			want = 1
		} else if text == "0123456789TeStToken" {
			prefix, want = "testtoken", 10
		}
		b.Run("Short/"+text, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if got := IndexStringIgnoreCaseASCII(text, prefix); got != want {
					b.Fatalf("got %d, want %d", got, want)
				}
			}
		})
	}
}
