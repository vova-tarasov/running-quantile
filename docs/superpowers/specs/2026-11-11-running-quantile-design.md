# Running Quantile — Design

Date: 2026-11-11 · Status: draft, awaiting approval

Source task: `../Product_Metrics_Take_Home_Question_-_Running_Quantile.pdf` (one level above the project folder).

## 1. Intent

This is a take-home exercise for a ClickHouse interview. The program reads a quantile `q` and a field name as
arguments, then the path of a JSON Lines file from stdin. For every record it prints the q-quantile of
that field over all valid records seen so far.

The solution will be discussed and extended live in a follow-up interview. That means:
- the code must be small and easy to explain;
- correctness must be demonstrated by tests, not asserted;
- every performance claim in the README must come from a measurement.

The project is a fresh start. An earlier attempt exists in `../running-quantile/`. It is left untouched,
and no code is copied from it.

## 2. Goals

1. **Exact** quantiles: linear interpolation between order statistics (Hyndman–Fan type 7, numpy/R default).
   - The PDF example must be reproduced byte for byte: `5 4.5 6.5 5.75 8 7.25` for q = 0.75, field `latency_ms`.
2. **CPU-efficient.**
   - A hand-written JSON scanner.
   - Zero heap allocations per record in steady state.
   - Buffered I/O.
3. **Memory-efficient.**
   - O(1) memory for q = 0 and q = 1.
   - Otherwise, memory follows the number of **distinct** values when values repeat, and never exceeds about
     8 bytes per value.
   - No 2× spikes when storage grows.
   - A huge line can never exhaust memory.
4. **Scale.** It handles a 24 GB input file (~150M records), checked by an on-demand stress test.
5. **TDD.** Every behaviour is introduced by a failing test first.
6. **Tests cover every edge case** in section 6, including bad arguments, bad input and the 24 GB run.
   - Statement coverage is 100%, except the one-line `main()` wrapper.
7. **Every function and method** (including unexported ones and test functions) has a doc comment. A test enforces this.
8. **README.** It covers:
   - how to build, run and test;
   - the quantile definition and the algorithm, with its complexity;
   - every input-handling decision;
   - measured performance and memory.
9. **A Makefile** with `build`, `test`, `cover`, `bench`, `fuzz` and `stress` targets.
10. **Review after each iteration.** Every plan task ends with a review before the next one starts.

## 3. Non-goals

- Approximate quantiles. The README mentions them only as a possible extension.
- Nested field paths. `a.b` is a literal key name.
- Numeric strings (`"5"`).
- Any CLI options beyond the two positional arguments.

## 4. Anti-goals (it fails even if it "works")

- Any third-party module, or `GOEXPERIMENT` (e.g. `jsonv2`). Only the Go 1.26 standard library is allowed.
- Output that differs from the type-7 definition for any valid input.
- Silently losing records. Every skipped record is counted and the total is reported.
- Memory proportional to line length for absurdly long lines (see the line-length cap in §6.3).
- A slow or flaky default `go test`. The 24 GB run is opt-in only.

## 5. Constraints

- Go 1.26.0 is installed at `/usr/local/go`, and `go.mod` says `go 1.26`.
- Development machine: Apple M4, 10 cores, 32 GB RAM, 426 GB free disk.
- Only the standard library is allowed, per the PDF.

## 6. Behaviour (decided)

### 6.1 Interface

```
echo /abs/path/file.jsonl | running-quantile <q> <field>
```

- The binary is named `running-quantile`.
- The path is read from the **first line** of stdin.
  - Only the trailing `\n` or `\r\n` is removed. Other whitespace is kept, because it can be part of a path.
  - Anything after the first line of stdin is ignored.
- A relative path is accepted, even though the PDF says the path is absolute.
- Output: one line on stdout per **valid** record.

### 6.2 Arguments

