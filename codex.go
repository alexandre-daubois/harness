package harness

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// CodexHarness drives Codex in headless exec mode.
type CodexHarness struct{}

func (CodexHarness) Binary() string { return "codex" }

func (CodexHarness) Args(j Job) []string {
	var args []string
	if j.BaseURL != "" {
		args = append(args, "-c", "openai_base_url="+strconv.Quote(j.BaseURL))
	}
	args = append(args,
		"exec",
		"--json",
		"--sandbox", "danger-full-access",
		"--skip-git-repo-check",
	)
	if j.Model != "" {
		args = append(args, "--model", j.Model)
	}
	if j.ResumeSessionID != "" {
		args = append(args, "resume", j.ResumeSessionID)
	}
	return append(args, CodexHarness{}.Prompt(j))
}

func (CodexHarness) Prompt(j Job) string {
	return explicitSkillPrompt(j, "./skills/"+j.SkillName)
}

func (CodexHarness) ParseStream(r io.Reader, emit func(Event)) {
	scanJSONL(r, emit, parseCodexLine)
}

type codexLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id"`
	ThreadID  string          `json:"thread_id"`
	Text      string          `json:"text"`
	Message   string          `json:"message"`
	Tool      string          `json:"tool"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Error     string          `json:"error"`
	Item      *codexItem      `json:"item"`
	Usage     *codexUsage     `json:"usage"`
}

type codexItem struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Message string          `json:"message"`
	Command string          `json:"command"`
	Tool    string          `json:"tool"`
	Name    string          `json:"name"`
	Input   json.RawMessage `json:"input"`
}

type codexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

func parseCodexLine(raw []byte, emit func(Event)) {
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return
	}
	var event codexLine
	if err := json.Unmarshal(raw, &event); err != nil {
		if strings.HasPrefix(line, "Reading additional input from stdin") {
			return
		}
		emit(Event{Kind: KindText, Text: line})
		return
	}
	switch {
	case isCodexSessionEvent(event) && (event.SessionID != "" || event.ThreadID != ""):
		id := event.SessionID
		if id == "" {
			id = event.ThreadID
		}
		emit(Event{Kind: KindSession, SessionID: id})
	case event.Type == "turn.started":
	case event.Type == "turn.completed":
		var usage Usage
		if event.Usage != nil {
			usage = Usage{
				InputTokens:     event.Usage.InputTokens,
				OutputTokens:    event.Usage.OutputTokens,
				CacheReadTokens: event.Usage.CachedInputTokens,
			}
		}
		emit(Event{Kind: KindResult, Usage: usage, Turns: 1})
	case event.Type == "item.started":
	case event.Item != nil && event.Item.Type == "error":
		emit(Event{Kind: KindError, Text: event.Item.Message})
	case event.Item != nil && event.Item.Text != "":
		emit(Event{Kind: KindText, Text: event.Item.Text})
	case event.Item != nil && isCodexToolItem(event.Item.Type):
		name := codexToolName(event.Item)
		emit(Event{Kind: KindTool, Tool: name, Text: codexToolText(event.Item)})
	case event.Type == "tool" || event.Tool != "":
		name := event.Tool
		if name == "" {
			name = event.Name
		}
		emit(Event{Kind: KindTool, Tool: name, Text: summariseInput(name, event.Input)})
	case event.Error != "":
		emit(Event{Kind: KindError, Text: event.Error})
	case event.Text != "":
		emit(Event{Kind: KindText, Text: event.Text})
	case event.Message != "":
		emit(Event{Kind: KindText, Text: event.Message})
	default:
		emit(Event{Kind: KindText, Text: line})
	}
}

func isCodexSessionEvent(event codexLine) bool {
	switch event.Type {
	case "thread.started", "session.created", "init":
		return true
	default:
		return false
	}
}

func isCodexToolItem(itemType string) bool {
	return strings.Contains(itemType, "command") || strings.Contains(itemType, "tool")
}

func codexToolName(item *codexItem) string {
	for _, name := range []string{item.Tool, item.Name} {
		if name != "" {
			return name
		}
	}
	if strings.Contains(item.Type, "command") {
		return "command"
	}
	return item.Type
}

func codexToolText(item *codexItem) string {
	if item.Command != "" {
		return item.Command
	}
	return summariseInput(codexToolName(item), item.Input)
}

func (CodexHarness) SkillDir(workspace, name string) string {
	return filepath.Join(workspace, "skills", name)
}

func (CodexHarness) GuideFilename() string { return "AGENTS.md" }

func (CodexHarness) EgressHosts() []string {
	return []string{"api.openai.com", "auth0.openai.com", "chatgpt.com"}
}

func (CodexHarness) Env(_ string) []string {
	env := []string{
		"RUST_LOG=error,opentelemetry_sdk=off,opentelemetry_otlp=off",
		"OMO_CODEX_SEND_ANONYMOUS_TELEMETRY=0",
		"OMO_CODEX_DISABLE_POSTHOG=1",
	}
	return append(env, passthroughEnv("CODEX_API_KEY")...)
}

func (CodexHarness) StateEnv(dir string) []string {
	return []string{"CODEX_HOME=" + dir}
}

func (CodexHarness) DefaultModels() []ModelDefault {
	return []ModelDefault{
		{Name: "GPT-5.3 Codex", ID: "gpt-5.3-codex", Tier: "high"},
		{Name: "GPT-5.4 mini", ID: "gpt-5.4-mini", Tier: "mid"},
		{Name: "GPT-5.4", ID: "gpt-5.4"},
		{Name: "GPT-5.5", ID: "gpt-5.5", Tier: "max"},
		{Name: "GPT-5.2", ID: "gpt-5.2"},
	}
}

var codexAccountPhrases = []string{
	"rate_limit",
	"rate limit",
	"too many requests",
	"429",
	"insufficient_quota",
	"quota exceeded",
	"invalid_api_key",
	"incorrect api key",
	"account is not active",
}

func (CodexHarness) AccountErrorText(s string) string {
	return matchAccountPhrase(s, codexAccountPhrases)
}
