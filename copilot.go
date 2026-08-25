package harness

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CopilotHarness drives GitHub Copilot CLI in non-interactive prompt mode.
// Its arguments and JSONL mapping target Copilot CLI 1.0.80 while retaining
// compatibility with the prompt-mode stream introduced in 1.0.75.
type CopilotHarness struct{}

func (CopilotHarness) Binary() string { return "copilot" }

// Args enables autopilot and tool use without interactive confirmation. The
// caller must isolate the process and workspace because --allow-all approves
// every tool exposed after any AllowedTools filter is applied.
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
	if tools := copilotAvailableTools(j); tools != "" {
		args = append(args, "--available-tools="+tools)
	}
	if id := safeSessionID(j.ResumeSessionID); id != "" {
		args = append(args, "--resume="+id)
	}
	return args
}

var copilotClaudeToolAliases = map[string][]string{
	"Bash":      {"bash", "read_bash", "stop_bash", "list_bash"},
	"Edit":      {"edit"},
	"Glob":      {"glob"},
	"Grep":      {"grep"},
	"Read":      {"view"},
	"Skill":     {"skill"},
	"Task":      {"task", "read_agent", "list_agents", "write_agent"},
	"WebFetch":  {"web_fetch"},
	"WebSearch": {"web_search"},
	"Write":     {"create", "edit"},
}

func copilotAvailableTools(j Job) string {
	if strings.TrimSpace(j.AllowedTools) == "" {
		return ""
	}
	raw := strings.Split(j.AllowedTools, ",")
	tools := make([]string, 0, len(raw)+2)
	seen := make(map[string]struct{}, len(raw)+2)
	appendTool := func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		tools = append(tools, name)
	}
	for _, name := range raw {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		aliases, ok := copilotClaudeToolAliases[name]
		if !ok {
			aliases = []string{name}
		}
		for _, alias := range aliases {
			appendTool(alias)
		}
	}
	if j.SkillName != "" {
		appendTool("skill")
	}
	return strings.Join(tools, ",")
}

func (CopilotHarness) Prompt(j Job) string {
	return explicitSkillPrompt(j, "./.github/skills/"+j.SkillName)
}

// ParseStream combines per-call token usage, cumulative billing checkpoints,
// and root-agent turns into one result emitted after Copilot's final envelope.
// Sub-agent calls remain part of the usage total, but their nested conversation
// events stay out of the parent stream.
func (CopilotHarness) ParseStream(r io.Reader, emit func(Event)) {
	state := copilotStreamState{
		result: Event{Kind: KindResult},
	}
	scanJSONL(r, emit, state.parseLine)
	if state.sawTerminal {
		state.result.CostUSD = state.estimatedCostUSD
		if state.sawUsageCheckpoint {
			state.result.CostUSD = state.checkpointCostUSD
		}
		// Per-call billing is scoped to this invocation, unlike a checkpoint
		// restored from an earlier lifetime of a resumed session.
		if state.sawBilledUsage {
			state.result.CostUSD = state.billedCostUSD
		}
		state.result.Usage.OutputTokens += state.unreportedMessageOutputTokenTotal()
		emit(state.result)
	}
}

// copilotLine contains the shared envelope fields used by prompt-mode JSONL.
// Unknown event types pass through as text so a CLI update remains visible.
type copilotLine struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	AgentID   string          `json:"agentId"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parentId"`
	SessionID string          `json:"sessionId"`
	ExitCode  *int            `json:"exitCode"`
}

type copilotStreamState struct {
	result                Event
	resultAPICallID       string
	resultChunks          []string
	messageReasoning      map[string]string
	messageOutputTokens   map[string]int
	usageOutputAPICalls   map[string]struct{}
	unkeyedOutputTokens   int
	sawUnkeyedUsageOutput bool
	estimatedCostUSD      float64
	checkpointCostUSD     float64
	billedCostUSD         float64
	sawUsageCheckpoint    bool
	sawBilledUsage        bool
	sawTerminal           bool
}

