package regexp2

import (
	"slices"
	"strings"
	"testing"
)

func TestBasicSplit(t *testing.T) {
	re := MustCompile("a(.)c(.)e")
	vals, err := re.Split("123abcde456aBCDe789", -1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{"123", "b", "d", "456aBCDe789"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestBasicSplit_IgnoreCase(t *testing.T) {
	re := MustCompile("a(.)c(.)e", IgnoreCase)
	vals, err := re.Split("123abcde456aBCDe789", -1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{"123", "b", "d", "456", "B", "D", "789"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestSplitRightToLeftRegression(t *testing.T) {
	for _, tt := range []struct {
		pattern, input string
		count          int
		want           []string
	}{
		{`-`, "a-b-c", -1, []string{"a", "b", "c"}},
		{`-`, "a-b-c-d", 2, []string{"a-b", "c", "d"}},
		{`-`, "a-b-c", 1, []string{"a-b-c"}},
		{`-`, "abc", -1, []string{"abc"}},
		{`-`, "é-猫-日", -1, []string{"é", "猫", "日"}},
		{`(-)(:)`, "a-:b-:c", -1, []string{"a", ":", "-", "b", ":", "-", "c"}},
		{`(?=.)`, "abc", -1, []string{"", "a", "b", "c"}},
	} {
		t.Run(tt.pattern+tt.input, func(t *testing.T) {
			got, err := MustCompile(tt.pattern, RightToLeft).Split(tt.input, tt.count)
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("got %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestSplit_ZeroWidth(t *testing.T) {
	re := MustCompile(`(?<=\G..)(?=..)`)
	vals, err := re.Split("aabbccdd", -1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{"aa", "bb", "cc", "dd"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestSplit_LimitCountRemainder(t *testing.T) {
	re := MustCompile("a(.)c(.)e", IgnoreCase)
	vals, err := re.Split("123abcde456aBCDe789abcde", 2)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{"123", "b", "d", "456", "B", "D", "789abcde"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestSplit_LimitCount1(t *testing.T) {
	re := MustCompile("a(.)c(.)e", IgnoreCase)
	vals, err := re.Split("123abcde456aBCDe789abcde", 1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{"123abcde456aBCDe789abcde"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestSplit_UnicodePrefixCaptures(t *testing.T) {
	re := MustCompile(`(needle)`)
	prefix := strings.Repeat("pré", 8)
	input := prefix + "needle" + "tail"
	vals, err := re.Split(input, -1)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{prefix, "needle", "tail"}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}

func TestSplit_LimitCount0(t *testing.T) {
	re := MustCompile("a(.)c(.)e", IgnoreCase)
	vals, err := re.Split("123abcde456aBCDe789abcde", 0)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if want, got := []string{}, vals; !slices.Equal(want, got) {
		t.Errorf("wanted %v got %v", want, got)
	}
}
