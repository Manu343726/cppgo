package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/gen"
)

// header is the fixture header, relative to this package's directory.
var header = filepath.Join("..", "..", "internal", "gen", "testdata", "mathx", "vector.hpp")

// exec runs the CLI and returns its exit code and streams.
func exec(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// mustExec runs the CLI and requires success.
func mustExec(t *testing.T, args ...string) (string, string) {
	t.Helper()
	code, stdout, stderr := exec(args...)
	require.Equal(t, exitOK, code, "cppgo %v: %s", args, stderr)
	return stdout, stderr
}

// manifest is the minimal command line for the mathx fixture.
func manifest(out string) []string {
	return []string{"gen", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out}
}

func TestRunNoArguments(t *testing.T) {
	code, stdout, stderr := exec()
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "missing command")
	assert.Contains(t, stderr, "usage:")
}

func TestRunUnknownCommand(t *testing.T) {
	code, stdout, stderr := exec("frobnicate")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unknown command "frobnicate"`)
	assert.Contains(t, stderr, "usage:")
}

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"version", "-v", "--version"} {
		t.Run(arg, func(t *testing.T) {
			code, stdout, stderr := exec(arg)
			assert.Equal(t, exitOK, code)
			assert.Equal(t, "cppgo "+version+"\n", stdout)
			assert.Empty(t, stderr)
		})
	}
}

func TestRunVersionFlagValue(t *testing.T) {
	old := version
	version = "v9.9.9"
	t.Cleanup(func() { version = old })

	code, stdout, _ := exec("version")
	assert.Equal(t, exitOK, code)
	assert.Equal(t, "cppgo v9.9.9\n", stdout)
}

func TestRunHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"-h"}, {"--help"}} {
		t.Run(args[0], func(t *testing.T) {
			code, stdout, stderr := exec(args...)
			assert.Equal(t, exitOK, code)
			assert.Contains(t, stdout, "usage:")
			assert.Contains(t, stdout, "cppgo gen")
			assert.Empty(t, stderr)
		})
	}
}

func TestRunHelpTopics(t *testing.T) {
	cases := map[string]string{
		"gen":     "usage: cppgo gen",
		"check":   "usage: cppgo check",
		"version": "usage: cppgo version",
		"help":    "usage:",
	}
	for topic, want := range cases {
		t.Run(topic, func(t *testing.T) {
			code, stdout, stderr := exec("help", topic)
			assert.Equal(t, exitOK, code)
			assert.Contains(t, stdout, want)
			assert.Empty(t, stderr)
		})
	}
}

func TestRunHelpUnknownTopic(t *testing.T) {
	code, stdout, stderr := exec("help", "nonsense")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unknown command "nonsense"`)
}

func TestGenHelpFlag(t *testing.T) {
	for _, args := range [][]string{{"gen", "-h"}, {"gen", "--help"}, {"check", "-h"}} {
		t.Run(args[0]+args[1], func(t *testing.T) {
			code, stdout, stderr := exec(args...)
			assert.Equal(t, exitOK, code)
			assert.Contains(t, stdout, "usage: cppgo "+args[0])
			assert.Empty(t, stderr)
		})
	}
}

func TestGenWritesFiles(t *testing.T) {
	out := t.TempDir()
	stdout, stderr := mustExec(t, manifest(out)...)
	assert.Contains(t, stdout, "thunk", "the report names the carriers")
	// The fixture has no compilation database, so the generator warns that the
	// flags are an assumption. A warning is not a failure.
	assert.NotContains(t, stderr, "cppgo gen:")

	for _, name := range []string{gen.GenFileName, gen.AsmFileName, gen.ShimHeaderFileName, gen.ShimSourceFileName} {
		data, err := os.ReadFile(filepath.Join(out, name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, data, name)
	}
}

func TestGenQuiet(t *testing.T) {
	out := t.TempDir()
	args := append(manifest(out), "-q")
	stdout, stderr := mustExec(t, args...)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "cppgo gen:")
}

func TestCheckUpToDate(t *testing.T) {
	out := t.TempDir()
	require.Equal(t, exitOK, mustGen(t, manifest(out)...))

	code, stdout, stderr := exec("check", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out)
	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "generated files are up to date")
	assert.NotContains(t, stderr, "cppgo check:")
}

