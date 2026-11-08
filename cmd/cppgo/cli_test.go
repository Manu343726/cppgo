package main

import (
	"bytes"
	"os"
	osexec "os/exec"
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

	for _, name := range []string{gen.GenFileName, gen.AsmFileName} {
		data, err := os.ReadFile(filepath.Join(out, name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, data, name)
	}
	// The shim's sources live one directory down (a Go package directory may
	// not hold C++), and the shim cppgo compiled sits beside the package.
	for _, name := range []string{gen.ShimHeaderFileName, gen.ShimSourceFileName} {
		data, err := os.ReadFile(gen.ShimPath(out, name))
		require.NoError(t, err, name)
		assert.NotEmpty(t, data, name)
	}
	require.FileExists(t, filepath.Join(out, gen.ShimLibraryName("mathx")))
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
		{"a direct binding needs the library", []string{"gen", "-header", header, "-package", "p"}, "a direct binding needs -library"},
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

// stdDequeHeader is a standard-library type outside DESIGN.md's table: the
// generator must refuse it, not guess.
var stdDequeHeader = filepath.Join("..", "..", "internal", "gen", "testdata", "errors", "stddeque.hpp")

// tmplHeader is the class template fixture.
var tmplHeader = filepath.Join("..", "..", "internal", "gen", "testdata", "tmpl", "cell.hpp")

// TestGenUnimplementedConstructFailsWithoutWriting is the integrity gate: an
// unimplemented construct is exit 1, a one-line message, and no output files.
func TestGenUnimplementedConstructFailsWithoutWriting(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := exec("gen", "-header", stdDequeHeader, "-package", "err", "-library", "liberr", "-out", out)
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "outside the emitted table")
	assert.Contains(t, stderr, "; ")
	_, err := os.Stat(out)
	assert.True(t, os.IsNotExist(err), "a failed generation must not create the output directory")
}

// TestGenInstantiate exercises the flag the template error names.
func TestGenInstantiate(t *testing.T) {
	out := t.TempDir()
	mustExec(t, "gen", "-header", tmplHeader, "-package", "tmpl", "-library", "libtmpl",
		"-instantiate", "tmpl::Cell<int>", "-out", out)
	data, err := os.ReadFile(filepath.Join(out, gen.GenFileName))
	require.NoError(t, err)
	assert.Contains(t, string(data), "type CellInt struct")
	assert.NotContains(t, string(data), "CellFloat64", "only the named instantiation is emitted")
}

// TestGenInstantiateUnmatched checks the typo is an error, not an empty package.
func TestGenInstantiateUnmatched(t *testing.T) {
	code, stdout, stderr := exec("gen", "-header", tmplHeader, "-package", "tmpl", "-library", "libtmpl",
		"-instantiate", "tmpl::Cell<float>", "-out", t.TempDir())
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "names no instantiation")
}

// badShimHeader is the template whose thunk cannot compile.
var badShimHeader = filepath.Join("..", "..", "internal", "gen", "testdata", "badshim", "badshim.hpp")

// TestGenUncompilableShimFails proves the shim self-check is a generation
// gate: the thunk does not compile, so the command exits 1 and writes nothing.
func TestGenUncompilableShimFails(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out")
	code, stdout, stderr := exec("gen", "-header", badShimHeader, "-package", "bad", "-library", "libbad",
		"-instantiate", "bad::Bad<int>", "-out", out)
	assert.Equal(t, exitError, code)
	assert.Empty(t, stdout)
	assert.Contains(t, stderr, "the generated shim does not compile")
	assert.Contains(t, stderr, "bad::Bad<int>::get")
	_, err := os.Stat(out)
	assert.True(t, os.IsNotExist(err), "a failed generation must not create the output directory")
}

// buildHeader is the two-phase fixture header, absolute so the scan's module
// directory can be anywhere.
func buildHeader(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "internal", "gen", "testdata", "box", "box.hpp"))
	require.NoError(t, err)
	return path
}

// writeScanModule writes a throwaway module with a program that instantiates
// the box fixture's generic API.
func writeScanModule(t *testing.T, repo string) string {
	t.Helper()
	dir := t.TempDir()
	gomod := "module cppgo-e2e\n\ngo 1.27.0\n\nrequire github.com/Manu343726/cppgo v0.0.0\n\nreplace github.com/Manu343726/cppgo => " + repo + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644))
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "app"), 0o755))
	program := `package app

import "cppgo-e2e/box"

func Run() error {
	b, err := box.NewBox[int32](7)
	if err != nil {
		return err
	}
	return b.Close()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app", "app.go"), []byte(program), 0o644))
	return dir
}

// TestBuildHelp covers the third command's own help.
func TestBuildHelp(t *testing.T) {
	for _, args := range [][]string{{"build", "-h"}, {"help", "build"}} {
		code, stdout, stderr := exec(args...)
		assert.Equal(t, exitOK, code)
		assert.Contains(t, stdout, "usage: cppgo build")
		assert.Contains(t, stdout, "two-phase")
		assert.Empty(t, stderr)
	}
	code, stdout, _ := exec("help")
	assert.Equal(t, exitOK, code)
	assert.Contains(t, stdout, "cppgo build")
}

// TestBuildCollectsInstantiations runs the CLI's second phase against a real
// module: gen writes the generic package, build scans the program and emits the
// explicit instantiation.
func TestBuildCollectsInstantiations(t *testing.T) {
	if _, err := osexec.LookPath("go"); err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	moduleDir := writeScanModule(t, repo)
	headerPath := buildHeader(t)
	out := filepath.Join(moduleDir, "box")

	mustExec(t, "gen", "-header", headerPath, "-package", "box", "-out", out)
	stdout, _ := mustExec(t, "build", "-header", headerPath, "-package", "box", "-out", out,
		"-scan", "./app", "-scan-dir", moduleDir)
	assert.Contains(t, stdout, "template class ::box::Box<std::int32_t>;")
	assert.Contains(t, stdout, "generic API")
	assert.FileExists(t, filepath.Join(out, "libcppgo_box.so"))

	// A check with the same scan compares the shim built for the same
	// instantiations: the program is an input to the shim's source.
	checked, _ := mustExec(t, "check", "-header", headerPath, "-package", "box", "-out", out,
		"-scan", "./app", "-scan-dir", moduleDir)
	assert.Contains(t, checked, "generated files are up to date")

	code, _, stderr := exec("check", "-header", headerPath, "-package", "box", "-out", out,
		"-scan", "./app", "-scan-dir", moduleDir, "-instantiate", "tmpl::Cell<int>")
	assert.Equal(t, exitError, code)
	assert.Contains(t, stderr, "names no instantiation")
}

// TestBuildScanErrors covers the second phase's failure through the CLI.
func TestBuildScanErrors(t *testing.T) {
	if _, err := osexec.LookPath("go"); err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	moduleDir := writeScanModule(t, repo)
	headerPath := buildHeader(t)
	out := filepath.Join(moduleDir, "box")
	mustExec(t, "gen", "-header", headerPath, "-package", "box", "-out", out)

	code, _, stderr := exec("build", "-header", headerPath, "-package", "box", "-out", out,
		"-scan", "./absent", "-scan-dir", moduleDir)
	assert.Equal(t, exitError, code)
	assert.Contains(t, stderr, "scanning")
}

// TestBuildMissingHeader still reports manifest errors before the scan runs.
func TestBuildMissingHeader(t *testing.T) {
	code, _, stderr := exec("build")
	assert.Equal(t, exitUsage, code)
	assert.Contains(t, stderr, "-header is required")
}
