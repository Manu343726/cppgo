package cppgo_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
)

// The chain fixture: three objects, each needing the next, so a set can only be
// loaded bottom-up. The file names are the sonames — that is what an embedded set
// holds — and they are chosen so that name order is not load order.
const (
	chainBase = "libcppgo_chain_zzz_base.so.1"
	chainMid  = "libcppgo_chain_mmm_mid.so.1"
	chainTop  = "libcppgo_chain_aaa_top.so.1"
	chainSym  = "cppgo_chain_top"

	chainBaseSource = `
#include <cstdint>

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_chain_base(void) {
	return 3;
}
`

	chainMidSource = `
#include <cstdint>

extern "C" std::int64_t cppgo_chain_base(void);

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_chain_mid(void) {
	return cppgo_chain_base() + 1;
}
`

	chainTopSource = `
#include <cstdint>

extern "C" std::int64_t cppgo_chain_mid(void);

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_chain_top(void) {
	return 2 * cppgo_chain_mid();
}
`
)

// buildChain compiles the three objects into one directory and returns it.
func buildChain(t *testing.T) string {
	t.Helper()

	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("no g++ available to build the chain fixture: %v", err)
	}

	dir := t.TempDir()
	flags := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17"}

	compile := func(source, name string, extra ...string) {
		src := filepath.Join(t.TempDir(), "fixture.cc")
		require.NoError(t, os.WriteFile(src, []byte(source), 0o644))

		args := append(append([]string{}, flags...), "-Wl,-soname,"+name)
		args = append(args, extra...)
		args = append(args, "-o", filepath.Join(dir, name), src)
		runCompiler(t, gxx, args...)
	}

	compile(chainBaseSource, chainBase)
	compile(chainMidSource, chainMid, "-L"+dir, "-l:"+chainBase)
	compile(chainTopSource, chainTop, "-L"+dir, "-l:"+chainMid)

	return dir
}

// chainObjects reads the built chain into a MapFS, as though it had been
// embedded.
func chainObjects(t *testing.T, dir string, names ...string) fstest.MapFS {
	t.Helper()

	objects := fstest.MapFS{}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		objects[name] = &fstest.MapFile{Data: data, Mode: 0o644}
	}

	return objects
}

// TestPreloadEmbeddedChain is the whole set: three objects extracted together,
// loaded bottom-up, and the top one calls through the chain.
//
// Load order is measured rather than assumed: the top object is first in name
// order and its dependency is not on any search path, so a preload that did not
// resolve the graph would fail here.
func TestPreloadEmbeddedChain(t *testing.T) {
	dir := buildChain(t)
	isolateCache(t)

	lib, err := cppgo.OpenFS(chainObjects(t, dir, chainBase, chainMid, chainTop), chainTop)
	require.NoError(t, err)
	defer func() { _ = lib.Close() }()

	assert.EqualValues(t, 8, callInt(t, fn(t, lib, chainSym), [6]uintptr{}))

	// The whole set is one directory, which is what makes $ORIGIN and the
	// preload agree.
	entry := filepath.Dir(lib.Path())
	for _, name := range []string{chainBase, chainMid, chainTop} {
		_, err := os.Stat(filepath.Join(entry, name))
		require.NoErrorf(t, err, "%s is extracted beside the other objects", name)
	}
}

// TestPreloadMissingDependency: a dependency the set omits and the loader cannot
// find either is an error naming it, raised before the object that needs it is
// mapped.
func TestPreloadMissingDependency(t *testing.T) {
	dir := buildChain(t)
	isolateCache(t)

	_, err := cppgo.OpenFS(chainObjects(t, dir, chainTop), chainTop)
	require.Error(t, err)

	var depErr *cppgo.DependencyError
	require.ErrorAs(t, err, &depErr)
	assert.Equal(t, chainTop, depErr.Object)
	assert.Equal(t, chainMid, depErr.Name)

	message := err.Error()
	assert.Contains(t, message, chainMid)
	assert.Contains(t, message, chainTop)
	assert.Contains(t, message, "package it with the library")
}

