package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFile is a saved `go test -bench` output, in the exact shape the command
// writes: the header lines it must ignore, two -count runs per arm (so the
// median is a real median), and the GOMAXPROCS suffix at two different values
// so the stripping is exercised rather than assumed.
const runFile = `goos: linux
goarch: amd64
pkg: example/arm
cpu: AMD Ryzen 7 6800HS with Radeon Graphics
BenchmarkCall/go_direct-16   8833557   6.00 ns/op   0 B/op   0 allocs/op
BenchmarkCall/go_direct-16   8833557   8.00 ns/op   0 B/op   0 allocs/op
BenchmarkCall/cppgo-1        1456125   42.00 ns/op   0 B/op   0 allocs/op
BenchmarkCall/cppgo-1        1456125   46.00 ns/op   9 B/op   1 allocs/op
PASS
ok  	example/arm	0.800s
`

// writeRun saves runFile (or text) under a temp dir and returns its path.
func writeRun(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// TestReadFileAlignsArmsAndCountsRuns pins the parser: the GOMAXPROCS suffix is
// machine-specific and must not reach the table, and every -count run is kept
// so the median is taken over them.
func TestReadFileAlignsArmsAndCountsRuns(t *testing.T) {
	groups := map[string]map[string]*stats{}
	n, err := readFile(writeRun(t, "arm.txt", runFile), "cppgo", groups)
	if err != nil {
		t.Fatalf("readFile: %v", err)
	}
	if want := 4; n != want {
		t.Fatalf("readFile counted %d benchmark lines, want %d (the four Benchmark lines; goos:, pkg:, cpu:, PASS and ok are not benchmarks)", n, want)
	}
	call, ok := groups["BenchmarkCall"]
	if !ok {
		t.Fatalf("no BenchmarkCall group; got groups %v", keys(groups))
	}
	direct := call["go_direct"]
	if direct == nil {
		t.Fatalf("go_direct arm missing; the -16 suffix was not stripped (%v)", keys(call))
	}
	if want := 2; len(direct.ns) != want {
		t.Fatalf("go_direct kept %d runs, want %d (both -count runs must be kept for the median)", len(direct.ns), want)
	}
	if got, want := median(direct.ns), 7.0; got != want {
		t.Fatalf("go_direct median ns/op = %v, want %v (the midpoint of 6.00 and 8.00)", got, want)
	}
	// The second cppgo run is the one with an allocation; with two runs the
	// median is the midpoint of 0 and 9 B/op, and of 0 and 1 alloc/op.
	cppgo := call["cppgo"]
	if got, want := median(cppgo.bytes), 4.5; got != want {
		t.Fatalf("cppgo median B/op = %v, want %v (the midpoint of 0 and 9)", got, want)
	}
	if got, want := median(cppgo.allocs), 0.5; got != want {
		t.Fatalf("cppgo median allocs/op = %v, want %v (the midpoint of 0 and 1)", got, want)
	}
}

// TestReadFileRejectsAnEmptyRun is the guard that matters most: a file with no
// benchmark lines is a command that measured nothing, and it must fail instead
// of producing an empty column in the table.
func TestReadFileRejectsAnEmptyRun(t *testing.T) {
	path := writeRun(t, "empty.txt", "goos: linux\nPASS\nok  example/arm  0.1s\n")
	if _, err := readFile(path, "arm", map[string]map[string]*stats{}); err == nil {
		t.Fatal("readFile accepted a run with no benchmark lines; an arm with no measurements would vanish from the table")
	}
}

// TestMedian is the aggregation the table is built on. Half of these values
// are odd-sized so the two branches are both exercised.
func TestMedian(t *testing.T) {
	cases := []struct {
		name   string
		values []float64
		want   float64
		source string
	}{
		{"single", []float64{42}, 42, "the only run"},
		{"odd", []float64{8, 2, 5}, 5, "the middle of 2, 5, 8"},
		{"even", []float64{8, 2, 6, 4}, 5, "the midpoint of 4 and 6"},
		{"unsorted", []float64{100, 1, 10}, 10, "the middle of 1, 10, 100, after sorting"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := median(tc.values); got != tc.want {
				t.Fatalf("median(%v) = %v, want %v (%s)", tc.values, got, tc.want, tc.source)
			}
		})
	}
}

// TestRewriteKeepsProseAndIsIdempotent: the README's prose is hand-written and
// must survive, and a second write of the same run must produce the same bytes
// so `make table` never churns the file.
func TestRewriteKeepsProseAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "README.md")
	original := "intro\n\n" + beginTable + "\nold\n" + endTable + "\n\ntail\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	if err := rewrite(path, beginTable, endTable, "### new\n"); err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	once, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the rewritten file: %v", err)
	}
	for _, want := range []string{"intro\n", "### new", "\ntail\n"} {
		if !strings.Contains(string(once), want) {
			t.Fatalf("rewritten README lost %q:\n%s", want, once)
		}
	}
	if strings.Contains(string(once), "old") {
		t.Fatalf("rewritten README still carries the previous block:\n%s", once)
	}
	if err := rewrite(path, beginTable, endTable, "### new\n"); err != nil {
		t.Fatalf("second rewrite: %v", err)
	}
	twice, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the file after the second rewrite: %v", err)
	}
	if string(twice) != string(once) {
		t.Fatalf("a second rewrite changed the file; the command is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
	}
}

// TestRewriteRefusesMissingMarkers: a README whose markers were edited away is
// not one this command may guess its way through.
func TestRewriteRefusesMissingMarkers(t *testing.T) {
	path := writeRun(t, "README.md", "no markers here\n")
	if err := rewrite(path, beginTable, endTable, "x"); err == nil {
		t.Fatal("rewrite accepted a file without the BEGIN marker")
	}
}

// TestRenderTableRatio is the comparison column: a ratio against go_direct
// when the group has that arm, and an explicit "—" when it does not.
func TestRenderTableRatio(t *testing.T) {
	groups := map[string]map[string]*stats{
		"BenchmarkCall": {
			"go_direct": {ns: []float64{10}},
			"cgo":       {ns: []float64{100}},
		},
		"BenchmarkReentrant": {
			"cgo": {ns: []float64{400}},
		},
	}
	out := renderTable(groups)
	if !strings.Contains(out, "| `cgo` | 100.0 | 100.0–100.0 | — | — | 10.00× |") {
		t.Fatalf("the cgo row did not report 100 ns/op against a 10 ns/op go_direct baseline as 10.00×:\n%s", out)
	}
	if !strings.Contains(out, "| `cgo` | 400.0 | 400.0–400.0 | — | — | — |") {
		t.Fatalf("a group without go_direct did not report an em dash in the ratio column:\n%s", out)
	}
	if !strings.Contains(out, "### `BenchmarkCall`") || !strings.Contains(out, "### `BenchmarkReentrant`") {
		t.Fatalf("a group is missing its heading:\n%s", out)
	}
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
