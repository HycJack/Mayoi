package settings

import "strings"

// The permissions of the assistant: what it may do in the folder without
// asking. They are the presets of a coding agent that changes files and
// runs commands, and each says what the user is asked before.
const (
	// PermReadOnly reads and searches the folder, and changes nothing:
	// no write, no command. Nothing asks, because nothing can go wrong.
	PermReadOnly = "read-only"
	// PermAsk shows a card before every write and every command.
	PermAsk = "ask"
	// PermWorkspaceWrite writes and edits the files of the folder without
	// a card, and asks before a command, which Mayoi cannot confine.
	PermWorkspaceWrite = "workspace-write"
	// PermFullAccess writes and runs what the assistant asks without a
	// card at all.
	PermFullAccess = "danger-full-access"
)

// Permissions is every permission the assistant may be given, in the order
// a list shows them: the one that asks the most first.
func Permissions() []string {
	return []string{PermReadOnly, PermAsk, PermWorkspaceWrite, PermFullAccess}
}

// KnownPermission reports whether name is one of them, so that a settings
// file of another version, or of a hand, still means something.
func KnownPermission(name string) bool {
	for _, p := range Permissions() {
		if p == name {
			return true
		}
	}
	return false
}

// Permission is what the assistant may do without asking. A file written
// before the preset existed says it in the two settings that came first:
// approving every edit was full access when commands were allowed, and
// nothing else was a card before everything.
func (s AISettings) Permission() string {
	if KnownPermission(s.Permissions) {
		return s.Permissions
	}
	switch {
	case s.ApproveEdits && s.AllowBash:
		return PermFullAccess
	case s.ApproveEdits:
		return PermWorkspaceWrite
	}
	return PermAsk
}

// Writes reports whether the permission lets the assistant change files.
func (s AISettings) Writes() bool { return s.Permission() != PermReadOnly }

// Runs reports whether the permission lets the assistant run commands.
func (s AISettings) Runs() bool { return s.Permission() != PermReadOnly }

// Allows reports whether the user trusts the program named, so that its
// commands run without a card.
func (s AISettings) Allows(program string) bool {
	for _, allowed := range s.Allowed {
		if strings.EqualFold(allowed, program) {
			return true
		}
	}
	return false
}

// Allow trusts a program: its commands run without a card from the next on.
func (s *AISettings) Allow(program string) {
	program = strings.ToLower(strings.TrimSpace(program))
	if program == "" || s.Allows(program) {
		return
	}
	// In the order they were allowed, since the settings file is read by
	// people too.
	s.Allowed = append(s.Allowed, program)
}

// ForgetAllowed stops trusting every program, so that the next command
// asks again.
func (s *AISettings) ForgetAllowed() { s.Allowed = nil }

// settled turns a settings file of before the presets into one of them:
// the two settings it replaces say what the permission is, and are written
// no more.
func (s *AISettings) settled() {
	s.Permissions = s.Permission()
	s.ApproveEdits, s.AllowBash = false, false
}
