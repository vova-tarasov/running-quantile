# running-quantile

`running-quantile` reads a JSON Lines file and, after every valid record, prints the **exact** q-quantile of one
numeric field over all the valid records seen so far.

```
echo /abs/path/file.jsonl | ./running-quantile <q> <field>
```

It is written in Go 1.26 and uses only the standard library.

## Build, run, test

| Command | What it does |
|---|---|
| `make build` | builds `./running-quantile` (`go build -o running-quantile .`) |
| `make test` | checks gofmt, runs `go vet` (also with the `stress` build tag), then `go test ./...` |
| `make cover` | runs the tests with a coverage profile and prints the coverage of every function |
| `make bench` | runs the benchmarks |
| `make clean` | removes the binary and the coverage profile |

With the six records from the task saved in `/tmp/example.jsonl`:

```
$ make build
$ echo /tmp/example.jsonl | ./running-quantile 0.75 latency_ms
5
4.5
6.5
5.75
8
7.25
```

`go test ./...` covers:
- unit tests for every file;
- the trackers, checked after every insert against a sort-and-interpolate reference;
- the JSON scanner, checked against `encoding/json` by a fuzz test (its seed corpus runs on every `go test`);
- the whole program through `run(args, stdin, stdout, stderr)` (and `process` for a read error): the task's
  example, every argument case, every input-file error, each skip reason, the warning cap, the line-ending
  variants, and read and write errors;
- the real binary, built once into a temporary directory and run as a subprocess.

## The quantile

For the n values seen so far, sorted as x[0] ≤ x[1] ≤ … ≤ x[n−1]:

```
h = (n − 1) · q
k = floor(h)
f = h − k
Q = x[k] + f · (x[k+1] − x[k])      (Q = x[k] when f = 0)
```

This is linear interpolation between the two nearest order statistics: Hyndman and Fan's type 7, the default
of numpy (`method="linear"`) and R (`type = 7`). q = 0 gives the minimum and q = 1 the maximum.

**Why type 7.** It reproduces the example in the task. For the second record, the values are 3 and 5 and q = 0.75.
Type 7 gives 3 + 0.75 · (5 − 3) = 4.5, as the task expects. The other eight Hyndman–Fan definitions give 4 or 5.

The whole example (q = 0.75, field `latency_ms`):

| Record | Value | Sorted so far | h = (n − 1) · 0.75 | Output |
|---|---|---|---|---|
| 1 | 5 | 5 | 0 | 5 |
| 2 | 3 | 3 5 | 0.75 | 3 + 0.75 · (5 − 3) = 4.5 |
| 3 | 8 | 3 5 8 | 1.5 | 5 + 0.5 · (8 − 5) = 6.5 |
| 4 | 1 | 1 3 5 8 | 2.25 | 5 + 0.25 · (8 − 5) = 5.75 |
| 5 | 9 | 1 3 5 8 9 | 3 | 8 |
| 6 | 4 | 1 3 4 5 8 9 | 3.75 | 5 + 0.75 · (8 − 5) = 7.25 |

## Input handling

### Interface

```
echo /abs/path/file.jsonl | running-quantile <q> <field>
```

- The path is read from the **first line** of stdin.
  - Only the trailing `\n` or `\r\n` is removed. Other whitespace is kept, because it can be part of a path.
  - Anything after the first line of stdin is ignored.
  - At most 4098 bytes of stdin are read. A first line longer than 4096 bytes is an error.
- A relative path is accepted (relative to the working directory), even though the task says the path is absolute.
- Output: one line on stdout per **valid** record.

### Arguments

