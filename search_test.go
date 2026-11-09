package cppgo_test

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
)

// The search fixture: one exported function whose value is compiled in, so which
// copy the loader chose is observable.
const searchFixtureSource = `
#include <cstdint>

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_search_marker(void) {
    return CPPGO_SEARCH_MARKER;
}
`

// buildSearchLibrary compiles the probe into dir under name and returns its path.
//
// The object declares no soname on purpose: the loader then knows each copy by
// its path, so two directories can hold two different copies of one name and the
// test can tell which one was loaded.
func buildSearchLibrary(t *testing.T, dir, name string, marker int64) string {
	t.Helper()

	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("no g++ available to build the search fixture: %v", err)
	}

	source := filepath.Join(t.TempDir(), "probe.cc")
	require.NoError(t, os.WriteFile(source, []byte(searchFixtureSource), 0o644))

	out := filepath.Join(dir, name)
	runCompiler(t, gxx, "-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17",
		"-DCPPGO_SEARCH_MARKER="+strconv.FormatInt(marker, 10), "-o", out, source)

	return out
}

// searchOnce opens search, reads the fixture's marker and returns both facts.
//
// A child process and the tests that need no child run the same code, so the body
// is measured in the process whose profile is written.
func searchOnce(t *testing.T, search cppgo.LibrarySearch) (path string, value int64) {
	t.Helper()

	lib, err := search.Open()
	require.NoError(t, err)
	defer func() { _ = lib.Close() }()

	return lib.Path(), callInt(t, fn(t, lib, "cppgo_search_marker"), [6]uintptr{})
}

// TestSearchHelperProcess is the body of the child processes the search tests
// start: glibc reads LD_LIBRARY_PATH when a process starts, so the only way to
// measure it is a process that inherited it.
func TestSearchHelperProcess(t *testing.T) {
	if os.Getenv("CPPGO_SEARCH_HELPER") != "1" {
		t.Skip("re-executed by the search tests")
	}

	path, value := searchOnce(t, cppgo.LibrarySearch{
		Name:     os.Getenv("CPPGO_SEARCH_NAME"),
		Override: os.Getenv("CPPGO_SEARCH_OVERRIDE"),
	})

	fmt.Fprintf(os.Stdout, "cppgo search path=%s\n", path)
	fmt.Fprintf(os.Stdout, "cppgo search value=%d\n", value)
}

// searchChildEnv is the environment of a child process: the parent's, with every
// variable these tests own replaced rather than appended, because a duplicate is
// read as the first one and the parent may well have set it.
func searchChildEnv(overrides ...string) []string {
	owned := map[string]bool{
		"CPPGO_SEARCH_HELPER":   true,
		"CPPGO_SEARCH_NAME":     true,
		"CPPGO_SEARCH_OVERRIDE": true,
		cppgo.LibraryPathEnv:    true,
		"LD_LIBRARY_PATH":       true,
	}
	for _, kv := range overrides {
		if key, _, ok := strings.Cut(kv, "="); ok {
			owned[key] = true
		}
	}

	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); !owned[key] {
			env = append(env, kv)
		}
	}

	return append(env, overrides...)
}

// runSearchChild starts the helper process and returns what it printed.
func runSearchChild(t *testing.T, overrides ...string) string {

	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestSearchHelperProcess$")
	cmd.Env = searchChildEnv(append(overrides, "CPPGO_SEARCH_HELPER=1")...)

	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "the child process failed:\n%s", output)

	return string(output)
}

// searchResult reads the path and the marker value a child reported.
func searchResult(t *testing.T, output string) (path string, value int64) {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if rest, ok := strings.CutPrefix(line, "cppgo search path="); ok {
			path = strings.TrimSpace(rest)
		}
		if rest, ok := strings.CutPrefix(line, "cppgo search value="); ok {
			parsed, err := strconv.ParseInt(strings.TrimSpace(rest), 10, 64)
			require.NoErrorf(t, err, "child output:\n%s", output)
			value = parsed
		}
	}
	require.NotEmptyf(t, path, "the child reported no path:\n%s", output)

	return path, value
}

