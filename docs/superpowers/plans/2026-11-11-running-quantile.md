# Running Quantile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI that prints, for every valid record of a JSON Lines file, the exact running q-quantile of one numeric field. It must be CPU- and memory-efficient up to a 24 GB input.

**Architecture:** One `package main` in a flat folder, single-threaded.

- A buffered line reader feeds a hand-written zero-allocation JSON field scanner.
- Values go into an exact quantile tracker:
  - min or max for q ∈ {0, 1};
  - otherwise a distinct-values mode that switches one way to two chunked heaps.
- Results are formatted like `encoding/json` and written through a large buffered writer.
- The file layout refines spec §7.5 for focus: `heap.go`, `distinct.go` and `pipeline.go` are split out.

**Tech Stack:** Go 1.26 standard library only (`bufio`, `strconv`, `unicode/utf8`, `encoding/json` for test oracles only, `go/parser` for the doc-comment gate, `testing` including native fuzzing).

**Spec:** `docs/superpowers/specs/2026-11-11-running-quantile-design.md` (read it; this plan argues from it).

## Global Constraints

- Go 1.26 standard library only. No third-party modules and no `GOEXPERIMENT`. `go.mod`: `module running-quantile` / `go 1.26`.
- The binary is named `running-quantile`. Usage: `echo /path/file.jsonl | running-quantile <q> <field>`.
- Exit codes: `0` success (even with skipped records), `1` I/O error, `2` bad arguments.
- Stderr formats (exact):
  - per-record warning: `line N: <reason>`;
  - at most 10 warnings;
  - final line `skipped K invalid record` / `skipped K invalid records` when K > 0;
  - errors: `error: <message>`;
  - bad arguments: `error: <message>` followed by the usage line.
- Reasons (exact text):
  - `malformed JSON`
  - `not a JSON object`
  - `missing field "<f>"`
  - `field "<f>" is not a number`
  - `field "<f>" is out of float64 range`
  - `line too long (over 64 MiB)`
- Limits:
  - max line 64 MiB (`64 << 20` bytes of content, excluding the line ending);
  - max JSON nesting depth 10000, where the top-level object is depth 1 (identical to `encoding/json`);
  - max stdin path line 4096 bytes;
  - warnings cap 10.
- **Every function and method** (including test functions and helpers) has a doc comment that starts with its name. This is enforced by `TestEveryFunctionHasDocComment` from Task 1 on.
- **Coverage:** 100% of statements in every function except `main`. This is enforced by `make cover` (Task 14).
- **TDD:** each behaviour is introduced by a failing test, which you run and see fail before implementing.
- **Commits:** one per task, after the task's review passes.
  - The subject is one imperative line (e.g. `Add chunked float heap`).
  - No body, no prefix like `feat:`, and **no `Co-Authored-By` or any AI attribution**.
- Run all commands from the repo root `/Users/volodymyr/vova/clickhouse/running-quantile-v2`.

## Review Focus

These are inputs the spec implies but no table row spells out, most likely first. Each one has a test in the task named.

1. **JSONL content piped on stdin instead of a path** (`cat file.jsonl | running-quantile 0.5 v`), including a multi-megabyte first line with no newline.
   - Expected: exit 1 with a clear error, after reading at most ~4 KiB of stdin. Tests in Task 7 and Task 8.
2. **The consumer stops reading** (`… | head -n 1`, or a failing stdout).
   - Expected: the program stops promptly instead of reading the rest of a huge file. Tests in Task 8.
3. **Field names that need escaping or are non-ASCII** (`lat"ency`, `a\b`, `café`, `😀`).
   - Expected: they match records whose key is written raw or escaped (`"caf\u00e9"`, `"\ud83d\ude00"`). Tests in Task 5.
4. **The field name appearing only nested, inside a string, or as a prefix or suffix of another key**
   (`{"x":{"v":1}}`, `{"note":"\"v\": 5"}`, `{"vv":1}`, `{"v_ms":1}`).
   - Expected: `missing field`, never a false match. Tests in Task 4.
5. **Valid but unusual number spellings** (`1E+2`, `-0.0`, `0e0`, `1e-400`, `9007199254740993`, a 400-digit integer, `5e-324`).
   - Expected: parsed exactly as `strconv.ParseFloat` parses them, and `-0.0` prints as `0`. Tests in Task 4 and Task 6.

---

## Phase 1 — submittable core (Tasks 1–8)

### Task 1: Scaffold, doc-comment gate, Makefile

**Files:**
- Create: `go.mod`, `.gitignore` (`/running-quantile`, `/coverage.out`), `Makefile`, `doc_test.go`

**Interfaces:**
- Produces: `undocumentedFuncs(dir string) ([]string, error)`.
  - It returns `"<file>:<line> <name>"` for every `*ast.FuncDecl` in `dir/*.go`, including `_test.go` and build-tagged files.
  - A function is flagged when its doc comment is missing, or when `Doc.Text()` does not start with `<name> `.
- Produces: `make build test cover bench clean`. Tasks 11 and 13 add `fuzz` and `stress`.

- [ ] **Step 1: Write the failing tests** in `doc_test.go`
  - `TestUndocumentedFuncsFlagsMissingAndMisnamedDocs`
    - Write a temp dir file `x.go` containing `package x`, plus:
      - `func a() {}` with no doc;
      - `// b does b.` + `func b() {}`;
      - `// wrong.` + `func c() {}`;
      - `type T int` + `// m does m.` + `func (T) m() {}`.
    - Assert the result is exactly the entries for `a` and `c` (order by line).
  - `TestEveryFunctionHasDocComment`
    - `undocumentedFuncs(".")` must return an empty slice.
    - On failure, print every entry.
