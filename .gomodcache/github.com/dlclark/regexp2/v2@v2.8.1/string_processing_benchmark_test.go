package regexp2

import (
	"strings"
	"testing"
)

func BenchmarkStringProcessing(b *testing.B) {
	for _, tc := range []struct {
		name, input string
		options     RegexOptions
	}{
		{"ASCII", "needle" + strings.Repeat("x", 64<<10), None},
		{"Unicode", "needle" + strings.Repeat("界", 21845), None},
		{"UnicodeRTL", strings.Repeat("界", 21845) + "needle", RightToLeft},
	} {
		b.Run("FindAll/"+tc.name, func(b *testing.B) {
			re := MustCompile(`needle`, tc.options)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := re.FindAllStringIndex(tc.input, 1)
				if err != nil || len(got) != 1 || tc.input[got[0][0]:got[0][1]] != "needle" {
					b.Fatal(got, err)
				}
			}
		})
		b.Run("FindMatch/"+tc.name, func(b *testing.B) {
			re := MustCompile(`needle`, tc.options)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := re.FindStringMatch(tc.input)
				if err != nil || got == nil || got.String() != "needle" {
					b.Fatal(got, err)
				}
			}
		})
	}
	for _, tc := range []struct {
		name, input string
		options     RegexOptions
	}{
		{"Miss", strings.Repeat("x", 64<<10), None},
		{"LateHit", strings.Repeat("x", 64<<10) + "needle", None},
		{"LateHitRTL", strings.Repeat("x", 64<<10) + "needle", RightToLeft},
		{"ManyHitsRTL", strings.Repeat("xxneedlexx", 256), RightToLeft},
		{"ShortHit", "xxneedlexx", None},
	} {
		b.Run("Replace/"+tc.name, func(b *testing.B) {
			re := MustCompile(`needle`, tc.options)
			want := strings.ReplaceAll(tc.input, "needle", "pin")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := re.Replace(tc.input, "pin", -1, -1)
				if err != nil || got != want {
					b.Fatal(got, err)
				}
			}
		})
	}
	for _, tc := range []struct {
		name, pattern string
		want          int
	}{
		{"NoCaptures", `,`, 1001}, {"Captures", `(,)`, 2001},
	} {
		b.Run("Split/"+tc.name, func(b *testing.B) {
			re := MustCompile(tc.pattern)
			input := strings.Repeat("abc,", 1000) + "abc"
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, err := re.Split(input, -1)
				if err != nil || len(got) != tc.want {
					b.Fatal(len(got), err)
				}
			}
		})
	}
}

func BenchmarkSplitUnicodePrefix(b *testing.B) {
	re := MustCompile(`(needle)`)
	prefix := strings.Repeat("界", 21845)
	input := prefix + "needle尾"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := re.Split(input, -1)
		if err != nil || len(got) != 3 || got[0] != prefix || got[1] != "needle" || got[2] != "尾" {
			b.Fatal(got, err)
		}
	}
}
