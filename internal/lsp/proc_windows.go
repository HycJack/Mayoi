//go:build windows

package lsp

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow keeps a console program from opening a window of its own
// beside the editor.
const createNoWindow = 0x08000000

// setHideWindow starts the server without a console window.
func setHideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}

// killTree ends the server and whatever it started: a language server runs
// the tools of its language, and those would outlive it.
func killTree(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err == nil {
		return
	}
	cmd.Process.Kill()
}