type copilotMessageData struct {
	APICallID     string `json:"apiCallId"`
	ChunkCount    *int   `json:"chunkCount"`
	ChunkIndex    *int   `json:"chunkIndex"`
	Content       string `json:"content"`
	MessageID     string `json:"messageId"`
	OutputTokens  *int   `json:"outputTokens"`
	ReasoningText string `json:"reasoningText"`
}

type copilotReasoningData struct {
	Content string `json:"content"`
}

type copilotIntentData struct {
	Intent string `json:"intent"`
}

type copilotToolData struct {
	ToolName  string          `json:"toolName"`
	Arguments json.RawMessage `json:"arguments"`
}

type copilotTurnData struct {
	TurnID string `json:"turnId"`
}

type copilotUsageData struct {
	APICallID        string                          `json:"apiCallId"`
	Model            string                          `json:"model"`
	InputTokens      *int                            `json:"inputTokens"`
	OutputTokens     *int                            `json:"outputTokens"`
	CacheReadTokens  *int                            `json:"cacheReadTokens"`
	CacheWriteTokens *int                            `json:"cacheWriteTokens"`
	CopilotUsage     *copilotBilledUsage             `json:"copilotUsage"`
	QuotaSnapshots   map[string]copilotQuotaSnapshot `json:"quotaSnapshots"`
}

type copilotBilledUsage struct {
	TotalNanoAIU float64 `json:"totalNanoAiu"`
}

type copilotUsageCheckpointData struct {
	TotalNanoAIU *float64 `json:"totalNanoAiu"`
}

type copilotQuotaSnapshot struct {
	HasQuota                         *bool    `json:"hasQuota"`
	EntitlementRequests              *float64 `json:"entitlementRequests"`
	IsUnlimitedEntitlement           bool     `json:"isUnlimitedEntitlement"`
	Overage                          float64  `json:"overage"`
	OverageAllowedWithExhaustedQuota bool     `json:"overageAllowedWithExhaustedQuota"`
	RemainingPercentage              *float64 `json:"remainingPercentage"`
	ResetDate                        string   `json:"resetDate"`
	UsageAllowedWithExhaustedQuota   bool     `json:"usageAllowedWithExhaustedQuota"`
	UsedRequests                     *float64 `json:"usedRequests"`
}

type copilotErrorData struct {
	Message    string          `json:"message"`
	Error      json.RawMessage `json:"error"`
	ErrorType  string          `json:"errorType"`
	ErrorCode  string          `json:"errorCode"`
	StatusCode *int            `json:"statusCode"`
}

type copilotAbortData struct {
	Reason string `json:"reason"`
}

type copilotInfoData struct {
	Message string `json:"message"`
}

type copilotModelCallFailureData struct {
	ErrorCode      string                          `json:"errorCode"`
	ErrorMessage   string                          `json:"errorMessage"`
	ErrorType      string                          `json:"errorType"`
	FailureKind    string                          `json:"failureKind"`
	Model          string                          `json:"model"`
	QuotaSnapshots map[string]copilotQuotaSnapshot `json:"quotaSnapshots"`
	StatusCode     *int                            `json:"statusCode"`
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
		state.handleMessage(&event, line, emit)
	case "assistant.reasoning":
		state.handleReasoning(&event, line, emit)
	case "assistant.intent":
		handleCopilotIntent(&event, line, emit)
	case "tool.execution_start":
		handleCopilotTool(&event, line, emit)
	case "assistant.usage":
		state.handleUsage(event.Data, line, emit)
	case "model.call_failure":
		handleCopilotModelCallFailure(event.Data, line, emit)
	case "session.usage_checkpoint":
		state.handleUsageCheckpoint(event.Data, line, emit)
	case "assistant.turn_end":
		state.handleTurnEnd(&event, line, emit)
	case "result":
		state.handleResult(&event, emit)
	case "session.error", "error":
		emitCopilotError(event.Data, line, emit)
	case "abort":
		emitCopilotAbort(event.Data, line, emit)
	case "session.info", "session.warning":
		handleCopilotInfo(event.Data, line, emit)
	case "assistant.message_delta",
		"assistant.message_start",
		"assistant.reasoning_delta",
		"assistant.streaming_delta",
		"assistant.tool_call_delta",
		"assistant.turn_start",
		"assistant.idle",
		"model.call_start",
		"model.call_end",
		"mcp.prompts.list_changed",
		"mcp.resources.list_changed",
		"mcp.tools.list_changed",
		"session.idle",
		"session.mcp_server_status_changed",
		"session.mcp_servers_loaded",
		"session.skills_loaded",
		"session.task_complete",
		"session.tools_updated",
		"session.usage_info",
		"tool.execution_complete",
		"tool.execution_partial_result",
		"tool.execution_progress",
		"user.message":
	default:
		emit(Event{Kind: KindText, Text: line})
	}
}

