//go:build stress

package main

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"time"
)

// watchPeakRSS starts polling VmHWM, the peak resident set size, in /proc/<pid>/status of the started process p
// every 10 ms. The returned function, called once p has been waited for, stops polling and returns the largest
// value read, so growth in the last 10 ms is missed.
//
// Maxrss can't be used on linux: os/exec starts the child with vfork, so until exec it shares the parent's memory,
// and the kernel keeps the parent's peak RSS (gigabytes in the stress test) as the child's Maxrss.
func watchPeakRSS(p *os.Process) func(ps *os.ProcessState) (bytes int64, ok bool) {
	path := fmt.Sprintf("/proc/%d/status", p.Pid)
	stop, done := make(chan struct{}), make(chan int64)
	go func() {
		var peak int64
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			peak = max(peak, readVmHWM(path))
			select {
			case <-stop:
				done <- peak
				return
			case <-tick.C:
			}
		}
	}()
	return func(*os.ProcessState) (int64, bool) {
		close(stop)
		peak := <-done
		return peak, peak > 0
	}
}

// readVmHWM returns the VmHWM value in the /proc status file at path, in bytes, or 0 if it can't be read: the
// process has exited, for example.
func readVmHWM(path string) int64 {
	status, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	_, rest, ok := bytes.Cut(status, []byte("\nVmHWM:"))
	if !ok {
		return 0
	}
	line, _, _ := bytes.Cut(rest, []byte("\n")) // "\t    7680 kB"
	fields := bytes.Fields(line)
	if len(fields) == 0 {
		return 0
	}
	kib, err := strconv.ParseInt(string(fields[0]), 10, 64)
	if err != nil {
		return 0
	}
	return kib << 10
}
