package regexp2

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func FuzzFindAllStringIndexByteOffsets(f *testing.F) {
	for _, input := range []string{"", "abc", "é界😀aé", "\xff\xc0\x80a\xed\xa0\x80界\xe2\x82", "pré needle界needle tail", "aab ab"} {
		f.Add(input)
	}
	patterns := []string{`.`, `a?`, `[é界]+`, `needle|界+`, `(?<=a)b`, `\bneedle\b`, `(a)+b`}
	var regexps []*Regexp
	for _, pattern := range patterns {
		regexps = append(regexps, MustCompile(pattern), MustCompile(pattern, RightToLeft))
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		// Build an independent byte-position oracle from Go's UTF-8 range walk.
		offsets := make([]int, 0, len(input)+1)
		for i := range input {
			offsets = append(offsets, i)
		}
		offsets = append(offsets, len(input))
		for _, re := range regexps {
			for _, n := range []int{-1, 0, 1, 3} {
				want, err := re.FindAllRunesIndex([]rune(input), n)
				if err != nil {
					t.Fatal(err)
				}
				for _, pair := range want {
					pair[0], pair[1] = offsets[pair[0]], offsets[pair[1]]
				}
				got, err := re.FindAllStringIndex(input, n)
				if err != nil || !slices.EqualFunc(got, want, slices.Equal[[]int]) {
					t.Fatalf("%q RTL=%v input=%q n=%d: got %v, %v; want %v", re.String(), re.RightToLeft(), input, n, got, err, want)
				}
			}
		}
	})
}

func TestStringCaptureByteOffsets(t *testing.T) {
	for _, input := range []string{
		strings.Repeat("a", 128), strings.Repeat("界😀", 64),
		strings.Repeat("\xff\x80", 64), "a\xffé\xc0\x80界\xe2\x82",
	} {
		t.Run(fmt.Sprintf("%x", input[:min(8, len(input))]), func(t *testing.T) {
			offsets := make([]int, 0, len(input)+1)
			for i := range input {
				offsets = append(offsets, i)
			}
			offsets = append(offsets, len(input))
			re := MustCompile(`(.)+`)
			m, err := re.FindStringMatch(input)
			if err != nil || m == nil {
				t.Fatal(m, err)
			}
			captures := m.GroupByNumber(1).Captures
			if len(captures) != len(offsets)-1 {
				t.Fatal(len(captures), offsets)
			}
			for i := len(captures) - 1; i >= 0; i-- {
				start, length := captures[i].ByteRange()
				if start != offsets[i] || length != offsets[i+1]-offsets[i] || captures[i].String() != input[offsets[i]:offsets[i+1]] {
					t.Fatalf("capture %d: %d,%d %q", i, start, length, captures[i].String())
				}
			}
		})
	}
}

