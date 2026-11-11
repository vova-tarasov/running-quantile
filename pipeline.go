package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
)

const (
	maxWarnings  = 10       // skipped records reported one by one on stderr
	readBufSize  = 1 << 20  // input buffer; lines that fit in it are never copied
	writeBufSize = 1 << 20  // output buffer
	maxLineBytes = 64 << 20 // longest line content accepted; longer lines are skipped
)

// utf8BOM is the byte order mark that is ignored at the start of the first line.
var utf8BOM = []byte("\xef\xbb\xbf")

// process reads JSON Lines from in and, after every valid record, writes the q-quantile of field over the valid
// records so far to out. Blank lines are ignored. Invalid records are counted in skipped, and the first maxWarnings
// get a warning on stderr. It stops at the first read or write error and returns it. It does not flush out.
func process(in io.Reader, q float64, field string, out *bufio.Writer, stderr io.Writer) (skipped int, err error) {
	lines := newLineReader(in, readBufSize, maxLineBytes)
	t := newTracker(q)
	for n := 1; ; n++ {
		line, err := lines.next()
		var v float64
		var r reason
		switch err {
		case nil:
			if n == 1 {
				line = bytes.TrimPrefix(line, utf8BOM)
			}
			if len(bytes.TrimLeft(line, " \t\r")) == 0 {
				continue
			}
			v, r = extractNumber(line, field)
		case errLineTooLong:
			r = lineTooLong
		case io.EOF:
			return skipped, nil
		default:
			return skipped, fmt.Errorf("reading line %d: %w", n, err)
		}
		if r != valid {
			skipped++
			if skipped <= maxWarnings {
				fmt.Fprintf(stderr, "line %d: %s\n", n, r.describe(field))
			}
			continue
		}
		t.Add(v)
		// A formatted float64 and its newline take at most 26 bytes, so with 32 available the append below never
		// reallocates. A failed Flush makes out keep its error, and the Write below returns it.
		if out.Available() < 32 {
			out.Flush()
		}
		if _, err := out.Write(append(appendFloat(out.AvailableBuffer(), t.Quantile()), '\n')); err != nil {
			return skipped, err
		}
	}
}
