package cxx

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDatabase writes a compile_commands.json for the fixture, with the entry
// shape a real build system produces (command, not arguments).
func writeDatabase(t *testing.T, dir, compiler string) string {
	t.Helper()
	entries := []compileCommand{{
		Directory: dir,
		File:      filepath.Join(dir, "mathx.cpp"),
		Command:   compileCommandFor(compiler, dir),
	}}
	data, err := json.MarshalIndent(entries, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(dir, "compile_commands.json")
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func TestDiscoverExplicitArgs(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")

	flags, err := Discover(DiscoverOptions{File: header, Args: []string{"-std=c++20", "-DFEATURE=1"}})
	require.NoError(t, err)
	assert.Equal(t, SourceArgs, flags.Source)
	assert.Equal(t, "arguments", flags.Source.String())
	assert.False(t, flags.Assumed())
	// The language is always made explicit: libclang would treat a .h as C.
	assert.Equal(t, []string{"-std=c++20", "-DFEATURE=1", "-x", "c++-header"}, flags.Args)
	assert.Equal(t, "flags supplied by the caller", flags.Detail)
}

func TestDiscoverKeepsExplicitLanguage(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")
	flags, err := Discover(DiscoverOptions{File: header, Args: []string{"-x", "c++"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"-x", "c++"}, flags.Args, "an explicit -x must not be overridden")
}

func TestDiscoverCompileDatabase(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")
	database := writeDatabase(t, dir, compiler)

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Equal(t, "compile_commands.json", flags.Source.String())
	assert.Equal(t, database, flags.CompileDatabase)
	assert.Contains(t, flags.Detail, database)
	assert.Equal(t, compiler, flags.Compiler)
	assert.Contains(t, flags.Args, "-std=c++17")
	assert.Contains(t, flags.Args, "-I"+dir)
	// The compiler itself, -c, -o and its value, and the input file are gone.
	assert.NotContains(t, flags.Args, compiler)
	assert.NotContains(t, flags.Args, "-c")
	assert.NotContains(t, flags.Args, "-o")
	assert.NotContains(t, flags.Args, filepath.Join(dir, "mathx.cpp"))
	assert.NotContains(t, flags.Args, filepath.Join(dir, "mathx.o"))
	assert.Equal(t, "-x", flags.Args[len(flags.Args)-2])
	assert.Equal(t, "c++-header", flags.Args[len(flags.Args)-1])
}

func TestDiscoverExplicitCompileCommandsPath(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	build := filepath.Join(dir, "build")
	require.NoError(t, os.Mkdir(build, 0o755))
	database := writeDatabase(t, dir, compiler)
	require.NoError(t, os.Rename(database, filepath.Join(build, "compile_commands.json")))

	// The database is not an ancestor of the header any more: only the
	// explicit path can find it.
	header := filepath.Join(dir, "mathx.hpp")
	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceFallback, flags.Source)

	flags, err = Discover(DiscoverOptions{File: header, CompileCommands: build})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Equal(t, filepath.Join(build, "compile_commands.json"), flags.CompileDatabase)
	assert.Contains(t, flags.Detail, "-compile-commands")

	// A direct path to the file works as well.
	flags, err = Discover(DiscoverOptions{File: header, CompileCommands: filepath.Join(build, "compile_commands.json")})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(build, "compile_commands.json"), flags.CompileDatabase)
}

func TestDiscoverNearestDatabaseAncestor(t *testing.T) {
	compiler := cxxCompiler(t)
	root := copyFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "include", "mathx"), 0o755))
	header := filepath.Join(root, "include", "mathx", "mathx.hpp")
	data, err := os.ReadFile(filepath.Join(root, "mathx.hpp"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(header, data, 0o644))
	writeDatabase(t, root, compiler)

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Contains(t, flags.Detail, "nearest ancestor")
}

