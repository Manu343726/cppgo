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

// writePackage materialises a small but complete cppgo input package in a
// fresh directory and returns its path.
func writePackage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `// Package mathx is a sample binding package.
package mathx

//cppgo:library libmathx

// AddI64 adds two integers.
//
//cppgo:external add_i64
func AddI64(a, b int64) int64
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "mathx.go"), []byte(src), 0o644))
	return dir
}

// exec runs the CLI and returns its exit code and streams.
func exec(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
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
	dir := writePackage(t)
	out := t.TempDir()

	code, stdout, stderr := exec("gen", "-dir", dir, "-out", out)
	require.Equal(t, exitOK, code, stderr)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)

	for _, name := range []string{gen.GenFileName, gen.AsmFileName, gen.ShimFileName, gen.SourceFileName} {
		data, err := os.ReadFile(filepath.Join(out, name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, data, name)
	}
}

func TestGenInPlace(t *testing.T) {
	dir := writePackage(t)

	code, _, stderr := exec("gen", "-dir", dir)
	require.Equal(t, exitOK, code, stderr)

	// The input file is rewritten in place and still holds the directive.
	data, err := os.ReadFile(filepath.Join(dir, "mathx.go"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "//cppgo:external add_i64")
	assert.Contains(t, string(data), "return cppgoCallAddI64(a, b)")
	assert.NoFileExists(t, filepath.Join(dir, gen.SourceFileName))
}

func TestCheckUpToDate(t *testing.T) {
	dir := writePackage(t)
	out := t.TempDir()
	require.Equal(t, exitOK, mustExec(t, "gen", "-dir", dir, "-out", out))

	code, stdout, stderr := exec("check", "-dir", dir, "-out", out)
	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "generated files are up to date")
	assert.Empty(t, stderr)
}

func TestCheckStale(t *testing.T) {
	dir := writePackage(t)
	out := t.TempDir()
	require.Equal(t, exitOK, mustExec(t, "gen", "-dir", dir, "-out", out))

	require.NoError(t, os.WriteFile(filepath.Join(out, gen.GenFileName), []byte("// tampered\n"), 0o644))

	code, stdout, stderr := exec("check", "-dir", dir, "-out", out)
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "out of date")
	assert.Contains(t, stderr, gen.GenFileName)
}

func TestCheckMissingFiles(t *testing.T) {
	dir := writePackage(t)
	code, _, stderr := exec("check", "-dir", dir, "-out", t.TempDir())
	assert.Equal(t, exitError, code)
	assert.Contains(t, stderr, "out of date")
}

func TestCheckQuiet(t *testing.T) {
	dir := writePackage(t)
	out := t.TempDir()
	require.Equal(t, exitOK, mustExec(t, "gen", "-dir", dir, "-out", out))

	code, stdout, stderr := exec("check", "-dir", dir, "-out", out, "-q")
	assert.Equal(t, exitOK, code)
	assert.Empty(t, stdout)
	assert.Empty(t, stderr)
}

func TestGenCheckFlagEqualsCheckCommand(t *testing.T) {
	dir := writePackage(t)
	out := t.TempDir()
	require.Equal(t, exitOK, mustExec(t, "gen", "-dir", dir, "-out", out))

	code, stdout, _ := exec("gen", "-dir", dir, "-out", out, "-check")
	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "up to date")
}

func TestGenGenerationError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.go"), []byte("package bad\n\n//cppgo:external x\nfunc X()\n"), 0o644))

	code, stdout, stderr := exec("gen", "-dir", dir)
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "cppgo gen:")
	assert.Contains(t, stderr, "missing //cppgo:library")
}

func TestGenMissingDir(t *testing.T) {
	code, stdout, stderr := exec("gen", "-dir", filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "cppgo gen:")
}

func TestGenPathIsNotADirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.go")
	require.NoError(t, os.WriteFile(file, []byte("package x\n"), 0o644))

	code, stdout, stderr := exec("gen", "-dir", file)
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "is not a directory")
}

func TestGenEmptyDirFlag(t *testing.T) {
	code, stdout, stderr := exec("gen", "-dir", "")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "-dir must not be empty")
}

func TestGenUnknownFlag(t *testing.T) {
	code, stdout, stderr := exec("gen", "-bogus")
	assert.Equal(t, exitUsage, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "flag provided but not defined")
	assert.Contains(t, stderr, "usage: cppgo gen")
}

func TestGenUnexpectedArgument(t *testing.T) {
	code, stdout, stderr := exec("gen", "extra")
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
	code, _, stderr := exec("check", "extra")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, `cppgo check: unexpected argument "extra"`)
}

func TestCheckMissingDir(t *testing.T) {
	code, _, stderr := exec("check", "-dir", filepath.Join(t.TempDir(), "nope"))
	assert.Equal(t, exitError, code)
	assert.Contains(t, stderr, "cppgo check:")
}

func TestGenOutputPathUnwritable(t *testing.T) {
	dir := writePackage(t)
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o644))

	code, stdout, stderr := exec("gen", "-dir", dir, "-out", filepath.Join(blocker, "sub"))
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "cppgo gen:")
}

// mustExec runs the CLI and requires success, returning the exit code.
func mustExec(t *testing.T, args ...string) int {
	t.Helper()
	code, _, stderr := exec(args...)
	require.Equal(t, exitOK, code, "cppgo %v: %s", args, stderr)
	return code
}
