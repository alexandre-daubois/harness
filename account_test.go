package harness

import (
	"testing"
	"time"
)

func TestAccountHelpers(t *testing.T) {
	t.Parallel()

	transient := "rate limit reached"
	revoked := "Disabled Claude subscription access. Ask your admin to enable access."
	if !AccountErrorResumable(transient) {
		t.Fatal("transient error is not resumable")
	}
	if AccountErrorResumable(revoked) {
		t.Fatal("revoked error is resumable")
	}
	if got := PreferAccountErrorText(transient, revoked); got != revoked {
		t.Errorf("PreferAccountErrorText() = %q", got)
	}

	first := &RateLimitInfo{Status: "rejected", ResetsAt: 100}
	second := &RateLimitInfo{Status: "rejected", ResetsAt: 200}
	if got := PreferRateLimitReset(first, second); got != second {
		t.Errorf("PreferRateLimitReset() = %p, want %p", got, second)
	}
	want := time.Unix(200, 0).UTC()
	if got := ResumableReset(transient, second); got == nil || !got.Equal(want) {
		t.Errorf("ResumableReset() = %v, want %v", got, want)
	}
}