// TestSearchLoaderLibraryPath is the LD_LIBRARY_PATH half of the order: the
// object is in no cppgo location at all and only the loader's own search can
// find it.
func TestSearchLoaderLibraryPath(t *testing.T) {
	dir := t.TempDir()
	name := "libcppgo_search_ld.so"
	buildSearchLibrary(t, dir, name, 11)

	output := runSearchChild(t, "CPPGO_SEARCH_NAME="+name, "LD_LIBRARY_PATH="+dir)
	path, value := searchResult(t, output)

	assert.Equal(t, name, path, "the bare name is what was loaded, not a path")
	assert.EqualValues(t, 11, value)
}

// TestSearchCPPGOLibraryPath is the middle step: the loader cannot find the
// object, and CPPGO_LIBRARY_PATH names the directory it is in.
func TestSearchCPPGOLibraryPath(t *testing.T) {
	dir := t.TempDir()
	name := "libcppgo_search_env.so"
	buildSearchLibrary(t, dir, name, 21)
	t.Setenv(cppgo.LibraryPathEnv, dir)

	path, value := searchOnce(t, cppgo.LibrarySearch{Name: name})

	assert.Equal(t, filepath.Join(dir, name), path)
	assert.EqualValues(t, 21, value)
}

// TestSearchCPPGOLibraryPathList exercises the list: entries are tried in order,
// an empty entry is skipped, and the first entry that holds the name wins.
func TestSearchCPPGOLibraryPathList(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	dir := t.TempDir()
	name := "libcppgo_search_list.so"
	buildSearchLibrary(t, dir, name, 22)

	separator := string(os.PathListSeparator)
	t.Setenv(cppgo.LibraryPathEnv, missing+separator+separator+dir+separator)

	path, value := searchOnce(t, cppgo.LibrarySearch{Name: name})

	assert.Equal(t, filepath.Join(dir, name), path)
	assert.EqualValues(t, 22, value)
}

// TestSearchCPPGOLibraryPathNamesTheObject covers the ADR-0003 spelling: an entry
// may name the object itself rather than the directory it is in.
func TestSearchCPPGOLibraryPathNamesTheObject(t *testing.T) {
	dir := t.TempDir()
	name := "libcppgo_search_file.so"
	path := buildSearchLibrary(t, dir, name, 23)
	t.Setenv(cppgo.LibraryPathEnv, path)

	loaded, value := searchOnce(t, cppgo.LibrarySearch{Name: name})

	assert.Equal(t, path, loaded)
	assert.EqualValues(t, 23, value)
}

// TestSearchCPPGOBeatsLoader is the priority question: the same name in two
// directories, the loader finding one through LD_LIBRARY_PATH and cppgo naming
// the other.
func TestSearchCPPGOBeatsLoader(t *testing.T) {
	loaderDir := t.TempDir()
	cppgoDir := t.TempDir()
	name := "libcppgo_search_both.so"
	buildSearchLibrary(t, loaderDir, name, 31)
	buildSearchLibrary(t, cppgoDir, name, 32)

	t.Run("the cppgo override wins", func(t *testing.T) {
		output := runSearchChild(t,
			"CPPGO_SEARCH_NAME="+name,
			cppgo.LibraryPathEnv+"="+cppgoDir,
			"LD_LIBRARY_PATH="+loaderDir,
		)
		path, value := searchResult(t, output)

		assert.Equal(t, filepath.Join(cppgoDir, name), path)
		assert.EqualValues(t, 32, value)
	})

	t.Run("the loader is the control", func(t *testing.T) {
		output := runSearchChild(t, "CPPGO_SEARCH_NAME="+name, "LD_LIBRARY_PATH="+loaderDir)
		path, value := searchResult(t, output)

		assert.Equal(t, name, path)
		assert.EqualValues(t, 31, value, "without the override the loader's copy is the one")
	})
}

