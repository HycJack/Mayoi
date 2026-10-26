package helpers

import (
	"strings"
	"testing"
)

func BenchmarkIndexOf_ASCIIMiss(b *testing.B) {
	hay := []rune(strings.Repeat("zzzzzzzzzz", 1000))
	needle := []rune("needle")
	b.SetBytes(int64(len(hay)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if IndexOf(hay, needle) != -1 {
			b.Fatal("unexpected hit")
		}
	}
}

func BenchmarkIndexOfAny1_ASCIIMiss(b *testing.B) {
	hay := []rune(strings.Repeat("zzzzzzzzzz", 1000))
	b.SetBytes(int64(len(hay)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if IndexOfAny1(hay, 'q') != -1 {
			b.Fatal("unexpected hit")
		}
	}
}

func BenchmarkIndexOfAny_ASCIISetMiss(b *testing.B) {
	hay := []rune(strings.Repeat("zzzzzzzzzz", 1000))
	find := []rune("abcdw")
	b.SetBytes(int64(len(hay)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if IndexOfAny(hay, find) != -1 {
			b.Fatal("unexpected hit")
		}
	}
}

func BenchmarkIsASCII(b *testing.B) {
	s := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 400)
	b.SetBytes(int64(len(s)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !IsASCII(s) {
			b.Fatal("expected ASCII")
		}
	}
}

func BenchmarkDecodeString_ASCII(b *testing.B) {
	s := strings.Repeat("abcdefghijklmnopqrstuvwxyz", 400)
	buf := make([]rune, len(s))
	b.SetBytes(int64(len(s)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		n, ascii := DecodeString(s, buf)
		if !ascii || n != len(s) {
			b.Fatal(n, ascii)
		}
	}
}
