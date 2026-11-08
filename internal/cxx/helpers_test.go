package cxx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipWithoutLibclang loads libclang or skips the test with the reason. CI
// installs libclang-dev, so a skip here means the environment is incomplete,
// not that the test is optional.
func skipWithoutLibclang(t *testing.T) *Libclang {
	t.Helper()
	library, err := Load()
	if err != nil {
		if errors.Is(err, ErrLibclangNotFound) {
			// In CI a skip is a lie: it hides the fact that the introspection
			// tests - and therefore most of this package's coverage - did not
			// run. require it there, skip only on a developer machine.
			if os.Getenv("CPPGO_REQUIRE_LIBCLANG") != "" {
				require.FailNowf(t, "libclang is required but could not be loaded", "%v", err)
			}
			t.Skipf("libclang is not installed, so C++ introspection cannot run: %v", err)
		}
		require.NoError(t, err, "loading libclang")
	}
	t.Cleanup(func() {
		// Tests that close the handle themselves are allowed: the cleanup
		// must not turn a deliberate close into a failure.
		if !library.isClosed() {
			require.NoError(t, library.Close())
		}
	})
	return library
}

// fixturePath is the absolute path of a file under testdata.
func fixturePath(t *testing.T, parts ...string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join(append([]string{"testdata"}, parts...)...))
	require.NoError(t, err)
	return abs
}

// copyFixture copies the mathx fixture into a fresh directory, so a test can
// add its own compile_commands.json or .clangd without disturbing its
// neighbours.
func copyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"mathx.hpp", "mathx.cpp"} {
		data, err := os.ReadFile(fixturePath(t, "mathx", name))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), data, 0o644))
	}
	return dir
}

// assertLayoutIsContractual asserts the layout promise a Record makes, on
// whichever branch the environment produces. Whether libclang completes an
// instantiated template's layout depends on the standard headers it can see:
// the pinned CI image completes it, a bare checkout may not. Both outcomes are
// valid, but they have different shapes, and the model must be internally
// consistent on either one.
//
// The source of the contract is Record's doc comment in record.go (SizeBytes,
// AlignBytes and LayoutKnown) and DESIGN.md "Mirrorable record -> Go struct":
//
//   - LayoutKnown true: SizeBytes and AlignBytes are real positive values and
//     every non-bitfield field offset is real and non-negative, so the emitter
//     can mirror the layout;
//   - LayoutKnown false: SizeBytes and AlignBytes are exactly -1 -- a size the
//     model could not trust is reported as unknown, never guessed -- and the
//     emitter takes the opaque accessor path.
//
// Fields and members are deliberately not part of this helper: they must be
// reachable in both branches, so callers assert them separately.
func assertLayoutIsContractual(t *testing.T, rec Record) {
	t.Helper()
	if !rec.LayoutKnown {
		assert.Equal(t, int64(-1), rec.SizeBytes,
			"%s: an unknown layout is reported as -1, not guessed", rec.QualifiedName)
		assert.Equal(t, int64(-1), rec.AlignBytes,
			"%s: an unknown layout has no alignof", rec.QualifiedName)
		return
	}
	assert.Positive(t, rec.SizeBytes, "%s: a known layout has a real sizeof", rec.QualifiedName)
	assert.Positive(t, rec.AlignBytes, "%s: a known layout has a real alignof", rec.QualifiedName)
	for _, field := range rec.Fields {
		if field.IsBitField {
			continue
		}
		assert.GreaterOrEqual(t, field.OffsetBytes, int64(0),
			"%s.%s: a known layout has a real field offset", rec.QualifiedName, field.Name)
	}
}

// cxxCompiler returns a C++ compiler able to build a shared library, or skips.
// It is only needed by the tests that build a real .so; the introspection tests
// need nothing but libclang.
func cxxCompiler(t *testing.T) string {
	t.Helper()
	if cxx := os.Getenv("CXX"); cxx != "" {
		if _, err := exec.LookPath(cxx); err == nil {
			return cxx
		}
	}
	for _, candidate := range []string{"clang++", "g++", "c++"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("no C++ compiler available to build the fixture library")
	return ""
}

// buildFixtureLibrary compiles mathx.cpp into a shared object and returns its
// path. The compile flags are the ones a real project would use, which is also
// what the compile_commands.json scenarios put in the database.
func buildFixtureLibrary(t *testing.T, dir, compiler string) string {
	t.Helper()
	out := filepath.Join(dir, "libmathx.so")
	cmd := exec.Command(compiler, "-std=c++17", "-O1", "-fPIC", "-shared", "-I"+dir,
		"-o", out, filepath.Join(dir, "mathx.cpp"))
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "building the fixture library: %s", output)
	return out
}

// compileCommandFor is the command line a compiler database would record for
// mathx.cpp, in the same shape a real build system writes.
func compileCommandFor(compiler, dir string) string {
	return strings.Join([]string{
		compiler, "-std=c++17", "-O1", "-fPIC", "-I" + dir, "-c",
		filepath.Join(dir, "mathx.cpp"), "-o", filepath.Join(dir, "mathx.o"),
	}, " ")
}

// withCandidates replaces the library search list for the duration of a test.
func withCandidates(t *testing.T, paths []string) {
	t.Helper()
	saved := candidatePaths
	candidatePaths = func() []string { return paths }
	t.Cleanup(func() { candidatePaths = saved })
}

// command runs a command and returns its combined output.
func command(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	output, err := exec.Command(name, args...).CombinedOutput()
	return string(output), err
}

// unsafePointerOf converts a pointer to unsafe.Pointer.
func unsafePointerOf(key *visitKey) unsafe.Pointer { return unsafe.Pointer(key) }