func TestDiscoverClangdAddRemove(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")
	writeClangd(t, dir, `
CompileFlags:
  Add: [-DFROM_CLANGD=1, -std=c++20]
  Remove: [-std=c++17]
`)

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceClangd, flags.Source)
	assert.Equal(t, ".clangd", flags.Source.String())
	assert.False(t, flags.Assumed())
	assert.Equal(t, []string{filepath.Join(dir, ".clangd")}, flags.ClangdFiles)
	assert.NotContains(t, flags.Args, "-std=c++17", "Remove must drop the database/default flag")
	assert.Contains(t, flags.Args, "-std=c++20")
	assert.Contains(t, flags.Args, "-DFROM_CLANGD=1")
}

func TestDiscoverClangdScalarAndWildcard(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")
	// clangd accepts a bare scalar for Add, and "-W*" as a prefix pattern.
	writeClangd(t, dir, `
CompileFlags:
  Add: -DPREFIX_MATCH
  Remove: [-I*]
`)

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceClangd, flags.Source)
	assert.Contains(t, flags.Args, "-DPREFIX_MATCH")
	for _, arg := range flags.Args {
		assert.NotContains(t, arg, "-I", "the wildcard Remove must drop every -I flag: %q", arg)
	}
}

func TestDiscoverClangdCompiler(t *testing.T) {
	dir := copyFixture(t)
	source := filepath.Join(dir, "mathx.cpp")
	writeClangd(t, dir, `
CompileFlags:
  Compiler: clang++
`)
	flags, err := Discover(DiscoverOptions{File: source})
	require.NoError(t, err)
	assert.Equal(t, "clang++", flags.Compiler)
	assert.Equal(t, SourceClangd, flags.Source)
	assert.Equal(t, []string{"-std=c++17", "-I" + dir, "-x", "c++"}, flags.Args)
}

func TestDiscoverClangdCompilationDatabase(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	build := filepath.Join(dir, "out")
	require.NoError(t, os.Mkdir(build, 0o755))
	database := writeDatabase(t, dir, compiler)
	require.NoError(t, os.Rename(database, filepath.Join(build, "compile_commands.json")))
	writeClangd(t, dir, `
CompileFlags:
  CompilationDatabase: out
`)

	flags, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Equal(t, filepath.Join(build, "compile_commands.json"), flags.CompileDatabase)
	assert.Contains(t, flags.Detail, "CompileFlags.CompilationDatabase")
}

func TestDiscoverClangdAncestorMerge(t *testing.T) {
	dir := copyFixture(t)
	sub := filepath.Join(dir, "src")
	require.NoError(t, os.Mkdir(sub, 0o755))
	header := filepath.Join(sub, "mathx.hpp")
	data, err := os.ReadFile(filepath.Join(dir, "mathx.hpp"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(header, data, 0o644))

	writeClangd(t, dir, `
CompileFlags:
  Add: [-DOUTER=1]
`)
	writeClangd(t, sub, `
CompileFlags:
  Add: [-DINNER=1]
  Remove: [-std=c++17]
`)

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceClangd, flags.Source)
	assert.Equal(t, []string{filepath.Join(dir, ".clangd"), filepath.Join(sub, ".clangd")}, flags.ClangdFiles,
		"ancestors must be merged outermost first")
	// Add lists are concatenated outermost first, as clangd does, and Remove
	// applies to the base command line, not to somebody else's Add.
	assert.Contains(t, flags.Args, "-DOUTER=1")
	assert.Contains(t, flags.Args, "-DINNER=1")
	assert.NotContains(t, flags.Args, "-std=c++17", "the nearer file must be able to remove a base flag")
}

func TestDiscoverFallback(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")

	flags, err := Discover(DiscoverOptions{File: header})
	require.NoError(t, err)
	assert.Equal(t, SourceFallback, flags.Source)
	assert.Equal(t, "fallback", flags.Source.String())
	assert.True(t, flags.Assumed(), "the generator must be able to say these flags are a guess")
	assert.Contains(t, flags.Detail, "assumed")
	assert.Contains(t, flags.Detail, dir)
	assert.Equal(t, []string{"-std=c++17", "-I" + dir, "-x", "c++-header"}, flags.Args)
	assert.Empty(t, flags.CompileDatabase)
}