func (state *copilotStreamState) handleMessage(
	event *copilotLine,
	fallback string,
	emit func(Event),
) {
	var data copilotMessageData
	if !decodeCopilotData(event.Data, &data, fallback, emit) {
		return
	}
	state.recordMessageOutput(data)
	if event.AgentID != "" {
		return
	}
	if event.ID != "" && data.ReasoningText != "" {
		if state.messageReasoning == nil {
			state.messageReasoning = make(map[string]string)
		}
		state.messageReasoning[event.ID] = data.ReasoningText
	}
	state.emitMessage(data, emit)
}

func (state *copilotStreamState) handleReasoning(
	event *copilotLine,
	fallback string,
	emit func(Event),
) {
	if event.AgentID != "" {
		return
	}
	var data copilotReasoningData
	if !decodeCopilotData(event.Data, &data, fallback, emit) {
		return
	}
	if data.Content == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	if event.ParentID != "" {
		if reasoning, ok := state.messageReasoning[event.ParentID]; ok {
			delete(state.messageReasoning, event.ParentID)
			if data.Content == reasoning {
				return
			}
		}
	}
	emit(Event{Kind: KindThinking, Text: data.Content})
}

func handleCopilotIntent(event *copilotLine, fallback string, emit func(Event)) {
	if event.AgentID != "" {
		return
	}
	var data copilotIntentData
	if !decodeCopilotData(event.Data, &data, fallback, emit) {
		return
	}
	if data.Intent == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	emit(Event{Kind: KindThinking, Text: data.Intent})
}

func handleCopilotTool(event *copilotLine, fallback string, emit func(Event)) {
	if event.AgentID != "" {
		return
	}
	var data copilotToolData
	if !decodeCopilotData(event.Data, &data, fallback, emit) {
		return
	}
	if data.ToolName == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	emit(Event{
		Kind: KindTool,
		Tool: data.ToolName,
		Text: summariseInput(data.ToolName, data.Arguments),
	})
}

func (state *copilotStreamState) handleUsage(
	raw json.RawMessage,
	fallback string,
	emit func(Event),
) {
	var data copilotUsageData
	if !decodeCopilotData(raw, &data, fallback, emit) {
		return
	}
	if data.Model == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	state.addUsage(data)
	emitCopilotRateLimits(data.QuotaSnapshots, emit)
}

