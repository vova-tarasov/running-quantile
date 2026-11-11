//go:build stress && !darwin && !linux

package main

import "os"

// watchPeakRSS returns a function that reports that the peak resident set size is not available on this OS.
func watchPeakRSS(p *os.Process) func(ps *os.ProcessState) (bytes int64, ok bool) {
	return func(*os.ProcessState) (int64, bool) { return 0, false }
}
