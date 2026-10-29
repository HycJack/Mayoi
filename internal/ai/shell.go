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

// DefaultCommandTimeout is how long a command may run when the settings
// say nothing: long enough for a build or a test suite, short enough that
// one that waits for input does not wait forever.
const DefaultCommandTimeout = 2 * time.Minute

// mostCommandTimeout is as long as the settings can make a command run.
const mostCommandTimeout = 30 * time.Minute

// Sh runs a command in dir with the default timeout, as ShFor does.
func Sh(ctx context.Context, dir, command string) (string, error) {
	return ShFor(0)(ctx, dir, command)
}

// ShFor is the command runner of a settings file: a command runs at most
// timeout, 0 for DefaultCommandTimeout, and the model reads at most
// maxOutput of what it printed.
func ShFor(timeout time.Duration) func(ctx context.Context, dir, command string) (string, error) {
	if timeout <= 0 {
		timeout = DefaultCommandTimeout
	}
	timeout = min(timeout, mostCommandTimeout)
	return func(ctx context.Context, dir, command string) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
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
}
