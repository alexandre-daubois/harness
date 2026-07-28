package harness

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
)

const DefaultMaxTurns = 30

// Job contains the resolved inputs for one CLI invocation.
type Job struct {
	// Workspace is the command's working directory. Paths passed to a CLI are
	// relative to it.
	Workspace string

	// SkillName selects a staged SKILL.md directory. An empty value means no
	// staged skill.
	SkillName string

	// Prompt is the user turn for a fresh run. When it is empty and SkillName
	// is set, the harness builds an activation prompt.
	Prompt string

	// SystemPrompt supplies additional instructions. Claude receives it via
	// --system-prompt. Run writes it to the project guide file used by the
	// other backends.
	SystemPrompt string

	Model    string
	Effort   string
	MaxTurns int

	// OutputFile is a workspace-relative path the skill should write. It is
	// empty for free-form runs.
	OutputFile string

	// AllowedTools is Claude's comma-separated tool allowlist. Other backends
	// leave tool restrictions to their caller's sandbox.
	AllowedTools string

	// BaseURL overrides the model API endpoint where the backend supports it.
	BaseURL string

	// ResumeSessionID continues a prior conversation. ResumePrompt is the
	// corrective turn used for the resumed invocation.
	ResumeSessionID string
	ResumePrompt    string
}

// Harness describes the CLI-specific parts of an agent invocation.
type Harness interface {
	Binary() string
	Args(Job) []string
	Prompt(Job) string
	ParseStream(io.Reader, func(Event))
	SkillDir(workspace, name string) string
	GuideFilename() string
	EgressHosts() []string
	Env(baseURL string) []string
	StateEnv(dir string) []string
	AccountErrorText(string) string
	DefaultModels() []ModelDefault
}

// ModelDefault is one model offered by a backend.
type ModelDefault struct {
	Name string
	ID   string
	Tier string
}

var harnesses = map[string]Harness{
	"":         ClaudeHarness{},
	"claude":   ClaudeHarness{},
	"codex":    CodexHarness{},
	"copilot":  CopilotHarness{},
	"opencode": OpencodeHarness{},
}

// ByName resolves a backend name. The empty name selects Claude.
//
//nolint:ireturn // callers need the common interface selected by the registry
func ByName(name string) (Harness, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if h, ok := harnesses[name]; ok {
		return h, nil
	}
	return nil, fmt.Errorf("harness: unknown backend %q, must be one of %s", name, Names())
}

// Name returns the registered name of h. An unregistered implementation falls
// back to its binary name.
func Name(h Harness) string {
	if h == nil {
		return ""
	}
	typ := reflect.TypeOf(h)
	for name, registered := range harnesses {
		if name != "" && reflect.TypeOf(registered) == typ {
			return name
		}
	}
	return h.Binary()
}

// Names returns the registered backend names in lexical order.
func Names() string {
	names := make([]string, 0, len(harnesses)-1)
	for name := range harnesses {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func passthroughEnv(keys ...string) []string {
	var entries []string
	for _, key := range keys {
		if os.Getenv(key) != "" {
			entries = append(entries, key)
		}
	}
	return entries
}