func TestDiscoverDatabaseWithoutMatchingEntry(t *testing.T) {
	dir := copyFixture(t)
	other := t.TempDir()
	// Two entries, neither of which concerns our header: the header is in a
	// directory of its own, so no source includes it either.
	entries := []compileCommand{
		{Directory: other, File: filepath.Join(other, "a.cpp"), Command: "g++ -std=c++17 -c a.cpp"},
		{Directory: other, File: filepath.Join(other, "b.cpp"), Command: "g++ -std=c++17 -c b.cpp"},
	}
	data, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compile_commands.json"), data, 0o644))

	flags, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	require.NoError(t, err)
	assert.Equal(t, SourceFallback, flags.Source)
	assert.Contains(t, flags.Detail, "no entry")
	assert.Contains(t, flags.Detail, "assumed")
}

func TestDiscoverDatabaseWithArguments(t *testing.T) {
	dir := copyFixture(t)
	entries := []compileCommand{{
		Directory: dir,
		File:      filepath.Join(dir, "mathx.cpp"),
		Arguments: []string{"clang++", "-std=c++17", "-I" + dir, "-c", filepath.Join(dir, "mathx.cpp")},
	}}
	data, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compile_commands.json"), data, 0o644))

	flags, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Equal(t, []string{"-std=c++17", "-I" + dir, "-x", "c++-header"}, flags.Args)
}

func TestDiscoverSingleEntryDatabaseBorrowedByHeader(t *testing.T) {
	dir := copyFixture(t)
	entries := []compileCommand{{
		Directory: dir,
		File:      "mathx.cpp", // relative: the database's directory resolves it
		Command:   "g++ -std=c++17 -Done=1 -c mathx.cpp",
	}}
	data, err := json.Marshal(entries)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "compile_commands.json"), data, 0o644))

	flags, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Contains(t, flags.Args, "-Done=1")
}

func TestDiscoverErrors(t *testing.T) {
	dir := copyFixture(t)
	header := filepath.Join(dir, "mathx.hpp")

	_, err := Discover(DiscoverOptions{})
	assert.Error(t, err, "no file")

	_, err = Discover(DiscoverOptions{File: filepath.Join(dir, "absent.hpp")})
	assert.Error(t, err)

	_, err = Discover(DiscoverOptions{File: header, CompileCommands: filepath.Join(dir, "absent.json")})
	assert.Error(t, err)

	empty := filepath.Join(dir, "emptydb")
	require.NoError(t, os.Mkdir(empty, 0o755))
	_, err = Discover(DiscoverOptions{File: header, CompileCommands: empty})
	assert.Error(t, err, "a directory without compile_commands.json")

	bad := filepath.Join(dir, "compile_commands.json")
	require.NoError(t, os.WriteFile(bad, []byte("{not json"), 0o644))
	_, err = Discover(DiscoverOptions{File: header})
	assert.Error(t, err)

	require.NoError(t, os.WriteFile(bad, []byte(`[{"directory":"`+dir+`","file":"`+dir+`/mathx.cpp"}]`), 0o644))
	_, err = Discover(DiscoverOptions{File: header})
	assert.Error(t, err, "an entry with neither command nor arguments")

	require.NoError(t, os.Remove(bad))
	writeClangd(t, dir, "CompileFlags: [this is: not, valid: yaml\n")
	_, err = Discover(DiscoverOptions{File: header})
	assert.Error(t, err, "a malformed .clangd must be reported")
}

func TestDiscoverExplicitPathMissing(t *testing.T) {
	dir := copyFixture(t)
	_, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp"), CompileCommands: filepath.Join(dir, "nope", "compile_commands.json")})
	assert.Error(t, err)
}

func TestSplitCommand(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"g++ -std=c++17 -c a.cpp", []string{"g++", "-std=c++17", "-c", "a.cpp"}},
		{`g++ -DNAME="a b" -c a.cpp`, []string{"g++", "-DNAME=a b", "-c", "a.cpp"}},
		{`g++ -DNAME='a b'`, []string{"g++", "-DNAME=a b"}},
		{"g++   -c\ta.cpp", []string{"g++", "-c", "a.cpp"}},
		{`g++ -I"dir with space" x.cpp`, []string{"g++", "-Idir with space", "x.cpp"}},
		{`g++ -DP=\"quoted\"`, []string{"g++", `-DP="quoted"`}},
		{"", nil},
	} {
		got, err := splitCommand(tc.command)
		require.NoErrorf(t, err, "splitting %q", tc.command)
		assert.Equal(t, tc.want, got, "splitting %q", tc.command)
	}

	_, err := splitCommand(`g++ -DNAME="unterminated`)
	assert.Error(t, err)
}

