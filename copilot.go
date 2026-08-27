package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// CopilotHarness drives GitHub Copilot CLI in non-interactive prompt mode.
// Its arguments and JSONL mapping target Copilot CLI 1.0.80 while retaining
// compatibility with the prompt-mode stream introduced in 1.0.75.
type CopilotHarness struct{}

func (CopilotHarness) Binary() string { return "copilot" }

// Args enables autopilot and tool use without interactive confirmation. The
// caller must isolate the process and workspace because --allow-all grants the
// CLI every tool it exposes.
func (CopilotHarness) Args(j Job) []string {
	maxTurns := j.MaxTurns
	if maxTurns <= 0 {
		maxTurns = DefaultMaxTurns
	}
	args := []string{
		"-p", CopilotHarness{}.Prompt(j),
		"--output-format", "json",
		"--autopilot",
		"--max-autopilot-continues", strconv.Itoa(maxTurns),
		"--allow-all",
		"--no-ask-user",
		"--no-auto-update",
		"--no-color",
		"--no-remote-export",
	}
	if j.Model != "" {
		args = append(args, "--model", j.Model)
	}
	if j.Effort != "" {
		args = append(args, "--effort", j.Effort)
	}
	if id := safeSessionID(j.ResumeSessionID); id != "" {
		args = append(args, "--resume="+id)
	}
	return args
}

func (CopilotHarness) Prompt(j Job) string {
	return explicitSkillPrompt(j, "./.github/skills/"+j.SkillName)
}

// ParseStream combines per-call token usage and cumulative billing checkpoints
// into one result emitted after Copilot's final envelope. This gives callers
// the same single-run totals exposed by the other backends.
func (CopilotHarness) ParseStream(r io.Reader, emit func(Event)) {
	state := copilotStreamState{result: Event{Kind: KindResult}}
	scanJSONL(r, emit, state.parseLine)
	if !state.sawTerminal {
		return
	}
	state.result.CostUSD = state.estimatedCostUSD
	if state.sawCheckpoint {
		state.result.CostUSD = state.checkpointCostUSD
	}
	emit(state.result)
}

// copilotLine contains the shared envelope fields used by prompt-mode JSONL.
// Unknown event types pass through as text so a CLI update remains visible.
type copilotLine struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	SessionID string          `json:"sessionId"`
	ExitCode  *int            `json:"exitCode"`
}

// copilotStreamState accumulates the terminal result across the JSONL stream.
type copilotStreamState struct {
	result            Event
	estimatedCostUSD  float64
	checkpointCostUSD float64
	sawCheckpoint     bool
	sawTerminal       bool
}

type copilotMessageData struct {
	Content       string `json:"content"`
	ReasoningText string `json:"reasoningText"`
}

type copilotReasoningData struct {
	Content string `json:"content"`
}

type copilotToolData struct {
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
}

type copilotUsageData struct {
	Model            string `json:"model"`
	InputTokens      int    `json:"inputTokens"`
	OutputTokens     int    `json:"outputTokens"`
	CacheReadTokens  int    `json:"cacheReadTokens"`
	CacheWriteTokens int    `json:"cacheWriteTokens"`
}

type copilotUsageCheckpointData struct {
	TotalNanoAIU *float64 `json:"totalNanoAiu"`
}

type copilotErrorData struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

