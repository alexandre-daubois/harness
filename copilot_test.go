package harness

import (
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCopilotArgs(t *testing.T) {
	t.Parallel()

	args := CopilotHarness{}.Args(Job{
		Prompt:          "Check it.",
		SkillName:       "audit",
		Model:           "claude-sonnet-4.6",
		Effort:          "high",
		MaxTurns:        7,
		AllowedTools:    "Read,Write,Grep,Glob",
		ResumeSessionID: "session-1",
		ResumePrompt:    "Check it.",
	})
	for _, want := range []string{
		"-p",
		"Check it.",
		"--output-format",
		"json",
		"--autopilot",
		"7",
		"--allow-all",
		"--no-ask-user",
		"--no-remote-export",
		"claude-sonnet-4.6",
		"--effort",
		"high",
		"--available-tools=view,create,edit,grep,glob,skill,task_complete",
		"--resume=session-1",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("Args() missing %q: %v", want, args)
		}
	}
}

func TestCopilotArgsDefaultsAndToolNormalization(t *testing.T) {
	t.Parallel()

	args := CopilotHarness{}.Args(Job{
		Prompt:          "Check it.",
		ResumeSessionID: "--resume-latest",
	})
	maxIndex := slices.Index(args, "--max-autopilot-continues")
	if maxIndex < 0 || maxIndex+1 >= len(args) || args[maxIndex+1] != "30" {
		t.Fatalf("default max turns missing from %v", args)
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--resume=") {
			t.Errorf("unsafe resume id was passed: %v", args)
		}
		if strings.HasPrefix(arg, "--available-tools=") {
			t.Errorf("empty tool filter was passed: %v", args)
		}
	}

	tests := []struct {
		name string
		job  Job
		want string
	}{
		{
			name: "Claude read-write aliases",
			job: Job{
				SkillName:    "recon",
				AllowedTools: "Read,Write,Grep,Glob",
			},
			want: "view,create,edit,grep,glob,skill,task_complete",
		},
		{
			name: "shell web and agents",
			job: Job{
				AllowedTools: "Bash,WebFetch,WebSearch,Task",
			},
			want: "bash,read_bash,stop_bash,list_bash,web_fetch,web_search,task,read_agent,list_agents,write_agent,task_complete",
		},
		{
			name: "native names and duplicates",
			job: Job{
				SkillName:    "audit",
				AllowedTools: " view, Skill, ,grep,edit ",
			},
			want: "view,skill,grep,edit,task_complete",
		},
	}
	for _, test := range tests {
		if got := copilotAvailableTools(test.job); got != test.want {
			t.Errorf("%s: copilotAvailableTools() = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestCopilotStreamFixture(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/copilot-1.0.75.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	var events []Event
	CopilotHarness{}.ParseStream(file, func(event Event) {
		events = append(events, event)
	})
	if len(events) != 5 {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	kinds := []string{
		KindThinking,
		KindTool,
		KindText,
		KindSession,
		KindResult,
	}
	for i, want := range kinds {
		if events[i].Kind != want {
			t.Errorf("event %d kind = %q, want %q", i, events[i].Kind, want)
		}
	}
	if events[1].Text != "go test ./..." {
		t.Errorf("tool summary = %q", events[1].Text)
	}
	if events[4].Turns != 1 || events[4].Usage.CacheReadTokens != 80 {
		t.Errorf("result event = %+v", events[4])
	}
	if events[4].Text != "Done." {
		t.Errorf("result text = %q", events[4].Text)
	}
	if events[4].CostUSD <= 0 {
		t.Errorf("result cost = %f, want a list-price estimate", events[4].CostUSD)
	}
	if events[3].SessionID != "34870a09-5067-4978-97bc-10d0d112ef64" {
		t.Errorf("session event = %+v", events[3])
	}
}

func TestCopilotStreamAccumulatesOneTerminalResult(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.usage","data":{"model":"claude-sonnet-4.6","inputTokens":100,"outputTokens":10,"cacheReadTokens":20,"cacheWriteTokens":5}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"0"}}`,
		`{"type":"assistant.usage","data":{"model":"claude-opus-4.6","inputTokens":200,"outputTokens":30,"cacheReadTokens":40,"cacheWriteTokens":7}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"1"}}`,
		`{"type":"result","sessionId":"session-1","exitCode":0}`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 2 {
		t.Fatalf("events = %+v, want session and one result", events)
	}
	if events[0].Kind != KindSession || events[0].SessionID != "session-1" {
		t.Errorf("first event = %+v, want session", events[0])
	}
	result := events[1]
	if result.Kind != KindResult || result.Turns != 2 {
		t.Fatalf("terminal event = %+v, want two-turn result", result)
	}
	wantUsage := Usage{
		InputTokens:      300,
		OutputTokens:     40,
		CacheReadTokens:  60,
		CacheWriteTokens: 12,
	}
	if result.Usage != wantUsage {
		t.Errorf("usage = %+v, want %+v", result.Usage, wantUsage)
	}
	const wantCost = 0.0019785
	if math.Abs(result.CostUSD-wantCost) > 1e-12 {
		t.Errorf("cost = %.12f, want %.12f", result.CostUSD, wantCost)
	}
}

func TestCopilotStreamUsesLatestUsageCheckpointCost(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.usage","data":{"model":"claude-sonnet-4.6","inputTokens":1000000,"outputTokens":1000000}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"0"}}`,
		`{"type":"session.usage_checkpoint","data":{"totalNanoAiu":7313250000,"totalPremiumRequests":1}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"1"}}`,
		`{"type":"session.usage_checkpoint","data":{"totalNanoAiu":8148135000,"totalPremiumRequests":1}}`,
		`{"type":"result","sessionId":"session-1","exitCode":0}`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 2 {
		t.Fatalf("events = %+v, want session and result", events)
	}
	result := events[1]
	const wantCostUSD = 0.08148135
	if math.Abs(result.CostUSD-wantCostUSD) > 1e-12 {
		t.Errorf("cost = %.12f, want %.12f", result.CostUSD, wantCostUSD)
	}
}

func TestCopilotStreamFiltersSubagentConversation(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.intent","data":{"intent":"Checking the parent task."}}`,
		`{"type":"assistant.message","agentId":"child-1","data":{"content":"nested answer"}}`,
		`{"type":"assistant.reasoning","agentId":"child-1","data":{"content":"nested reasoning"}}`,
		`{"type":"tool.execution_start","agentId":"child-1","data":{"toolName":"shell","arguments":{"command":"nested"}}}`,
		`{"type":"assistant.usage","agentId":"child-1","data":{"model":"claude-sonnet-4.6","inputTokens":50,"outputTokens":5}}`,
		`{"type":"assistant.turn_end","agentId":"child-1","data":{"turnId":"child"}}`,
		`{"type":"assistant.message","data":{"content":"parent answer"}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"parent"}}`,
		`{"type":"result","sessionId":"session-1","exitCode":0}`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 4 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].Kind != KindThinking || events[0].Text != "Checking the parent task." {
		t.Errorf("intent event = %+v", events[0])
	}
	if events[1].Kind != KindText || events[1].Text != "parent answer" {
		t.Errorf("parent message = %+v", events[1])
	}
	result := events[3]
	if result.Kind != KindResult || result.Text != "parent answer" || result.Turns != 1 {
		t.Errorf("result = %+v", result)
	}
	if result.Usage.InputTokens != 50 || result.Usage.OutputTokens != 5 {
		t.Errorf("subagent usage was not retained: %+v", result.Usage)
	}
}

func TestCopilotStreamRequiresTerminalEnvelope(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.usage","data":{"model":"claude-sonnet-4.6","inputTokens":100}}`,
		`{"type":"assistant.turn_end","data":{"turnId":"0"}}`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 0 {
		t.Fatalf("unterminated stream emitted events: %+v", events)
	}
}

func TestCopilotStreamQuotaAndErrors(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.usage","data":{"model":"claude-sonnet-4.6","inputTokens":10,"quotaSnapshots":{"chat":{"hasQuota":true,"resetDate":"2026-08-26T12:00:00Z"},"premium_interactions":{"hasQuota":false,"overage":1,"overageAllowedWithExhaustedQuota":false,"usageAllowedWithExhaustedQuota":false,"resetDate":"2026-08-27T12:00:00Z"}}}}`,
		`{"type":"session.error","data":{"errorType":"quota","errorCode":"quota_exceeded","message":"Quota exceeded.","statusCode":429}}`,
		`{"type":"abort","data":{"reason":"user_cancelled"}}`,
		`{"type":"result","sessionId":"session-1","exitCode":2}`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 5 {
		t.Fatalf("events = %+v", events)
	}
	limit := events[0]
	if limit.Kind != KindRateLimit || limit.RateLimit == nil || !limit.RateLimit.Rejected() {
		t.Fatalf("rate limit = %+v", limit)
	}
	if limit.RateLimit.Type != "premium_interactions" {
		t.Errorf("rate limit type = %q", limit.RateLimit.Type)
	}
	wantReset := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	if got := limit.RateLimit.ResetTime(); got == nil || !got.Equal(wantReset) {
		t.Errorf("reset = %v, want %v", got, wantReset)
	}
	if events[1].Kind != KindError ||
		!strings.Contains(events[1].Text, "quota_exceeded") ||
		!strings.Contains(events[1].Text, "status 429") {
		t.Errorf("typed error = %+v", events[1])
	}
	if events[2].Kind != KindError || events[2].Text != "copilot aborted: user_cancelled" {
		t.Errorf("abort = %+v", events[2])
	}
	if events[3].Kind != KindError || !strings.Contains(events[3].Text, "code 2") {
		t.Errorf("exit error = %+v", events[3])
	}
	if events[4].Kind != KindResult {
		t.Errorf("terminal result = %+v", events[4])
	}
	for _, event := range events {
		if event.Kind == KindSession {
			t.Errorf("failed run emitted resumable session: %+v", event)
		}
	}
}

func TestCopilotStreamSurfacesMalformedAndUnknownEvents(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`{"type":"assistant.reasoning","data":"invalid"}`,
		`{"type":"future.event","data":{"value":1}}`,
		`not-json`,
	}, "\n")

	var events []Event
	CopilotHarness{}.ParseStream(strings.NewReader(stream), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 3 {
		t.Fatalf("events = %+v", events)
	}
	for i, event := range events {
		if event.Kind != KindText {
			t.Errorf("event %d = %+v", i, event)
		}
	}
}

func TestCopilotEnvIncludesBYOKConfiguration(t *testing.T) {
	keys := []string{
		"COPILOT_GITHUB_TOKEN",
		"GH_TOKEN",
		"GITHUB_TOKEN",
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
	}
	for _, key := range keys {
		t.Setenv(key, "sensitive")
	}

	env := CopilotHarness{}.Env("https://models.example.test/v1")
	for _, key := range keys {
		if !slices.Contains(env, key) {
			t.Errorf("Env() missing bare passthrough key %q: %v", key, env)
		}
		if slices.Contains(env, key+"=sensitive") {
			t.Errorf("Env() embedded value for %q", key)
		}
	}
	if !slices.Contains(env, "COPILOT_PROVIDER_BASE_URL=https://models.example.test/v1") {
		t.Errorf("Env() missing base URL: %v", env)
	}

	withoutBYOK := CopilotHarness{}.Env("")
	for _, key := range keys[3:] {
		if slices.Contains(withoutBYOK, key) {
			t.Errorf("Env() passed %q without a base URL: %v", key, withoutBYOK)
		}
	}
}

func TestCopilotAccountErrors(t *testing.T) {
	t.Parallel()

	h := CopilotHarness{}
	transient := "request failed: session_quota_exceeded"
	if got := h.AccountErrorText(transient); got != transient {
		t.Errorf("AccountErrorText() = %q", got)
	}
	if !AccountErrorResumable(transient) {
		t.Errorf("AccountErrorResumable(%q) = false", transient)
	}
	permanent := "rate_limit: billing_not_configured"
	if got := h.AccountErrorText(permanent); got != permanent {
		t.Errorf("AccountErrorText() = %q", got)
	}
	if AccountErrorResumable(permanent) {
		t.Errorf("AccountErrorResumable(%q) = true", permanent)
	}
}

func TestCopilotDefaultModelTiers(t *testing.T) {
	t.Parallel()

	models := CopilotHarness{}.DefaultModels()
	if len(models) == 0 || models[0].ID != "claude-sonnet-4.6" {
		t.Fatalf("default models = %+v", models)
	}
	wantTiers := map[string]string{
		"claude-sonnet-4.6": "mid",
		"claude-opus-4.6":   "high",
		"claude-opus-5":     "max",
	}
	for id, want := range wantTiers {
		index := slices.IndexFunc(models, func(model ModelDefault) bool {
			return model.ID == id
		})
		if index < 0 {
			t.Errorf("DefaultModels() missing %q", id)
			continue
		}
		if models[index].Tier != want {
			t.Errorf("model %q tier = %q, want %q", id, models[index].Tier, want)
		}
	}
}
