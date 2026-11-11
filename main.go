package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
)

// usage is printed after an argument error.
const usage = "usage: running-quantile <q in [0, 1]> <field>   (the input file path is read from stdin)"

// main runs the program on the process's arguments and standard streams and exits with its exit code.
func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run reads the path of a JSON Lines file from stdin and writes the running q-quantile of field to stdout.
// It returns the exit code: 0 on success, even with skipped records; 1 on an I/O error; 2 on bad arguments.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	q, field, err := parseArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n%s\n", err, usage)
		return 2
	}
	path, err := readPath(stdin)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	defer f.Close()
	out := bufio.NewWriterSize(stdout, writeBufSize)
	skipped, err := process(f, q, field, out, stderr)
	if flushErr := out.Flush(); err == nil {
		err = flushErr
	}
	if skipped > 0 {
		noun := "records"
		if skipped == 1 {
			noun = "record"
		}
		fmt.Fprintf(stderr, "skipped %d invalid %s\n", skipped, noun)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// parseArgs returns the quantile and the field name given as the two arguments, or an error for the first problem.
// q must be a JSON number in [0, 1]; -0 becomes 0.
func parseArgs(args []string) (q float64, field string, err error) {
	if len(args) != 2 {
		return 0, "", fmt.Errorf("expected 2 arguments, got %d", len(args))
	}
	q, err = strconv.ParseFloat(args[0], 64)
	if !isJSONNumber(args[0]) || err != nil || q < 0 || q > 1 {
		return 0, "", fmt.Errorf("invalid quantile %q: must be a number in [0, 1]", args[0])
	}
	if args[1] == "" {
		return 0, "", errors.New("field name must not be empty")
	}
	if q == 0 {
		q = 0 // turns -0 into 0
	}
	return q, args[1], nil
}
