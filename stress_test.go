//go:build stress

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// stressMaxBytes is the default and the largest size of the file TestStress24GB writes.
const stressMaxBytes int64 = 24_000_000_000

// stressShortestRecord is less than the length of any valid record line (at least 165 bytes), so a stress file of
// n bytes holds fewer than n/stressShortestRecord valid records.
const stressShortestRecord = 160

// stressCheckpoints are the valid-record counts, besides the last one, at which TestStress24GB checks the output.
var stressCheckpoints = []int{1, 1e6, 1e7, 1e8}

// stressInvalidLines are the invalid lines the stress file cycles through, each invalid for both latency_ms and
// duration_us: malformed JSON, not a JSON object, missing field, not a number, and out of float64 range.
var stressInvalidLines = []string{
	`{"timestamp": "2026-06-18T10:00:00Z", "latency_ms": 5, "duration_us": 1.5`,
	`[1,2,3]`,
	`{"timestamp": "2026-06-18T10:00:00Z", "customer_id": "acme", "bytes_sent": 8421}`,
	`{"latency_ms": "5", "duration_us": "1.5"}`,
	`{"latency_ms": 1e400, "duration_us": 1e400}`,
}

// The values of the stress file's text fields, and the timestamp of line 0.
var (
	stressCustomers = []string{"acme", "globex", "initech", "umbrella"}
	stressRegions   = []string{"us-east", "us-west", "eu-central"}
	stressEndpoints = []string{"/api/v1/query", "/api/v1/insert", "/api/v1/health"}
	stressStart     = time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
)

// stressFile is a generated stress file and the output the binary must produce for it.
type stressFile struct {
	size                  int64
	lines, valid, invalid int
	checkpoints           []int    // valid-record counts at which the output is checked, ascending; the last is valid
	latency, duration     []string // expected output lines at checkpoints for 0.99 latency_ms and 0.5 duration_us
}

// TestStress24GB writes a seeded JSON Lines file of at most RQ_STRESS_BYTES bytes (default and maximum 24 GB),
// runs the binary on it for latency_ms at q = 0.99 and for the all-distinct duration_us at q = 0.5, and checks each
// run's exit code, line count, output at the checkpoints, skipped count and peak RSS.
func TestStress24GB(t *testing.T) {
	limit := stressLimit(t)
	path := filepath.Join(t.TempDir(), "stress.jsonl")
	f := writeStressFile(t, path, limit)
	// Return the reference data (about 3 GB at 24 GB) to the OS before the runs. Under memory pressure macOS
	// compresses some of the binary's pages, which Maxrss does not count, so pressure makes the RSS check read low.
	debug.FreeOSMemory()
	// The RSS bounds rest on measurements at 24 GB (128M valid records) on an Apple M4:
	//   - latency_ms has few distinct values, so the tracker stays in distinct mode. Peak RSS was 7.5 MiB against
	//     the 64 MiB bound; storing every value would take about 977 MiB, which the bound catches.
	//   - duration_us is all distinct, so the tracker switches to heaps at 8 B per value. The binary run alone
	//     peaked at 988.6 MiB, about 8.1 B per value, against the bound 8·n·1.25 + 64 MiB (1285.2 MiB).
	// minRSS is the least the run can truly need: 8 B per value once the heaps hold every value.
	for _, c := range []struct {
		q, field       string
		want           []string
		minRSS, maxRSS int64
	}{
		{"0.99", "latency_ms", f.latency, 0, 64 << 20},
		{"0.5", "duration_us", f.duration, 8 * int64(f.valid), int64(8*float64(f.valid)*1.25) + 64<<20},
	} {
		t.Run(c.field, func(t *testing.T) { runStress(t, path, f, c.q, c.field, c.want, c.minRSS, c.maxRSS) })
	}
}

