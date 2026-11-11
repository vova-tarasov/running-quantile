package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// specExample is the JSON Lines example from the task PDF.
const specExample = `{"timestamp": "2026-06-18T10:00:01Z", "customer_id": "acme", "region": "us-east", "endpoint": "/api/v1/query", "latency_ms": 5, "bytes_sent": 8421}
{"timestamp": "2026-06-18T10:00:02Z", "customer_id": "acme", "region": "us-east", "endpoint": "/api/v1/query", "latency_ms": 3, "bytes_sent": 9102}
{"timestamp": "2026-06-18T10:00:03Z", "customer_id": "acme", "region": "us-west", "endpoint": "/api/v1/insert", "latency_ms": 8, "bytes_sent": 8950}
{"timestamp": "2026-06-18T10:00:04Z", "customer_id": "globex", "region": "us-east", "endpoint": "/api/v1/query", "latency_ms": 1, "bytes_sent": 7200}
{"timestamp": "2026-06-18T10:00:05Z", "customer_id": "acme", "region": "us-east", "endpoint": "/api/v1/query", "latency_ms": 9, "bytes_sent": 5600}
{"timestamp": "2026-06-18T10:00:06Z", "customer_id": "globex", "region": "us-west", "endpoint": "/api/v1/insert", "latency_ms": 4, "bytes_sent": 7800}
`

// specOutput is the output the task PDF gives for specExample with q = 0.75 and field latency_ms.
const specOutput = "5\n4.5\n6.5\n5.75\n8\n7.25\n"

// result is what one run of the program produced: its exit code and everything it wrote to stdout and stderr.
type result struct {
	code           int
	stdout, stderr string
}

// runWithStdin calls run with args and the given stdin, and returns what it produced.
func runWithStdin(stdin string, args ...string) result {
	var stdout, stderr strings.Builder
	code := run(args, strings.NewReader(stdin), &stdout, &stderr)
	return result{code, stdout.String(), stderr.String()}
}

// writeTemp writes content to input.jsonl in a new temporary directory and returns the file's path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// runOnContent writes content to a temporary file and calls run with args and the file's path on stdin.
func runOnContent(t *testing.T, content string, args ...string) result {
	t.Helper()
	return runWithStdin(writeTemp(t, content)+"\n", args...)
}

// mustNotRead is a stdin that fails the test if anything reads it.
type mustNotRead struct{ t *testing.T }

// Read fails the test and reports the end of input.
func (r mustNotRead) Read([]byte) (int, error) {
	r.t.Error("stdin was read")
	return 0, io.EOF
}

// TestRunSpecExample checks the example from the task PDF end to end.
func TestRunSpecExample(t *testing.T) {
	got := runOnContent(t, specExample, "0.75", "latency_ms")
	if want := (result{0, specOutput, ""}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// invalidQuantile returns the error message for the rejected quantile argument s.
func invalidQuantile(s string) string {
	return `invalid quantile "` + s + `": must be a number in [0, 1]`
}

// badArgs lists the argument lists that spec §6.2 rejects, with the error message for each.
var badArgs = []struct {
	args []string
	msg  string
}{
	{nil, "expected 2 arguments, got 0"},
	{[]string{"0.5"}, "expected 2 arguments, got 1"},
	{[]string{"0.5", "v", "x"}, "expected 2 arguments, got 3"},
	{[]string{"abc", "v"}, invalidQuantile("abc")},
	{[]string{"", "v"}, invalidQuantile("")},
	{[]string{"1.", "v"}, invalidQuantile("1.")},
	{[]string{"05", "v"}, invalidQuantile("05")},
	{[]string{"1.1", "v"}, invalidQuantile("1.1")},
	{[]string{"-0.1", "v"}, invalidQuantile("-0.1")},
	{[]string{"1e400", "v"}, invalidQuantile("1e400")},
	{[]string{"NaN", "v"}, invalidQuantile("NaN")},
	{[]string{"Inf", "v"}, invalidQuantile("Inf")},
	{[]string{"0x1p-2", "v"}, invalidQuantile("0x1p-2")},
	{[]string{"1_0", "v"}, invalidQuantile("1_0")},
	{[]string{".5", "v"}, invalidQuantile(".5")},
	{[]string{"+0.5", "v"}, invalidQuantile("+0.5")},
	{[]string{" 0.5", "v"}, invalidQuantile(" 0.5")},
	{[]string{"0.5 ", "v"}, invalidQuantile("0.5 ")},
	{[]string{"0.5", ""}, "field name must not be empty"},
}

// TestParseArgs checks the accepted quantile spellings and the error for every argument list spec §6.2 rejects.
func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		args []string
		q    float64
	}{
		{[]string{"0", "v"}, 0},
		{[]string{"1", "v"}, 1},
		{[]string{"-0", "v"}, 0},
		{[]string{"1e-1", "v"}, 0.1},
		{[]string{"0.75", "latency_ms"}, 0.75},
	} {
		q, field, err := parseArgs(c.args)
		if err != nil || q != c.q || math.Signbit(q) || field != c.args[1] {
			t.Errorf("parseArgs(%q) = %v, %q, %v; want %v, %q, nil", c.args, q, field, err, c.q, c.args[1])
		}
	}
	for _, c := range badArgs {
		if _, _, err := parseArgs(c.args); err == nil || err.Error() != c.msg {
			t.Errorf("parseArgs(%q) error = %v, want %q", c.args, err, c.msg)
		}
	}
}

