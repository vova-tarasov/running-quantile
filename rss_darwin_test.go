//go:build stress

package main

import (
	"os"
	"syscall"
)

// watchPeakRSS returns a function that, called once the started process p has been waited for, returns its peak
// resident set size from its ProcessState ps. On darwin Maxrss is in bytes.
func watchPeakRSS(p *os.Process) func(ps *os.ProcessState) (bytes int64, ok bool) {
	return func(ps *os.ProcessState) (int64, bool) {
		return ps.SysUsage().(*syscall.Rusage).Maxrss, true
	}
}
