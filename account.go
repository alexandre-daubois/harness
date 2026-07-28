package harness

import (
	"strings"
	"time"
)

// AccountError reports a provider-level account problem for which immediately
// retrying the command is unlikely to help.
type AccountError struct {
	Detail  string
	ResetAt *time.Time
}

func (e *AccountError) Error() string {
	if e.Detail == "" {
		return "model API account unavailable"
	}
	return "model API account unavailable: " + e.Detail
}

var transientLimitPhrases = []string{
	"usage limit",
	"session limit",
	"plan limit",
	"rate limit",
	"rate_limit",
	"too many requests",
	"quota exceeded",
	"credit balance",
	"429",
}

var accessRevokedPhrases = []string{
	"disabled claude subscription access",
	"use an anthropic api key instead",
	"ask your admin to enable access",
	"access has been revoked",
}

func matchAccountPhrase(s string, lists ...[]string) string {
	text := strings.TrimSpace(s)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	for _, list := range lists {
		for _, phrase := range list {
			if strings.Contains(lower, phrase) {
				return text
			}
		}
	}
	return ""
}

func claudeAccountErrorText(s string) string {
	return matchAccountPhrase(s, transientLimitPhrases, accessRevokedPhrases)
}

func accountErrorAccessRevoked(s string) bool {
	lower := strings.ToLower(s)
	for _, phrase := range accessRevokedPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// AccountErrorResumable reports whether an account error describes a
// transient limit rather than revoked access.
func AccountErrorResumable(s string) bool {
	if accountErrorAccessRevoked(s) {
		return false
	}
	lower := strings.ToLower(s)
	for _, phrase := range transientLimitPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// PreferAccountErrorText keeps the first account error unless a later message
// identifies revoked access.
func PreferAccountErrorText(current, candidate string) string {
	switch {
	case candidate == "":
		return current
	case current == "":
		return candidate
	case accountErrorAccessRevoked(candidate) && !accountErrorAccessRevoked(current):
		return candidate
	default:
		return current
	}
}

// PreferRateLimitReset returns the rejected rate limit with the later reset.
func PreferRateLimitReset(current, candidate *RateLimitInfo) *RateLimitInfo {
	if candidate == nil || !candidate.Rejected() || candidate.ResetTime() == nil {
		return current
	}
	if current == nil {
		return candidate
	}
	currentReset := current.ResetTime()
	candidateReset := candidate.ResetTime()
	if currentReset == nil || candidateReset.After(*currentReset) {
		return candidate
	}
	return current
}

// ResumableReset returns a rejected limit's reset time when the associated
// account error is transient.
func ResumableReset(errText string, limit *RateLimitInfo) *time.Time {
	if !AccountErrorResumable(errText) || limit == nil || !limit.Rejected() {
		return nil
	}
	return limit.ResetTime()
}