| Case | Result |
|---|---|
| Not exactly 2 arguments | usage on stderr, exit 2, file not read |
| `q` is not a JSON-style decimal number (grammar: `-?(0\|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?`) | usage, exit 2 |
| `q` outside [0, 1] (e.g. `1.1`, `-0.1`) | usage, exit 2 |
| `q` given as `NaN`, `Inf`, hex (`0x1p-2`), underscores (`1_0`), `.5`, `+0.5`, or with surrounding spaces | usage, exit 2 (they fail the grammar) |
| `q` = `-0` | accepted, treated as 0 |
| `q` like `1e-1`, `0.75`, `1`, `0` | accepted |
| Empty field name `""` | usage, exit 2 (a JSON key `""` can't be selected; deliberate) |

My call: q uses the same number grammar as JSON, so the rule is short and easy to explain. Go's
`strconv.ParseFloat` would also accept hex, underscores, `inf` and `nan`. The program checks the grammar
first and only then calls `ParseFloat`.

### 6.3 Input file and records

| Case | Treatment |
|---|---|
| Empty stdin, or an empty first line | error on stderr, exit 1 |
| Path that doesn't exist, can't be read, or is a directory | error on stderr, exit 1 |
| Read error in the middle of the file | stdout written so far is flushed, error on stderr, exit 1 |
| Empty file, or no valid records | no stdout, exit 0 (plus the skipped count if > 0) |
| Blank or whitespace-only line | ignored silently, not counted as skipped |
| CRLF line endings, UTF-8 BOM at the start of line 1, no final newline | accepted |
| Invalid JSON (truncated, trailing garbage, `NaN`/`Infinity`, leading zeros `05`, `+5`, `.5`, `1.`, control characters inside strings, bad escapes) | **skipped** (`malformed JSON`) |
| Valid JSON that isn't an object (`[1]`, `5`, `"x"`, `null`) | **skipped** (`not a JSON object`) |
| Field absent (match is top-level, exact, case-sensitive, after unescaping, so `"\u0076"` matches `v`) | **skipped** (`missing field`) |
| Field is not a number (`"5"`, `true`, `null`, `[]`, `{}`) | **skipped** (`not a number`) |
| Number beyond float64 (`1e400`) | **skipped** (`out of float64 range`) |
| Number that underflows (`1e-400`) | accepted as 0 (this is `strconv.ParseFloat` behaviour) |
| Integers above 2^53 | accepted, rounded to the nearest float64 (documented) |
| `-0` | accepted as 0, so `-0` is never printed |
| Duplicate keys | **last** occurrence wins (same as `encoding/json`); if the last one isn't a number, the record is skipped |
| Invalid UTF-8 bytes inside strings | tolerated (same as `encoding/json`'s `Valid`) |
| Line longer than 64 MiB | **skipped** (`line too long`) without buffering it: the rest of the line is drained in place |

**Skipped-record reporting:**
- The first 10 skipped records are printed on stderr as `line N: <reason>`. N is the 1-based physical line number.
- At the end, if any record was skipped, a final `skipped K invalid record(s)` line is printed.
- The exit code stays 0.

### 6.4 Output format

- Use `strconv.AppendFloat(v, 'f', -1, 64)` (shortest round-trip decimal, no exponent) when 1e-6 ≤ |v| < 1e21 or v = 0.
- Outside that range, use the exponent form `'e'`, cleaned the way `encoding/json` cleans it (`1e-7`, `1e+21`).
- This is exactly how `encoding/json` prints float64. Typical metrics print as `5`, `4.5`, `1234567`, and extreme values don't print as 300-digit strings.

### 6.5 Exit codes

| Code | Meaning |
|---|---|
| 0 | success, including when records were skipped |
| 1 | I/O error (stdin, open, read, or write) |
| 2 | bad arguments |

## 7. Algorithm (decided)

### 7.1 Quantile definition

For n sorted values x[0..n-1]:

```
h = (n-1)*q   (float64)
k = floor(h)
f = h - k
Q = x[k] + f*(x[k+1] - x[k])   (Q = x[k] when f = 0)
```

- If `x[k+1] - x[k]` overflows (only possible for opposite-sign values near ±MaxFloat64), use `(1-f)*x[k] + f*x[k+1]` instead.
- `h` is computed in float64. A result can differ from numpy/R in the last digit (e.g. `100*0.29 = 28.999999999999996`). The README documents this.
- k never decreases as n grows, but code must **not** assume it moves by at most 1 per record. Rebalancing uses loops.

### 7.2 Trackers

| q | Structure | Memory | Time per record |
|---|---|---|---|
| 0 | running minimum | O(1) | O(1) |
| 1 | running maximum | O(1) | O(1) |
| other | two heaps: a max-heap `low` holds the k+1 smallest values, a min-heap `high` holds the rest. x[k] and x[k+1] are the two tops. | 8 B/value | O(log n) |

- Exact quantiles need Ω(n) memory in general: any stored value can become the answer later.
- The heaps store float64 in **fixed-size chunks** (e.g. 64 KiB), not one growing slice. A slice that grows
  copies everything into a bigger array and leaves the old one as garbage. Chunks are allocated once and
  never copied, so peak memory stays about 1× the data.
- Measured on 2026-11-11 (M4, slice-backed binary heaps, 100M values): 20–52 ns per record.
  - The tracker is not the bottleneck.
- `container/heap` is not used, because it boxes every value in an interface.

**Repeated-values mode (user decision, 2026-11-11: add it).** For 0 < q < 1, the tracker starts in a
*distinct* mode and switches one way to plain heaps when that saves memory.

- **Distinct mode.**
  - Each distinct value is stored once, with a count, in a `map[float64]count`.
  - A *cursor* marks the distinct value that holds x[k]. The tracker also keeps the cursor value's count and
    `below`, the number of values smaller than the cursor value. So the cursor's copies occupy ranks
    `below … below+count-1`.
  - Distinct values smaller than the cursor value sit in a max-heap; larger ones sit in a min-heap. Both use
    the same chunked heap type.
  - Moving the cursor one distinct value is one pop plus one push. x[k+1] is either another copy of the
    cursor value or the top of the upper heap.
  - Cost: O(log d) per record, where d is the number of distinct values. Memory: roughly 30–40 B per
    distinct value, independent of the record count. The builder measures the exact figure.
- **Switch to heaps.**
  - It happens when storing every value would cost no more memory than the distinct structure, using an
    estimate like `8·n ≤ bytesPerDistinct·d`.
  - It never happens while d is below a floor (e.g. 2^16), so small inputs never pay for it.
  - The check is O(1) after each Add. The switch is one O(n) pass:
    - values below the cursor, plus the copies of the cursor value up to x[k], go to the low heap;
    - everything else goes to the high heap;
    - both heaps are then heapified.
  - Peak memory during the switch is about distinct structure + heaps, which is about 2× the distinct
    structure, briefly. The README documents this.
  - The switch never goes back to distinct mode.
- **Correctness bar.** Distinct mode and the adaptive tracker must give **bit-for-bit identical** results to
  the heap tracker after every insert, on every oracle input in §8.2. That includes:
  - a switch while the cursor value's copies straddle x[k] and x[k+1] (they have to be split between the
    two heaps);
  - data that starts with few distinct values and later becomes mostly unique.
- **Thresholds.** The floor and the bytes-per-distinct constant are chosen by measurement and written in the README.

### 7.3 JSON scanning

- A single pass over the line validates the **whole** line against the JSON grammar (RFC 8259) and finds the
  top-level key equal to `field`.
- Keys that are plain ASCII with no backslash are compared as raw bytes (the fast path).
- Any other key is decoded the way `encoding/json` decodes it, then compared. Decoding covers:
  - `\uXXXX` escapes and surrogate pairs;
  - lone surrogates and invalid UTF-8 bytes, which both become U+FFFD.
- My call: decode keys exactly like `encoding/json`, so the fuzz oracle (§8.3) and the scanner always agree on
  which key matches.
- Nested objects and arrays are validated and skipped. A matching key inside a nested object never matches.
- The target value's number bytes go to `strconv.ParseFloat(string(b), 64)`. This was measured at 0 allocations.
- Nesting depth is capped (e.g. 10,000 levels; deeper is `malformed JSON`), so recursion can't overflow the stack.
  - The scanner may use an explicit stack or bounded recursion; the builder chooses.
- Measured per record of the PDF example (M4): `encoding/json` into a map takes 1,135 ns and 24 allocations;
  `json.Valid` takes 286 ns with 0 allocations; a bare byte loop takes 45 ns. The target is ≤ ~150 ns with 0 allocations.

### 7.4 I/O

- **Reading:** `bufio.Reader` (~1 MiB buffer) with `ReadSlice('\n')`, so ordinary lines are never copied.
  - Lines longer than the buffer are assembled in a side buffer up to the 64 MiB cap.
  - Beyond the cap, the rest of the line is drained without storing it.
- **Writing:** `bufio.Writer` (~1 MiB), with numbers formatted straight into `AvailableBuffer()`, so there are no allocations.
- **Single-threaded (user decision, 2026-11-11).**
  - One goroutine reads, parses, tracks and writes. That is the least total CPU work per record.
  - A multi-core parse pipeline is described in the README as a possible next step only.

### 7.5 Code layout

- One `package main` in a flat folder: `main.go` (args, wiring, exit codes), `input.go` (path, line reading),
  `scan.go` (JSON field extraction), `quantile.go` (trackers and heaps), `format.go` (number output).
- Test files sit next to each file.
- Go is installed, so there's no build step beyond `go build`.

## 8. Testing (decided)

All tests use only the standard library (`testing`, including native fuzzing).

1. **Unit tests per file.** Table-driven, with one row per edge case in §6.
2. **Oracle tests.**
   - Every tracker is checked after every insert against a sort-and-interpolate reference. Inputs include:
     - random floats and small ints with many duplicates;
     - ascending, descending and constant values;
     - mixed signs and ±MaxFloat64.
   - Values of q tested: 0, the smallest positive float64 (5e-324), 0.01, 0.25, 0.5, 0.75, 0.99,
     0.9999999999999999 (the largest float64 below 1), 1, and a random q.
3. **Scanner vs `encoding/json`.**
   - Native fuzz tests check that, for any input line, the scanner's verdict (valid or not, object or not,
     field present, number or not, value) agrees with an `encoding/json` reference built from `json.Valid`
     and `Unmarshal`.
   - The seed corpus runs on every `go test`; `make fuzz` runs it longer.
