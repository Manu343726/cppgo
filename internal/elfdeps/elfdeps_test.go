package elfdeps_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/elfdeps"
)

// The fixtures are C++ on purpose: linking them pulls in libstdc++, libm and
// libc, which is the runtime half of a real dependency tree.
const (
	depSource = `
#include <cstdint>
#include <string>

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_deps_dep_value(std::int64_t v) {
    return v + static_cast<std::int64_t>(std::string("x").size());
}
`

	userSource = `
#include <cstdint>
#include <string>

extern "C" std::int64_t cppgo_deps_dep_value(std::int64_t v);

extern "C" __attribute__((visibility("default"))) std::int64_t
cppgo_deps_user_value(std::int64_t v) {
    return cppgo_deps_dep_value(v) +
           static_cast<std::int64_t>(std::to_string(v).size());
}
`
)

// The file name and the soname differ on purpose: the loader records the soname
// in a DT_NEEDED entry, so a resolver that only knows the file the library was
// built as fails.
const (
	depFile   = "libcppgo_deps_dep.so.1.0.0"
	depSOName = "libcppgo_deps_dep.so.1"
	userName  = "libcppgo_deps_user.so"
)

// build compiles one shared object into dir and returns its path.
func build(t *testing.T, dir, name, source string, extra ...string) string {
	t.Helper()

	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("no g++ available to build the ELF fixture: %v", err)
	}

	src := filepath.Join(t.TempDir(), "fixture.cc")
	require.NoError(t, os.WriteFile(src, []byte(source), 0o644))

	out := filepath.Join(dir, name)
	args := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17"}
	args = append(args, extra...)
	args = append(args, "-o", out, src)
	if output, err := exec.Command(gxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, output)
	}

	return out
}

// buildPair builds the dependency and a user library that needs it by soname,
// and returns the directory, the dependency under the name the loader resolves
// (its soname) and the user library.
func buildPair(t *testing.T) (dir, dep, user string) {
	t.Helper()

	dir = t.TempDir()
	dep = copyInto(t, build(t, dir, depFile, depSource, "-Wl,-soname,"+depSOName), dir, depSOName)
	user = build(t, dir, userName, userSource, "-L"+dir, "-l:"+depFile)

	return dir, dep, user
}

// names returns the DT_NEEDED names of a slice of dependencies.
func names(deps []elfdeps.Dependency) []string {
	out := make([]string, len(deps))
	for i, dep := range deps {
		out[i] = dep.Name
	}

	return out
}

func TestRead(t *testing.T) {
	_, dep, user := buildPair(t)

	obj, err := elfdeps.Read(user)
	require.NoError(t, err)

	assert.Equal(t, user, obj.Path)
	assert.Empty(t, obj.SOName, "the fixture declares no soname of its own")
	assert.Contains(t, obj.Needed, depSOName, "the soname is what the linker recorded")
	assert.Empty(t, obj.RPath)
	assert.Empty(t, obj.RunPath)

	read, err := elfdeps.Read(dep)
	require.NoError(t, err)
	assert.Equal(t, depSOName, read.SOName)
}