// TestRunRejectsBadArguments checks that every rejected argument list exits with 2 and prints the error and the
// usage, without reading stdin.
func TestRunRejectsBadArguments(t *testing.T) {
	for _, c := range badArgs {
		var stdout, stderr strings.Builder
		code := run(c.args, mustNotRead{t}, &stdout, &stderr)
		want := "error: " + c.msg + "\nusage: running-quantile <q in [0, 1]> <field>   (the input file path is read from stdin)\n"
		if code != 2 || stdout.Len() != 0 || stderr.String() != want {
			t.Errorf("run(%q) = %d, %q, %q; want 2, \"\", %q", c.args, code, stdout.String(), stderr.String(), want)
		}
	}
}

// TestRunSkipsInvalidRecords checks that a record skipped for each reason gets its exact warning and is counted,
// while the valid records around it are still used.
func TestRunSkipsInvalidRecords(t *testing.T) {
	const frame = `{"v":2,"pad":""}`
	tooLong := `{"v":2,"pad":"` + strings.Repeat("x", maxLineBytes+1-len(frame)) + `"}`
	for _, c := range []struct{ line, reason string }{
		{`{"v":`, "malformed JSON"},
		{`[1]`, "not a JSON object"},
		{`{"x":1}`, `missing field "v"`},
		{`{"v":"5"}`, `field "v" is not a number`},
		{`{"v":1e400}`, `field "v" is out of float64 range`},
		{tooLong, "line too long (over 64 MiB)"},
	} {
		got := runOnContent(t, "{\"v\":1}\n"+c.line+"\n{\"v\":3}\n", "0.5", "v")
		want := result{0, "1\n2\n", "line 2: " + c.reason + "\nskipped 1 invalid record\n"}
		if got != want {
			t.Errorf("line 2 = %.40q (%d bytes): got %+v, want %+v", c.line, len(c.line), got, want)
		}
	}
}