// TestSearchOverrideBeatsEnvironment is CppgoLibraryPath against the cppgo
// environment: the exact path wins.
func TestSearchOverrideBeatsEnvironment(t *testing.T) {
	envDir := t.TempDir()
	overrideDir := t.TempDir()
	name := "libcppgo_search_var.so"
	buildSearchLibrary(t, envDir, name, 41)
	override := buildSearchLibrary(t, overrideDir, name, 42)
	t.Setenv(cppgo.LibraryPathEnv, envDir)

	path, value := searchOnce(t, cppgo.LibrarySearch{Name: name, Override: override})

	assert.Equal(t, override, path)
	assert.EqualValues(t, 42, value)
}

// TestSearchOverrideBeatsEverything puts the three sources in one process: the
// exact path, the cppgo list and the loader's search.
func TestSearchOverrideBeatsEverything(t *testing.T) {
	loaderDir := t.TempDir()
	cppgoDir := t.TempDir()
	overrideDir := t.TempDir()
	name := "libcppgo_search_all.so"
	buildSearchLibrary(t, loaderDir, name, 51)
	buildSearchLibrary(t, cppgoDir, name, 52)
	override := buildSearchLibrary(t, overrideDir, name, 53)

	output := runSearchChild(t,
		"CPPGO_SEARCH_NAME="+name,
		"CPPGO_SEARCH_OVERRIDE="+override,
		cppgo.LibraryPathEnv+"="+cppgoDir,
		"LD_LIBRARY_PATH="+loaderDir,
	)
	path, value := searchResult(t, output)

	assert.Equal(t, override, path)
	assert.EqualValues(t, 53, value)
}

// TestSearchEmbedded is the deployment path: the set is embedded, so the library
// is extracted to the private cache and loaded from there with nothing in the
// environment.
func TestSearchEmbedded(t *testing.T) {
	dir := buildEmbedded(t)
	base := isolateCache(t)

	lib, err := cppgo.LibrarySearch{
		Name: embeddedMainName,
		FS:   embeddedObjects(t, dir),
	}.Open()
	require.NoError(t, err)
	defer func() { _ = lib.Close() }()

	assert.Equal(t, cacheRoot(base), filepath.Dir(filepath.Dir(lib.Path())),
		"the object comes from the cache, not from the build directory")
	assert.EqualValues(t, 10, callInt(t, fn(t, lib, embeddedSymbol), [6]uintptr{2, 3}))
}

// TestSearchDoesNotUseTheBuildDirectory is the correction this order was written
// for: neither the working directory nor the directory the shim sits in is ever
// a place to look. The object is right there and the search still fails.
func TestSearchDoesNotUseTheBuildDirectory(t *testing.T) {
	dir := t.TempDir()
	name := "libcppgo_search_beside.so"
	buildSearchLibrary(t, dir, name, 61)
	t.Setenv(cppgo.LibraryPathEnv, "")
	t.Chdir(dir)

	_, err := cppgo.LibrarySearch{Name: name}.Open()
	require.Error(t, err, "the build directory is not a search path")

	var searchErr *cppgo.SearchError
	require.ErrorAs(t, err, &searchErr)
	assert.Equal(t, name, searchErr.Name)
}

func TestSearchMissing(t *testing.T) {
	envDir := t.TempDir()
	name := "libcppgo_search_absent.so"
	t.Setenv(cppgo.LibraryPathEnv, envDir)

	_, err := cppgo.LibrarySearch{Name: name}.Open()
	require.Error(t, err)

	var searchErr *cppgo.SearchError
	require.ErrorAs(t, err, &searchErr)
	assert.Equal(t, name, searchErr.Name)
	assert.Equal(t, []string{filepath.Join(envDir, name)}, searchErr.Paths)
	require.Error(t, searchErr.Err)

	// The message is the deployment instruction: where it looked, and the two
	// variables that would change the answer.
	message := err.Error()
	assert.Contains(t, message, filepath.Join(envDir, name))
	assert.Contains(t, message, cppgo.LibraryPathEnv)
	assert.Contains(t, message, "CppgoLibraryPath")
	assert.Contains(t, message, "LD_LIBRARY_PATH")
	assert.Contains(t, message, name)
}