func TestReplaceStringFastPaths(t *testing.T) {
	for _, option := range []RegexOptions{None, RightToLeft} {
		for _, input := range []string{"xxneedlexxneedlexx", "éneedle界needle😀", "\xffneedle\x80needle\xe2\x82"} {
			re := MustCompile(`needle`, option)
			got, err := re.Replace(input, "pin", -1, -1)
			// Pattern replacement has always re-encoded unmatched invalid UTF-8.
			want := strings.ReplaceAll(string([]rune(input)), "needle", "pin")
			if err != nil || got != want {
				t.Fatalf("RTL=%v input=%q: got %q,%v; want %q", re.RightToLeft(), input, got, err, want)
			}
		}
	}
	for _, tc := range []struct {
		pattern, input, replacement, want string
		start, count                      int
	}{
		{`needle`, "prefixneedleTAIL", "$`|$&|$'|$_", "prefixprefix|needle|TAIL|prefixneedleTAILTAIL", -1, -1},
		{`(?<=prefix)(needle)`, "prefixneedleTAIL", "[$1]", "prefix[needle]TAIL", -1, -1},
		{`\Gneedle`, "xxneedleneedleTAIL", "pin", "xxpinpinTAIL", 2, -1},
		{`needle`, "xxneedleneedleTAIL", "pin", "xxpinneedleTAIL", 0, 1},
		{`needle`, "\xffnone\x80", "pin", "\xffnone\x80", -1, -1},
		{`aaba|aaca|bada`, strings.Repeat("a", 80) + "ca?bada!", "[$&]", strings.Repeat("a", 78) + "[aaca]?[bada]!", -1, -1},
		{`(?i)ab`, "aX AB!ab?", "[$&]", "aX [AB]![ab]?", -1, -1},
		{`[ACEGIK]{2,4}`, "A!ACE?IK", "[$&]", "A![ACE]?[IK]", -1, -1},
		{`aa[ \n]$`, "aaa\n", "[$&]", "a[aa\n]", -1, -1},
		{`aaba|aaca|bada`, strings.Repeat("a", 80), "[$&]", strings.Repeat("a", 80), -1, -1},
	} {
		re := MustCompile(tc.pattern)
		got, err := re.Replace(tc.input, tc.replacement, tc.start, tc.count)
		if err != nil || got != tc.want {
			t.Fatalf("%s on %q: got %q,%v; want %q", tc.pattern, tc.input, got, err, tc.want)
		}
	}
	re := MustCompile(`needle`)
	for _, input := range []string{"é-no-match", strings.Repeat("é", 64)} {
		for _, start := range []int{1, len(input) + 1} {
			if _, err := re.Replace(input, "pin", start, -1); err == nil {
				t.Fatalf("startAt=%d: expected validation error before rejecting miss", start)
			}
		}
	}
}

func TestSplitReusableCaptures(t *testing.T) {
	for _, tc := range []struct {
		pattern, input string
		count          int
		want           []string
	}{
		{`,`, "a,b,c,d", 2, []string{"a", "b", "c,d"}},
		{`(,)|(;)`, "a,b;c", -1, []string{"a", ",", "", "b", "", ";", "c"}},
		{`(?<10>,)|(?<2>;)`, "a,b;c", -1, []string{"a", "", ",", "b", ";", "", "c"}},
		{`(?:(?<x>,)|;)+`, "a,,b;c,,d", -1, []string{"a", ",", "b", "", "c", ",", "d"}},
		{`(?<x>,)+(?<-x>;)+`, "a,,;b,;c", -1, []string{"a", ",", "b", "", "c"}},
		{`(?<=\G..)(?=..)`, "aabbccdd", -1, []string{"aa", "bb", "cc", "dd"}},
		{``, "é界", -1, []string{"", "é", "界", ""}},
		{`(needle)`, "é界needlex\xffneedle😀", -1, []string{"é界", "needle", "x\xff", "needle", "😀"}},
		{`(?<=(é))needle`, "éneedle界éneedle😀", -1, []string{"é", "é", "界é", "é", "😀"}},
		{`aaba|aaca|bada`, strings.Repeat("a", 80) + "ca?bada!", -1, []string{strings.Repeat("a", 78), "?", "!"}},
		{`(?i)ab`, "aX AB!ab?", -1, []string{"aX ", "!", "?"}},
		{`[ACEGIK]{2,4}`, "A!ACE?IK", -1, []string{"A!", "?", ""}},
		{`aa[ \n]$`, "aaa\n", -1, []string{"a", ""}},
		{`aaba|aaca|bada`, strings.Repeat("a", 80), -1, []string{strings.Repeat("a", 80)}},
	} {
		t.Run(tc.pattern, func(t *testing.T) {
			re := MustCompile(tc.pattern)
			// Repeated calls also cover reused capture counts and balancing storage.
			for i := 0; i < 3; i++ {
				got, err := re.Split(tc.input, tc.count)
				if err != nil || !slices.Equal(got, tc.want) {
					t.Fatalf("got %#v,%v; want %#v", got, err, tc.want)
				}
				if _, err := re.Replace(strings.Repeat(tc.input, 2), "changed", -1, -1); err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(got, tc.want) {
					t.Fatal("returned strings changed after pooled buffer reuse")
				}
			}
		})
	}
}