func TestReadRejects(t *testing.T) {
	dir := t.TempDir()

	t.Run("not an ELF file", func(t *testing.T) {
		path := filepath.Join(dir, "libbroken.so")
		require.NoError(t, os.WriteFile(path, []byte("this is not an ELF file\n"), 0o644))

		_, err := elfdeps.Read(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "libbroken.so")
	})

	t.Run("an executable", func(t *testing.T) {
		gxx, err := exec.LookPath("g++")
		if err != nil {
			t.Skipf("no g++ available: %v", err)
		}
		src := filepath.Join(dir, "main.cc")
		require.NoError(t, os.WriteFile(src, []byte("int main() { return 0; }\n"), 0o644))
		path := filepath.Join(dir, "program")
		if output, err := exec.Command(gxx, "-no-pie", "-o", path, src).CombinedOutput(); err != nil {
			t.Fatalf("build the executable: %v\n%s", err, output)
		}

		_, err = elfdeps.Read(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a shared object")
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := elfdeps.Read(filepath.Join(dir, "libabsent.so"))
		require.Error(t, err)
	})
}

func TestTree(t *testing.T) {
	dir, dep, user := buildPair(t)

	tree, err := elfdeps.Resolver{LibraryPath: []string{dir}}.Tree(user)
	require.NoError(t, err)

	assert.Equal(t, user, tree.Path)
	assert.Empty(t, tree.Missing)

	byName := map[string]elfdeps.Dependency{}
	index := map[string]int{}
	for i, d := range tree.Order {
		byName[d.Name] = d
		index[d.Name] = i
	}

	custom, ok := byName[depSOName]
	require.True(t, ok, "the custom dependency is in the closure")
	assert.Equal(t, dep, custom.Path)
	assert.Equal(t, depSOName, custom.SOName)
	assert.Equal(t, user, custom.NeededBy)
	assert.False(t, custom.Runtime)

	// The C/C++ runtime is resolved too, and classified as what a deployment
	// does not carry.
	for _, name := range []string{"libstdc++.so.6", "libc.so.6"} {
		dep, ok := byName[name]
		require.True(t, ok, "%s is in the closure", name)
		assert.True(t, dep.Runtime, "%s is runtime", name)
		assert.NotEmpty(t, dep.Path)
	}

	// Load order: libstdc++ is placed after the libc it needs, so a preload can
	// load it at that point.
	assert.Less(t, index["libc.so.6"], index["libstdc++.so.6"])

	// The packaging choice reads the two halves apart.
	assert.Equal(t, []string{depSOName}, names(tree.NonRuntime()))
	assert.Contains(t, names(tree.Runtime()), "libstdc++.so.6")
}

func TestTreeMissing(t *testing.T) {
	dir, dep, user := buildPair(t)
	require.NoError(t, os.Remove(dep))

	tree, err := elfdeps.Resolver{LibraryPath: []string{dir}}.Tree(user)
	require.NoError(t, err)

	require.Len(t, tree.Missing, 1)
	missing := tree.Missing[0]
	assert.Equal(t, depSOName, missing.Name)
	assert.Empty(t, missing.Path)
	assert.Equal(t, user, missing.NeededBy)
	assert.False(t, missing.Runtime)

	// What did resolve is still reported.
	assert.NotEmpty(t, tree.Order)
}

func TestTreeReportsUnreadableDependency(t *testing.T) {
	dir := t.TempDir()

	// A dependency the linker records and that is then replaced by something
	// that is not an ELF object: the name resolves, the file does not.
	dep := copyInto(t, build(t, dir, depFile, depSource, "-Wl,-soname,"+depSOName), dir, depSOName)
	user := build(t, dir, userName, userSource, "-L"+dir, "-l:"+depFile)
	require.NoError(t, os.WriteFile(dep, []byte("this is not an ELF file\n"), 0o644))

	tree, err := elfdeps.Resolver{LibraryPath: []string{dir}}.Tree(user)
	require.NoError(t, err)

	require.Len(t, tree.Missing, 1)
	assert.Equal(t, depSOName, tree.Missing[0].Name)
	assert.Empty(t, tree.Missing[0].Path)
}

// copyInto copies a fixture into dir under name and returns the copy's path.
func copyInto(t *testing.T, src, dir, name string) string {
	t.Helper()

	data, err := os.ReadFile(src)
	require.NoError(t, err)
	dst := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(dst, data, 0o755))

	return dst
}

// requester reads the fixture pair's user library, which is the object the
// search order is measured from.
func requester(t *testing.T, user string) *elfdeps.Object {
	t.Helper()

	obj, err := elfdeps.Read(user)
	require.NoError(t, err)

	return obj
}

