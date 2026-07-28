package harness

import (
	"os"
	"slices"
	"testing"
)

func TestCopilotArgs(t *testing.T) {
	t.Parallel()

	args := CopilotHarness{}.Args(Job{
		Prompt:          "Check it.",
		Model:           "claude-sonnet-4.6",
		MaxTurns:        7,
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
		"claude-sonnet-4.6",
		"--resume=session-1",
	} {
		if !slices.Contains(args, want) {
			t.Errorf("Args() missing %q: %v", want, args)
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
	if len(events) != 6 {
		t.Fatalf("got %d events: %+v", len(events), events)
	}
	kinds := []string{
		KindThinking,
		KindTool,
		KindText,
		KindResult,
		KindResult,
		KindSession,
	}
	for i, want := range kinds {
		if events[i].Kind != want {
			t.Errorf("event %d kind = %q, want %q", i, events[i].Kind, want)
		}
	}
	if events[1].Text != "go test ./..." {
		t.Errorf("tool summary = %q", events[1].Text)
	}
	if events[3].Usage.CacheReadTokens != 80 {
		t.Errorf("usage event = %+v", events[3])
	}
	if events[5].SessionID != "34870a09-5067-4978-97bc-10d0d112ef64" {
		t.Errorf("session event = %+v", events[5])
	}
}
