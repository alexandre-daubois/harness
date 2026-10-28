package modelproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alpha-omega-security/harness"
)

func spent(requests, input, output, cacheRead, cacheWrite int) Usage {
	return Usage{Usage: harness.Usage{InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite}, Requests: requests}
}

// spend relays one request to an upstream answering with body, and returns what the run spent once revoked.
func spend(t *testing.T, p Provider, route string, status int, contentType, body string) Usage {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(up.Close)
	p.Upstream = up.URL
	mp, err := New(p, "real-key", "allowed", Options{})
	if err != nil {
		t.Fatal(err)
	}
	token, revoke := mp.Issue(t.Context())
	srv := httptest.NewServer(mp)
	t.Cleanup(srv.Close)

	resp := call(t, srv, http.MethodPost, PathPrefix+route, withKey("Authorization", "Bearer "+token))
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()

	return revoke()
}

func TestUsageOfAResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		provider    Provider
		route       string
		contentType string
		body        string
		want        Usage
	}{
		"anthropic message": {Anthropic, "/v1/messages", "application/json",
			`{"type":"message","usage":{"input_tokens":10,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":5}}`,
			spent(1, 60, 5, 30, 20)},
		"anthropic stream": {Anthropic, "/v1/messages", "text/event-stream; charset=utf-8",
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":30,\"output_tokens\":1}}}\n\n" +
				"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"usage\"}}\n\n" +
				"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":42}}\n\n",
			spent(1, 40, 42, 30, 0)},
		"responses stream": {OpenAI, "/v1/responses", "text/event-stream",
			"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"usage\":null}}\n\n" +
				"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":100,\"output_tokens\":7}}}\n\n",
			spent(1, 100, 7, 0, 0)},
		"responses cached input": {OpenAI, "/v1/responses", "application/json",
			`{"usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":80},"output_tokens":7}}`,
			spent(1, 100, 7, 80, 0)},
		"stream without usage": {OpenAI, "/v1/responses", "text/event-stream",
			"data: not json\n\ndata: {\"usage\":\"many\"}\n\ndata: [DONE]\n\n",
			spent(1, 0, 0, 0, 0)},
		"token count": {Anthropic, "/v1/messages/count_tokens", "application/json", `{"input_tokens":10}`, Usage{}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := spend(t, tc.provider, tc.route, http.StatusOK, tc.contentType, tc.body); got != tc.want {
				t.Errorf("usage = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestUsageReaderKeepsAnUnterminatedLastEvent(t *testing.T) {
	var got Usage
	r := &usageReader{
		ReadCloser: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":3,\"output_tokens\":2}}}")),
		stream:     true,
		done:       func(u Usage) { got = u },
	}

	_, _ = io.Copy(io.Discard, r)

	if want := spent(1, 3, 2, 0, 0); got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestUsageReaderCountsAStreamClosedEarly(t *testing.T) {
	var calls []Usage
	body := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial"
	r := &usageReader{
		ReadCloser: io.NopCloser(strings.NewReader(body)),
		stream:     true,
		done:       func(u Usage) { calls = append(calls, u) },
	}

	_, _ = io.ReadFull(r, make([]byte, len(body)-5))
	_ = r.Close()
	_ = r.Close()

	if want := []Usage{spent(1, 10, 1, 0, 0)}; len(calls) != 1 || calls[0] != want[0] {
		t.Errorf("usage = %+v, want %+v", calls, want)
	}
}

func TestUsageSkipsFailedRequests(t *testing.T) {
	if got := spend(t, Anthropic, "/v1/messages", http.StatusTooManyRequests, "application/json", `{"usage":{"input_tokens":10,"output_tokens":5}}`); got != (Usage{}) {
		t.Errorf("a failed request was counted: %+v", got)
	}
}

func TestUsageIsCountedPerToken(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"usage":{"input_tokens":10,"output_tokens":5}}`)
	}))
	t.Cleanup(up.Close)
	p := Anthropic
	p.Upstream = up.URL
	mp, err := New(p, "real-key", "allowed", Options{})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mp)
	t.Cleanup(srv.Close)
	first, revokeFirst := mp.Issue(t.Context())
	_, revokeSecond := mp.Issue(t.Context())

	for range 2 {
		resp := call(t, srv, http.MethodPost, PathPrefix+"/v1/messages", withKey("X-Api-Key", first))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	if got := revokeSecond(); got != (Usage{}) {
		t.Errorf("the second run was charged: %+v", got)
	}
	if got, want := revokeFirst(), spent(2, 20, 10, 0, 0); got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
	if got := revokeFirst(); got != (Usage{}) {
		t.Errorf("a revoked token still reports usage: %+v", got)
	}
}