func TestSanitizeArgs(t *testing.T) {
	file := "/src/mathx.cpp"
	args := sanitizeArgs([]string{
		"clang++", "-std=c++17", "-c", "-o", "mathx.o", "-oobj.o",
		file, "/src/other.cpp", "-I/inc", "/src/thing.h", "value",
	}, file)
	assert.Equal(t, []string{"clang++", "-std=c++17", "-I/inc", "value"}, args)
}

func TestSanitizeArgsKeepsFlagValues(t *testing.T) {
	// "-include mathx.hpp" names a header that is force-included, not the input
	// file: dropping it would change the translation unit.
	file := "/src/mathx.cpp"
	args := sanitizeArgs([]string{"clang++", "-include", "forced.hpp", "-I", "dir", "-x", "c++", file}, file)
	assert.Equal(t, []string{"clang++", "-include", "forced.hpp", "-I", "dir", "-x", "c++"}, args)
}

func TestNearestDatabase(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))
	assert.Empty(t, nearestDatabase(nested))

	database := filepath.Join(root, "compile_commands.json")
	require.NoError(t, os.WriteFile(database, []byte("[]"), 0o644))
	assert.Equal(t, database, nearestDatabase(nested))
	assert.Equal(t, database, nearestDatabase(root))
}

func TestIncludes(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "a.cpp")
	require.NoError(t, os.WriteFile(source, []byte("#include <vector>\n#include \"mathx.hpp\"\n"), 0o644))
	assert.True(t, includes(source, "mathx.hpp"))
	assert.False(t, includes(source, "other.hpp"))
	assert.False(t, includes(filepath.Join(dir, "missing.cpp"), "mathx.hpp"))

	angled := filepath.Join(dir, "b.cpp")
	require.NoError(t, os.WriteFile(angled, []byte("#include <mathx.hpp>\n"), 0o644))
	assert.True(t, includes(angled, "mathx.hpp"))

	commented := filepath.Join(dir, "c.cpp")
	require.NoError(t, os.WriteFile(commented, []byte("// #include \"mathx.hpp\"\n"), 0o644))
	assert.False(t, includes(commented, "mathx.hpp"), "only real preprocessor lines count")
}

func TestFlagSourceString(t *testing.T) {
	assert.Equal(t, "arguments", SourceArgs.String())
	assert.Equal(t, "compile_commands.json", SourceCompileDatabase.String())
	assert.Equal(t, ".clangd", SourceClangd.String())
	assert.Equal(t, "fallback", SourceFallback.String())
	assert.Equal(t, "unknown", FlagSource(99).String())
}

func TestYamlStringListRejectsMapping(t *testing.T) {
	dir := copyFixture(t)
	writeClangd(t, dir, `
CompileFlags:
  Add:
    -std: c++20
`)
	_, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	assert.Error(t, err)
}

func TestExtensionHelpers(t *testing.T) {
	for _, name := range []string{"a.h", "a.HH", "a.hxx", "a.hpp", "a.inl", "a.ipp", "a.tcc", "a.tpp"} {
		assert.True(t, isHeaderFile(name), name)
	}
	for _, name := range []string{"a.cc", "a.cpp", "a.cxx", "a.c++", "a.cp"} {
		assert.True(t, isSourceFile(name), name)
	}
	assert.False(t, isHeaderFile("a.cpp"))
	assert.False(t, isSourceFile("a.hpp"))
	assert.True(t, isCxx("clang++"))
	assert.True(t, isCxx("/usr/bin/g++"))
	assert.True(t, isCxx("cxx"))
	assert.False(t, isCxx(""))
	assert.False(t, isCxx("gcc"))
}

// writeClangd writes a .clangd file into dir.
func writeClangd(t *testing.T, dir, body string) string {
	t.Helper()
	path := filepath.Join(dir, ".clangd")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}
