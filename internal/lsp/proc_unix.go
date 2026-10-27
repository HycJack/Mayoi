//go:build !windows

package lsp

import (
	"os/exec"
	"syscall"
)

// setHideWindow is nothing here: only Windows shows a console window for a
// program started from a windowed app.
func setHideWindow(cmd *exec.Cmd) {}

// killTree ends the server and whatever it started: a language server runs
// the tools of its language, and those would outlive it.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Its own group, which -pid sends the end to, children and all.
	syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	cmd.Process.Kill()
}
