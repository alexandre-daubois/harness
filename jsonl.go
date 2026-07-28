package harness

import (
	"bufio"
	"io"
)

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