4. **End to end through `run(args, stdin, stdout, stderr)`.**
   - The PDF example.
   - Every row of the §6 tables.
   - The warning cap.
   - A line over the 64 MiB cap.
   - A read error mid-file (via a failing reader).
   - A write error (via a failing writer).
5. **Binary tests.** The test builds the real binary once and runs it as a subprocess. These check the real
   stdin, exit codes and the PDF example.
6. **Allocation and memory tests.**
   - Zero allocations per record in steady state (`testing.AllocsPerRun`).
   - Chunked heaps grow without copying.
   - q = 0 and q = 1 use constant memory.
7. **Doc-comment test.** It parses every `.go` file with `go/parser` and fails if any function or method lacks a doc comment.
8. **Benchmarks** for the scanner, the trackers, formatting, and the whole pipeline.
9. **Stress test (on demand: `make stress`, build tag `stress`).**
   - It writes a seeded, deterministic JSONL file of **at most 24 GB** (24×10^9 bytes, ~150M records) to `t.TempDir()`.
     - Records are shaped like the PDF example, plus one extra float field with nearly all-distinct values.
     - About 0.1% of lines are invalid, covering every reason in §6.3.
     - `TMPDIR` controls the location.
   - It runs the built binary twice:
     - `latency_ms` (few distinct values) at q = 0.99;
     - the all-distinct float field at q = 0.5.
   - It asserts:
     - exit code 0;
     - stdout line count equals the number of valid records;
     - the skipped count on stderr matches;
     - the values at checkpoints (1M, 10M, 100M and the last record) equal the sort-based reference;
     - peak RSS (from `ProcessState.SysUsage()`) stays under a stated bound.
   - It logs time and throughput. Stdout is streamed and checked, never stored in full.
   - The file is deleted afterwards.

