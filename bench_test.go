package main

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"math/rand/v2"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// BenchmarkExtractNumber measures scanning one record: the task's PDF line, and the same line with the field's
// key escaped so that it takes the decoding path.
func BenchmarkExtractNumber(b *testing.B) {
	escaped := strings.Replace(pdfLine, `"latency_ms"`, `"latency\u005fms"`, 1)
	for _, c := range []struct{ name, line string }{
		{"pdf", pdfLine},
		{"escaped-key", escaped},
	} {
		b.Run(c.name, func(b *testing.B) {
			line := []byte(c.line)
			if v, r := extractNumber(line, "latency_ms"); r != valid || v != 5 {
				b.Fatalf("extractNumber(%q) = (%v, %v), want (5, valid)", line, v, r)
			}
			b.SetBytes(int64(len(line)))
			b.ReportAllocs()
			for b.Loop() {
				extractNumber(line, "latency_ms")
			}
		})
	}
}

// BenchmarkAppendFloat measures formatting a cycle of typical quantiles: integers, short and long fractions, and
// the exponent forms.
func BenchmarkAppendFloat(b *testing.B) {
	vals := []float64{5, 4.5, 1234567, 7.25, 523.1234567891234, 0.001953125, 1e-7, 1e21}
	buf := make([]byte, 0, 32)
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		buf = appendFloat(buf[:0], vals[i%len(vals)])
	}
}

// trackerBenchValues is how many values each tracker in BenchmarkTrackerAdd receives before it is replaced by a
// fresh one, so the measured Add cost is that of a tracker growing from 0 to this size whatever b.N is.
const trackerBenchValues = 1 << 20

// BenchmarkTrackerAdd measures Add plus Quantile (what process calls per record) for each tracker at q = 0.5, on
// random floats (all distinct) and on ints 0..999 (few distinct values).
func BenchmarkTrackerAdd(b *testing.B) {
	r := rand.New(rand.NewPCG(3, 4))
	inputs := []struct {
		name string
		vals []float64
	}{
		{"random", make([]float64, trackerBenchValues)},
		{"ints", make([]float64, trackerBenchValues)},
	}
	for i := range trackerBenchValues {
		inputs[0].vals[i] = r.Float64() * 1000
		inputs[1].vals[i] = float64(r.IntN(1000))
	}
	for _, tr := range []struct {
		name  string
		fresh func() tracker
	}{
		{"heaps", func() tracker { return newHeapTracker(0.5) }},
		{"distinct", func() tracker { return newDistinctTracker(0.5) }},
		{"adaptive", func() tracker { return newTracker(0.5) }},
	} {
		for _, in := range inputs {
			b.Run(tr.name+"/"+in.name, func(b *testing.B) {
				var t tracker
				b.ReportAllocs()
				for i := 0; b.Loop(); i++ {
					j := i % len(in.vals)
					if j == 0 {
						b.StopTimer()
						t = nil
						runtime.GC() // the previous tracker's garbage is not part of Add's cost
						t = tr.fresh()
						b.StartTimer()
					}
					t.Add(in.vals[j])
					t.Quantile()
				}
			})
		}
	}
}

// pdfShapedLines returns n JSON Lines records shaped like the task's example, seeded: a timestamp that grows by
// one second per record, a customer, region and endpoint, latency_ms = int(exp(3 + 0.8·N(0,1))) and bytes_sent
// uniform in 100..20000.
func pdfShapedLines(n int) []byte {
	r := rand.New(rand.NewPCG(5, 6))
	customers := []string{"acme", "globex", "initech", "umbrella"}
	regions := []string{"us-east", "us-west", "eu-central"}
	endpoints := []string{"/api/v1/query", "/api/v1/insert", "/api/v1/health"}
	start := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	var out []byte
	for i := range n {
		out = append(out, `{"timestamp": "`...)
		out = start.Add(time.Duration(i)*time.Second).AppendFormat(out, time.RFC3339)
		out = append(out, `", "customer_id": "`...)
		out = append(out, customers[r.IntN(len(customers))]...)
		out = append(out, `", "region": "`...)
		out = append(out, regions[r.IntN(len(regions))]...)
		out = append(out, `", "endpoint": "`...)
		out = append(out, endpoints[r.IntN(len(endpoints))]...)
		out = append(out, `", "latency_ms": `...)
		out = strconv.AppendInt(out, int64(math.Exp(3+0.8*r.NormFloat64())), 10)
		out = append(out, `, "bytes_sent": `...)
		out = strconv.AppendInt(out, int64(100+r.IntN(19901)), 10)
		out = append(out, "}\n"...)
	}
	return out
}

// benchRecords is the number of records BenchmarkProcess runs through process per iteration.
const benchRecords = 1_000_000

// BenchmarkProcess measures the whole pipeline (read, scan, track, format, write) over 1M PDF-shaped records held
// in memory, at q = 0.5 on latency_ms. MB/s is input throughput.
func BenchmarkProcess(b *testing.B) {
	in := pdfShapedLines(benchRecords)
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	for b.Loop() {
		out := bufio.NewWriterSize(io.Discard, writeBufSize)
		if skipped, err := process(bytes.NewReader(in), 0.5, "latency_ms", out, io.Discard); skipped != 0 || err != nil {
			b.Fatalf("process: skipped %d, err %v", skipped, err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*benchRecords), "ns/record")
}
