package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// binaryPath is the running-quantile binary that TestMain builds for the binary tests.
var binaryPath string

// TestMain builds the binary once into a temporary directory, runs the tests, and removes the directory.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "running-quantile-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(dir, "running-quantile")
	if out, err := exec.Command("go", "build", "-o", binaryPath, ".").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "go build: %v\n%s", err, out)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// runBinary runs the built binary with args and stdin, and returns what it produced.
func runBinary(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var exitErr *exec.ExitError
	if err := cmd.Run(); err != nil && !errors.As(err, &exitErr) {
		t.Fatal(err)
	}
	return result{cmd.ProcessState.ExitCode(), stdout.String(), stderr.String()}
}

// TestBinarySpecExample checks the example from the task PDF through the real binary and its real stdin.
func TestBinarySpecExample(t *testing.T) {
	got := runBinary(t, writeTemp(t, specExample)+"\n", "0.75", "latency_ms")
	if want := (result{0, specOutput, ""}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestBinaryExitCodes checks the exit codes of the real binary for a bad argument and a missing file.
func TestBinaryExitCodes(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	for _, c := range []struct {
		name, stdin string
		args        []string
		code        int
	}{
		{"bad q", missing + "\n", []string{"1.5", "v"}, 2},
		{"missing file", missing + "\n", []string{"0.5", "v"}, 1},
	} {
		if got := runBinary(t, c.stdin, c.args...); got.code != c.code || got.stdout != "" || !strings.HasPrefix(got.stderr, "error: ") {
			t.Errorf("%s: got %+v, want exit code %d and an error", c.name, got, c.code)
		}
	}
}

// TestBinaryStopsWhenStdoutClosed checks that the binary stops early when the reader of its stdout goes away. Its
// ~24 MB of output is far more than a pipe holds, so once the test has read one line and closed the read end, the
// binary cannot finish: it must die of SIGPIPE on a write to the closed pipe, as Go programs do on macOS and Linux,
// within 10 s. TestProcessStopsOnWriteError shows that process stops reading at the first write error.
func TestBinaryStopsWhenStdoutClosed(t *testing.T) {
	var in []byte
	for i := range 3_000_000 {
		in = append(in, `{"v":`...)
		in = strconv.AppendInt(in, int64(i), 10)
		in = append(in, "}\n"...)
	}
	cmd := exec.Command(binaryPath, "1", "v")
	cmd.Stdin = strings.NewReader(writeTemp(t, string(in)) + "\n")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	stdout.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		cmd.Process.Kill()
		<-done
		t.Fatal("running-quantile did not exit within 10 s after its stdout was closed")
	}
	if ws := cmd.ProcessState.Sys().(syscall.WaitStatus); !ws.Signaled() || ws.Signal() != syscall.SIGPIPE {
		t.Fatalf("running-quantile ended with %q, want it killed by SIGPIPE", cmd.ProcessState)
	}
}