func TestSplitUnicodePrefixByteSpans(t *testing.T) {
	for _, prefix := range []string{strings.Repeat("pré界", 512), strings.Repeat("é\xff界", 512)} {
		input := prefix + " needle-é-needle!"
		for _, pattern := range []string{`needle`, `(needle)`, `\b(needle)\b`} {
			re := MustCompile(pattern)
			want := []string{prefix + " ", "-é-", "!"}
			if pattern != `needle` {
				want = []string{prefix + " ", "needle", "-é-", "needle", "!"}
			}
			got, err := re.Split(input, -1)
			if err != nil || !slices.Equal(got, want) {
				t.Fatalf("%q: got %q, %v; want %q", pattern, got, err, want)
			}
		}
		re := MustCompile(`(?<=(界))needle`)
		got, err := re.Split(prefix+"needle界needle!", -1)
		want := []string{prefix, "界", "界", "界", "!"}
		if err != nil || !slices.Equal(got, want) {
			t.Fatalf("lookbehind: got %q, %v; want %q", got, err, want)
		}
	}
}

func TestSplitRegisteredEngineCapturesAndErrors(t *testing.T) {
	failure := errors.New("engine failure")
	re := newEngineRegexp("split-engine", newCompileConfig(nil), RuntimeEngineData{
		CapSize: 2, LeftContextKnown: true,
		StringPrefixFilter: stringIndexPrefixFilter(",", false, 1),
		FindFirstChar: func(r *Runner) bool {
			for r.Runtextpos < r.Runtextend {
				if r.Runtext[r.Runtextpos] == ',' || r.Runtext[r.Runtextpos] == '!' {
					return true
				}
				r.Runtextpos++
			}
			return false
		},
		Execute: func(r *Runner) error {
			if r.Runtext[r.Runtextpos] == '!' {
				return failure
			}
			start := r.Runtextpos
			r.Runtextpos++
			r.Capture(1, start, r.Runtextpos)
			r.Capture(0, start, r.Runtextpos)
			return nil
		},
		ExecuteQuick: func(r *Runner) error { t.Fatal("Split must retain captures"); return nil },
	})
	got, err := re.Split("é,界,😀", -1)
	if err != nil || !slices.Equal(got, []string{"é", ",", "界", ",", "😀"}) {
		t.Fatal(got, err)
	}
	// Preserve error propagation, including the existing look-ahead scan after
	// the last processed delimiter when count is exhausted.
	for _, count := range []int{-1, 2} {
		if got, err := re.Split("a,b,c!", count); got != nil || !errors.Is(err, failure) {
			t.Fatal(got, err)
		}
	}
	got, err = re.Split("x,y", -1)
	if err != nil || !slices.Equal(got, []string{"x", ",", "y"}) {
		t.Fatal(got, err)
	}
}

func TestStringProcessingAllocations(t *testing.T) {
	skipIfAllocsUnreliable(t)
	re := MustCompile(`needle`)
	input := "needle" + strings.Repeat("界", 21845)
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := re.FindAllStringIndex(input, 1); err != nil {
			panic(err)
		}
	}); allocs > 5 {
		t.Fatalf("FindAllStringIndex allocations=%v; want <=5", allocs)
	}
	split := MustCompile(`(,)`)
	input = strings.Repeat("abc,", 1000) + "abc"
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := split.Split(input, -1); err != nil {
			panic(err)
		}
	}); allocs > 30 {
		t.Fatalf("Split allocations=%v; want <=30", allocs)
	}
}

func TestSplitConcurrentPoolUse(t *testing.T) {
	re := MustCompile(`(needle)`)
	input := "éneedle界needle😀"
	want := []string{"é", "needle", "界", "needle", "😀"}
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			for j := 0; j < 50; j++ {
				got, err := re.Split(input, -1)
				if err != nil || !slices.Equal(got, want) {
					t.Fatal(got, err)
				}
			}
		})
	}
}
