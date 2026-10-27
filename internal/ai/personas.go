package ai

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxMemory is how much of MAYOI.md the assistant reads.
const maxMemory = 32 << 10

// memoryFor is how long a project's memory is kept before it is read
// again.
const memoryFor = 30 * time.Second

// Persona is how the assistant answers: its name, what it is for, and
// what it is told to be.
type Persona struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	Instructions string `json:"instructions"`
	// BuiltIn says the persona comes with Mayoi, so settings do not keep
	// it and cannot change it.
	BuiltIn bool `json:"-"`
}

// builtInPersonas are the five the panel offers. The instructions are
// ported from terax-clone's agents.ts, where they were written for a
// terminal; only the words that named it changed.
var builtInPersonas = []Persona{
	{
		ID:          "coder",
		Name:        "Coder",
		Description: "General-purpose coding assistant. Writes, edits, and runs.",
		Instructions: "You are an expert software engineer pair-programming inside the user's editor.\n" +
			"- Read files before editing them. Match existing patterns and naming.\n" +
			"- Prefer the smallest correct change. Don't refactor adjacent code unprompted.\n" +
			"- After non-trivial edits, run the project's checks (type-check, lint, test) when you can.\n" +
			"- Keep responses tight: short prose, code blocks with language fences.",
		BuiltIn: true,
	},
	{
		ID:          "architect",
		Name:        "Architect",
		Description: "Design and tradeoffs. Plans before code.",
		Instructions: "You are a senior software architect.\n" +
			"- Before proposing code, restate the problem in one sentence and surface 2–3 viable approaches with real tradeoffs.\n" +
			"- Recommend one with reasoning. Call out risks: scalability, coupling, data consistency, migration, blast radius.\n" +
			"- Reference the actual repo (read key files) before generalizing. No hand-wavy advice.\n" +
			"- Output structure: Problem · Options · Recommendation · Risks · Next steps.",
		BuiltIn: true,
	},
	{
		ID:          "reviewer",
		Name:        "Code Reviewer",
		Description: "Reviews diffs for correctness, perf, security.",
		Instructions: "You are a meticulous code reviewer.\n" +
			"- Focus on what tools cannot catch: logic errors, edge cases, race conditions, layer violations, perf cliffs (N+1, unneeded re-renders), security (injection, auth, secrets), data integrity.\n" +
			"- Skip formatting / naming / inferred-type nits — linters handle those.\n" +
			"- Output: `[MUST/SHOULD/NIT] file:line — issue → fix`. If nothing real, say \"Looks good.\"\n" +
			"- Verify each finding against the actual file before reporting it.",
		BuiltIn: true,
	},
	{
		ID:          "security",
		Name:        "Security",
		Description: "Threat-models changes and flags vulns.",
		Instructions: "You are an application-security engineer.\n" +
			"- Threat-model the change: what attacker, what asset, what trust boundary is crossed.\n" +
			"- Look specifically for: input validation at boundaries, authn/authz bypass, secret exposure, SSRF, path traversal, SQLi/XSS/CSRF, deserialization, dependency CVEs, insecure defaults.\n" +
			"- For each finding: severity, exploit sketch, concrete fix. Prefer fixes that close the class of bug, not the one report.\n" +
			"- If the change is benign, say so explicitly — don't fabricate findings.",
		BuiltIn: true,
	},
	{
		ID:          "designer",
		Name:        "Designer",
		Description: "UI/UX critique and refinement.",
		Instructions: "You are a senior product designer with a strong taste for restrained, modern UI.\n" +
			"- Critique on: hierarchy, spacing, density, contrast, motion, affordance, empty/error states.\n" +
			"- Propose concrete changes, with CSS values when helpful. Keep consistent with the surrounding design system.\n" +
			"- Avoid generic \"make it pop\" advice. Be specific about what's wrong and why.",
		BuiltIn: true,
	},
}

// Personas is what the panel offers: the five that come with Mayoi, then
// the ones the user wrote.
func Personas(custom []Persona) []Persona {
	out := make([]Persona, 0, len(builtInPersonas)+len(custom))
	out = append(out, builtInPersonas...)
	for _, p := range custom {
		if p.ID == "" || p.Instructions == "" {
			continue
		}
		p.BuiltIn = false
		out = append(out, p)
	}
	return out
}

// PersonaByID is the persona id, or nil when there is none.
func PersonaByID(ps []Persona, id string) *Persona {
	for i := range ps {
		if ps[i].ID == id {
			return &ps[i]
		}
	}
	return nil
}

// SystemFor is what the assistant is told of itself: the persona's
// instructions, or the base prompt when there is no persona, then what
// the folder says of itself.
func SystemFor(p Persona, base, memory string) string {
	var sb strings.Builder
	if strings.TrimSpace(p.Instructions) == "" {
		sb.WriteString(base)
	} else {
		sb.WriteString(strings.TrimSpace(p.Instructions))
	}
	if memory != "" {
		sb.WriteString("\n\n# What this project says of itself (MAYOI.md)\n\n")
		sb.WriteString(memory)
	}
	return sb.String()
}

// memoryCache keeps what each folder's MAYOI.md said, and when.
var memoryCache struct {
	mu    sync.Mutex
	byDir map[string]cachedMemory
}

type cachedMemory struct {
	text string
	at   time.Time
}

// Memory is what the folder's MAYOI.md says, cut short at maxMemory, or
// "" when it has none. A folder is read again every memoryFor.
func Memory(root string) string {
	if root == "" {
		return ""
	}
	memoryCache.mu.Lock()
	got, ok := memoryCache.byDir[root]
	memoryCache.mu.Unlock()
	if ok && time.Since(got.at) < memoryFor {
		return got.text
	}
	text := readMemory(root)
	memoryCache.mu.Lock()
	if memoryCache.byDir == nil {
		memoryCache.byDir = map[string]cachedMemory{}
	}
	memoryCache.byDir[root] = cachedMemory{text: text, at: time.Now()}
	memoryCache.mu.Unlock()
	return text
}

// readMemory reads MAYOI.md of the folder, cut short.
func readMemory(root string) string {
	b, err := os.ReadFile(filepath.Join(root, "MAYOI.md"))
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(b))
	// The ellipsis is three bytes, and stays within the cap.
	if len(s) > maxMemory-len("…") {
		// Cut on a line, so the model does not read half a sentence as
		// a whole one.
		s = s[:maxMemory-len("…")]
		if i := strings.LastIndexByte(s, '\n'); i > 0 {
			s = s[:i]
		}
		s += "…"
	}
	return s
}

// Env is what the assistant is told of where the user is, so that a
// question about "this file" means the one on screen.
func Env(root, file string) string {
	var sb strings.Builder
	sb.WriteString("<env>\n")
	sb.WriteString("workspace: " + root + "\n")
	if file != "" {
		sb.WriteString("file: " + file + "\n")
	}
	sb.WriteString("</env>")
	return sb.String()
}
