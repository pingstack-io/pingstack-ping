// Package tail keeps the last lines a job printed, for failure reports.
package tail

import (
	"bytes"
	"sync"
)

// Buffer is an io.Writer that keeps at most maxLines complete lines (plus any
// unfinished one) and at most maxBytes in total, dropping the oldest first.
// It is safe to share between a job's stdout and stderr.
type Buffer struct {
	mu       sync.Mutex
	lines    [][]byte
	partial  []byte
	maxLines int
	maxBytes int
}

func New(maxLines, maxBytes int) *Buffer {
	return &Buffer{maxLines: maxLines, maxBytes: maxBytes}
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.maxLines <= 0 {
		return len(p), nil
	}
	data := p
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			b.partial = append(b.partial, data...)
			if len(b.partial) > b.maxBytes {
				b.partial = b.partial[len(b.partial)-b.maxBytes:]
			}
			break
		}
		line := append(b.partial, data[:i+1]...)
		b.partial = nil
		if len(line) > b.maxBytes {
			line = line[len(line)-b.maxBytes:]
		}
		b.lines = append(b.lines, line)
		if len(b.lines) > b.maxLines {
			b.lines = b.lines[len(b.lines)-b.maxLines:]
		}
		data = data[i+1:]
	}
	return len(p), nil
}

// String returns the kept output, trimmed from the front to maxBytes.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := bytes.Join(append(append([][]byte{}, b.lines...), b.partial), nil)
	if len(out) > b.maxBytes {
		out = out[len(out)-b.maxBytes:]
		if i := bytes.IndexByte(out, '\n'); i >= 0 && i < len(out)-1 {
			out = out[i+1:]
		}
	}
	return string(out)
}
