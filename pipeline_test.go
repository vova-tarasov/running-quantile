package main

import (
	"bufio"
	"bytes"
	"errors"
	"strconv"
	"strings"
	"testing"
)

// failingWriter is a writer whose every write fails with err.
type failingWriter struct{ err error }

// Write writes nothing and returns w.err.
func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// TestProcessReadErrorMidFile checks that a read error stops process with the number of the line being read,
// after the records before it were written.
func TestProcessReadErrorMidFile(t *testing.T) {
	boom := errors.New("boom")
	var stdout bytes.Buffer
	var stderr strings.Builder
	out := bufio.NewWriter(&stdout)
	skipped, err := process(&failingReader{data: "{\"v\":1}\n{\"v\":3}\n", err: boom}, 0.5, "v", out, &stderr)
	if err := out.Flush(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(err, boom) || err.Error() != "reading line 3: boom" {
		t.Errorf("error = %v, want reading line 3: boom", err)
	}
	if skipped != 0 || stdout.String() != "1\n2\n" || stderr.Len() != 0 {
		t.Errorf("got skipped %d, stdout %q, stderr %q; want 0, \"1\\n2\\n\", \"\"", skipped, stdout.String(), stderr.String())
	}
}

// TestProcessStopsOnWriteError checks that process returns the first write error without reading on: of a ~16 MB
// input it reads less than 8 MiB, since about 100k records fill the 1 MiB output buffer before the first write.
func TestProcessStopsOnWriteError(t *testing.T) {
	var in []byte
	for i := range 1_000_000 {
		in = append(in, `{"v":`...)
		in = strconv.AppendFloat(in, 1000000.5+float64(i), 'f', -1, 64)
		in = append(in, "}\n"...)
	}
	boom := errors.New("boom")
	cr := &countingReader{r: bytes.NewReader(in)}
	var stderr strings.Builder
	_, err := process(cr, 1, "v", bufio.NewWriterSize(failingWriter{boom}, writeBufSize), &stderr)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want boom", err)
	}
	if cr.n >= 8<<20 {
		t.Fatalf("read %d of %d input bytes after the write error, want < %d", cr.n, len(in), 8<<20)
	}
}