- [ ] **Step 2: Run `go test ./...`.** Expected: build failure, because `undocumentedFuncs` is undefined.
- [ ] **Step 3: Implement `undocumentedFuncs`** with `go/parser.ParseFile(..., parser.ParseComments)` over `filepath.Glob(dir+"/*.go")`.
- [ ] **Step 4: Write the Makefile.** It uses POSIX make only, and `.PHONY` covers every target.
  - `build`: `go build -o running-quantile .`
  - `test`:
    - fail if `gofmt -l .` prints anything;
    - then `go vet ./...` and `go vet -tags stress ./...`;
    - then `go test ./...`.
  - `cover`: `go test -coverprofile=coverage.out ./...`, then `go tool cover -func=coverage.out`. The 100% gate is added in Task 14.
  - `bench`: `go test -run '^$' -bench . -benchmem ./...`
  - `clean`: remove the binary and `coverage.out`.
- [ ] **Step 5: Run `make test`.** Expected: PASS. The test file's own functions must carry docs.
- [ ] **Step 6: Commit** `Add project scaffold and doc-comment gate`.

### Task 2: Chunked float heap

**Files:** Create `heap.go`, `heap_test.go`

**Interfaces:**
- Produces: `type floatHeap struct`, a binary **min**-heap of float64 stored in fixed chunks.
  - Chunks are `[]*[chunkSize]float64`, with `chunkBits = 13`, so 8192 values or 64 KiB per chunk. Task 12 may tune this.
  - The zero value is ready to use.
  - Methods: `len() int`, `top() float64`, `push(v float64)`, `pop() float64`, `appendUnordered(v float64)`, `heapify()`.
  - Callers get a max-heap by storing negated values.

- [ ] **Step 1: Failing tests**
  - `TestFloatHeapPopsInSortedOrder`: 30,000 seeded `rand.NormFloat64()` values (several chunks) are pushed. Popping returns them exactly in `slices.Sorted` order, and `len()` counts down.
  - `TestFloatHeapHeapifyAfterAppendUnordered`: the same values, added with `appendUnordered` and then `heapify()`, pop in sorted order.
  - `TestFloatHeapGrowsWithoutCopying`: pushing `1<<20` values allocates at most `n*8*11/10` bytes. Measure via the `runtime.MemStats.TotalAlloc` delta, with a helper `allocatedBytes(f func()) uint64` in this file.
- [ ] **Step 2: Run `go test -run FloatHeap ./...`.** Expected: FAIL (undefined).
- [ ] **Step 3: Implement `floatHeap`.**
  - Index chunk `i>>chunkBits`, then slot `i&(chunkSize-1)`.
  - Allocate a new chunk only when `n == len(chunks)*chunkSize`.
  - No `container/heap`.
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add chunked float heap`.

### Task 3: Quantile definition and core trackers

**Files:** Create `quantile.go`, `quantile_test.go`

**Interfaces:**
- Consumes: `floatHeap` (Task 2).
- Produces:
  - `type tracker interface { Add(v float64); Quantile() float64 }`. `Quantile` is never called before the first `Add`; document this.
  - `rank(n int, q float64) (k int, f float64)`: `h := float64(n-1)*q; k = floor(h); f = h-k`.
  - `lerp(a, b, f float64) float64`: `a + f*(b-a)`. If `b-a` is ±Inf, it uses `(1-f)*a + f*b` instead.
  - `minTracker`, `maxTracker`: O(1) memory.
  - `heapTracker` with `newHeapTracker(q float64) *heapTracker`.
    - `low` is a max-heap (negated values) holding the k+1 smallest values; `high` holds the rest.
    - On Add, push to `low` if it's empty or `v <= max(low)`, otherwise to `high`.
    - Then rebalance with **loops** (`for low.len() > k+1 …`, `for low.len() < k+1 …`). Never assume k moved by one.
    - Quantile is `-low.top()` when `f == 0`, else `lerp(-low.top(), high.top(), f)`.
  - `newTracker(q float64) tracker`: `0` → `&minTracker{}`, `1` → `&maxTracker{}`, otherwise `newHeapTracker(q)`. Task 10 replaces the last case.
  - Test helper (later tasks use it): `referenceQuantile(seen []float64, q float64) float64` sorts a copy and returns `lerp(s[k], s[k+1], f)`, or `s[k]` when `f == 0`.
  - Test helper: `oracleInputs()`, a map from name to a seeded generator `func(i int) float64` covering:
    - random floats;
    - small ints 0..9;
    - ascending;
    - descending;
    - constant;
    - mixed sign ×1000;
    - alternating ±MaxFloat64.
  - Test helper: `oracleQs = []float64{0, 5e-324, 0.01, 0.25, 0.5, 0.75, 0.99, 0.9999999999999999, 1}`, plus one seeded random q.

- [ ] **Step 1: Failing tests**
  - `TestRank`, with exact cases:
    - `(1, 0.75)` → `(0, 0)`;
    - `(2, 0.75)` → `(0, 0.75)`;
    - `(6, 0.75)` → `(3, 0.75)`;
    - `(101, 0.29)` → `k == 28` and `f > 0.99`;
    - `(5, 1)` → `(4, 0)`.
  - `TestLerp`:
    - `lerp(3, 5, 0.75) == 4.5`;
    - `lerp(-math.MaxFloat64, math.MaxFloat64, 0.5) == 0`;
    - `lerp(2, 2, 0.3) == 2`.
  - `TestHeapTrackerSpecExample`: values `5, 3, 8, 1, 9, 4` at q = 0.75 give exactly `5, 4.5, 6.5, 5.75, 8, 7.25` after each Add.
  - `TestTrackersMatchReference`:
    - For every tracker returned by `newTracker(q)`, and every input × q in the oracle set, 600 Adds.
    - After each Add, `Quantile()` must be **bit-identical** to `referenceQuantile`.
  - `TestMinMaxTrackersUseConstantMemory`: for q ∈ {0, 1}, 1M Adds allocate ≤ 1 KiB.
- [ ] **Step 2: Run `go test ./...`.** Expected: FAIL (undefined).
- [ ] **Step 3: Implement** the items listed under Produces.
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add exact quantile trackers`.