// TestPreloadUnloadsWhatItTook: a set that fails part way has already loaded the
// objects before the failure, and those handles are released rather than left
// behind. The failure itself is a missing dependency of the last object.
func TestPreloadUnloadsWhatItTook(t *testing.T) {
	dir := buildChain(t)
	isolateCache(t)

	gxx, err := exec.LookPath("g++")
	require.NoError(t, err)

	// A dependency that exists where the fixture was built but is left out of
	// the embedded set: the loader cannot find it by name at run time either.
	const gone = "libcppgo_chain_gone.so.1"
	compileSource(t, gxx, dir, gone, `
extern "C" __attribute__((visibility("default"))) int
cppgo_chain_gone(void) {
	return 1;
}
`, "-Wl,-soname,"+gone)

	broken := "libcppgo_chain_nnn_broken.so.1"
	compileSource(t, gxx, dir, broken, `
extern "C" int cppgo_chain_gone(void);

extern "C" __attribute__((visibility("default"))) int
cppgo_chain_broken(void) {
	return cppgo_chain_gone();
}
`, "-Wl,-soname,"+broken, "-L"+dir, "-l:"+gone)

	objects := chainObjects(t, dir, chainBase, chainMid, broken)
	_, err = cppgo.OpenFS(objects, broken)
	require.Error(t, err)

	var depErr *cppgo.DependencyError
	require.ErrorAs(t, err, &depErr)
	assert.Equal(t, broken, depErr.Object)
	assert.Equal(t, gone, depErr.Name)
}

// TestPreloadUndefinedSymbol: an object the loader cannot map at all fails the
// preload, and the error names the object and the symbol.
func TestPreloadUndefinedSymbol(t *testing.T) {
	dir := buildChain(t)
	isolateCache(t)

	gxx, err := exec.LookPath("g++")
	require.NoError(t, err)

	const broken = "libcppgo_chain_nnn_undefined.so.1"
	compileSource(t, gxx, dir, broken, `
extern "C" int cppgo_chain_absent(void);

extern "C" __attribute__((visibility("default"))) int
cppgo_chain_undefined(void) {
	return cppgo_chain_absent();
}
`, "-Wl,-soname,"+broken)

	objects := chainObjects(t, dir, chainBase, chainMid, broken)
	_, err = cppgo.OpenFS(objects, broken)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "preload the embedded set")
	assert.Contains(t, err.Error(), broken)
	assert.Contains(t, err.Error(), "cppgo_chain_absent")
}

// TestPreloadSystemRuntime is the other side of the omission: the set carries one
// object and the C++ runtime it needs comes from the system's own search, which is
// what a packaged C++ side relies on.
func TestPreloadSystemRuntime(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("no g++ available to build the runtime fixture: %v", err)
	}
	isolateCache(t)

	const name = "libcppgo_runtime_only.so"
	src := filepath.Join(t.TempDir(), "runtime.cc")
	require.NoError(t, os.WriteFile(src, []byte(`
#include <cstdint>
#include <string>

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_runtime_size(void) {
	return static_cast<std::int64_t>(std::string("cppgo").size());
}
`), 0o644))
	dir := t.TempDir()
	args := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17",
		"-o", filepath.Join(dir, name), src}
	runCompiler(t, gxx, args...)

	lib, err := cppgo.OpenFS(chainObjects(t, dir, name), name)
	require.NoError(t, err)
	defer func() { _ = lib.Close() }()

	assert.EqualValues(t, 5, callInt(t, fn(t, lib, "cppgo_runtime_size"), [6]uintptr{}))
}

// TestPreloadReportsABrokenObject: a set member that is not a shared object is
// the loader's failure, reported as the object it could not open.
func TestPreloadUnloadableObject(t *testing.T) {
	isolateCache(t)

	const name = "libcppgo_unloadable.so"
	lib, err := cppgo.OpenFS(fstest.MapFS{
		name: &fstest.MapFile{Data: []byte("this is not an ELF file\n"), Mode: 0o644},
	}, name)
	require.Error(t, err)
	assert.Nil(t, lib)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, name, filepath.Base(openErr.Path))
}

// compileSource builds one shared object named after its soname into dir.
func compileSource(t *testing.T, gxx, dir, name, source string, extra ...string) string {
	t.Helper()

	src := filepath.Join(t.TempDir(), "fixture.cc")
	require.NoError(t, os.WriteFile(src, []byte(source), 0o644))

	out := filepath.Join(dir, name)
	args := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17"}
	args = append(args, extra...)
	args = append(args, "-o", out, src)
	runCompiler(t, gxx, args...)

	return out
}
