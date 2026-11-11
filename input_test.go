package main

import (
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// countingReader counts the bytes read from the wrapped reader.
type countingReader struct {
	r io.Reader
	n int
}

// Read reads from the wrapped reader and counts the bytes.
func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

// failingReader returns its data and then the given error.
type failingReader struct {
	data string
	err  error
}

// Read returns the remaining data, then the error.
func (f *failingReader) Read(p []byte) (int, error) {
	if f.data == "" {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// repeatReader yields the byte b forever.
type repeatReader struct{ b byte }

// Read fills p with the repeated byte.
func (r repeatReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.b
	}
	return len(p), nil
}

// TestReadPath checks the accepted first-line spellings.
func TestReadPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/a/b c.jsonl\n", "/a/b c.jsonl"},
		{"/a\r\n", "/a"},
		{"/a", "/a"},
		{"/a\nextra\n", "/a"},
		{" /a \n", " /a "},
		{strings.Repeat("x", maxPathBytes) + "\n", strings.Repeat("x", maxPathBytes)},
		{strings.Repeat("x", maxPathBytes) + "\r\n", strings.Repeat("x", maxPathBytes)},
		{strings.Repeat("x", maxPathBytes), strings.Repeat("x", maxPathBytes)},
	}
	for _, tc := range tests {
		got, err := readPath(strings.NewReader(tc.in))
		if err != nil || got != tc.want {
			t.Errorf("readPath(%.20q) = %.20q, %v; want %.20q", tc.in, got, err, tc.want)
		}
	}
}

// TestReadPathErrors checks the error cases and the read limit.
func TestReadPathErrors(t *testing.T) {
	const tooLong = "first line of stdin is longer than 4096 bytes; expected a file path"
	for _, in := range []string{"", "\n", "\r\n"} {
		if _, err := readPath(strings.NewReader(in)); err == nil || err.Error() != "expected a file path on stdin" {
			t.Errorf("readPath(%q) error = %v", in, err)
		}
	}
	cr := &countingReader{r: strings.NewReader(strings.Repeat("x", 1<<20))}
	if _, err := readPath(cr); err == nil || err.Error() != tooLong {
		t.Errorf("long input error = %v", err)
	}
	if cr.n > maxPathBytes+2 {
		t.Errorf("read %d bytes, limit %d", cr.n, maxPathBytes+2)
	}
	if _, err := readPath(strings.NewReader(strings.Repeat("x", maxPathBytes+1) + "\n")); err == nil || err.Error() != tooLong {
		t.Errorf("4097-byte line error = %v", err)
	}
	boom := errors.New("boom")
	if _, err := readPath(&failingReader{err: boom}); !errors.Is(err, boom) {
		t.Errorf("read error = %v, want wrapped boom", err)
	}
}

// TestReadPathReturnsAtFirstNewline checks that readPath returns as soon as the first line is complete, without
// waiting for the end of stdin, which an interactive or slow writer may send much later or never.
func TestReadPathReturnsAtFirstNewline(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()
	go pw.Write([]byte("/some/path.jsonl\n"))
	var path string
	var err error
	done := make(chan struct{})
	go func() {
		path, err = readPath(pr)
		close(done)
	}()
	select {
	case <-done:
		if err != nil || path != "/some/path.jsonl" {
			t.Fatalf("readPath = %q, %v; want \"/some/path.jsonl\"", path, err)
		}
	case <-time.After(time.Second):
		t.Fatal("readPath did not return within 1 s of a complete first line; it waits for the end of stdin")
	}
}