## 8a. Delivery order and platforms (my calls after the builder check)

- **Core first.** The plan delivers a submittable core before any hardening:
  - correct pipeline, end-to-end tests, the PDF example, and the README.
  - Then, as separate tasks: the zero-allocation scanner, chunked heaps, fuzzing, the doc-comment test, and
    the 24 GB stress test.
- **The 24 GB stress run happens twice:**
  - once when the stress test is first written;
  - once at the end, to produce the README numbers.
  - It is not re-run after every performance change; benchmarks cover those.
- **Platforms.**
  - macOS (arm64) and Linux (amd64/arm64) are supported and tested.
  - Peak RSS is read via a small build-tagged helper, because `Maxrss` is in bytes on darwin and KiB on Linux.
  - On other OSes the stress test skips the RSS assertion.
  - The Makefile uses only POSIX make features and the `go` tool, so it works with BSD and GNU make.
- **README numbers** are measured on the M4 and labelled as such.

## 9. Left to the builder

- Exact buffer and chunk sizes, chosen by measurement.
- Internal function names and signatures.
- Scanner structure (explicit stack or bounded recursion).
- The exact peak-RSS bounds for the stress test, derived from measurement and written into the README.
- Benchmark set and fuzz targets beyond those listed.
- Whether the number fast path (integers without fraction or exponent) beats `ParseFloat`. Keep it only if measured faster.

## 10. Process (user decisions, 2026-11-11)

- **Review.**
  - After every plan task, a fresh reviewer agent checks the work against this spec and for code quality.
  - Findings are fixed before the next task starts, and execution continues without stopping for the user.
  - The user gets a short summary at the end of each phase.
- **Git.**
  - `running-quantile-v2/` becomes a git repository.
  - Each reviewed iteration is one commit.
  - Commit subjects are one imperative line, with no body and **no AI attribution** (user's global instructions).
- **Folder name.** `running-quantile-v2/` sits next to the untouched `running-quantile/`. That's my call; renaming later is trivial.