func (state *copilotStreamState) parseLine(raw []byte, emit func(Event)) {
	line := strings.TrimSpace(string(raw))
	if line == "" {
		return
	}
	var event copilotLine
	if err := json.Unmarshal(raw, &event); err != nil {
		emit(Event{Kind: KindText, Text: line})
		return
	}
	switch event.Type {
	case "assistant.message":
		emitCopilotMessage(event.Data, emit)
	case "assistant.reasoning":
		var data copilotReasoningData
		if json.Unmarshal(event.Data, &data) == nil && data.Content != "" {
			emit(Event{Kind: KindThinking, Text: data.Content})
		}
	case "tool.execution_start":
		var data copilotToolData
		if json.Unmarshal(event.Data, &data) == nil {
			emit(Event{
				Kind: KindTool,
				Tool: data.ToolName,
				Text: summariseInput(data.ToolName, data.Arguments),
			})
		}
	case "assistant.usage":
		var data copilotUsageData
		if json.Unmarshal(event.Data, &data) == nil {
			state.addUsage(data)
		}
	case "session.usage_checkpoint":
		var data copilotUsageCheckpointData
		if json.Unmarshal(event.Data, &data) == nil && data.TotalNanoAIU != nil {
			// Checkpoints are cumulative for the session, so the latest value
			// replaces earlier values and any token-based estimate.
			state.checkpointCostUSD = copilotNanoAIUCostUSD(*data.TotalNanoAIU)
			state.sawCheckpoint = true
		}
	case "assistant.turn_end":
		state.result.Turns++
	case "result":
		state.sawTerminal = true
		if event.SessionID != "" {
			emit(Event{Kind: KindSession, SessionID: event.SessionID})
		}
		if event.ExitCode != nil && *event.ExitCode != 0 {
			emit(Event{Kind: KindError, Text: fmt.Sprintf("copilot exited with code %d", *event.ExitCode)})
		}
	case "abort", "session.error", "error":
		emitCopilotError(event.Data, line, emit)
	case "assistant.message_delta",
		"assistant.message_start",
		"assistant.reasoning_delta",
		"assistant.tool_call_delta",
		"assistant.turn_start",
		"assistant.idle",
		"model.call_start",
		"model.call_end",
		"session.mcp_server_status_changed",
		"session.mcp_servers_loaded",
		"session.skills_loaded",
		"session.tools_updated",
		"session.usage_info",
		"user.message":
	default:
		emit(Event{Kind: KindText, Text: line})
	}
}

func (state *copilotStreamState) addUsage(data copilotUsageData) {
	usage := Usage{
		InputTokens:      data.InputTokens,
		OutputTokens:     data.OutputTokens,
		CacheReadTokens:  data.CacheReadTokens,
		CacheWriteTokens: data.CacheWriteTokens,
	}
	state.estimatedCostUSD += CostFromUsage(data.Model, usage)
	state.result.Usage.InputTokens += usage.InputTokens
	state.result.Usage.OutputTokens += usage.OutputTokens
	state.result.Usage.CacheReadTokens += usage.CacheReadTokens
	state.result.Usage.CacheWriteTokens += usage.CacheWriteTokens
}

const (
	nanoAIUPerAICredit = 1e9
	usdPerAICredit     = 0.01
)

// copilotNanoAIUCostUSD converts Copilot's billing unit to USD. One AI credit
// is $0.01 and nano-AIU uses the SI nano scale.
func copilotNanoAIUCostUSD(totalNanoAIU float64) float64 {
	return totalNanoAIU / nanoAIUPerAICredit * usdPerAICredit
}

func emitCopilotMessage(raw json.RawMessage, emit func(Event)) {
	var data copilotMessageData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	if data.ReasoningText != "" {
		emit(Event{Kind: KindThinking, Text: data.ReasoningText})
	}
	if data.Content != "" {
		emit(Event{Kind: KindText, Text: data.Content})
	}
}

func emitCopilotError(raw json.RawMessage, fallback string, emit func(Event)) {
	var data copilotErrorData
	if json.Unmarshal(raw, &data) == nil {
		if data.Message != "" {
			emit(Event{Kind: KindError, Text: data.Message})
			return
		}
		if data.Error != "" {
			emit(Event{Kind: KindError, Text: data.Error})
			return
		}
	}
	emit(Event{Kind: KindError, Text: fallback})
}

func (CopilotHarness) SkillDir(workspace, name string) string {
	return filepath.Join(workspace, ".github", "skills", name)
}

