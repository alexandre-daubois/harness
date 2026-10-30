package harness

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// sessionRun records the jobs it receives. A job resuming "dead" fails with
// failure after emitting events, any other job succeeds as session "fresh".
func sessionRun(failure error, events ...Event) (func(context.Context, Job, func(Event)) error, *[]Job) {
	var jobs []Job
	return func(_ context.Context, j Job, emit func(Event)) error {
		jobs = append(jobs, j)
		if j.ResumeSessionID == "dead" {
			for _, event := range events {
				emit(event)
			}
			return failure
		}
		emit(Event{Kind: KindSession, SessionID: "fresh"})
		return nil
	}, &jobs
}

func TestRunSessionReportsSession(t *testing.T) {
	run, jobs := sessionRun(nil)
	var events []Event
	s, err := RunSession(t.Context(), run, Job{Prompt: "go"}, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err)
	}
	if s != (Session{ID: "fresh"}) {
		t.Errorf("session = %+v", s)
	}
	if len(*jobs) != 1 || len(events) != 1 || events[0].SessionID != "fresh" {
		t.Errorf("jobs = %+v, events = %+v", *jobs, events)
	}
}

func TestRunSessionReportsMaxTurns(t *testing.T) {
	failure := errors.New("exit status 1")
	run, _ := sessionRun(failure, Event{Kind: KindSession, SessionID: "dead"}, Event{Kind: KindError, Text: maxTurnsText})
	s, err := RunSession(t.Context(), run, Job{ResumeSessionID: "dead"}, nil)
	if !errors.Is(err, failure) {
		t.Fatalf("err = %v", err)
	}
	if s != (Session{ID: "dead", MaxTurns: true}) {
		t.Errorf("session = %+v", s)
	}
}

func TestRunSessionRestartsFreshResume(t *testing.T) {
	run, jobs := sessionRun(errors.New("exit status 1"), Event{Kind: KindError, Text: "No conversation found"})
	var texts []string
	s, err := RunSession(t.Context(), run, Job{Prompt: "start over", ResumeSessionID: "dead", ResumePrompt: "fix it"}, func(event Event) {
		if event.Kind == KindText {
			texts = append(texts, event.Text)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "fresh" {
		t.Errorf("session = %+v", s)
	}
	if len(*jobs) != 2 || (*jobs)[1].ResumeSessionID != "" || (*jobs)[1].Prompt != "start over" {
		t.Errorf("jobs = %+v", *jobs)
	}
	if !slices.ContainsFunc(texts, func(text string) bool { return strings.Contains(text, "restarting fresh") }) {
		t.Errorf("texts = %q", texts)
	}
}

func TestRunSessionKeepsFailedResume(t *testing.T) {
	failure := errors.New("exit status 1")
	for name, tc := range map[string]struct {
		job     Job
		failure error
		events  []Event
	}{
		"resume prompt only": {Job{ResumeSessionID: "dead", ResumePrompt: "fix it"}, failure, nil},
		"account error":      {Job{Prompt: "go", ResumeSessionID: "dead"}, &AccountError{Detail: "quota exceeded"}, nil},
		"session announced":  {Job{Prompt: "go", ResumeSessionID: "dead"}, failure, []Event{{Kind: KindSession, SessionID: "dead"}}},
		"fresh run":          {Job{Prompt: "go", ResumeSessionID: ""}, failure, nil},
	} {
		t.Run(name, func(t *testing.T) {
			run, jobs := sessionRun(tc.failure, tc.events...)
			if tc.job.ResumeSessionID == "" {
				run = func(_ context.Context, j Job, _ func(Event)) error {
					*jobs = append(*jobs, j)
					return failure
				}
			}
			_, err := RunSession(t.Context(), run, tc.job, nil)
			if !errors.Is(err, tc.failure) {
				t.Errorf("err = %v, want %v", err, tc.failure)
			}
			if len(*jobs) != 1 {
				t.Errorf("jobs = %+v, want one run", *jobs)
			}
		})
	}
}
