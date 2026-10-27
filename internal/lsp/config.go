package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Preset is a language server Mayoi knows: what starts it, which files it
// serves, and which files mark the folder that is its project.
//
// The list is the one terax-clone ships (its
// `frontend/src/modules/lsp/lib/presets.ts`), which is the set of servers
// an editor of this kind is expected to start. A server that is not
// installed is not a problem: the preset is passed over for the next that
// serves the same file.
type Preset struct {
	// ID, Name and Command are what the server is called: Command is the
	// program and its arguments, run in the folder of the project.
	ID      string
	Name    string
	Command []string
	// Extensions are the file extensions (`go`, `tsx`) it serves, and
	// LanguageIDs the protocol's name for each of them.
	Extensions  []string
	LanguageIDs map[string]string
	// Markers are the files that say a folder is a project of this
	// server: the walk up from the file stops at the first that has one.
	Markers []string
	// Options are the initialization options the server is given.
	Options any
	// Env is the environment the server is started with, over Mayoi's.
	Env map[string]string
}

// presets are the servers, in the order a file with several is asked for
// one: Python is pyright's before it is ruff's, as in terax-clone.
var presets = []Preset{
	{
		ID: "typescript", Name: "TypeScript",
		Command:    []string{"typescript-language-server", "--stdio"},
		Extensions: []string{"ts", "tsx", "js", "jsx", "mjs", "cjs"},
		LanguageIDs: map[string]string{
			"ts": "typescript", "tsx": "typescriptreact",
			"js": "javascript", "jsx": "javascriptreact",
			"mjs": "javascript", "cjs": "javascript",
		},
		Markers: []string{"tsconfig.json", "jsconfig.json", "package.json"},
		Options: map[string]any{"maxTsServerMemory": 3072},
	},
	{
		ID: "rust-analyzer", Name: "Rust",
		Command:     []string{"rust-analyzer"},
		Extensions:  []string{"rs"},
		LanguageIDs: map[string]string{"rs": "rust"},
		Markers:     []string{"Cargo.toml"},
		// Measured by terax-clone: the default profile settles at about
		// 3 GB resident, this one at about 1 GB, trading analysis inside
		// proc macros and cargo-check diagnostics.
		Options: map[string]any{
			"cachePriming": map[string]any{"enable": false},
			"lru":          map[string]any{"capacity": 32},
			"checkOnSave":  false,
			"procMacro":    map[string]any{"enable": false},
			"cargo":        map[string]any{"buildScripts": map[string]any{"enable": false}},
			"diagnostics":  map[string]any{"disabled": []string{"unresolved-proc-macro", "unresolved-macro-call"}},
		},
		Env: map[string]string{"CARGO_BUILD_JOBS": "2"},
	},
	{
		ID: "pyright", Name: "Python",
		Command:     []string{"pyright-langserver", "--stdio"},
		Extensions:  []string{"py"},
		LanguageIDs: map[string]string{"py": "python"},
		Markers:     []string{"pyproject.toml", "setup.py", "requirements.txt", "Pipfile"},
	},
	{
		ID: "ruff", Name: "Ruff",
		Command:     []string{"ruff", "server"},
		Extensions:  []string{"py"},
		LanguageIDs: map[string]string{"py": "python"},
		Markers:     []string{"pyproject.toml", "ruff.toml", ".ruff.toml", "setup.py", "requirements.txt"},
	},
	{
		ID: "gopls", Name: "Go",
		Command:     []string{"gopls"},
		Extensions:  []string{"go"},
		LanguageIDs: map[string]string{"go": "go"},
		Markers:     []string{"go.mod", "go.work"},
	},
	{
		ID: "clangd", Name: "C/C++",
		Command:    []string{"clangd"},
		Extensions: []string{"c", "h", "cpp", "hpp", "cc", "hh"},
		LanguageIDs: map[string]string{
			"c": "c", "h": "c", "cpp": "cpp", "hpp": "cpp", "cc": "cpp", "hh": "cpp",
		},
		Markers: []string{"compile_commands.json", "CMakeLists.txt", "Makefile", ".clangd"},
	},
	{
		ID: "zls", Name: "Zig",
		Command:     []string{"zls"},
		Extensions:  []string{"zig"},
		LanguageIDs: map[string]string{"zig": "zig"},
		Markers:     []string{"build.zig"},
	},
	{
		ID: "lua-ls", Name: "Lua",
		Command:     []string{"lua-language-server"},
		Extensions:  []string{"lua"},
		LanguageIDs: map[string]string{"lua": "lua"},
		Markers:     []string{".luarc.json", ".luarc.jsonc"},
	},
	{
		ID: "ruby-lsp", Name: "Ruby",
		Command:     []string{"ruby-lsp"},
		Extensions:  []string{"rb"},
		LanguageIDs: map[string]string{"rb": "ruby"},
		Markers:     []string{"Gemfile"},
	},
	{
		ID: "intelephense", Name: "PHP",
		Command:     []string{"intelephense", "--stdio"},
		Extensions:  []string{"php"},
		LanguageIDs: map[string]string{"php": "php"},
		Markers:     []string{"composer.json"},
	},
	{
		ID: "yaml-ls", Name: "YAML",
		Command:     []string{"yaml-language-server", "--stdio"},
		Extensions:  []string{"yaml", "yml"},
		LanguageIDs: map[string]string{"yaml": "yaml", "yml": "yaml"},
		Markers:     []string{".git"},
	},
	{
		ID: "bash-ls", Name: "Shell",
		Command:    []string{"bash-language-server", "start"},
		Extensions: []string{"sh", "bash", "zsh"},
		LanguageIDs: map[string]string{
			"sh": "shellscript", "bash": "shellscript", "zsh": "shellscript",
		},
		Markers: []string{".git"},
	},
	{
		ID: "json-ls", Name: "JSON",
		Command:     []string{"vscode-json-language-server", "--stdio"},
		Extensions:  []string{"json", "jsonc"},
		LanguageIDs: map[string]string{"json": "json", "jsonc": "jsonc"},
		Markers:     []string{".git"},
	},
	{
		ID: "css-ls", Name: "CSS",
		Command:     []string{"vscode-css-language-server", "--stdio"},
		Extensions:  []string{"css", "scss", "less"},
		LanguageIDs: map[string]string{"css": "css", "scss": "scss", "less": "less"},
		Markers:     []string{"package.json", ".git"},
	},
	{
		ID: "html-ls", Name: "HTML",
		Command:     []string{"vscode-html-language-server", "--stdio"},
		Extensions:  []string{"html", "htm"},
		LanguageIDs: map[string]string{"html": "html", "htm": "html"},
		Markers:     []string{"package.json", ".git"},
	},
	{
		ID: "svelte-ls", Name: "Svelte",
		Command:     []string{"svelteserver", "--stdio"},
		Extensions:  []string{"svelte"},
		LanguageIDs: map[string]string{"svelte": "svelte"},
		Markers:     []string{"svelte.config.js", "package.json"},
	},
	{
		ID: "vue-ls", Name: "Vue",
		Command:     []string{"vue-language-server", "--stdio"},
		Extensions:  []string{"vue"},
		LanguageIDs: map[string]string{"vue": "vue"},
		Markers:     []string{"vite.config.ts", "vite.config.js", "package.json"},
	},
	{
		ID: "sourcekit", Name: "Swift",
		Command:     []string{"sourcekit-lsp"},
		Extensions:  []string{"swift"},
		LanguageIDs: map[string]string{"swift": "swift"},
		Markers:     []string{"Package.swift"},
	},
}

