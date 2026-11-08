package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIncludeRoots pins the rule that decides which system headers belong to
// the header being bound: the longest include search directory that contains
// it, and its own directory when no include path does.
func TestIncludeRoots(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "lib", "eigen3", "Eigen", "Dense")
	require.NoError(t, os.MkdirAll(filepath.Dir(header), 0o755))
	require.NoError(t, os.WriteFile(header, []byte("\n"), 0o644))

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "no include path falls back to the header's directory", args: []string{"-std=c++17"}, want: filepath.Join(dir, "lib", "eigen3", "Eigen")},
		{name: "joined -I", args: []string{"-I" + filepath.Join(dir, "lib", "eigen3")}, want: filepath.Join(dir, "lib", "eigen3")},
		{name: "separated -isystem", args: []string{"-isystem", filepath.Join(dir, "lib", "eigen3")}, want: filepath.Join(dir, "lib", "eigen3")},
		{name: "joined -isystem", args: []string{"-isystem" + filepath.Join(dir, "lib", "eigen3")}, want: filepath.Join(dir, "lib", "eigen3")},
		{name: "the longest containing path wins", args: []string{
			"-I" + filepath.Join(dir, "lib"),
			"-I" + filepath.Join(dir, "lib", "eigen3"),
		}, want: filepath.Join(dir, "lib", "eigen3")},
		{name: "a path that does not contain the header is ignored", args: []string{
			"-I" + filepath.Join(dir, "other"),
		}, want: filepath.Join(dir, "lib", "eigen3", "Eigen")},
		{name: "-iquote and -idirafter count", args: []string{
			"-iquote", filepath.Join(dir, "lib", "eigen3"),
			"-idirafter", filepath.Join(dir, "lib"),
		}, want: filepath.Join(dir, "lib", "eigen3")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{tc.want}, includeRoots(header, tc.args))
		})
	}
}

// TestProjectBindsASystemHeaderUnderANamedRoot is the regression for a library
// installed where the compiler treats it as a system header: the header the
// caller named is the API, so its declarations are read even when the include
// path marks the directory as system.
func TestProjectBindsASystemHeaderUnderANamedRoot(t *testing.T) {
	skipWithoutLibclang(t)
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
		return path
	}
	write("lib.hpp", `
namespace lib {
/** A counting type. */
class Counter {
public:
    Counter();
    int value() const;
};
int sum(int a, int b);
}
`)
	header := write("user.hpp", "#include \"lib.hpp\"\n")

	project, err := Open(Options{
		Header: header,
		Args:   []string{"-std=c++17", "-xc++-header", "-isystem", dir},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	// The type is read from a system header, which the include graph marks as
	// one; the root is what makes it part of the API.
	assert.NotEmpty(t, project.Symbols(), "the named header's declarations are the API")
	record, ok := project.Record("lib::Counter")
	require.True(t, ok, "lib::Counter must be read from the system header")
	assert.True(t, record.LayoutKnown)
	_, ok = project.Lookup("lib::sum")
	assert.True(t, ok)
}

// TestProjectKeepsAnotherSystemRootOut is the other half: a system header the
// caller did not name stays out of the model, so the standard library and a
// dependency's internals never reach the emitter.
func TestProjectKeepsAnotherSystemRootOut(t *testing.T) {
	skipWithoutLibclang(t)
	dir := t.TempDir()
	named := filepath.Join(dir, "named")
	other := filepath.Join(dir, "other")
	require.NoError(t, os.MkdirAll(named, 0o755))
	require.NoError(t, os.MkdirAll(other, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(other, "dep.hpp"), []byte(`
namespace dep { class Hidden { public: int x; }; int hidden(); }
`), 0o644))
	header := filepath.Join(named, "api.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include "dep.hpp"
namespace api { int visible(int v); }
`), 0o644))

	project, err := Open(Options{
		Header: header,
		Args:   []string{"-std=c++17", "-xc++-header", "-isystem", named, "-isystem", other},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	_, ok := project.Lookup("api::visible")
	assert.True(t, ok, "the named header's own declaration is read")
	_, ok = project.Lookup("dep::hidden")
	assert.False(t, ok, "a dependency's system header is not the API")
	_, ok = project.Record("dep::Hidden")
	assert.False(t, ok)
}
