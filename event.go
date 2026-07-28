package harness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	KindThinking  = "thinking"
	KindText      = "text"
	KindTool      = "tool"
	KindResult    = "result"
	KindError     = "error"
	KindSession   = "session"
	KindRateLimit = "rate_limit"

	lineLimit = 300
)

// Event is one backend-neutral item from an agent's output stream.
type Event struct {
	Kind      string
	Tool      string
	Text      string
	CostUSD   float64
	Turns     int
	Usage     Usage
	SessionID string
	RateLimit *RateLimitInfo
}

// Usage is a result event's token accounting.
type Usage struct {
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	CacheReadTokens  int `json:"cache_read_input_tokens"`
	CacheWriteTokens int `json:"cache_creation_input_tokens"`
}

// RateLimitInfo contains subscription limit status reported by a backend.
type RateLimitInfo struct {
	Status         string `json:"status"`
	OverageStatus  string `json:"overageStatus"`
	IsUsingOverage bool   `json:"isUsingOverage"`
	ResetsAt       int64  `json:"resetsAt"`
	Type           string `json:"rateLimitType"`
}

// ResetTime converts ResetsAt to UTC. It returns nil for an absent or invalid
// reset time.
func (r *RateLimitInfo) ResetTime() *time.Time {
	if r == nil || r.ResetsAt <= 0 {
		return nil
	}
	reset := time.Unix(r.ResetsAt, 0).UTC()
	return &reset
}

// Rejected reports whether the limit currently blocks requests.
func (r *RateLimitInfo) Rejected() bool {
	if r == nil {
		return false
	}
	if strings.EqualFold(r.Status, "rejected") {
		return true
	}
	return r.IsUsingOverage && strings.EqualFold(r.OverageStatus, "rejected")
}

func summariseInput(tool string, raw json.RawMessage) string {
	var input map[string]any
	_ = json.Unmarshal(raw, &input)
	switch strings.ToLower(tool) {
	case "bash", "command", "shell":
		if command, _ := input["command"].(string); command != "" {
			return command
		}
	case "read", "write", "edit":
		for _, key := range []string{"file_path", "path"} {
			if path, _ := input[key].(string); path != "" {
				return path
			}
		}
	case "grep", "glob":
		if pattern, _ := input["pattern"].(string); pattern != "" {
			return pattern
		}
	}
	if len(raw) > 0 {
		return truncate(string(raw))
	}
	return ""
}

func truncate(s string) string {
	if len(s) <= lineLimit {
		return s
	}
	return s[:lineLimit] + fmt.Sprintf("... (%d chars)", len(s))
}

// FormatEvent renders an event as one plain-text log line.
func FormatEvent(e Event) string {
	switch e.Kind {
	case KindThinking:
		return "[thinking] " + truncate(e.Text)
	case KindTool:
		return fmt.Sprintf("[%s] %s", strings.ToLower(e.Tool), truncate(e.Text))
	case KindResult:
		return fmt.Sprintf("[result] cost=$%.4f turns=%d %s", e.CostUSD, e.Turns, truncate(e.Text))
	case KindSession:
		return "[session] " + e.SessionID
	case KindRateLimit:
		if e.RateLimit == nil {
			return "[rate-limit]"
		}
		line := "[rate-limit] " + e.RateLimit.Type + " " + e.RateLimit.Status
		if reset := e.RateLimit.ResetTime(); reset != nil {
			line += " resets " + reset.Format("2006-01-02 15:04 UTC")
		}
		return line
	case KindError:
		return "[error] " + e.Text
	default:
		return e.Text
	}
}