func TestResolveSearchOrder(t *testing.T) {
	dir, dep, user := buildPair(t)
	obj := requester(t, user)

	t.Run("LD_LIBRARY_PATH", func(t *testing.T) {
		path, ok := elfdeps.Resolver{LibraryPath: []string{dir}}.Resolve(obj, depSOName)
		require.True(t, ok)
		assert.Equal(t, dep, path)
	})

	t.Run("RPATH before LD_LIBRARY_PATH", func(t *testing.T) {
		rpath := t.TempDir()
		want := copyInto(t, dep, rpath, depSOName)
		withRPath := build(t, dir, "libcppgo_deps_rpath.so", userSource,
			"-Wl,--disable-new-dtags,-rpath,"+rpath, "-L"+dir, "-l:"+depFile)
		requester := requester(t, withRPath)
		require.Equal(t, []string{rpath}, requester.RPath)
		require.Empty(t, requester.RunPath, "the old dtags give DT_RPATH")

		path, ok := elfdeps.Resolver{LibraryPath: []string{dir}}.Resolve(requester, depSOName)
		require.True(t, ok)
		assert.Equal(t, want, path, "DT_RPATH wins over LD_LIBRARY_PATH")
	})

	t.Run("LD_LIBRARY_PATH before RUNPATH", func(t *testing.T) {
		runpath := t.TempDir()
		copyInto(t, dep, runpath, depSOName)
		withRunPath := build(t, dir, "libcppgo_deps_runpath.so", userSource,
			"-Wl,--enable-new-dtags,-rpath,"+runpath, "-L"+dir, "-l:"+depFile)
		requester := requester(t, withRunPath)
		require.Equal(t, []string{runpath}, requester.RunPath)
		require.Empty(t, requester.RPath, "the new dtags give DT_RUNPATH")

		path, ok := elfdeps.Resolver{LibraryPath: []string{dir}}.Resolve(requester, depSOName)
		require.True(t, ok)
		assert.Equal(t, dep, path, "LD_LIBRARY_PATH wins over DT_RUNPATH")

		path, ok = elfdeps.Resolver{LibraryPath: []string{}}.Resolve(requester, depSOName)
		require.True(t, ok)
		assert.Equal(t, filepath.Join(runpath, depSOName), path, "and RUNPATH is the next step")
	})

	t.Run("$ORIGIN in a run path", func(t *testing.T) {
		beside := t.TempDir()
		depThere := copyInto(t, dep, beside, depSOName)
		userThere := build(t, beside, "libcppgo_deps_origin.so", userSource,
			"-Wl,-rpath,$ORIGIN", "-L"+dir, "-l:"+depFile)
		requester := requester(t, userThere)

		path, ok := elfdeps.Resolver{LibraryPath: []string{}}.Resolve(requester, depSOName)
		require.True(t, ok)
		assert.Equal(t, depThere, path, "$ORIGIN is the directory the object sits in")
	})

	t.Run("the object itself", func(t *testing.T) {
		given := filepath.Join(dir, depSOName)
		path, ok := elfdeps.Resolver{LibraryPath: []string{}}.Resolve(obj, given)
		require.True(t, ok)
		assert.Equal(t, given, path)

		_, ok = elfdeps.Resolver{LibraryPath: []string{}}.Resolve(obj, filepath.Join(dir, "no-such.so"))
		assert.False(t, ok)
	})

	t.Run("the default directories", func(t *testing.T) {
		defaults := t.TempDir()
		want := copyInto(t, dep, defaults, depSOName)

		path, ok := elfdeps.Resolver{
			LibraryPath: []string{},
			Cache:       filepath.Join(t.TempDir(), "no-cache"),
			DefaultDirs: []string{defaults},
		}.Resolve(obj, depSOName)
		require.True(t, ok)
		assert.Equal(t, want, path)
	})
}

func TestResolveNothing(t *testing.T) {
	_, _, user := buildPair(t)
	obj := requester(t, user)

	// The zero resolver reads the process's LD_LIBRARY_PATH, the system cache and
	// the platform defaults, and none of them can hold this name.
	_, ok := elfdeps.Resolver{}.Resolve(obj, "libcppgo_deps_nowhere.so.9")
	assert.False(t, ok)
}

func TestIsRuntime(t *testing.T) {
	cases := []struct {
		name    string
		runtime bool
	}{
		{"libc.so.6", true},
		{"libm.so.6", true},
		{"libstdc++.so.6", true},
		{"libgcc_s.so.1", true},
		{"ld-linux-x86-64.so.2", true},
		{"linux-vdso.so.1", true},
		{"libasan.so.8", true},
		{"/usr/lib/libc.so.6", true},
		{"libcppgo_deps_dep.so.1", false},
		{"libmathx.so", false},
		{"libz.so.1", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.runtime, elfdeps.IsRuntime(tc.name))
		})
	}
}

func TestTreeUnreadableRoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libbroken.so")
	require.NoError(t, os.WriteFile(path, []byte("this is not an ELF file\n"), 0o644))

	tree, err := elfdeps.Resolver{}.Tree(path)
	require.Error(t, err)
	assert.Nil(t, tree)
}

func TestResolveWithoutRequester(t *testing.T) {
	dir, dep, _ := buildPair(t)

	// No requester means no run path to expand: the search is LD_LIBRARY_PATH
	// and the loader's own files.
	path, ok := elfdeps.Resolver{LibraryPath: []string{dir}}.Resolve(nil, depSOName)
	require.True(t, ok)
	assert.Equal(t, dep, path)
}