func TestCheckStale(t *testing.T) {
	out := t.TempDir()
	require.Equal(t, exitOK, mustGen(t, manifest(out)...))
	require.NoError(t, os.WriteFile(filepath.Join(out, gen.GenFileName), []byte("// tampered\n"), 0o644))

	code, stdout, stderr := exec("check", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out)
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "out of date")
	assert.Contains(t, stderr, gen.GenFileName)
}

func TestCheckMissingFiles(t *testing.T) {
	code, _, stderr := exec("check", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", t.TempDir())
	assert.Equal(t, exitError, code)
	assert.Contains(t, stderr, "out of date")
}

func TestCheckQuiet(t *testing.T) {
	out := t.TempDir()
	require.Equal(t, exitOK, mustGen(t, manifest(out)...))

	code, stdout, stderr := exec("check", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out, "-q")
	assert.Equal(t, exitOK, code)
	assert.Empty(t, stdout)
	assert.NotContains(t, stderr, "cppgo check:")
}

func TestGenCheckFlagEqualsCheckCommand(t *testing.T) {
	out := t.TempDir()
	require.Equal(t, exitOK, mustGen(t, manifest(out)...))

	args := append(manifest(out), "-check")
	code, stdout, _ := exec(args...)
	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "up to date")
}

func TestGenManifestErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing package", []string{"gen", "-header", header, "-library", "l"}, "not a valid Go package name"},
		{"missing library", []string{"gen", "-header", header, "-package", "p"}, "-library is required"},
		{"missing header file", []string{"gen", "-header", "nope.hpp", "-package", "p", "-library", "l"}, "nope.hpp"},
		{"unmatched bind", []string{"gen", "-header", header, "-package", "p", "-library", "l", "-bind", "mathx::Nope"}, "names nothing"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := exec(tc.args...)
			assert.Equal(t, exitError, code)
			assert.Empty(t, stdout)
			assert.Contains(t, stderr, tc.want)
		})
	}
}

func TestGenMissingHeader(t *testing.T) {
	code, stdout, stderr := exec("gen", "-package", "p", "-library", "l")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "-header is required")
}

func TestGenEmptyOut(t *testing.T) {
	code, stdout, stderr := exec("gen", "-header", header, "-package", "p", "-library", "l", "-out", "")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "-out must not be empty")
}

func TestGenBindFiltersTheReport(t *testing.T) {
	out := t.TempDir()
	stdout, _ := mustExec(t, "gen", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out, "-bind", "mathx::Vector")
	assert.Contains(t, stdout, "mathx::Vector::norm")
	assert.NotContains(t, stdout, "mathx::saxpy")
}

func TestGenRename(t *testing.T) {
	out := t.TempDir()
	mustExec(t, "gen", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", out,
		"-rename", "mathx::Vector::norm=Norm2")
	data, err := os.ReadFile(filepath.Join(out, gen.GenFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), "func (v *Vector) Norm2() (float64, error)")
}

func TestGenUnknownFlag(t *testing.T) {
	code, stdout, stderr := exec("gen", "-header", header, "-package", "p", "-library", "l", "-bogus")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "flag provided but not defined")
	assert.Contains(t, stderr, "usage: cppgo gen")
}

func TestGenUnexpectedArgument(t *testing.T) {
	code, stdout, stderr := exec("gen", "-header", header, "-package", "p", "-library", "l", "extra")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, `unexpected argument "extra"`)
	assert.Contains(t, stderr, "usage: cppgo gen")
}

func TestCheckUnknownFlag(t *testing.T) {
	code, _, stderr := exec("check", "-bogus")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, "usage: cppgo check")
}

func TestCheckUnexpectedArgument(t *testing.T) {
	code, _, stderr := exec("check", "-header", header, "-package", "p", "-library", "l", "extra")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, `cppgo check: unexpected argument "extra"`)
}

func TestGenOutputPathUnwritable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	code, stdout, stderr := exec("gen", "-header", header, "-package", "mathx", "-library", "libmathx", "-out", filepath.Join(blocker, "sub"))
	assert.Equal(t, exitError, code)
	_ = stdout
	assert.Contains(t, stderr, "cppgo gen:")
}

// mustGen requires a successful gen and returns its exit code.
func mustGen(t *testing.T, args ...string) int {
	t.Helper()
	code, _, stderr := exec(args...)
	require.Equal(t, exitOK, code, "cppgo %v: %s", args, stderr)
	return code
}