### Task 4: Validating JSON field scanner

**Files:** Create `scan.go`, `scan_test.go`

**Interfaces:**
- Produces:
  - `type reason uint8` with constants `valid`, `malformedJSON`, `notObject`, `missingField`, `notNumber`, `outOfRange`, `lineTooLong`.
  - `(r reason) describe(field string) string`, which returns the exact Global Constraints texts and uses `%q` for the field.
  - `extractNumber(line []byte, field string) (float64, reason)`.
  - `isJSONNumber(s string) bool`. It is true iff the whole of `s` matches `-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?`. Task 8 uses it for q.
  - `const maxDepth = 10000`.

**Algorithm:**
- **Single pass.** Skip JSON whitespace (space, `\t`, `\n`, `\r`).
  - If the first value isn't `{`, validate it as any JSON value. The result is `notObject` if it is valid and the rest is whitespace, otherwise `malformedJSON`.
  - Otherwise, walk the top-level members.
- **Strings.** Reject bytes `< 0x20`. Escapes must be one of `"\/bfnrt` or `u` + 4 hex digits. Other bytes, including invalid UTF-8, pass.
- **Literals.** Exact `true`, `false`, `null`.
- **Nested containers.** Validate them with a depth counter (explicit stack or bounded recursion; builder's choice). Depth `> maxDepth` → `malformedJSON`.
- **Field matching.** In this task a key matches when its raw bytes equal `field` and contain no `\`; Task 5 adds decoding.
  - Remember the **last** matching member's value: its kind (number or not) and its byte range.
- **Precedence.** After the closing `}`, only whitespace may follow. Order of checks:
  - malformed → `malformedJSON`;
  - no match → `missingField`;
  - last match not a number → `notNumber`;
  - then `strconv.ParseFloat(string(b), 64)`; on `ErrRange` → `outOfRange`;
  - normalise `-0` to `0`.
- **Allocations.** None on the valid path for numbers ≤ 32 bytes (measured: `ParseFloat(string(b))` doesn't allocate).

- [ ] **Step 1: Failing tests** (table-driven, `field = "v"` unless noted)
  - `TestExtractNumberValid`:
    - `{"v": 5}` → 5;
    - the PDF line with field `latency_ms` → 5;
    - `  {"v":1}  ` → 1;
    - `{"a":[1,{"b":null}],"v":-2.5,"c":"x"}` → -2.5;
    - `{"v":"x","v":2}` → 2 (last wins);
    - `{"s":"<byte 0xff>","v":5}` → 5 (invalid UTF-8 inside strings is tolerated).
    - Review Focus 5 rows:
      - `1E+2` → 100;
      - `-0.0` → 0, checked with `math.Signbit == false`;
      - `0e0` → 0;
      - `1e-400` → 0;
      - `9007199254740993` → 9007199254740992;
      - a 400-digit integer → `ParseFloat`'s result;
      - `5e-324` → 5e-324.
  - `TestExtractNumberReasons`, one row per spec §6.3 case:
    - `malformedJSON`:
      - `{"v": 5`;
      - `{"v": 5} x`;
      - `{"v": NaN}`, `{"v": Infinity}`;
      - `{"v": 05}`, `{"v": +5}`, `{"v": .5}`, `{"v": 1.}`, `{"v": 1e}`, `{"v": -}`;
      - `{"v": "a<TAB byte>b"}`;
      - `{"v": "\x"}`;
      - `{"v": "\u12"}`;
      - `{"v" 5}`;
      - `{"v": 5,}`;
      - `{,}`;
      - `{"v": tru}`;
      - a NUL byte;
      - an empty line.
    - `notObject`: `[1]`, `5`, `"x"`, `null`.
    - `missingField` (Review Focus 4): `{}`, `{"V": 5}`, `{"x":{"v":1}}`, `{"note":"\"v\": 5"}`, `{"vv":1}`, `{"v_ms":1}`.
    - `notNumber`: `{"v":"5"}`, `{"v":true}`, `{"v":null}`, `{"v":[]}`, `{"v":{}}`, `{"v":2,"v":"x"}`.
    - `outOfRange`: `{"v":1e400}`, `{"v":-1e400}`.
  - `TestExtractNumberDepthLimit`:
    - `{"a":` + 9999×`[` + 9999×`]` + `}` (depth 10000) is valid, so `missingField`.
    - At depth 10001 it is `malformedJSON`.
    - Both verdicts agree with `json.Valid`.
  - `TestReasonDescribe`: every reason's exact text for field `v`. `missingField` with field `a"b` gives `missing field "a\"b"`.
  - `TestIsJSONNumber`:
    - true for `0`, `-0`, `0.75`, `1`, `1e-1`, `1E+2`;
    - false for `""`, `.5`, `+0.5`, `1.`, `05`, `0x1p-2`, `1_0`, `NaN`, `Inf`, ` 0.5`, `0.5 `.
  - `TestExtractNumberDoesNotAllocate`: `testing.AllocsPerRun(1000, …)` on the PDF line is `0`.
- [ ] **Step 2: Run `go test -run 'ExtractNumber|Reason|IsJSONNumber' ./...`.** Expected: FAIL.
- [ ] **Step 3: Implement** per the Algorithm above.
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add validating JSON field scanner`.

### Task 5: encoding/json-compatible key matching and scanner fuzz oracle

**Files:** Modify `scan.go`. Create `scan_fuzz_test.go`.

**Interfaces:**
- Consumes: `extractNumber`, `reason` (Task 4).
- Produces: `keyEquals(raw []byte, field string) bool`. `raw` is the key's bytes between the quotes.
  - **Fast path:** no `\` and all bytes `< 0x80` → `string(raw) == field` (no allocation).
  - **Otherwise** decode incrementally and compare against `field` as you go, with zero allocations, exactly like `encoding/json`'s `unquoteBytes`:
    - simple escapes;
    - `\uXXXX`;
    - a high surrogate followed by a valid `\u` low surrogate → one rune;
    - any other surrogate → U+FFFD, and a following `\u` is decoded on its own;
    - a non-ASCII byte sequence that `utf8.DecodeRune` rejects (`RuneError`, size 1) → U+FFFD.
  - `extractNumber` uses `keyEquals` for every key.
- Produces (test helper): `oracleExtract(line []byte, field string) (float64, reason)`, built from `encoding/json`:
  - `!json.Valid` → `malformedJSON`;
  - `Unmarshal` into `any` is not a `map[string]any` → `notObject`;
  - `Unmarshal` into `map[string]json.RawMessage`; missing key → `missingField`;
  - raw value's first byte isn't `-` or a digit → `notNumber`;
  - `ParseFloat` error → `outOfRange`;
  - otherwise the value, with `-0` → `0`.

- [ ] **Step 1: Failing tests**
  - `TestKeyEquals`, with exact cases:
    - `\u0076` = `v`;
    - `a\"b` = `a"b`;
    - `a\\b` = `a\b`;
    - `caf\u00e9` = `café`; raw `café` = `café`;
    - `\ud83d\ude00` = `😀`;
    - `\ud800` = `"\uFFFD"`;
    - `\ud800\u0076` = `"\uFFFDv"`;
    - `\udc00` = `"\uFFFD"`;
    - raw byte `\xff` = `"\uFFFD"`;
    - `v` ≠ `vv`;
    - `\u0076` ≠ `V`.
  - `TestExtractNumberMatchesEscapedAndUnicodeKeys` (Review Focus 3): fields `lat"ency`, `a\b`, `café`, `😀`, each matched in both raw and escaped spellings.
  - `TestExtractNumberWithEscapedKeyDoesNotAllocate`: AllocsPerRun on `{"\u0076": 5}` is `0`.
  - `FuzzExtractNumber(f, line []byte, field string)`:
    - The result must equal `oracleExtract`: the same reason, and when the reason is `valid`, the same value bits.
    - Seed corpus: every line from Task 4's tables and the cases above. `go test` runs the seeds.
- [ ] **Step 2: Run `go test ./...`.** Expected: FAIL (`keyEquals` is undefined; escaped-key rows fail).
- [ ] **Step 3: Implement `keyEquals`** and wire it into `extractNumber`.
- [ ] **Step 4: Run** `go test ./...`, then `go test -run '^$' -fuzz FuzzExtractNumber -fuzztime 2m .`.
  - Expected: PASS, and no new failing inputs.
  - Any input the fuzzer finds stays in `testdata/fuzz/` as a regression seed: fix the bug, then commit the file.
- [ ] **Step 5: Commit** `Match keys like encoding/json and fuzz the scanner`.

### Task 6: Number formatting

**Files:** Create `format.go`, `format_test.go`

**Interfaces:**
- Produces: `appendFloat(dst []byte, v float64) []byte`. It mirrors `encoding/json`'s float64 encoder:
  - format `'f'` with precision `-1`;
  - format `'e'` when `v != 0` and (`|v| < 1e-6` or `|v| >= 1e21`);
  - then shorten a 2-digit negative exponent (`e-07` → `e-7`).

- [ ] **Step 1: Failing tests**
  - `TestAppendFloatExamples`:
    - `5`→`5`, `4.5`→`4.5`, `7.25`→`7.25`;
    - `1234567`→`1234567`, `-2.5`→`-2.5`, `0`→`0`;
    - `1e-6`→`0.000001`, `1e-7`→`1e-7`;
    - `1e20`→`100000000000000000000`, `1e21`→`1e+21`;
    - `math.MaxFloat64`→`1.7976931348623157e+308`;
    - `5e-324`→`5e-324`;
    - `9007199254740992`→`9007199254740992`.
  - `TestAppendFloatMatchesEncodingJSON`: 200,000 seeded random finite float64 values, from both random bit patterns and `NormFloat64()*10^k`. Each must equal `json.Marshal(v)`.
  - `TestAppendFloatDoesNotAllocate`: AllocsPerRun with a pre-sized dst is `0`.
- [ ] **Step 2: Run `go test -run AppendFloat ./...`.** Expected: FAIL.
- [ ] **Step 3: Implement `appendFloat`.**
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add encoding/json-style number formatting`.

### Task 7: Path and line input

**Files:** Create `input.go`, `input_test.go`

**Interfaces:**
- Produces:
  - `const maxPathBytes = 4096`.
  - `readPath(stdin io.Reader) (string, error)`. It returns the first line of stdin with only a trailing `\n` or `\r\n` removed.
    - It reads at most `maxPathBytes+2` bytes. Use `io.LimitReader`.
    - Errors:
      - `expected a file path on stdin` for an empty stdin or empty first line;
      - `first line of stdin is longer than 4096 bytes; expected a file path` when there is no line end within the limit and the content exceeds 4096 bytes;
      - read errors are wrapped.
  - `var errLineTooLong = errors.New("line too long")`.
  - `newLineReader(r io.Reader, bufSize, maxLine int) *lineReader`, which wraps `bufio.NewReaderSize(r, bufSize)`.
  - `(lr *lineReader) next() ([]byte, error)` returns exactly one physical line per call:
    - `(line, nil)`, with `\n` and one trailing `\r` removed. The slice is valid until the next call;
    - `(nil, errLineTooLong)` when the content exceeds `maxLine`. The rest of that line is consumed **without being stored**;
    - `(nil, io.EOF)` at the end;
    - `(nil, err)` on a read error.
  - `next()` is zero-copy (`ReadSlice`) when a line fits the buffer. Longer lines are assembled in a reused side buffer up to `maxLine`.

- [ ] **Step 1: Failing tests**
  - `TestReadPath`:
    - `"/a/b c.jsonl\n"` → `/a/b c.jsonl` (inner space kept);
    - `"/a\r\n"` → `/a`;
    - `"/a"` (no newline) → `/a`;
    - `"/a\nextra\n"` → `/a`;
    - `" /a \n"` → `" /a "`.
  - `TestReadPathErrors`:
    - `""` and `"\n"` → `expected a file path on stdin`;
    - 1 MiB of `x` with no newline → the "longer than 4096 bytes" error, after reading ≤ 4098 bytes (check with a counting reader). This is Review Focus 1;
    - a reader that fails → the wrapped error.
  - `TestLineReaderSplitsLines`: input `"a\r\nb\n\nc"` with buffer 16 → `a`, `b`, `` (empty), `c`, then `io.EOF`.
  - `TestLineReaderLongLines` (buffer 16, maxLine 64):
    - a 40-byte line is returned intact;
    - a 64-byte line is returned;
    - a 65-byte line → `errLineTooLong`, and the next line is still read correctly;
    - a too-long last line with no newline → `errLineTooLong`, then `io.EOF`.
  - `TestLineReaderTooLongLineIsNotBuffered`: buffer 4 KiB, maxLine 64 KiB, one 32 MiB line followed by `x`. Allocated bytes stay ≤ 256 KiB, and the next line is `x`.
  - `TestLineReaderPropagatesReadError`: a reader that returns data, then `errors.New("boom")` → `next` returns that error.
  - `TestLineReaderDoesNotAllocatePerLine`: AllocsPerRun over 1,000 short lines is `0` per line.
- [ ] **Step 2: Run `go test -run 'ReadPath|LineReader' ./...`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add path and line readers`.

### Task 8: CLI, pipeline, binary tests, README v1

**Files:** Create `main.go`, `pipeline.go`, `main_test.go`, `pipeline_test.go`, `binary_test.go`, `README.md`

**Interfaces:**
- Consumes: `newTracker`, `extractNumber`, `reason.describe`, `isJSONNumber`, `appendFloat`, `readPath`, `newLineReader`, `errLineTooLong`.
- Produces:
  - `const usage = "usage: running-quantile <q in [0, 1]> <field>   (the input file path is read from stdin)"`.
  - `parseArgs(args []string) (q float64, field string, err error)`. It checks, in order:
    - `len != 2` → `expected 2 arguments, got N`;
    - `!isJSONNumber(q)`, a `ParseFloat` error, or q outside [0, 1] → `invalid quantile "<s>": must be a number in [0, 1]`;
    - empty field → `field name must not be empty`;
    - `-0` → `0`.
  - Constants in `pipeline.go`: `maxWarnings = 10`, `readBufSize = 1 << 20`, `writeBufSize = 1 << 20`, `maxLineBytes = 64 << 20`.
  - `process(in io.Reader, q float64, field string, out *bufio.Writer, stderr io.Writer) (skipped int, err error)`. Per physical line, numbered from 1:
    - strip the UTF-8 BOM on line 1;
    - ignore lines that contain only whitespace (space, tab, CR);
    - `errLineTooLong` → reason `lineTooLong`;
    - an invalid record → `skipped++`, and a warning while `skipped <= maxWarnings`;
    - a valid record → `Add`, then format into `out.AvailableBuffer()` and write. Flush first if fewer than 32 bytes are available, so the append never reallocates;
    - stop on the **first** write error;
    - a read error → `reading line N: <err>`.
  - `run(args []string, stdin io.Reader, stdout, stderr io.Writer) int`: parse arguments (exit 2), `readPath`, `os.Open` (exit 1), `process`, flush, the summary line, `error:` (exit 1).
  - `main()` is `os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))`.
  - Test helpers in `main_test.go`:
    - `const specExample` (the 6 PDF lines);
    - `type result struct{ code int; stdout, stderr string }`;
    - `runOnContent(t, content string, args ...string) result` writes a temp file and passes its path on stdin;
    - `runWithStdin(stdin string, args ...string) result`.

- [ ] **Step 1: Failing tests**
  - `TestRunSpecExample`: `0.75 latency_ms` → `{0, "5\n4.5\n6.5\n5.75\n8\n7.25\n", ""}`.
  - `TestParseArgs`: one row per spec §6.2 case, plus accepted `0`, `1`, `-0` (→ 0, no signbit), `1e-1`, `0.75`.
  - `TestRunRejectsBadArguments`: every §6.2 reject row → exit 2, empty stdout, stderr contains `error:` and `usage:`. The file is not read: pass an `io.Reader` that fails the test if it is read.
  - `TestRunSkipsInvalidRecords`, with content `{"v":1}` / `<bad line>` / `{"v":3}` at q = 0.5:
    - stdout is `1\n2\n` and the exit code is 0;
    - stderr contains `line 2: <exact reason>` and `skipped 1 invalid record` (singular);
    - one row per reason, including a 64 MiB + 1 byte line for `line too long (over 64 MiB)`.
  - `TestRunCapsWarnings`: 15 lines of `{}` then `{"v":7}` → warnings only for lines 1–10, then `skipped 15 invalid records`, and stdout is `7\n`.
  - `TestRunAcceptsInputVariants`:
    - CRLF;
    - a BOM;
    - no final newline;
    - blank and whitespace-only lines, which are neither counted nor warned about;
    - an empty file → `{0, "", ""}`;
    - only invalid records (`{}`) → exit 0, empty stdout, stderr `line 1: missing field "v"\nskipped 1 invalid record\n`;
    - a valid line over 64 KiB.
  - `TestRunInputErrors`, all exit 1 with `error:`:
    - empty stdin;
    - a blank path;
    - a nonexistent file;
    - a directory;
    - an unreadable file (chmod 000; skip as root);
    - the JSONL content itself on stdin (Review Focus 1).
  - `TestRunAcceptsRelativePath`: `t.Chdir(dir)` with stdin `input.jsonl\n`.
  - `TestProcessReadErrorMidFile` calls `process` directly with a reader that returns 2 valid lines, then an error:
    - after a flush, stdout holds the 2 outputs;
    - the error is `reading line 3: boom`.
    - `run`'s exit-1 path for read errors is covered by the directory row of `TestRunInputErrors`.
  - `TestProcessStopsOnWriteError` (Review Focus 2):
    - **Setup.** Call `process` with a `bufio.NewWriterSize(failingWriter, writeBufSize)`. The writer fails on its first write.
    - **Input.** 1M lines `{"v":<1000000.5+i>}` (~20 MB) through a counting reader, at q = 1.
    - **Expect:**
      - the write error is returned;
      - fewer than 8 MiB of input was read. About 100k records fill the 1 MiB output buffer before the first write.
  - `TestRunReportsWriteError`: `run` with a stdout that always fails → exit 1, and stderr contains `error:`.
  - `binary_test.go`:
    - `TestMain` builds the binary once (`go build -o <tmp>/running-quantile .`) into the package variable `binaryPath string`, runs `m.Run()`, then removes the temp dir.
    - `TestBinarySpecExample`: real stdin, exit 0, exact output.
    - `TestBinaryExitCodes`: bad q → 2, missing file → 1.
    - `TestBinaryStopsWhenStdoutClosed` (Review Focus 2):
      - input: 3M lines `{"v":<i>}`, ~30 MB, which produce over 2 MiB of output;
      - read 1 line from `StdoutPipe`, then close it;
      - the process must exit within 10 s, with any status.
- [ ] **Step 2: Run `go test ./...`.** Expected: FAIL (undefined).
- [ ] **Step 3: Implement** `main.go` (`main`, `run`, `parseArgs`, `usage`) and `pipeline.go` (`process` and its constants).
- [ ] **Step 4: Run `make test`.** Expected: PASS, including the doc-comment gate.
- [ ] **Step 5: Write `README.md` v1.** It covers:
  - build, run and test commands (Makefile targets);
  - the quantile definition and why it's type 7, using the PDF example;
  - the input-handling tables, exit codes and output format, copied from spec §6;
  - the tracker design (min/max and heaps) with complexity;
  - numerics notes: `h` rounding, 2^53, underflow;
  - an empty "Performance" section, to be filled in Task 14.
- [ ] **Step 6: Smoke test.**
  - Run `make build && printf '%s\n' "$PWD/<tmp spec file>" | ./running-quantile 0.75 latency_ms`.
  - Expected output: `5 4.5 6.5 5.75 8 7.25`, one per line.
- [ ] **Step 7: Commit** `Add CLI, pipeline and README`.

## Phase 2 — memory, hardening, scale (Tasks 9–14)

### Task 9: Distinct-values tracker

**Files:** Create `distinct.go`, `distinct_test.go`

**Interfaces:**
- Consumes: `floatHeap`, `rank`, `lerp`, `heapTracker`, `newHeapTracker`, `oracleInputs`, `oracleQs`, `allocatedBytes`.
- Produces: `distinctTracker` and `newDistinctTracker(q float64) *distinctTracker`.
  - Methods: `Add`, `Quantile`, `distinct() int`, `toHeaps() *heapTracker`.
  - Field `n int` (record count), used by Task 10.

**Algorithm:**

State:
- `counts map[float64]int`;
- `lower` (a max-heap via negation) holding distinct values `< cur`;
- `upper` (a min-heap) holding distinct values `> cur`;
- `cur`, `curCount`, and `below` (the number of values `< cur`). The copies of `cur` hold ranks `below … below+curCount-1`.

```
Add(v): n++; c := counts[v]; counts[v] = c+1
  n == 1        → cur, curCount = v, 1; return
  v < cur       → below++; if c == 0 { lower.push(-v) }
  v > cur       → if c == 0 { upper.push(v) }
  v == cur      → curCount++
  k := rank(n, q).k
  for below > k            { upper.push(cur); cur = -lower.pop(); curCount = counts[cur]; below -= curCount }
  for below+curCount <= k  { lower.push(-cur); below += curCount; cur = upper.pop(); curCount = counts[cur] }
Quantile(): k, f := rank(n, q); f == 0 → cur; b := cur if k+1 < below+curCount else upper.top(); lerp(cur, b, f)
toHeaps(): per (v, c):
  inLow = c        if v < cur
  inLow = k+1-below if v == cur
  inLow = 0        if v > cur
  inLow copies of -v → low.appendUnordered; c-inLow copies of v → high.appendUnordered; heapify both
```

- [ ] **Step 1: Failing tests**
  - `TestDistinctMatchesHeapsBitForBit`:
    - inputs: `oracleInputs`, plus "few values in long runs" (`(i/50)%3`) and "duplicates, then all new" (`i<500` → `Intn(10)`, else `i+0.5`);
    - every value in `oracleQs`, 3,000 Adds;
    - `Quantile()` is bit-identical to `heapTracker` after every Add.
  - `TestDistinctToHeapsPreservesState`: at several points (including while the copies of `cur` straddle x[k] and x[k+1]: 600×`0`, 600×`1`, q = 0.5), `toHeaps()` continued with the same further values matches a fresh `heapTracker` bit for bit.
  - `TestDistinctMemoryFollowsDistinctValues`: 1M Adds of `i%100` allocate ≤ 256 KiB.
- [ ] **Step 2: Run `go test -run Distinct ./...`.** Expected: FAIL.
- [ ] **Step 3: Implement** per the Algorithm above.
- [ ] **Step 4: Run `go test ./...`.** Expected: PASS.
- [ ] **Step 5: Commit** `Add distinct-values quantile tracker`.

### Task 10: Adaptive tracker wired into `newTracker`

**Files:** Modify `distinct.go`, `quantile.go:newTracker`, and `README.md`. Test: `distinct_test.go`.

**Interfaces:**
- Produces:
  - `const distinctFloor = 1 << 16`.
  - `const bytesPerDistinct = <measured in Step 3>` (an int).
  - `adaptiveTracker` and `newAdaptiveTracker(q float64, floor int) *adaptiveTracker`, with method `switched() bool`.
  - After each `Add` in distinct mode, it switches to `d.toHeaps()` once `d.distinct() > floor && 8*d.n <= bytesPerDistinct*d.distinct()`. It never switches back.
  - `newTracker(q)` now returns `newAdaptiveTracker(q, distinctFloor)` for 0 < q < 1.

- [ ] **Step 1: Failing tests**
  - `TestAdaptiveMatchesHeapsBitForBit`: the same matrix as Task 9, using `newAdaptiveTracker(q, 64)`.
  - `TestAdaptiveSwitchesOnHighCardinality`: 1,000 distinct values with floor 64 → `switched()`.
  - `TestAdaptiveStaysDistinctOnLowCardinality`: 100k Adds of `i%50` → not switched.
  - `TestAdaptiveStaysDistinctWhenHeapsWouldBeBigger`: 10k Adds of `i%64`, then 100 new values → not switched.
  - `TestAdaptiveSwitchSplitsCursorCopies`: 600×0 and 600×1 alternating, then new values `i`, q = 0.5, floor 64. It matches heaps bit for bit after every Add, and `switched()` at the end.
  - `TestNewTrackerUsesAdaptive`: `newTracker(0.5)` is a `*adaptiveTracker`.
  - Existing `TestTrackersMatchReference` still passes.
- [ ] **Step 2: Run `go test ./...`.** Expected: FAIL.
- [ ] **Step 3: Measure `bytesPerDistinct`.**
  - Add a throwaway test, or benchmark with `-run`, that Adds 1M distinct values to a `distinctTracker` and reports `allocatedBytes / 1e6`. Round up.
  - Record the figure and the method in a comment on the constant and in the README.
- [ ] **Step 4: Implement**, then run `go test ./...`. Expected: PASS.
- [ ] **Step 5: README.** Explain the distinct mode, the switch rule, and the brief ~2× memory peak at the switch.
- [ ] **Step 6: Commit** `Switch adaptively between distinct and heap trackers`.

### Task 11: Tracker and pipeline fuzzing

**Files:** Create `fuzz_test.go`. Modify `Makefile`.

**Interfaces:** Consumes `newTracker`, `newHeapTracker`, `referenceQuantile`, `process`, `extractNumber`.

- [ ] **Step 1: Write the fuzz targets.**
  - `FuzzTrackers(f, data []byte, q float64)`:
    - skip if q is not in [0, 1] or is NaN;
    - decode `data` into int16-derived float64 values. Small ints force duplicates;
    - `newAdaptiveTracker(q, 8)`, `newTracker(q)` and `newHeapTracker(q)` all equal `referenceQuantile` bit for bit after every Add.
  - `FuzzProcess(f, content []byte, field string)`:
    - `process` at q = 0.5 never panics and returns a nil error;
    - the stdout line count equals the number of lines where `extractNumber` returns `valid` (BOM and blank rules applied);
    - every output line parses with `strconv.ParseFloat`.
  - Seeds: the spec example, and one line per reason.
- [ ] **Step 2: Run them.**
  - Run `go test ./...` (seeds), then `go test -run '^$' -fuzz FuzzTrackers -fuzztime 2m .` and the same for `FuzzProcess`.
  - Expected: PASS. Any found inputs are fixed and committed under `testdata/fuzz/`.
- [ ] **Step 3: Makefile `fuzz` target.** It runs each of `FuzzExtractNumber`, `FuzzTrackers` and `FuzzProcess` for `$(FUZZTIME)`, which defaults to `60s`. `go test -fuzz` accepts one target per run.
- [ ] **Step 4: Commit** `Add tracker and pipeline fuzz tests`.

### Task 12: Performance pass

**Files:** Create `bench_test.go`. Tune constants in `pipeline.go`, `heap.go` and `scan.go`. Modify `README.md`.

**Interfaces:** Consumes everything above. Produces no new API.

- [ ] **Step 1: Write benchmarks**, all with `b.ReportAllocs` and, where meaningful, `b.SetBytes`:
  - `BenchmarkExtractNumber` (PDF line; escaped-key line);
  - `BenchmarkAppendFloat`;
  - `BenchmarkTrackerAdd`, with sub-benchmarks for heaps, distinct and adaptive, each on random floats and on ints 0..999;
  - `BenchmarkProcess`, over 1M generated PDF-shaped lines from a `bytes.Reader`, reporting MB/s.
- [ ] **Step 2: Write a failing-or-passing gate.** `TestProcessAllocationsDoNotGrowWithRecords`:
  - `process` over 1k and over 101k low-cardinality lines (`latency_ms` in 0..99) from memory;
  - the difference in `runtime.MemStats.Mallocs` between the two runs is ≤ 10.
- [ ] **Step 3: Measure and tune**, one change at a time, keeping a change only if `make bench` improves:
  - `readBufSize` / `writeBufSize` ∈ {64 KiB, 256 KiB, 1 MiB, 4 MiB};
  - `chunkBits` ∈ {12, 13, 14, 16};
  - an integer fast path in `extractNumber` (≤ 15 digits, no fraction or exponent; exact conversion), kept only if faster;
  - targets: `BenchmarkExtractNumber` ≤ ~150 ns/op with 0 allocs on the PDF line, and no tracker regressions.
- [ ] **Step 4: Run `make test`.** Expected: PASS.
- [ ] **Step 5: README.** Add a "Performance" table of the measured numbers on the M4 (before and after each tuning change), and record the chosen constants.
- [ ] **Step 6: Commit** `Tune buffers and scanner for throughput`.

### Task 13: 24 GB stress test

**Files:**
- Create: `stress_test.go` (`//go:build stress`)
- Create: `rss_darwin_test.go`, `rss_linux_test.go`, `rss_other_test.go` (each `//go:build stress`; the last also `&& !darwin && !linux`)
- Modify: `Makefile`

**Interfaces:**
- Consumes: `rank`, `lerp`, `appendFloat`, and `binaryPath` (set by `TestMain`, Task 8).
- Produces:
  - `peakRSSBytes(ps *os.ProcessState) (bytes int64, ok bool)`. `Maxrss` is in bytes on darwin and ×1024 on linux; `ok=false` elsewhere.
  - `make stress`: `go test -tags stress -run TestStress -timeout 90m -v .`.

**Behaviour of `TestStress24GB`:**
- **Size.** `RQ_STRESS_BYTES` sets the size: default `24000000000`, maximum `24000000000`. A larger value is a test failure. A small value allows a quick smoke run.
- **Generator.** Seeded PCG writes to `t.TempDir()`/`stress.jsonl` through a 4 MiB `bufio.Writer`. It stops before the next line would exceed the size, and asserts the final size is ≤ the limit.
- **Records** have the PDF shape:
  - timestamp incrementing by 1 s, customer, region, endpoint;
  - `latency_ms = int(exp(3 + 0.8·N(0,1)))`;
  - `bytes_sent` uniform in 100..20000;
  - plus `duration_us = Float64()·1e6`, printed with `'f', -1` (nearly all distinct).
- **Invalid lines.** Every 1000th line is invalid, cycling through:
  - truncated JSON;
  - `[1,2,3]`;
  - a record without either field;
  - both fields as strings;
  - both fields `1e400`.
  - Every 997th line is blank.
  - Every invalid line is invalid for both fields.
- **Expected values.** Collect them while generating:
  - a count histogram for `latency_ms`;
  - `[]float64` for `duration_us`;
  - the invalid count.
- **Two binary runs:**
  - `0.99 latency_ms`;
  - `0.5 duration_us`.
- **Stdout** is streamed through a `bufio.Scanner` with a counter, capturing lines at valid-record checkpoints {1, 1e6, 1e7, 1e8, last} (those ≤ count).
- **Assertions:**
  - exit 0;
  - line count = valid count;
  - stderr ends with `skipped <invalid> invalid records`;
  - each checkpoint line equals `appendFloat` of the reference:
    - histogram walk for `latency_ms`;
    - sort a copy of the prefix, then `rank` and `lerp`, for `duration_us`;
  - peak RSS ≤ 64 MiB for `latency_ms`, and ≤ `8·n·1.25 + 64 MiB` for `duration_us`. Adjust these bounds by measurement and document them.
- **Logging.** Log generation time, run time, MB/s and peak RSS per run.

- [ ] **Step 1: Write a smoke version.** Write `TestStress24GB` and the RSS helpers. Run `RQ_STRESS_BYTES=200000000 make stress`.
  - Expected: PASS within about a minute.
  - Fix the generator and the checks until this passes.
- [ ] **Step 2: Full run (first of two).** Run `make stress` with the default 24 GB.
  - Expected: PASS.
  - Record the logged numbers for the README.
- [ ] **Step 3: Run `make test`.** Expected: PASS. The default suite does not run the stress test, and the doc gate covers the stress files.
- [ ] **Step 4: Commit** `Add on-demand 24 GB stress test`.

### Task 14: Coverage gate, Linux run, final numbers and README

**Files:** Modify `Makefile` (`cover` gate) and `README.md`.

- [ ] **Step 1: Coverage gate.** Make `cover` fail when any line of `go tool cover -func=coverage.out` other than `total:` and the `main` function is below `100.0%`. Use POSIX shell with `grep -v`. Run `make cover`.
  - Expected: PASS.
  - If a statement is uncovered, add a test. Don't delete code just to reach 100%, unless that code is truly unreachable, and say so in review.
- [ ] **Step 2: Linux.**
  - Run `docker run --rm -v "$PWD":/src -w /src golang:1.26 make test`.
  - Run the same with `--platform linux/amd64`.
  - Expected: PASS on both.
- [ ] **Step 3: Full stress run (second of two).** Run `make stress`. Expected: PASS.
- [ ] **Step 4: Final README.** Add:
  - the final performance and memory table (M4: 24 GB, both runs; time, MB/s, peak RSS);
  - where time goes (benchmarks);
  - "Testing" (what each suite covers and how to run it);
  - "Possible extensions":
    - approximate quantiles (DDSketch-style relative error, or KLL/GK rank error, with their bounds);
    - a multi-core parse pipeline.
  - Check every README claim against a measurement or a test.
- [ ] **Step 5: Run the gates.** Run `make test && make cover && make build`. Expected: all PASS.
- [ ] **Step 6: Commit** `Add coverage gate and final measurements`.
