package helpers

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func referenceIndexStringIgnoreCaseASCII(s, prefix string) int {
	for i := 0; i <= len(s)-len(prefix); i++ {
		match := true
		for j := 0; j < len(prefix); j++ {
			a, b := s[i+j], prefix[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestIndexStringIgnoreCaseASCIIBytes(t *testing.T) {
	// ASCII folding must not equate punctuation pairs such as '[' and '{',
	// and must preserve bytes outside ASCII, including invalid UTF-8.
	for hay := 0; hay < 256; hay++ {
		for needle := 0; needle < 256; needle++ {
			s, prefix := string([]byte{byte(hay)}), string([]byte{byte(needle)})
			want := referenceIndexStringIgnoreCaseASCII(s, prefix)
			if got := IndexStringIgnoreCaseASCII(s, prefix); got != want {
				t.Fatalf("IndexStringIgnoreCaseASCII(%q, %q) = %d, want %d", s, prefix, got, want)
			}
		}
	}
}

func TestIndexStringIgnoreCaseASCIICandidates(t *testing.T) {
	for _, offset := range []int{0, 1, 15, 16, 31, 32, 33, 63, 64, 65, 127, 128, 129, 255, 256, 257, 1023, 1024, 1025, 16384} {
		for _, text := range []string{
			strings.Repeat("x", offset) + "Ab",
			strings.Repeat("x", offset) + "aB",
			strings.Repeat("a", offset) + "AB",
			strings.Repeat("A", offset) + "ab",
			strings.Repeat("ax", offset) + "Ab",
			strings.Repeat("Ax", offset) + "aB",
			strings.Repeat("x", offset) + "AxaB",
			strings.Repeat("x", offset) + "axAb",
			strings.Repeat("x", offset) + "a",
		} {
			want := referenceIndexStringIgnoreCaseASCII(text, "ab")
			if got := IndexStringIgnoreCaseASCII(text, "ab"); got != want {
				t.Fatalf("offset %d: IndexStringIgnoreCaseASCII(%q, ab) = %d, want %d", offset, text, got, want)
			}
		}
	}
}

func TestIndexStringIgnoreCaseASCIIRandom(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	alphabet := []byte("aaAAabBcCxX[]{}@`_\x00\x80\xff")
	for n := 0; n < 5000; n++ {
		s, prefix := make([]byte, rng.IntN(513)), make([]byte, rng.IntN(12))
		for i := range s {
			s[i] = alphabet[rng.IntN(len(alphabet))]
		}
		for i := range prefix {
			prefix[i] = alphabet[rng.IntN(len(alphabet))]
		}
		if n%2 == 0 && len(prefix) <= len(s) {
			copy(s[rng.IntN(len(s)-len(prefix)+1):], prefix)
		}
		want := referenceIndexStringIgnoreCaseASCII(string(s), string(prefix))
		if got := IndexStringIgnoreCaseASCII(string(s), string(prefix)); got != want {
			t.Fatalf("IndexStringIgnoreCaseASCII(%q, %q) = %d, want %d", s, prefix, got, want)
		}
	}
}

func FuzzIndexStringIgnoreCaseASCII(f *testing.F) {
	f.Add("", "")
	f.Add("a", "ab")
	f.Add("AbCaBc", "aBc")
	f.Add(strings.Repeat("A", 1024)+"b", "ab")
	f.Add("[\x00\xffA{\x00\xffb", "{\x00\xffB")
	f.Fuzz(func(t *testing.T, s, prefix string) {
		want := referenceIndexStringIgnoreCaseASCII(s, prefix)
		if got := IndexStringIgnoreCaseASCII(s, prefix); got != want {
			t.Fatalf("IndexStringIgnoreCaseASCII(%q, %q) = %d, want %d", s, prefix, got, want)
		}
	})
}