// TestLineReaderSplitsLines checks line splitting and CR removal.
func TestLineReaderSplitsLines(t *testing.T) {
	lr := newLineReader(strings.NewReader("a\r\nb\n\nc"), 16, 64)
	for _, want := range []string{"a", "b", "", "c"} {
		got, err := lr.next()
		if err != nil || string(got) != want {
			t.Fatalf("next() = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := lr.next(); err != io.EOF {
		t.Fatalf("final next() error = %v, want io.EOF", err)
	}
}

// TestLineReaderLongLines checks lines longer than the buffer and the maxLine limit.
func TestLineReaderLongLines(t *testing.T) {
	l40, l64, l65 := strings.Repeat("a", 40), strings.Repeat("b", 64), strings.Repeat("c", 65)
	in := l40 + "\n" + l64 + "\r\n" + l65 + "\nnext\n" + l65 + "\r\n" + strings.Repeat("d", 200)
	lr := newLineReader(strings.NewReader(in), 16, 64)
	for _, want := range []string{l40, l64} {
		got, err := lr.next()
		if err != nil || string(got) != want {
			t.Fatalf("next() = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := lr.next(); err != errLineTooLong {
		t.Fatalf("65-byte line error = %v", err)
	}
	if got, err := lr.next(); err != nil || string(got) != "next" {
		t.Fatalf("after long line next() = %q, %v", got, err)
	}
	if _, err := lr.next(); err != errLineTooLong {
		t.Fatalf("65-byte CRLF line error = %v", err)
	}
	if _, err := lr.next(); err != errLineTooLong {
		t.Fatalf("too-long last line error = %v", err)
	}
	if _, err := lr.next(); err != io.EOF {
		t.Fatalf("after too-long last line error = %v, want io.EOF", err)
	}
}

// TestLineReaderLongLineAtEOF checks a long line without newline that fits maxLine.
func TestLineReaderLongLineAtEOF(t *testing.T) {
	l := strings.Repeat("a", 50)
	lr := newLineReader(strings.NewReader(l), 16, 64)
	if got, err := lr.next(); err != nil || string(got) != l {
		t.Fatalf("next() = %q, %v", got, err)
	}
	if _, err := lr.next(); err != io.EOF {
		t.Fatalf("error = %v, want io.EOF", err)
	}
}

// TestLineReaderTooLongLineIsNotBuffered checks that a 32 MiB line is skipped without allocating.
func TestLineReaderTooLongLineIsNotBuffered(t *testing.T) {
	const size = 32 << 20
	in := io.MultiReader(io.LimitReader(repeatReader{'a'}, size), strings.NewReader("\nx\n"))
	lr := newLineReader(in, 4<<10, 64<<10)
	var err1, err2 error
	var line []byte
	got := allocatedBytes(func() {
		_, err1 = lr.next()
		line, err2 = lr.next()
	})
	if err1 != errLineTooLong || err2 != nil || string(line) != "x" {
		t.Fatalf("got %v, %q, %v", err1, line, err2)
	}
	if got > 256<<10 {
		t.Fatalf("allocated %d bytes, limit %d", got, 256<<10)
	}
}

// TestLineReaderPropagatesReadError checks that read errors are returned as-is.
func TestLineReaderPropagatesReadError(t *testing.T) {
	boom := errors.New("boom")
	lr := newLineReader(&failingReader{data: "abc", err: boom}, 16, 64)
	if _, err := lr.next(); err != boom {
		t.Fatalf("short line error = %v, want boom", err)
	}
	lr = newLineReader(&failingReader{data: strings.Repeat("a", 100), err: boom}, 16, 64)
	if _, err := lr.next(); err != boom {
		t.Fatalf("long line error = %v, want boom", err)
	}
}

// TestLineReaderDoesNotAllocatePerLine checks that short lines are returned without allocating.
func TestLineReaderDoesNotAllocatePerLine(t *testing.T) {
	const n = 1000
	lr := newLineReader(strings.NewReader(strings.Repeat("{\"v\":1}\n", 2*n)), 4096, 1<<20)
	allocs := testing.AllocsPerRun(1, func() {
		for range n {
			if _, err := lr.next(); err != nil {
				t.Fatal(err)
			}
		}
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}
