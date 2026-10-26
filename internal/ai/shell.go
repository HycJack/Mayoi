package ai

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// maxOutput is how much of a command's output the model reads.
const maxOutput = 64 << 10

// shellTimeout is how long a command may run.
const shellTimeout = 60 * time.Second

// Sh runs a command in dir and returns what it printed, its errors
// included, at most maxOutput of it.
func Sh(ctx context.Context, dir, command string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, "cmd", "/c", command)
	} else {
		c = exec.CommandContext(ctx, "sh", "-c", command)
	}
	c.Dir = dir
	out, err := c.CombinedOutput()
	s := string(out)
	if len(s) > maxOutput {
		s = s[:maxOutput] + "\n…"
	}
	s = strings.TrimSpace(s)
	if err != nil {
		return s, fmt.Errorf("%s: %w", s, err)
	}
	return s, nil
}
