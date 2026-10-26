package helpers

import "testing"

func TestIsASCII(t *testing.T) {
	if !IsASCII("") || !IsASCII("abc123") || !IsASCII("\x00\x7f") {
		t.Fatal("expected ASCII strings")
	}
	if IsASCII("é") || IsASCII("a\u0080") || IsASCII("💩") {
		t.Fatal("expected non-ASCII strings")
	}
}

func TestDecodeStringASCII(t *testing.T) {
	s := "Hello, world!"
	buf := make([]rune, len(s))
	n, ascii := DecodeString(s, buf)
	if !ascii || n != len(s) {
		t.Fatalf("n=%d ascii=%v", n, ascii)
	}
	if got := string(buf[:n]); got != s {
		t.Fatalf("decoded %q", got)
	}
}

func TestDecodeStringUnicode(t *testing.T) {
	s := "héllo 💩"
	buf := make([]rune, len(s))
	n, ascii := DecodeString(s, buf)
	if ascii {
		t.Fatal("expected non-ASCII")
	}
	if got := string(buf[:n]); got != s {
		t.Fatalf("decoded %q", got)
	}
}
