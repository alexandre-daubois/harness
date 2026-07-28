package skills

import "testing"

func TestParseRepoSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw     string
		wantURL string
		wantRef string
	}{
		{
			raw:     "owner/repo",
			wantURL: "https://github.com/owner/repo",
		},
		{
			raw:     "owner/repo@v1.2.3",
			wantURL: "https://github.com/owner/repo",
			wantRef: "v1.2.3",
		},
		{
			raw:     "https://git.example.test/team/repo@main",
			wantURL: "https://git.example.test/team/repo",
			wantRef: "main",
		},
		{
			raw:     "https://token@git.example.test/team/repo",
			wantURL: "https://token@git.example.test/team/repo",
		},
	}
	for _, test := range tests {
		url, ref, err := ParseRepoSpec(test.raw)
		if err != nil {
			t.Errorf("ParseRepoSpec(%q): %v", test.raw, err)
			continue
		}
		if url != test.wantURL || ref != test.wantRef {
			t.Errorf("ParseRepoSpec(%q) = %q, %q", test.raw, url, ref)
		}
	}
	for _, raw := range []string{"", "owner", "ssh://git.example.test/repo"} {
		if _, _, err := ParseRepoSpec(raw); err == nil {
			t.Errorf("ParseRepoSpec(%q) succeeded", raw)
		}
	}
}
