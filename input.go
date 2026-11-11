package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// maxPathBytes is the longest accepted file path on the first line of stdin.
const maxPathBytes = 4096

// errLineTooLong reports a line whose content exceeds the line limit.
var errLineTooLong = errors.New("line too long")

// readPath returns the first line of stdin, without its line ending, reading at most maxPathBytes+2 bytes.
func readPath(stdin io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(stdin, maxPathBytes+2))
	if err != nil {
		return "", fmt.Errorf("reading file path from stdin: %w", err)
	}
	line := data
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		line = bytes.TrimSuffix(data[:i], []byte("\r"))
	}
	if len(line) > maxPathBytes {
		return "", errors.New("first line of stdin is longer than 4096 bytes; expected a file path")
	}
	if len(line) == 0 {
		return "", errors.New("expected a file path on stdin")
	}
	return string(line), nil
}

// lineReader splits a stream into lines without buffering lines over maxLine.
type lineReader struct {
	br      *bufio.Reader
	maxLine int
	long    []byte
	tooLong bool
}

// newLineReader returns a lineReader with a bufSize read buffer and a maxLine content limit.
func newLineReader(r io.Reader, bufSize, maxLine int) *lineReader {
	return &lineReader{br: bufio.NewReaderSize(r, bufSize), maxLine: maxLine}
}

// next returns the next line without its line ending; the slice is valid until the next call.
func (lr *lineReader) next() ([]byte, error) {
	line, err := lr.br.ReadSlice('\n')
	if err != bufio.ErrBufferFull {
		if err != nil && (err != io.EOF || len(line) == 0) {
			return nil, err
		}
		return lr.trim(line)
	}
	lr.long, lr.tooLong = lr.long[:0], false
	for err == bufio.ErrBufferFull {
		lr.accumulate(line)
		line, err = lr.br.ReadSlice('\n')
	}
	if err != nil && err != io.EOF {
		return nil, err
	}
	lr.accumulate(line)
	if lr.tooLong {
		return nil, errLineTooLong
	}
	return lr.trim(lr.long)
}

// accumulate appends a chunk to the side buffer unless the line is already known to be too long.
func (lr *lineReader) accumulate(chunk []byte) {
	if lr.tooLong {
		return
	}
	if len(lr.long)+len(chunk) > lr.maxLine+2 {
		lr.tooLong = true
		return
	}
	lr.long = append(lr.long, chunk...)
}

// trim removes the line ending from line and enforces the maxLine limit.
func (lr *lineReader) trim(line []byte) ([]byte, error) {
	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))
	if len(line) > lr.maxLine {
		return nil, errLineTooLong
	}
	return line, nil
}
