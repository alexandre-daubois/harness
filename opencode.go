package harness

import (
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
)

// OpencodeHarness drives OpenCode in headless run mode.
type OpencodeHarness struct{}

func (OpencodeHarness) Binary() string { return "opencode" }

func (OpencodeHarness) Args(j Job) []string {
	args := []string{
		"run",
		"--format", "json",
		"--auto",
	}
	if j.Model != "" {
		args = append(args, "--model", j.Model)
	}
	if j.ResumeSessionID != "" {
		args = append(args, "--session", j.ResumeSessionID)
	}
	return append(args, OpencodeHarness{}.Prompt(j))
}

func (OpencodeHarness) Prompt(j Job) string {
	return explicitSkillPrompt(j, "./.opencode/skill/"+j.SkillName)
}

func (OpencodeHarness) ParseStream(r io.Reader, emit func(Event)) {
	scanJSONL(r, emit, parseOpencodeLine)
}

type opencodeLine struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Part      *opencodePart   `json:"part"`
	Error     json.RawMessage `json:"error"`
}

type opencodePart struct {
	Type   string            `json:"type"`
	Text   string            `json:"text"`
	Tool   string            `json:"tool"`
	Name   string            `json:"name"`
	State  opencodeToolState `json:"state"`
	Cost   float64           `json:"cost"`
	Tokens *opencodeTokens   `json:"tokens"`
}

type opencodeToolState struct {
	Input json.RawMessage `json:"input"`
}

type opencodeTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

func parseOpencodeLine(raw []byte, emit func(Event)) {
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return
	}
	var event opencodeLine
	if err := json.Unmarshal(raw, &event); err != nil {
		emit(Event{Kind: KindText, Text: line})
		return
	}
	switch {
	case event.Type == "step_start" && event.SessionID != "":
		emit(Event{Kind: KindSession, SessionID: event.SessionID})
	case isOpencodeToolEvent(event):
		name := event.Part.Tool
		if name == "" {
			name = event.Part.Name
		}
		emit(Event{Kind: KindTool, Tool: name, Text: summariseInput(name, event.Part.State.Input)})
	case event.Type == "error" || len(event.Error) > 0:
		emit(Event{Kind: KindError, Text: opencodeErrorText(event.Error, line)})
	case isOpencodeReasoningEvent(event):
		emit(Event{Kind: KindThinking, Text: event.Part.Text})
	case isOpencodeTextEvent(event):
		emit(Event{Kind: KindText, Text: event.Part.Text})
	case event.Type == "step_finish" && event.Part != nil:
		emit(Event{
			Kind:    KindResult,
			CostUSD: event.Part.Cost,
			Turns:   1,
			Usage:   opencodeUsage(event.Part.Tokens),
		})
	case event.Type == "step_finish":
	default:
		emit(Event{Kind: KindText, Text: line})
	}
}

func opencodeUsage(tokens *opencodeTokens) Usage {
	if tokens == nil {
		return Usage{}
	}
	return Usage{
		InputTokens:      tokens.Input,
		OutputTokens:     tokens.Output + tokens.Reasoning,
		CacheReadTokens:  tokens.Cache.Read,
		CacheWriteTokens: tokens.Cache.Write,
	}
}

func isOpencodeToolEvent(event opencodeLine) bool {
	if event.Part == nil {
		return false
	}
	return event.Type == "tool" || event.Part.Type == "tool" ||
		event.Part.Tool != "" || event.Part.Name != ""
}

func isOpencodeReasoningEvent(event opencodeLine) bool {
	if event.Part == nil || event.Part.Text == "" {
		return false
	}
	return event.Type == "reasoning" || event.Part.Type == "reasoning"
}

func isOpencodeTextEvent(event opencodeLine) bool {
	if event.Part == nil || event.Part.Text == "" {
		return false
	}
	return event.Type == "text" || event.Part.Type == "text"
}

func opencodeErrorText(raw json.RawMessage, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var value struct {
		Message string `json:"message"`
		Name    string `json:"name"`
		Code    string `json:"code"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &value) == nil {
		for _, candidate := range []string{value.Data.Message, value.Message, value.Code, value.Name} {
			if candidate != "" {
				return candidate
			}
		}
	}
	return strings.TrimSpace(string(raw))
}

func (OpencodeHarness) SkillDir(workspace, name string) string {
	return filepath.Join(workspace, ".opencode", "skill", name)
}

func (OpencodeHarness) GuideFilename() string { return "AGENTS.md" }

func (OpencodeHarness) EgressHosts() []string {
	return []string{"models.dev", "api.openai.com", "*.anthropic.com"}
}

func (OpencodeHarness) Env(_ string) []string {
	env := []string{
		"OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_MODELS_FETCH=true",
		"OPENCODE_DISABLE_SHARE=true",
	}
	return append(env, passthroughEnv(
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"OPENCODE_CONFIG_CONTENT",
		"OPENCODE_AUTH_CONTENT",
	)...)
}

func (OpencodeHarness) StateEnv(dir string) []string {
	return []string{
		"OPENCODE_CONFIG_DIR=" + dir,
		"OPENCODE_DB=" + filepath.Join(dir, "opencode.db"),
	}
}

func (OpencodeHarness) DefaultModels() []ModelDefault {
	claude := ClaudeHarness{}.DefaultModels()
	models := make([]ModelDefault, len(claude))
	for i, model := range claude {
		models[i] = ModelDefault{
			Name: model.Name,
			ID:   "anthropic/" + model.ID,
			Tier: model.Tier,
		}
	}
	return models
}

var opencodeAccountPhrases = []string{
	"rate limit",
	"rate_limit",
	"too many requests",
	"429",
	"usage limit",
	"quota",
	"insufficient_quota",
	"invalid_api_key",
	"incorrect api key",
	"invalid x-api-key",
	"credit balance",
	"billing",
}

func (OpencodeHarness) AccountErrorText(s string) string {
	return matchAccountPhrase(s, opencodeAccountPhrases)
}
