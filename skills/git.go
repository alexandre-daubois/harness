package skills

import (
	"fmt"
	"strings"
)

// ParseRepoSpec splits an HTTPS repository specification into its clone URL
// and optional ref.
//
// Accepted forms are owner/repo[@ref] and https://host/path/repo[@ref].
func ParseRepoSpec(raw string) (url, ref string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("skills: empty repository spec")
	}
	if index := strings.Index(raw, "://"); index >= 0 {
		scheme := raw[:index+3]
		rest := raw[len(scheme):]
		if at := strings.LastIndex(rest, "@"); at > strings.LastIndex(rest, "/") {
			ref = rest[at+1:]
			rest = rest[:at]
		}
		url = scheme + rest
	} else {
		if at := strings.Index(raw, "@"); at >= 0 {
			ref = raw[at+1:]
			raw = raw[:at]
		}
		parts := strings.Split(raw, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("skills: expected owner/repo or https URL, got %q", raw)
		}
		url = "https://github.com/" + raw
	}
	if !strings.HasPrefix(url, "https://") {
		return "", "", fmt.Errorf("skills: repository must use https://, got %q", url)
	}
	return url, ref, nil
}