func (state *copilotStreamState) handleUsageCheckpoint(
	raw json.RawMessage,
	fallback string,
	emit func(Event),
) {
	var data copilotUsageCheckpointData
	if !decodeCopilotData(raw, &data, fallback, emit) {
		return
	}
	if data.TotalNanoAIU == nil || *data.TotalNanoAIU < 0 {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	// Checkpoints are cumulative, so the latest value replaces earlier values
	// and any token-based estimate rather than being added to them.
	state.checkpointCostUSD = copilotCheckpointCostUSD(*data.TotalNanoAIU)
	state.sawUsageCheckpoint = true
}

func (state *copilotStreamState) handleTurnEnd(
	event *copilotLine,
	fallback string,
	emit func(Event),
) {
	if event.AgentID != "" {
		return
	}
	var data copilotTurnData
	if !decodeCopilotData(event.Data, &data, fallback, emit) {
		return
	}
	if data.TurnID == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	state.result.Turns++
}

func (state *copilotStreamState) handleResult(event *copilotLine, emit func(Event)) {
	if state.sawTerminal {
		return
	}
	state.sawTerminal = true
	failed := event.ExitCode != nil && *event.ExitCode != 0
	if event.SessionID != "" && !failed {
		emit(Event{Kind: KindSession, SessionID: event.SessionID})
	}
	if failed {
		emit(Event{Kind: KindError, Text: fmt.Sprintf("copilot exited with code %d", *event.ExitCode)})
	}
}

func handleCopilotInfo(raw json.RawMessage, fallback string, emit func(Event)) {
	var data copilotInfoData
	if !decodeCopilotData(raw, &data, fallback, emit) {
		return
	}
	if data.Message == "" {
		emit(Event{Kind: KindText, Text: fallback})
		return
	}
	emit(Event{Kind: KindText, Text: data.Message})
}

func handleCopilotModelCallFailure(
	raw json.RawMessage,
	fallback string,
	emit func(Event),
) {
	var data copilotModelCallFailureData
	if !decodeCopilotData(raw, &data, fallback, emit) {
		return
	}
	emitCopilotRateLimits(data.QuotaSnapshots, emit)
	text := data.ErrorMessage
	if text == "" {
		text = "model call failed"
	}
	var details []string
	for _, detail := range []string{
		data.Model,
		data.FailureKind,
		data.ErrorType,
		data.ErrorCode,
	} {
		if detail != "" {
			details = append(details, detail)
		}
	}
	if data.StatusCode != nil {
		details = append(details, "status "+strconv.Itoa(*data.StatusCode))
	}
	if len(details) > 0 {
		text += " (" + strings.Join(details, ", ") + ")"
	}
	emit(Event{Kind: KindError, Text: text})
}

func (state *copilotStreamState) emitMessage(data copilotMessageData, emit func(Event)) {
	if data.ReasoningText != "" {
		emit(Event{Kind: KindThinking, Text: data.ReasoningText})
	}
	if data.Content != "" {
		emit(Event{Kind: KindText, Text: data.Content})
		state.recordResultMessage(data)
	}
}

func (state *copilotStreamState) recordResultMessage(data copilotMessageData) {
	// A model call can emit several complete message records around reasoning
	// boundaries. Reassemble those records, but replace the result when a later
	// API call starts so Text remains the final response rather than a transcript.
	if data.APICallID == "" ||
		data.ChunkCount == nil ||
		data.ChunkIndex == nil ||
		*data.ChunkCount <= 1 ||
		*data.ChunkIndex < 0 ||
		*data.ChunkIndex >= *data.ChunkCount {
		state.resultAPICallID = data.APICallID
		state.resultChunks = nil
		state.result.Text = data.Content
		return
	}
	if state.resultAPICallID != data.APICallID ||
		len(state.resultChunks) != *data.ChunkCount {
		state.resultAPICallID = data.APICallID
		state.resultChunks = make([]string, *data.ChunkCount)
	}
	state.resultChunks[*data.ChunkIndex] = data.Content
	state.result.Text = strings.Join(state.resultChunks, "")
}

func (state *copilotStreamState) recordMessageOutput(data copilotMessageData) {
	if data.OutputTokens == nil || *data.OutputTokens < 0 {
		return
	}
	key := data.APICallID
	if key == "" {
		key = data.MessageID
	}
	if key == "" {
		state.unkeyedOutputTokens += *data.OutputTokens
		return
	}
	if state.messageOutputTokens == nil {
		state.messageOutputTokens = make(map[string]int)
	}
	if *data.OutputTokens > state.messageOutputTokens[key] {
		state.messageOutputTokens[key] = *data.OutputTokens
	}
}

func (state *copilotStreamState) unreportedMessageOutputTokenTotal() int {
	if state.sawUnkeyedUsageOutput {
		return 0
	}
	total := state.unkeyedOutputTokens
	for apiCallID, tokens := range state.messageOutputTokens {
		if _, reported := state.usageOutputAPICalls[apiCallID]; !reported {
			total += tokens
		}
	}
	return total
}

func (state *copilotStreamState) addUsage(data copilotUsageData) {
	usage := Usage{
		InputTokens:      copilotTokenCount(data.InputTokens),
		OutputTokens:     copilotTokenCount(data.OutputTokens),
		CacheReadTokens:  copilotTokenCount(data.CacheReadTokens),
		CacheWriteTokens: copilotTokenCount(data.CacheWriteTokens),
	}
	if data.OutputTokens != nil && *data.OutputTokens >= 0 {
		if data.APICallID == "" {
			state.sawUnkeyedUsageOutput = true
		} else {
			if state.usageOutputAPICalls == nil {
				state.usageOutputAPICalls = make(map[string]struct{})
			}
			state.usageOutputAPICalls[data.APICallID] = struct{}{}
		}
	}
	state.estimatedCostUSD += CostFromUsage(data.Model, usage)
	if data.CopilotUsage != nil && data.CopilotUsage.TotalNanoAIU >= 0 {
		state.billedCostUSD += copilotCheckpointCostUSD(data.CopilotUsage.TotalNanoAIU)
		state.sawBilledUsage = true
	}
	state.result.Usage.InputTokens += usage.InputTokens
	state.result.Usage.OutputTokens += usage.OutputTokens
	state.result.Usage.CacheReadTokens += usage.CacheReadTokens
	state.result.Usage.CacheWriteTokens += usage.CacheWriteTokens
}

func copilotTokenCount(value *int) int {
	if value == nil || *value < 0 {
		return 0
	}
	return *value
}

const (
	nanoAIUPerAICredit = 1e9
	usdPerAICredit     = 0.01
)

func copilotCheckpointCostUSD(totalNanoAIU float64) float64 {
	// Copilot bills one AI credit as $0.01; nano-AIU uses the SI nano scale.
	return totalNanoAIU / nanoAIUPerAICredit * usdPerAICredit
}

func decodeCopilotData(raw json.RawMessage, target any, fallback string, emit func(Event)) bool {
	if len(raw) == 0 || json.Unmarshal(raw, target) != nil {
		emit(Event{Kind: KindText, Text: fallback})
		return false
	}
	return true
}

func emitCopilotRateLimits(snapshots map[string]copilotQuotaSnapshot, emit func(Event)) {
	keys := make([]string, 0, len(snapshots))
	for key, snapshot := range snapshots {
		exhausted := snapshot.HasQuota != nil && !*snapshot.HasQuota
		if snapshot.HasQuota == nil && !snapshot.IsUnlimitedEntitlement {
			exhausted = snapshot.RemainingPercentage != nil &&
				*snapshot.RemainingPercentage <= 0
			if snapshot.EntitlementRequests != nil && snapshot.UsedRequests != nil {
				exhausted = exhausted ||
					*snapshot.UsedRequests >= *snapshot.EntitlementRequests
			}
		}
		if exhausted {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		snapshot := snapshots[key]
		status := "allowed"
		if !snapshot.UsageAllowedWithExhaustedQuota &&
			!snapshot.OverageAllowedWithExhaustedQuota {
			status = "rejected"
		}
		overageStatus := "rejected"
		if snapshot.OverageAllowedWithExhaustedQuota {
			overageStatus = "allowed"
		}
		usingOverage := snapshot.Overage > 0 &&
			snapshot.OverageAllowedWithExhaustedQuota
		info := &RateLimitInfo{
			Status:         status,
			OverageStatus:  overageStatus,
			IsUsingOverage: usingOverage,
			Type:           key,
		}
		if reset, err := time.Parse(time.RFC3339, snapshot.ResetDate); err == nil {
			info.ResetsAt = reset.Unix()
		}
		emit(Event{Kind: KindRateLimit, RateLimit: info})
	}
}

func emitCopilotError(raw json.RawMessage, fallback string, emit func(Event)) {
	var data copilotErrorData
	if json.Unmarshal(raw, &data) == nil {
		text := data.Message
		if text == "" {
			text = copilotNestedErrorText(data.Error)
		}
		if text != "" {
			var details []string
			if data.ErrorType != "" {
				details = append(details, data.ErrorType)
			}
			if data.ErrorCode != "" {
				details = append(details, data.ErrorCode)
			}
			if data.StatusCode != nil {
				details = append(details, "status "+strconv.Itoa(*data.StatusCode))
			}
			if len(details) > 0 {
				text += " (" + strings.Join(details, ", ") + ")"
			}
			emit(Event{Kind: KindError, Text: text})
			return
		}
	}
	emit(Event{Kind: KindError, Text: fallback})
}

func copilotNestedErrorText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var nested struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &nested) == nil {
		return nested.Message
	}
	return ""
}

