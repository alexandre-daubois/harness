package harness

import (
	"slices"
	"strings"
	"testing"
)

func TestCodexArgsAndStream(t *testing.T) {
	t.Parallel()

	args := CodexHarness{}.Args(Job{
		Prompt:          "Check it.",
		Model:           "gpt-5.3-codex",
		BaseURL:         "https://models.example.test/v1",
		ResumeSessionID: "thread-1",
		ResumePrompt:    "Check it.",
	})
	for _, want := range []string{"exec", "--json", "gpt-5.3-codex", "resume", "thread-1", "Check it."} {
		if !slices.Contains(args, want) {
			t.Errorf("Args() missing %q: %v", want, args)
		}
	}

	input := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-1"}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"go test ./..."}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":5}}`,
	}, "\n")
	var events []Event
	CodexHarness{}.ParseStream(strings.NewReader(input), func(event Event) {
		events = append(events, event)
	})
	if len(events) != 4 {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	if events[1].Kind != KindTool || events[1].Text != "go test ./..." {
		t.Errorf("tool event = %+v", events[1])
	}
	if events[3].Usage.CacheReadTokens != 20 || events[3].Turns != 1 {
		t.Errorf("result event = %+v", events[3])
	}
}