// stressLimit returns the size limit of the stress file: stressMaxBytes when RQ_STRESS_BYTES is unset, otherwise
// its value, which must be an integer from 1 to stressMaxBytes.
func stressLimit(t *testing.T) int64 {
	s, ok := os.LookupEnv("RQ_STRESS_BYTES")
	if !ok {
		return stressMaxBytes
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || n > stressMaxBytes {
		t.Fatalf("RQ_STRESS_BYTES=%q: want an integer from 1 to %d", s, stressMaxBytes)
	}
	return n
}

// writeStressFile writes the stress file to path, stopping before the first line that would take it over limit
// bytes, and returns it with the expected output. Line i (from 1) is blank if i is a multiple of 997, otherwise
// invalid if i is a multiple of 1000 (so a multiple of both is blank), otherwise a valid record.
func writeStressFile(t *testing.T, path string, limit int64) stressFile {
	start := time.Now()
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(out, 4<<20)
	r := rand.New(rand.NewPCG(13, 24))
	var (
		f         stressFile
		line      []byte
		hist      []int   // hist[v] is the number of valid records so far with latency_ms = v
		snapshots [][]int // hist at each count of stressCheckpoints reached
		durations = make([]float64, 0, limit/stressShortestRecord)
	)
	for i := 1; ; i++ {
		blank, invalid := i%997 == 0, i%1000 == 0
		var latency int
		var duration float64
		line = line[:0]
		switch {
		case blank:
		case invalid:
			line = append(line, stressInvalidLines[f.invalid%len(stressInvalidLines)]...)
		default:
			line, latency, duration = appendStressRecord(line, r, i)
		}
		line = append(line, '\n')
		if f.size+int64(len(line)) > limit {
			break
		}
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
		f.size += int64(len(line))
		f.lines++
		switch {
		case blank:
		case invalid:
			f.invalid++
		default:
			f.valid++
			for latency >= len(hist) {
				hist = append(hist, 0)
			}
			hist[latency]++
			durations = append(durations, duration)
			if len(snapshots) < len(stressCheckpoints) && f.valid == stressCheckpoints[len(snapshots)] {
				snapshots = append(snapshots, slices.Clone(hist))
			}
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != f.size || info.Size() > limit {
		t.Fatalf("the stress file has %d bytes, want %d and at most %d", info.Size(), f.size, limit)
	}
	t.Logf("generated %d bytes: %d lines, %d valid records, %d invalid, in %v",
		f.size, f.lines, f.valid, f.invalid, time.Since(start).Round(time.Millisecond))

	start = time.Now()
	f.checkpoints = slices.Clone(stressCheckpoints[:len(snapshots)])
	if f.valid > 0 && !slices.Contains(f.checkpoints, f.valid) {
		f.checkpoints = append(f.checkpoints, f.valid)
		snapshots = append(snapshots, hist)
	}
	for j, c := range f.checkpoints {
		f.latency = append(f.latency, string(appendFloat(nil, histQuantile(snapshots[j], c, 0.99))))
		f.duration = append(f.duration, string(appendFloat(nil, referenceQuantile(durations[:c], 0.5))))
	}
	t.Logf("computed the expected lines at valid records %v in %v: latency_ms %q, duration_us %q",
		f.checkpoints, time.Since(start).Round(time.Millisecond), f.latency, f.duration)
	return f
}

// appendStressRecord appends line i of the stress file, drawn from r, and returns it with its latency_ms and
// duration_us. The record is shaped like the task's example (a timestamp i seconds after stressStart, a customer,
// region and endpoint, latency_ms = int(exp(3 + 0.8·N(0,1))) and bytes_sent uniform in 100..20000) plus
// duration_us = Float64()·1e6, which is written so that it parses back to exactly the returned value.
func appendStressRecord(dst []byte, r *rand.Rand, i int) ([]byte, int, float64) {
	latency := int(math.Exp(3 + 0.8*r.NormFloat64()))
	duration := r.Float64() * 1e6
	dst = append(dst, `{"timestamp": "`...)
	dst = stressStart.Add(time.Duration(i)*time.Second).AppendFormat(dst, time.RFC3339)
	dst = append(dst, `", "customer_id": "`...)
	dst = append(dst, stressCustomers[r.IntN(len(stressCustomers))]...)
	dst = append(dst, `", "region": "`...)
	dst = append(dst, stressRegions[r.IntN(len(stressRegions))]...)
	dst = append(dst, `", "endpoint": "`...)
	dst = append(dst, stressEndpoints[r.IntN(len(stressEndpoints))]...)
	dst = append(dst, `", "latency_ms": `...)
	dst = strconv.AppendInt(dst, int64(latency), 10)
	dst = append(dst, `, "bytes_sent": `...)
	dst = strconv.AppendInt(dst, int64(100+r.IntN(19901)), 10)
	dst = append(dst, `, "duration_us": `...)
	dst = strconv.AppendFloat(dst, duration, 'f', -1, 64)
	return append(dst, '}'), latency, duration
}

// histQuantile returns the type 7 q-quantile of the n values counted in hist, where hist[v] counts the value v.
func histQuantile(hist []int, n int, q float64) float64 {
	k, f := rank(n, q)
	if f == 0 {
		return histValue(hist, k)
	}
	return lerp(histValue(hist, k), histValue(hist, k+1), f)
}

// histValue returns the value at index i of the values counted in hist, in ascending order.
func histValue(hist []int, i int) float64 {
	for v, c := range hist {
		if i < c {
			return float64(v)
		}
		i -= c
	}
	panic("histValue: index out of range")
}

// runStress runs the binary with q and field on the stress file f at path, streaming its stdout, and checks the
// exit code, the line count, the lines at f's checkpoints against want, the skipped count and the peak RSS against
// maxRSS. A peak RSS below minRSS, the least the run truly needs, is logged as a low reading.
func runStress(t *testing.T, path string, f stressFile, q, field string, want []string, minRSS, maxRSS int64) {
	cmd := exec.Command(binaryPath, q, field)
	cmd.Stdin = strings.NewReader(path + "\n")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	peakRSS := watchPeakRSS(cmd.Process)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // big reads keep the pipe drained
	n, next := 0, 0
	for sc.Scan() {
		n++
		if next < len(f.checkpoints) && n == f.checkpoints[next] {
			if got := sc.Text(); got != want[next] {
				t.Errorf("line %d = %q, want %q", n, got, want[next])
			}
			next++
		}
	}
	if err := sc.Err(); err != nil {
		cmd.Process.Kill() // it may be blocked writing to the pipe that is no longer read
		t.Errorf("reading stdout: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Errorf("running-quantile %s %s: %v", q, field, err)
	}
	elapsed := time.Since(start)
	if n != f.valid {
		t.Errorf("got %d lines, want %d", n, f.valid)
	}
	// The binary prints the skipped count only when it skipped records, with "record" for exactly one.
	wantEnd := fmt.Sprintf("skipped %d invalid records\n", f.invalid)
	switch f.invalid {
	case 0:
		wantEnd = ""
	case 1:
		wantEnd = "skipped 1 invalid record\n"
	}
	if !strings.HasSuffix(stderr.String(), wantEnd) || f.invalid == 0 && stderr.Len() > 0 {
		t.Errorf("stderr is %q, want it to end with %q", stderr.String(), wantEnd)
	}
	t.Logf("%s %s: %d lines in %v, %.0f MB/s, %.0f records/s", q, field, n, elapsed.Round(time.Millisecond),
		float64(f.size)/1e6/elapsed.Seconds(), float64(n)/elapsed.Seconds())
	rss, ok := peakRSS(cmd.ProcessState)
	if !ok {
		t.Log("peak RSS is not available, so it is not checked")
		return
	}
	t.Logf("peak RSS %.1f MiB, bound %.1f MiB", float64(rss)/(1<<20), float64(maxRSS)/(1<<20))
	if rss < minRSS {
		t.Logf("note: peak RSS reads below the %.1f MiB (8 B per value) the heaps need, so some of the binary's "+
			"pages were compressed or swapped out (macOS memory compression); the true peak is higher",
			float64(minRSS)/(1<<20))
	}
	if rss > maxRSS {
		t.Errorf("peak RSS is %d bytes, want at most %d", rss, maxRSS)
	}
}
