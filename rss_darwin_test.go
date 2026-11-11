//go:build stress

package main

import (
	"os"
	"syscall"
)

// peakRSSBytes returns the peak resident set size of the finished process ps. On darwin Maxrss is in bytes.
func peakRSSBytes(ps *os.ProcessState) (bytes int64, ok bool) {
	return ps.SysUsage().(*syscall.Rusage).Maxrss, true
}
