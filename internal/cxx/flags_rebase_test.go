package cxx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRebaseArgs covers the shape of a compilation database's relative paths:
// both spellings of every include flag, absolute paths left alone, flags that
// are not paths untouched, and a database with no directory changing nothing.
//
// This is the defect v3 shipped: clangd rebases these paths and libclang does
// not, so a relative "-Iinclude" made the header's declarations invisible and a
// declaration that resolved some other way was bound instead.
func TestRebaseArgs(t *testing.T) {
	const dir = "/build/tree"

	cases := []struct {
		name string
		in   []string
		dir  string
		want []string
	}{
		{
			name: "joined include path",
			in:   []string{"g++", "-Iinclude", "-c", "a.cpp"},
			dir:  dir,
			want: []string{"g++", "-I/build/tree/include", "-c", "a.cpp"},
		},
		{
			name: "separate include path",
			in:   []string{"g++", "-I", "include", "-c", "a.cpp"},
			dir:  dir,
			want: []string{"g++", "-I", "/build/tree/include", "-c", "a.cpp"},
		},
		{
			name: "dot dot is cleaned",
			in:   []string{"-isystem", "../sys/include"},
			dir:  "/build/tree/sub",
			want: []string{"-isystem", "/build/tree/sys/include"},
		},
		{
			name: "isystem joined is not read as I",
			in:   []string{"-isystem/usr/local/include"},
			dir:  dir,
			want: []string{"-isystem/usr/local/include"},
		},
		{
			name: "iquote and idirafter",
			in:   []string{"-iquotefoo", "-idirafter", "bar"},
			dir:  dir,
			want: []string{"-iquote/build/tree/foo", "-idirafter", "/build/tree/bar"},
		},
		{
			name: "absolute paths are untouched",
			in:   []string{"-I/usr/include", "-I", "/opt/include"},
			dir:  dir,
			want: []string{"-I/usr/include", "-I", "/opt/include"},
		},
		{
			name: "include and imacros name files",
			in:   []string{"-include", "config.h", "-imacrosdefs.h"},
			dir:  dir,
			want: []string{"-include", "/build/tree/config.h", "-imacros/build/tree/defs.h"},
		},
		{
			name: "gcc's -I- is a marker, not a path",
			in:   []string{"-I-"},
			dir:  dir,
			want: []string{"-I-"},
		},
		{
			name: "non-path flags are untouched",
			in:   []string{"-std=c++17", "-DFEATURE=1", "-O2", "-fPIC"},
			dir:  dir,
			want: []string{"-std=c++17", "-DFEATURE=1", "-O2", "-fPIC"},
		},
		{
			name: "a defined macro that looks like a path stays as written",
			in:   []string{"-DINCLUDE_DIR=include"},
			dir:  dir,
			want: []string{"-DINCLUDE_DIR=include"},
		},
		{
			name: "no directory means nothing to rebase against",
			in:   []string{"-Iinclude"},
			dir:  "",
			want: []string{"-Iinclude"},
		},
		{
			name: "an include flag with no value is left alone",
			in:   []string{"-I"},
			dir:  dir,
			want: []string{"-I"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, rebaseArgs(tc.in, tc.dir))
		})
	}
}

// TestDiscoverRebasesDatabaseIncludePaths is the regression test at the level
// the bug was reported: a database entry whose `directory` is the build tree and
// whose command carries a relative -I. The flags the parser receives must name
// the absolute directory, because that is the one the declarations are in.
func TestDiscoverRebasesDatabaseIncludePaths(t *testing.T) {
	compiler := cxxCompiler(t)

	root := t.TempDir()
	build := filepath.Join(root, "build")
	include := filepath.Join(root, "include")
	require.NoError(t, os.MkdirAll(build, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(include, "ns"), 0o755))

	header := filepath.Join(include, "ns", "api.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace ns {
struct Point { double x, y; };
double offset(const Point &p) noexcept;
} // namespace ns
`), 0o644))

	// The source the entry is for lives in the build tree and includes the
	// header through the search path: exactly the shape that produced the
	// phantom binding.
	source := filepath.Join(build, "use.cpp")
	require.NoError(t, os.WriteFile(source, []byte("#include <ns/api.hpp>\n"), 0o644))

	entries := []compileCommand{{
		Directory: build,
		File:      "use.cpp",
		Command:   compiler + " -std=c++17 -I../include -c use.cpp -o use.o",
	}}
	data, err := json.MarshalIndent(entries, "", "  ")
	require.NoError(t, err)
	database := filepath.Join(build, "compile_commands.json")
	require.NoError(t, os.WriteFile(database, data, 0o644))

	flags, err := Discover(DiscoverOptions{File: header, CompileCommands: database})
	require.NoError(t, err)
	require.Equal(t, SourceCompileDatabase, flags.Source)

	assert.Contains(t, flags.Args, "-I"+include,
		"the relative -I../include must arrive as the absolute include directory")
	assert.NotContains(t, flags.Args, "-I../include",
		"a path left relative is resolved against cppgo's working directory, not the build's")
}

// TestProjectSeesTheDeclarationThroughARebasedIncludePath is the consequence of
// the rebasing at the parse: with the entry's relative -I left as written, the
// header that declares ns::offset is not found and the symbol is simply absent
// from the model — the parse that produced v3's phantom binding. With the path
// rebased, the declaration is there.
func TestProjectSeesTheDeclarationThroughARebasedIncludePath(t *testing.T) {
	skipWithoutLibclang(t)

	root := t.TempDir()
	build := filepath.Join(root, "build")
	include := filepath.Join(root, "include")
	require.NoError(t, os.MkdirAll(build, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(include, "ns"), 0o755))

	header := filepath.Join(include, "ns", "api.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace ns {
struct Point { double x, y; };
double offset(const Point &p) noexcept;
} // namespace ns
`), 0o644))

	source := filepath.Join(build, "use.cpp")
	require.NoError(t, os.WriteFile(source, []byte("#include <ns/api.hpp>\n"), 0o644))

	entries := []compileCommand{{
		Directory: build,
		File:      "use.cpp",
		Command:   "c++ -std=c++17 -I../include -c use.cpp -o use.o",
	}}
	data, err := json.MarshalIndent(entries, "", "  ")
	require.NoError(t, err)
	database := filepath.Join(build, "compile_commands.json")
	require.NoError(t, os.WriteFile(database, data, 0o644))

	project, err := Open(Options{Header: header, CompileCommands: database})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	var found bool
	for _, sym := range project.Symbols() {
		if sym.QualifiedName == "ns::offset" {
			found = true
			break
		}
	}
	assert.True(t, found, "the declaration reachable only through the rebased -I must be in the model")
}
