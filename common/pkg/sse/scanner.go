package sse

import (
	"bufio"
	"bytes"
	"io"
)

// NewScanner creates a new scanner for server sent events.
func NewScanner(r io.Reader) *Scanner {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), 4096)
	s.Split(split)
	return &Scanner{
		Scanner: s,
	}
}

// Scanner is a scanner for server sent events.
type Scanner struct {
	*bufio.Scanner
}

// split tokenizes the input. The arguments are an initial substring of the remaining unprocessed data and a flag,
// atEOF, that reports whether the Reader has no more data to give.
// The return values are the number of bytes to advance the input and the next token to return to the user,
// if any, plus an error, if any.
func split(data []byte, atEOF bool) (int, []byte, error) {
	if len(data) == 0 {
		return 0, nil, nil
	}

	// Find a double newline.
	delims := [][]byte{
		[]byte("\r\r"),
		[]byte("\n\n"),
		[]byte("\r\n\r\n"),
		[]byte("\n\n\n\n"),
	}
	pos := -1
	var dlen int
	for _, d := range delims {
		n := bytes.Index(data, d)
		if pos < 0 || (n >= 0 && n < pos) {
			pos = n
			dlen = len(d)
		}
	}

	if pos >= 0 {
		return pos + dlen, data[0:pos], nil
	}

	return len(data), data, nil
}
