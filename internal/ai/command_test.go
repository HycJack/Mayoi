package ai

import (
	"slices"
	"testing"
)

// TestTheProgramsOfACommand: the programs are the first word of each part,
// so that a command joined by && is not mistaken for the one it starts with.
func TestTheProgramsOfACommand(t *testing.T) {
	cases := []struct {
		command string
		want    []string
	}{
		{"go test ./...", []string{"go"}},
		{"go build && go vet", []string{"go"}},
		{"go test && rm -rf out", []string{"go", "rm"}},
		{"cd internal && npm run build", []string{"npm"}},
		{"FOO=1 GOFLAGS=-mod=mod go run .", []string{"go"}},
		{`"C:\Program Files\Go\bin\go.exe" build`, []string{"go"}},
		{"git status | grep main", []string{"git", "grep"}},
		{"ls -la; pwd", []string{"ls", "pwd"}},
		{"", nil},
		{"cd ..", nil},
	}
	for _, c := range cases {
		if got := CommandPrograms(c.command); !slices.Equal(got, c.want) {
			t.Errorf("%q runs %v, not %v", c.command, got, c.want)
		}
	}
}

// TestOneCommandIsOneProgramList: a command of a program already named is
// not named twice, so that allowing what a command runs is one decision.
func TestOneCommandIsOneProgramList(t *testing.T) {
	if got := CommandPrograms("go test && go vet && go build"); !slices.Equal(got, []string{"go"}) {
		t.Errorf("the programs are %v", got)
	}
}
