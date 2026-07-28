package harness

import (
	"bufio"
	"io"
)

// scanJSONL reads complete logical lines without bufio.Scanner's token limit.
// Agent output can contain very large thinking blocks or tool results; losing
// the remainder would also lose terminal usage, session, and error events.
// Non-EOF read failures become one error event.
func scanJSONL(r io.Reader, emit func(Event), parse func([]byte, func(Event))) {
	br := bufio.NewReader(r)
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			parse(raw, emit)
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			emit(Event{Kind: KindError, Text: "stream read: " + err.Error()})
			return
		}
	}
}