// Presets is the servers Mayoi knows, in the order they are asked for.
func Presets() []Preset {
	out := make([]Preset, len(presets))
	copy(out, presets)
	return out
}

// UseFake has every server be the fake one of the tests, started with
// start: a test of what Mayoi does with a server needs no server installed,
// and the same one runs on every machine. It is undone by RestorePresets.
func UseFake(extensions []string, command []string, env map[string]string) {
	presets = []Preset{{
		ID: "fake", Name: "Fake Server",
		Command: command, Extensions: extensions, Env: env,
		LanguageIDs: map[string]string{"fake": "fake", "go": "go"},
	}}
}

// RestorePresets puts back the servers UseFake replaced.
func RestorePresets() { presets = builtin }

// builtin are the servers Mayoi starts, which UseFake replaces.
var builtin = presets

// extension is the extension of a path, lowercased and without its dot.
func extension(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// Serves says whether a preset serves the file at path.
func (p Preset) Serves(path string) bool {
	ext := extension(path)
	for _, e := range p.Extensions {
		if e == ext {
			return true
		}
	}
	return false
}

// LanguageID is the protocol's name for the file at path, "" if this
// preset does not serve it.
func (p Preset) LanguageID(path string) string {
	return p.LanguageIDs[extension(path)]
}

// Installed says whether the server's program is where a command is found.
func (p Preset) Installed() bool {
	if len(p.Command) == 0 {
		return false
	}
	_, err := exec.LookPath(p.Command[0])
	return err == nil
}

// For returns the presets that serve the file at path: the first whose
// program is installed is the one Mayoi starts, and the others wait for a
// machine that has them.
func For(path string) []Preset {
	var out []Preset
	for _, p := range presets {
		if p.Serves(path) {
			out = append(out, p)
		}
	}
	return out
}

// Best is the preset to start for the file at path, if any is installed.
func Best(path string) (Preset, bool) {
	for _, p := range For(path) {
		if p.Installed() {
			return p, true
		}
	}
	return Preset{}, false
}

// Root is the folder of the project the file at path is in: the first on
// the way up that holds a marker, and the folder the editor has open when
// none does, and the file's own when even that is above it.
func Root(path string, markers []string, workspace string) string {
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return workspace
	}
	workspace, _ = filepath.Abs(workspace)
	for {
		if hasMarker(dir, markers) {
			return dir
		}
		if workspace != "" && strings.EqualFold(dir, workspace) {
			return workspace
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return workspace
		}
		dir = parent
	}
}

// hasMarker says whether dir holds one of the markers.
func hasMarker(dir string, markers []string) bool {
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(dir, m)); err == nil {
			return true
		}
	}
	return false
}
