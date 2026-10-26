package regexp2

import (
	"strings"
	"testing"
)

func TestRightToLeft_Basic(t *testing.T) {
	re := MustCompile(`foo\d+`, RightToLeft)
	s := "0123456789foo4567890foo1foo  0987"

	m, err := re.FindStringMatch(s)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "foo1", m.String(); want != got {
		t.Fatalf("Match 0 failed, wanted %v, got %v", want, got)
	}

	m, err = re.FindNextMatch(m)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "foo4567890", m.String(); want != got {
		t.Fatalf("Match 1 failed, wanted %v, got %v", want, got)
	}
}

func TestRightToLeft_StartAt(t *testing.T) {
	re := MustCompile(`\d`, RightToLeft)

	m, err := re.FindStringMatchStartingAt("0123", -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if m == nil {
		t.Fatalf("Expected match")
	}
	if want, got := "3", m.String(); want != got {
		t.Fatalf("Find failed, wanted '%v', got '%v'", want, got)
	}

}

func TestRightToLeft_Replace(t *testing.T) {
	re := MustCompile(`\d`, RightToLeft)
	s := "0123456789foo4567890foo         "
	str, err := re.Replace(s, "#", -1, 7)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := "0123456789foo#######foo         ", str; want != got {
		t.Fatalf("Replace failed, wanted '%v', got '%v'", want, got)
	}
}

func TestRightToLeft_UnicodePrefix(t *testing.T) {
	re := MustCompile(`ab`, RightToLeft)
	prefix := strings.Repeat("漢", 10)
	input := prefix + "abxxab"

	str, err := re.Replace(input, "X", -1, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if want, got := prefix+"XxxX", str; want != got {
		t.Fatalf("Replace failed, wanted '%v', got '%v'", want, got)
	}

	m, err := re.FindStringMatch(input)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if m == nil {
		t.Fatal("Expected match")
	}
	if want, got := "ab", m.String(); want != got {
		t.Fatalf("Find failed, wanted '%v', got '%v'", want, got)
	}
	if want, got := 14, m.RuneIndex; want != got {
		t.Fatalf("RuneIndex wanted %v got %v", want, got)
	}

	idxs, err := re.FindAllStringIndex(input, -1)
	if err != nil {
		t.Fatalf("Unexpected err: %v", err)
	}
	if len(idxs) != 2 {
		t.Fatalf("FindAllStringIndex = %v", idxs)
	}
	for _, pair := range idxs {
		if input[pair[0]:pair[1]] != "ab" {
			t.Fatalf("index pair %v slices to %q", pair, input[pair[0]:pair[1]])
		}
	}
}
