package modelproxy

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"

	"github.com/alpha-omega-security/harness"
)

// Usage counts billed requests and their tokens. InputTokens includes the cache reads and writes, which
// harness.CostFromUsage prices at their own rate.
type Usage struct {
	harness.Usage
	Requests int
}

func (u Usage) plus(o Usage) Usage {
	return Usage{
		Usage: harness.Usage{
			InputTokens:      u.InputTokens + o.InputTokens,
			OutputTokens:     u.OutputTokens + o.OutputTokens,
			CacheReadTokens:  u.CacheReadTokens + o.CacheReadTokens,
			CacheWriteTokens: u.CacheWriteTokens + o.CacheWriteTokens,
		},
		Requests: u.Requests + o.Requests,
	}
}

// usageReader reads the tokens a model response reports, as one JSON body or as server-sent events, while the
// response is relayed. Streamed counters are cumulative, so each one keeps its highest value.
type usageReader struct {
	io.ReadCloser
	stream bool
	buf    []byte
	tokens Usage
	done   func(Usage)
	once   sync.Once
}

func (u *usageReader) Read(b []byte) (int, error) {
	n, err := u.ReadCloser.Read(b)
	u.buf = append(u.buf, b[:n]...)
	for u.stream {
		line, rest, ok := bytes.Cut(u.buf, []byte("\n"))
		if !ok {
			break
		}
		u.observe(line)
		u.buf = rest
	}
	// The usage of a response larger than any model answer is lost rather than buffered.
	if len(u.buf) > maxBody {
		u.buf = nil
	}
	if err != nil {
		u.finish()
	}

	return n, err
}

func (u *usageReader) Close() error {
	u.finish()

	return u.ReadCloser.Close()
}

func (u *usageReader) finish() {
	u.once.Do(func() {
		u.observe(u.buf)
		u.tokens.Requests = 1
		u.done(u.tokens)
	})
}

type reported struct {
	InputTokens   int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	OutputTokens  int `json:"output_tokens"`
	InputDetails  struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"input_tokens_details"`
}

// observe looks where Anthropic and the Responses API put the usage: at the top of a message or of a
// message_delta event, under message in message_start and under response in response.completed.
func (u *usageReader) observe(data []byte) {
	if !bytes.Contains(data, []byte(`"usage"`)) {
		return
	}
	data = bytes.TrimPrefix(bytes.TrimSpace(data), []byte("data:"))
	var event struct {
		Usage   *reported `json:"usage"`
		Message struct {
			Usage *reported `json:"usage"`
		} `json:"message"`
		Response struct {
			Usage *reported `json:"usage"`
		} `json:"response"`
	}
	if json.Unmarshal(data, &event) != nil {
		return
	}
	for _, r := range []*reported{event.Usage, event.Message.Usage, event.Response.Usage} {
		if r == nil {
			continue
		}
		// Anthropic leaves cached tokens out of input_tokens, OpenAI counts them in and details the cache reads.
		u.tokens.InputTokens = max(u.tokens.InputTokens, r.InputTokens+r.CacheCreation+r.CacheRead)
		u.tokens.CacheReadTokens = max(u.tokens.CacheReadTokens, r.CacheRead+r.InputDetails.CachedTokens)
		u.tokens.CacheWriteTokens = max(u.tokens.CacheWriteTokens, r.CacheCreation)
		u.tokens.OutputTokens = max(u.tokens.OutputTokens, r.OutputTokens)
	}
}
