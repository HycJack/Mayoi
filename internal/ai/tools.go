package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"

	"mayoi/internal/vfs"
)

// Tools are the assistant's tools, rooted at a folder: a path outside it
// is refused, as are the absolute ones.
type Tools struct {
	Root string
	// Approve is asked before a write, nil to allow every one.
	Approve func(Change) bool
	// Exec runs a command in the folder, nil when commands are not
	// allowed, and then bash is not offered at all.
	Exec func(ctx context.Context, dir, command string) (string, error)
	// ReadOnly offers the tools that read the folder alone, as a folder
	// the assistant may look at and not touch: nothing asks, because
	// nothing it can do needs to.
	ReadOnly bool

	mu      sync.Mutex
	written []string
}

// Wrote is the files the tools changed, for the tabs that have them.
func (t *Tools) Wrote() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return slices.Clone(t.written)
}

// kept notes a file the tools changed.
func (t *Tools) kept(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.written = append(t.written, path)
}

// Sets is every tool the folder allows: the ones that read it, the ones
// that write it, and the command runner when there is one.
func (t *Tools) Sets() []agent.AgentTool {
	tools := t.ReadSet()
	if t.ReadOnly {
		return tools
	}
	tools = append(tools, t.WriteSet()...)
	if t.Exec == nil {
		return tools
	}
	return append(tools, agent.AgentTool{
		Name:        "bash",
		Description: "Run a shell command in the folder and read what it printed.",
		Label:       "Run",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}`),
		Execute:     t.Bash,
	})
}

// BashParams is what bash is asked: the command line to run.
type BashParams struct {
	Command string `json:"command"`
}

// Bash runs a command in the folder, once the user allowed it.
func (t *Tools) Bash(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p BashParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	if strings.TrimSpace(p.Command) == "" {
		return failure(errors.New("no command to run")), nil
	}
	if told, ok := t.approved(Change{Path: t.Root, Command: p.Command}); !ok {
		return told, nil
	}
	out, err := t.Exec(ctx, t.Root, p.Command)
	switch {
	case err != nil && out == "":
		return failure(err), nil
	case err != nil:
		return text(out + "\n" + err.Error()), nil
	case out == "":
		return text("the command printed nothing"), nil
	}
	return text(out), nil
}

// Change is what the assistant wants to do that the user must allow: a
// write, with the text before and after, or a command.
type Change struct {
	Path   string
	Before []byte
	After  []byte
	// Command is the command line a change runs, empty for a write.
	Command string
}

// WriteParams is what write is asked: the file and the text it takes.
type WriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditParams is what edit is asked: the file, the text to find in it
// and the text that takes its place.
type EditParams struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// maxFile is the biggest file a read takes, in bytes.
const maxFile = 8 << 20

// ReadParams is what read is asked: the file, and the lines of it.
type ReadParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}
// GlobParams is what glob is asked: the pattern of the files' paths.
type GlobParams struct {
	Pattern string `json:"pattern"`
}

// GrepParams is what grep is asked: the expression, and the files to look in.
type GrepParams struct {
	Pattern string `json:"pattern"`
	Include string `json:"include,omitempty"`
}

// Under is the path of p in root, and fails when it is outside it, or when
// it reaches outside it through a link.
func Under(root, p string) (string, error) {
	if root == "" {
		return "", errors.New("no folder is open")
	}
	// A path a model read from somewhere may arrive as a quoted string.
	if unquoted, err := strconv.Unquote(p); err == nil {
		p = unquoted
	}
	full := p
	if !startsAtARoot(full) {
		full = filepath.Join(root, filepath.FromSlash(p))
	}
	full = filepath.Clean(full)
	if !inside(root, full) {
		return "", fmt.Errorf("%s is outside the folder", p)
	}
	// A link inside the folder can point out of it, and writing through
	// one writes outside. Both sides resolve: on a Mac the folder itself
	// is often reached through a link, as /tmp is one.
	real, err := realPath(root, full)
	if err != nil {
		return "", err
	}
	realRoot, err := realPath(root, filepath.Clean(root))
	if err != nil {
		return "", err
	}
	if !inside(realRoot, real) {
		return "", fmt.Errorf("%s leads outside the folder", p)
	}
	return real, nil
}

// inside says dir holds full.
func inside(dir, full string) bool {
	d, err := filepath.Abs(dir)
	if err != nil {
		d = dir
	}
	rel, err := filepath.Rel(filepath.Clean(d), full)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// realPath is full with its links resolved, and with those of the deepest
// folder that exists: a file to be written has none yet.
func realPath(root, full string) (string, error) {
	if real, err := filepath.EvalSymlinks(full); err == nil {
		return real, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	var rest []string
	for dir := full; ; dir = filepath.Dir(dir) {
		real, err := filepath.EvalSymlinks(dir)
		if err == nil {
			for i := len(rest) - 1; i >= 0; i-- {
				real = filepath.Join(real, rest[i])
			}
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if parent := filepath.Dir(dir); parent == dir {
			return full, nil
		}
		rest = append(rest, filepath.Base(dir))
	}
}

// startsAtARoot says the path begins at a root of the system, absolute,
// a slash either way or a drive's own: a folder cannot hold any of those
// as a name.
func startsAtARoot(p string) bool {
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	// "C:file" is not absolute: it is file in that drive's own folder.
	return len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

// text is a result the model reads.
func text(s string) agent.AgentToolResult {
	return agent.AgentToolResult{Content: []core.ContentBlock{core.TextContent{Type: "text", Text: s}}}
}

// failure is a result the model reads, of what went wrong.
func failure(err error) agent.AgentToolResult {
	return agent.AgentToolResult{
		Content: []core.ContentBlock{core.TextContent{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

// Read is a file of the folder, numbered by line.
func (t *Tools) Read(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p ReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Under(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	// A file too big for a model to read is not read at all, rather than
	// filling the machine's memory first.
	if info, err := vfs.Stat(full); err == nil && info.Size() > maxFile {
		return failure(fmt.Errorf("%s is %d bytes, more than the %d a read takes: use offset and limit",
			p.Path, info.Size(), maxFile)), nil
	}
	data, err := vfs.ReadFile(full)
	if err != nil {
		return failure(err), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	from, to := 1, len(lines)
	if p.Offset > 0 {
		from = p.Offset
	}
	if p.Limit > 0 {
		to = min(to, from+p.Limit-1)
	}
	if from > len(lines) {
		return text(""), nil
	}
	var sb strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&sb, "%d\t%s\n", i, lines[i-1])
	}
	return text(sb.String()), nil
}

// Glob lists the files of the folder whose paths match the pattern, a
// pattern per path segment as the shell's, with ** for any of them.
func (t *Tools) Glob(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p GlobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	match, err := globMatch(p.Pattern)
	if err != nil {
		return failure(err), nil
	}
	var found []string
	for _, rel := range t.files() {
		if match(rel) {
			found = append(found, rel)
		}
	}
	sort.Strings(found)
	return text(strings.Join(found, "\n")), nil
}

// Grep finds the lines of the folder's files that match an expression,
// as the regular expression it is.
func (t *Tools) Grep(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p GrepParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	expr, err := regexp.Compile(p.Pattern)
	if err != nil {
		return failure(err), nil
	}
	var byName func(string) bool
	if p.Include != "" {
		include, err := globMatch(p.Include)
		if err != nil {
			return failure(err), nil
		}
		byName = include
	}
	var sb strings.Builder
	lines := 0
	for _, rel := range t.files() {
		if byName != nil && !byName(rel) {
			continue
		}
		data, err := vfs.ReadFile(filepath.Join(t.Root, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			if !expr.MatchString(line) {
				continue
			}
			fmt.Fprintf(&sb, "%s:%d: %s\n", rel, i+1, strings.TrimSpace(line))
			lines++
			// Enough of a file for the model to go on with.
			if lines >= maxGrepLines {
				return text(sb.String()), nil
			}
		}
	}
	return text(sb.String()), nil
}

// maxGrepLines is how many matches grep reports at most.
const maxGrepLines = 400

// files are the paths of the folder's files, relative to it, with the
// folders no one greps in left out.
func (t *Tools) files() []string {
	var out []string
	root := t.Root
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out
}

// skipDir says the folder is one the tools do not walk: the history of
// a work tree, the modules of a package, the trash of the system.
func skipDir(name string) bool {
	switch name {
	case ".git", "node_modules", ".mayoi", "__pycache__", ".venv":
		return true
	}
	return false
}

// globMatch is a matcher for a path pattern: ** is any number of
// segments, * and ? are as the shell's within one.
func globMatch(pattern string) (func(string) bool, error) {
	if pattern == "" {
		return nil, errors.New("no pattern to match")
	}
	expr, err := regexp.Compile(globRegexp(strings.TrimPrefix(pattern, "./")))
	if err != nil {
		return nil, fmt.Errorf("%s is not a pattern: %w", pattern, err)
	}
	return expr.MatchString, nil
}

// globRegexp is the regular expression of a path pattern.
func globRegexp(pattern string) string {
	var sb strings.Builder
	sb.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					sb.WriteString("(?:.*/)?")
				} else {
					sb.WriteString(".*")
				}
				continue
			}
			sb.WriteString("[^/]*")
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '|', '[', ']', '^', '$', '{', '}', '\\':
			sb.WriteString(`\` + string(c))
		default:
			sb.WriteString(string(c))
		}
	}
	sb.WriteString("$")
	return sb.String()
}

// approved asks the approver, if there is one, and reads a refusal as
// what the model is told.
func (t *Tools) approved(c Change) (agent.AgentToolResult, bool) {
	if t.Approve == nil || t.Approve(c) {
		return agent.AgentToolResult{}, true
	}
	return text("the user rejected this change"), false
}

// Write is a file of the folder, with the text it takes.
func (t *Tools) Write(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p WriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Under(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	before, _ := vfs.ReadFile(full)
	c := Change{Path: full, Before: before, After: []byte(p.Content)}
	if told, ok := t.approved(c); !ok {
		return told, nil
	}
	if _, err := vfs.WriteFile(full, c.After, 0o644); err != nil {
		return failure(err), nil
	}
	t.kept(full)
	return text("wrote " + p.Path), nil
}

// Edit is a file of the folder, with the one piece of text it has
// replaced.
func (t *Tools) Edit(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
	var p EditParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Under(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	before, err := vfs.ReadFile(full)
	if err != nil {
		return failure(err), nil
	}
	if !strings.Contains(string(before), p.Old) {
		return failure(fmt.Errorf("%q is not found in %s", p.Old, p.Path)), nil
	}
	after := strings.Replace(string(before), p.Old, p.New, 1)
	c := Change{Path: full, Before: before, After: []byte(after)}
	if told, ok := t.approved(c); !ok {
		return told, nil
	}
	if _, err := vfs.WriteFile(full, c.After, 0o644); err != nil {
		return failure(err), nil
	}
	t.kept(full)
	return text("edited " + p.Path), nil
}

// WriteSet is the tools that change the folder.
func (t *Tools) WriteSet() []agent.AgentTool {
	return []agent.AgentTool{
		{
			Name:        "write",
			Description: "Write a file of the folder, making it if it is new.",
			Label:       "Write",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
			Execute:     t.Write,
		},
		{
			Name:        "edit",
			Description: "Replace one piece of text in a file of the folder with another.",
			Label:       "Edit",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"}},"required":["path","old","new"]}`),
			Execute:     t.Edit,
		},
	}
}

// ReadSet is the tools that only look at the folder.
func (t *Tools) ReadSet() []agent.AgentTool {
	return []agent.AgentTool{
		{
			Name:        "read",
			Description: "Read a file of the folder. Numbered lines, with offset and limit for a page.",
			Label:       "Read",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`),
			Execute:     t.Read,
		},
		{
			Name:        "glob",
			Description: "List the files of the folder whose paths match a pattern, as **/*.go.",
			Label:       "Glob",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`),
			Execute:     t.Glob,
		},
		{
			Name:        "grep",
			Description: "Find the lines of the folder's files that match a regular expression.",
			Label:       "Grep",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"include":{"type":"string"}},"required":["pattern"]}`),
			Execute:     t.Grep,
		},
	}
}