// TestRunCapsWarnings checks that only the first 10 skipped records get a warning, while all of them are counted.
func TestRunCapsWarnings(t *testing.T) {
	var stderr strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&stderr, "line %d: missing field \"v\"\n", i)
	}
	stderr.WriteString("skipped 15 invalid records\n")
	got := runOnContent(t, strings.Repeat("{}\n", 15)+"{\"v\":7}\n", "0.5", "v")
	if want := (result{0, "7\n", stderr.String()}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestRunAcceptsInputVariants checks line endings, the BOM, blank lines, empty input, long lines and -0 at q = 0.5.
func TestRunAcceptsInputVariants(t *testing.T) {
	missingOn := func(line int) string {
		return fmt.Sprintf("line %d: missing field \"v\"\nskipped 1 invalid record\n", line)
	}
	for _, c := range []struct {
		name, content string
		want          result
	}{
		{"CRLF", "{\"v\":1}\r\n{\"v\":3}\r\n", result{0, "1\n2\n", ""}},
		{"BOM", "\xef\xbb\xbf{\"v\":1}\n{\"v\":3}\n", result{0, "1\n2\n", ""}},
		{"BOM after line 1", "{\"v\":1}\n\xef\xbb\xbf{\"v\":3}\n",
			result{0, "1\n", "line 2: malformed JSON\nskipped 1 invalid record\n"}},
		{"no final newline", "{\"v\":1}\n{\"v\":3}", result{0, "1\n2\n", ""}},
		{"blank and whitespace-only lines", "\n \t\n\r\r\n{\"v\":1}\n\t\r \n{}\n{\"v\":3}\n",
			result{0, "1\n2\n", missingOn(6)}},
		{"empty file", "", result{0, "", ""}},
		{"only blank lines", "\n\r\n \n", result{0, "", ""}},
		{"only invalid records", "{}\n", result{0, "", missingOn(1)}},
		{"valid line over 64 KiB", `{"pad":"` + strings.Repeat("x", 70000) + `","v":4}` + "\n", result{0, "4\n", ""}},
		{"valid line over the read buffer", `{"pad":"` + strings.Repeat("x", readBufSize) + `","v":4}` + "\n",
			result{0, "4\n", ""}},
		{"-0.0", "{\"v\":-0.0}\n", result{0, "0\n", ""}},
		{"-0", "{\"v\":-0}\n", result{0, "0\n", ""}},
	} {
		if got := runOnContent(t, c.content, "0.5", "v"); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// TestRunInputErrors checks that a missing or unusable input exits with 1 and a single error line, without output.
func TestRunInputErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.jsonl")
	unreadable := filepath.Join(dir, "unreadable.jsonl")
	if err := os.WriteFile(unreadable, []byte("{\"v\":1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	firstLine, _, _ := strings.Cut(specExample, "\n")
	for _, c := range []struct{ name, stdin, stderr string }{
		{"empty stdin", "", "error: expected a file path on stdin\n"},
		{"blank path", "\n", "error: expected a file path on stdin\n"},
		{"nonexistent file", missing + "\n", "error: open " + missing + ": no such file or directory\n"},
		{"directory", dir + "\n", "error: reading line 1: read " + dir + ": is a directory\n"},
		{"unreadable file", unreadable + "\n", "error: open " + unreadable + ": permission denied\n"},
		{"JSON Lines content on stdin", specExample, "error: open " + firstLine + ": no such file or directory\n"},
		{"multi-megabyte first line on stdin", strings.Repeat("x", 4<<20),
			"error: first line of stdin is longer than 4096 bytes; expected a file path\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "unreadable file" && os.Geteuid() == 0 {
				t.Skip("root can read a file with mode 000")
			}
			if got, want := runWithStdin(c.stdin, "0.5", "v"), (result{1, "", c.stderr}); got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

// TestRunAcceptsRelativePath checks that a path relative to the working directory is accepted.
func TestRunAcceptsRelativePath(t *testing.T) {
	t.Chdir(filepath.Dir(writeTemp(t, specExample)))
	got := runWithStdin("input.jsonl\n", "0.75", "latency_ms")
	if want := (result{0, specOutput, ""}); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestRunReportsWriteError checks that a failing stdout exits with 1, and that the error comes after the
// skipped-records summary.
func TestRunReportsWriteError(t *testing.T) {
	path := writeTemp(t, "x\n"+specExample)
	var stderr strings.Builder
	code := run([]string{"0.75", "latency_ms"}, strings.NewReader(path+"\n"), failingWriter{errors.New("boom")}, &stderr)
	want := "line 1: malformed JSON\nskipped 1 invalid record\nerror: boom\n"
	if code != 1 || stderr.String() != want {
		t.Fatalf("got %d, %q; want 1, %q", code, stderr.String(), want)
	}
}
