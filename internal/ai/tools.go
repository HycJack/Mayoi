package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"

	"mayoi/internal/vfs"
)

// Tools are the assistant's tools, rooted at a folder: a path outside it
// is refused, as are the absolute ones.
type Tools struct {
	Root string
}

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

// Under is the path of p in root, and fails when it is outside it.
func Under(root, p string) (string, error) {
	if root == "" {
		return "", errors.New("no folder is open")
	}
	full := p
	if !startsAtARoot(full) {
		full = filepath.Join(root, filepath.FromSlash(p))
	}
	full = filepath.Clean(full)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the folder", p)
	}
	return full, nil
}

// startsAtARoot says the path begins at a root of the system, absolute,
// or a slash either way: a folder cannot hold any of those as a name.
func startsAtARoot(p string) bool {
	return filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`)
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
