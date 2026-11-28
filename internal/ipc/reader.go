package ipc

import (
	"bufio"
	"errors"
	"io"
)

// Reader reads NDJSON frames from Host stdin with a hard frame size limit.
type Reader struct {
	scanner *bufio.Scanner
}

// NewReader builds a Reader. maxFrameBytes bounds a single frame, including
// the trailing newline.
func NewReader(r io.Reader, maxFrameBytes int) *Reader {
	if maxFrameBytes <= 0 {
		maxFrameBytes = 8 << 20
	}
	initial := 64 * 1024
	if maxFrameBytes < initial {
		initial = maxFrameBytes
	}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, initial), maxFrameBytes)
	return &Reader{scanner: scanner}
}

// Next returns the next frame, io.EOF at a clean end of stream, or a protocol
// error for malformed or oversized input.
func (r *Reader) Next() (Frame, error) {
	if !r.scanner.Scan() {
		if err := r.scanner.Err(); err != nil {
			if errors.Is(err, bufio.ErrTooLong) {
				return Frame{}, &Error{Code: CodeFrameTooLarge}
			}
			return Frame{}, &Error{Code: CodeFrameReadFailed, Err: err}
		}
		return Frame{}, io.EOF
	}
	return Parse(r.scanner.Bytes())
}