| Case | Result |
|---|---|
| Not exactly 2 arguments | error and usage on stderr, exit 2, stdin is not read |
| `q` is not a JSON-style decimal number (grammar: `-?(0\|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?`) | error and usage, exit 2 |
| `q` outside [0, 1] (e.g. `1.1`, `-0.1`, `1e400`) | error and usage, exit 2 |
| `q` given as `NaN`, `Inf`, hex (`0x1p-2`), underscores (`1_0`), `.5`, `+0.5`, or with surrounding spaces | error and usage, exit 2 (they fail the grammar) |
| `q` = `-0` | accepted, treated as 0 |
| `q` like `1e-1`, `0.75`, `1`, `0` | accepted |
| Empty field name `""` | error and usage, exit 2 (so a JSON key `""` can't be selected; deliberate) |

My call: q uses the same number grammar as JSON, so the rule is short and easy to explain. Go's
`strconv.ParseFloat` would also accept hex, underscores, `inf` and `nan`. The program checks the grammar first
and only then calls `ParseFloat`.

The field name is a literal top-level key: `a.b` is the key `a.b`, not a nested path.

### Input file and records

| Case | Treatment |
|---|---|
| Empty stdin, or an empty first line | error on stderr, exit 1 |
| First line of stdin longer than 4096 bytes | error on stderr, exit 1 |
| The JSON Lines content piped in instead of its path (`cat file.jsonl \| running-quantile …`) | its first line is not a file that can be opened (or is too long): error on stderr, exit 1 |
| Path that doesn't exist, can't be read, or is a directory | error on stderr, exit 1 (a directory fails at its first read: `reading line 1: …`) |
| Read error in the middle of the file | the output so far is flushed, then `error: reading line N: …` on stderr, exit 1 |
| Empty file, or no valid records | no stdout, exit 0 (plus the skipped count if > 0) |
| Blank or whitespace-only line (only spaces, tabs and CRs) | ignored silently, not counted as skipped; it still counts in the line numbers |
| CRLF line endings, UTF-8 BOM at the start of line 1, no final newline | accepted (a BOM anywhere else is `malformed JSON`) |
| Invalid JSON (truncated, trailing garbage, `NaN`/`Infinity`, leading zeros `05`, `+5`, `.5`, `1.`, control characters inside strings, bad escapes) | **skipped** (`malformed JSON`) |
| Nesting deeper than 10,000 levels (the top-level object is level 1; the same limit as `encoding/json`) | **skipped** (`malformed JSON`) |
| Valid JSON that isn't an object (`[1]`, `5`, `"x"`, `null`) | **skipped** (`not a JSON object`) |
| Field absent (match is top-level, exact, case-sensitive, after unescaping, so `"\u0076"` matches `v`) | **skipped** (`missing field "<f>"`) |
| Field is not a number (`"5"`, `true`, `null`, `[]`, `{}`) | **skipped** (`field "<f>" is not a number`) |
| Number beyond float64 (`1e400`) | **skipped** (`field "<f>" is out of float64 range`) |
| Number that underflows (`1e-400`) | accepted as 0 (this is `strconv.ParseFloat` behaviour) |
| Integers above 2^53 | accepted, rounded to the nearest float64 (`9007199254740993` becomes `9007199254740992`) |
| `-0` | accepted as 0, so `-0` is never printed |
| Duplicate keys | **last** occurrence wins (same as `encoding/json`); if the last one isn't a number, the record is skipped |
| Invalid UTF-8 bytes inside strings | tolerated (same as `encoding/json`) |
| Line longer than 64 MiB | **skipped** (`line too long (over 64 MiB)`) without buffering it: the rest of the line is drained in place |

Keys are matched after decoding them the way `encoding/json` decodes them, so a field name that needs escaping
or is not ASCII matches its key in any spelling:

| Field argument | Matching keys |
|---|---|
| `v` | `"v"`, `"\u0076"` |
| `café` | `"café"`, `"caf\u00e9"` |
| `😀` | `"😀"`, `"\ud83d\ude00"` |
| `lat"ency` | `"lat\"ency"`, `"lat\u0022ency"` |

A key that only looks similar never matches: `{"x":{"v":1}}` (nested), `{"note":"\"v\": 5"}` (inside a string),
`{"vv":1}` and `{"V":1}` are all `missing field "v"`. Numeric strings such as `"5"` are not numbers.

### Warnings and errors

All diagnostics go to stderr:

- Every skipped record gets a warning `line N: <reason>`, where N is the 1-based physical line number, for the
  first 10 skipped records only.
- At the end, if any record was skipped: `skipped K invalid record` (K = 1) or `skipped K invalid records`.
  Skipped records never change the exit code.
- Bad arguments: `error: <message>`, then the usage line.
- I/O errors: `error: <message>`, after the skipped count if there is one.

The reasons are `malformed JSON`, `not a JSON object`, `missing field "<f>"`, `field "<f>" is not a number`,
`field "<f>" is out of float64 range` and `line too long (over 64 MiB)`. For example:

```
line 2: malformed JSON
line 3: missing field "v"
skipped 2 invalid records
```

### Output format

- Numbers are printed exactly the way `encoding/json` prints a float64:
  - `strconv.AppendFloat(v, 'f', -1, 64)` (the shortest decimal that round-trips, with no exponent) when
    1e-6 ≤ |v| < 1e21 or v = 0;
  - otherwise the exponent form, cleaned up the same way: `1e-7`, `5e-324`, `1e+21`.
- So typical metrics print as `5`, `4.5` or `1234567`, and extreme values don't print as 300-digit strings.
- Output is buffered (1 MiB). It is flushed at the end, also after a read error, so the output for the records
  before the error is kept.
- When a write fails, the program stops at once instead of reading the rest of the input. If stdout is a pipe
  whose reader has gone away (`… | head -n 1`), the process ends with SIGPIPE, like other Unix tools. Any other
  write error is `error: …` with exit 1.

### Exit codes

| Code | Meaning |
|---|---|
| 0 | success, including when records were skipped |
| 1 | I/O error (stdin, open, read, or write) |
| 2 | bad arguments |

## How it works

A single goroutine handles each record in four steps:

1. **Reading.** A `bufio.Reader` with a 1 MiB buffer returns lines with `ReadSlice`, so lines up to 1 MiB are
   never copied. Longer lines are assembled in a reused side buffer up to the 64 MiB cap. Beyond the cap, the
   rest of the line is read and dropped without storing it, so a huge line can't exhaust memory.
2. **Scanning.** A hand-written scanner makes one pass over the line. It validates the whole line against the
   JSON grammar (RFC 8259) and remembers the value of the last top-level key equal to the field.
   - Keys that are plain ASCII with no backslash are compared as raw bytes. Any other key is decoded the way
     `encoding/json` decodes it (escapes, surrogate pairs, U+FFFD for lone surrogates and invalid UTF-8) and
     compared as it is decoded.
   - Nested objects and arrays are validated and skipped, with nesting capped at 10,000 levels so recursion can't
     overflow the stack.
   - The number's bytes go to `strconv.ParseFloat`.
3. **Tracking.** The value is added to a tracker, which returns the current quantile.
4. **Writing.** The quantile is formatted straight into the free space of a 1 MiB `bufio.Writer`
   (`AvailableBuffer`).

The line reader, the scanner and the formatter are each tested to allocate nothing per record.

### Trackers

| q | Structure | Memory | Time per record |
|---|---|---|---|
| 0 | running minimum | O(1) | O(1) |
| 1 | running maximum | O(1) | O(1) |
| other | two heaps | 8 bytes per value | O(log n) |

For 0 < q < 1, a max-heap `low` holds the k+1 smallest values and a min-heap `high` holds the rest, so x[k] is
the top of `low` and x[k+1] is the top of `high`.

- **Add** pushes the value onto the heap it belongs to, then moves tops from one heap to the other until `low`
  holds exactly k+1 values for the new n. k = floor((n − 1) · q) grows by at most one per record, so that is at
  most one move and Add is O(log n). (Float64 rounding could in principle make k jump by two, so the code loops
  instead of assuming a single move.)
- **Quantile** reads the two tops and interpolates: O(1).
- An exact quantile needs Ω(n) memory in general, because any stored value can become the answer later.
  The heaps store each value once, as a float64.
- The heaps keep their values in fixed-size chunks of 64 KiB (8192 values), not one growing slice. A slice that
  grows copies everything into a bigger array and leaves the old one as garbage; chunks are allocated once and
  never copied, so memory stays about 1× the data. A test checks that 2^20 pushes allocate at most 1.1× the
  bytes stored.
- `container/heap` is not used, because it boxes every value in an interface.

## Numerics

- **h is computed in float64.** `(n − 1) · q` can land just below an integer. For the values 0 … 100 and
  q = 0.29, h = 100 · 0.29 = 28.999999999999996, so k = 28 and f ≈ 1, and the program prints
  `28.999999999999996` where exact arithmetic gives 29. A result can therefore differ from other tools in the
  last digits.
- **Integers above 2^53** are rounded to the nearest float64 when parsed. The quantile is exact for the parsed
  values.
- **Underflow.** A number too small for float64 (`1e-400`) becomes 0, as `strconv.ParseFloat` returns it. The
  smallest subnormal, `5e-324`, is kept.
- **Overflow.** A number beyond float64 (`1e400`) is skipped. In the interpolation, `x[k+1] − x[k]` can only
  overflow for values of opposite sign near ±MaxFloat64; then `(1 − f) · x[k] + f · x[k+1]` is used instead.
- **Negative zero.** `-0` is read as 0, so it is never printed.
- JSON has no NaN or infinity, so every value is finite and the ordering is total.

## Performance

Measurements come in a later iteration.