func TestSearchMissingWithNoOverride(t *testing.T) {
	t.Setenv(cppgo.LibraryPathEnv, "")
	name := "libcppgo_search_absent_none.so"

	_, err := cppgo.LibrarySearch{Name: name}.Open()
	require.Error(t, err)

	var searchErr *cppgo.SearchError
	require.ErrorAs(t, err, &searchErr)
	assert.Empty(t, searchErr.Paths)
	assert.Contains(t, err.Error(), cppgo.LibraryPathEnv)
}

func TestSearchEmptyName(t *testing.T) {
	_, err := cppgo.LibrarySearch{}.Open()
	require.Error(t, err)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Empty(t, openErr.Path)
	assert.Contains(t, err.Error(), "empty library path")
}

// TestSearchBrokenOverride: an override that was given and cannot be loaded is
// the answer, never a reason to fall back to the search.
func TestSearchBrokenOverride(t *testing.T) {
	envDir := t.TempDir()
	dir := t.TempDir()
	name := "libcppgo_search_broken_var.so"
	buildSearchLibrary(t, envDir, name, 71)
	broken := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(broken, []byte("not an ELF object"), 0o644))
	t.Setenv(cppgo.LibraryPathEnv, envDir)

	_, err := cppgo.LibrarySearch{Name: name, Override: broken}.Open()
	require.Error(t, err)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, broken, openErr.Path)
}

// TestSearchBrokenEntry: an entry that holds the name is the answer even when the
// loader rejects the file. A search does not skip a file it found.
func TestSearchBrokenEntry(t *testing.T) {
	dir := t.TempDir()
	goodDir := t.TempDir()
	name := "libcppgo_search_broken_entry.so"
	buildSearchLibrary(t, goodDir, name, 72)
	broken := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(broken, []byte("not an ELF object"), 0o644))
	t.Setenv(cppgo.LibraryPathEnv, dir+string(os.PathListSeparator)+goodDir)

	_, err := cppgo.LibrarySearch{Name: name}.Open()
	require.Error(t, err)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, broken, openErr.Path)
}

// TestSearchBareName is the last step in a process that can measure it: a
// system library the loader's cache knows, resolved by its bare name.
func TestSearchBareName(t *testing.T) {
	t.Setenv(cppgo.LibraryPathEnv, "")

	lib, err := cppgo.LibrarySearch{Name: "libm.so.6"}.Open()
	if err != nil {
		t.Skipf("no libm.so.6 through the loader's own search: %v", err)
	}
	defer func() { _ = lib.Close() }()

	assert.Equal(t, "libm.so.6", lib.Path(), "the bare name is what the loader resolved")
	_, err = lib.Symbol("cos")
	require.NoError(t, err)
}

// TestSearchMissingExactPath: an entry that does not exist but is named like the
// object is reported as the file it was, not joined with itself.
func TestSearchMissingExactPath(t *testing.T) {
	name := "libcppgo_search_absent_path.so"
	missing := filepath.Join(t.TempDir(), name)
	t.Setenv(cppgo.LibraryPathEnv, missing)

	_, err := cppgo.LibrarySearch{Name: name}.Open()
	require.Error(t, err)

	var searchErr *cppgo.SearchError
	require.ErrorAs(t, err, &searchErr)
	assert.Equal(t, []string{missing}, searchErr.Paths)

	// The wrapped loader error is reachable through the chain.
	assert.NotNil(t, errors.Unwrap(err))
}