func emitCopilotAbort(raw json.RawMessage, fallback string, emit func(Event)) {
	var data copilotAbortData
	if json.Unmarshal(raw, &data) == nil && data.Reason != "" {
		emit(Event{Kind: KindError, Text: "copilot aborted: " + data.Reason})
		return
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
		env = append(env, passthroughEnv(
			"COPILOT_MODEL",
			"COPILOT_PROVIDER_API_KEY",
			"COPILOT_PROVIDER_BEARER_TOKEN",
			"COPILOT_PROVIDER_TYPE",
			"COPILOT_PROVIDER_WIRE_API",
			"COPILOT_PROVIDER_TRANSPORT",
			"COPILOT_PROVIDER_AZURE_API_VERSION",
			"COPILOT_PROVIDER_MODEL_ID",
			"COPILOT_PROVIDER_WIRE_MODEL",
			"COPILOT_PROVIDER_MAX_PROMPT_TOKENS",
			"COPILOT_PROVIDER_MAX_OUTPUT_TOKENS",
			"COPILOT_PROVIDER_HEADERS",
		)...)
	}
	return env
}

func (CopilotHarness) StateEnv(dir string) []string {
	return []string{"COPILOT_HOME=" + dir}
}

func (CopilotHarness) DefaultModels() []ModelDefault {
	// These IDs are accepted by Copilot CLI 1.0.80. Dotted Anthropic IDs are
	// distinct from Claude Code's hyphenated provider IDs.
	return []ModelDefault{
		{Name: "Claude Sonnet 4.6", ID: "claude-sonnet-4.6", Tier: "mid"},
		{Name: "Claude Opus 4.6", ID: "claude-opus-4.6", Tier: "high"},
		{Name: "Claude Opus 4.7", ID: "claude-opus-4.7"},
		{Name: "Claude Opus 4.8", ID: "claude-opus-4.8"},
		{Name: "Claude Opus 5.0", ID: "claude-opus-5", Tier: "max"},
		{Name: "Claude Sonnet 5.0", ID: "claude-sonnet-5"},
		{Name: "Claude Haiku 4.5", ID: "claude-haiku-4.5"},
		{Name: "Claude Fable 5", ID: "claude-fable-5"},
		{Name: "GPT-5.3 Codex", ID: "gpt-5.3-codex"},
		{Name: "GPT-5.4", ID: "gpt-5.4"},
	}
}

func (CopilotHarness) AccountErrorText(s string) string {
	return matchAccountPhrase(s, transientLimitPhrases, accessRevokedPhrases)
}
