//go:build stress

package main

import (
	"os"
	"syscall"
)

// peakRSSBytes returns the peak resident set size of the finished process ps. On linux Maxrss is in KiB.
func peakRSSBytes(ps *os.ProcessState) (bytes int64, ok bool) {
	return ps.SysUsage().(*syscall.Rusage).Maxrss * 1024, true
}