func (CopilotHarness) GuideFilename() string {
	return filepath.Join(".github", "copilot-instructions.md")
}

func (CopilotHarness) SystemPromptViaArgs() bool { return false }

func (CopilotHarness) EgressHosts() []string {
	// GitHub hosts authentication and MCP traffic; githubcopilot.com serves
	// Copilot's model and session APIs.
	return []string{
		"github.com",
		"api.github.com",
		"api.mcp.github.com",
		"*.githubcopilot.com",
	}
}

func (CopilotHarness) Env(baseURL string) []string {
	env := []string{
		"COPILOT_AUTO_UPDATE=false",
		"COPILOT_OTEL_ENABLED=false",
		"NO_COLOR=1",
	}
	env = append(env, passthroughEnv("COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN")...)
	if baseURL != "" {
		env = append(env, "COPILOT_PROVIDER_BASE_URL="+baseURL)
	}
	return env
}

func (CopilotHarness) StateEnv(dir string) []string {
	return []string{"COPILOT_HOME=" + dir}
}

func (CopilotHarness) DefaultModels() []ModelDefault {
	// This is Copilot CLI 1.0.80's /model --list --json catalog. The reported
	// current model, GPT-5.6 Sol, is moved first because callers treat the first
	// entry as the default. Dotted Anthropic IDs are distinct from Claude Code's
	// hyphenated IDs.
	return []ModelDefault{
		{Name: "GPT-5.6 Sol", ID: "gpt-5.6-sol"},
		{Name: "Claude Sonnet 5", ID: "claude-sonnet-5"},
		{Name: "Claude Opus 5", ID: "claude-opus-5", Tier: "max"},
		{Name: "Claude Opus 4.8", ID: "claude-opus-4.8"},
		{Name: "Claude Opus 4.7", ID: "claude-opus-4.7"},
		{Name: "Claude Sonnet 4.6", ID: "claude-sonnet-4.6", Tier: "mid"},
		{Name: "Claude Opus 4.6", ID: "claude-opus-4.6", Tier: "high"},
		{Name: "Claude Haiku 4.5", ID: "claude-haiku-4.5"},
		{Name: "GPT-5.6 Terra", ID: "gpt-5.6-terra"},
		{Name: "GPT-5.6 Luna", ID: "gpt-5.6-luna"},
		{Name: "GPT-5.5", ID: "gpt-5.5"},
		{Name: "GPT-5.4", ID: "gpt-5.4"},
		{Name: "GPT-5.4 mini", ID: "gpt-5.4-mini"},
		{Name: "GPT-5.3-Codex", ID: "gpt-5.3-codex"},
		{Name: "GPT-5 mini", ID: "gpt-5-mini"},
		{Name: "MAI-Code-1-Flash", ID: "mai-code-1-flash-picker"},
		{Name: "Gemini 3.7 Flash", ID: "gemini-3.7-flash"},
		{Name: "Gemini 3.6 Flash", ID: "gemini-3.6-flash"},
		{Name: "Gemini 3.5 Flash", ID: "gemini-3.5-flash"},
		{Name: "Gemini 3.1 Pro Preview", ID: "gemini-3.1-pro-preview"},
		{Name: "Grok 4.5", ID: "grok-4.5"},
		{Name: "Grok 4.6", ID: "grok-4.6"},
		{Name: "MAI-Code-1.1-Flash", ID: "mai-code-1.1-flash"},
	}
}

// copilotAccountPhrases cover authentication, entitlement, and request-limit
// failures where an immediate retry is unlikely to help.
var copilotAccountPhrases = []string{
	"rate limit",
	"too many requests",
	"quota",
	"not entitled",
	"copilot access",
	"authentication failed",
	"unauthorized",
	"forbidden",
	"token expired",
	"429",
}

func (CopilotHarness) AccountErrorText(s string) string {
	return matchAccountPhrase(s, copilotAccountPhrases)
}
