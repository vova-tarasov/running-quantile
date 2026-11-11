//go:build stress && !darwin && !linux

package main

import "os"

// peakRSSBytes reports that the peak resident set size is not available on this OS.
func peakRSSBytes(ps *os.ProcessState) (bytes int64, ok bool) {
	return 0, false
}
