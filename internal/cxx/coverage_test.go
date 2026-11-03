package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file covers the branches that only reachable-by-construction failures
// touch: injected libclang failures, unreadable files, malformed configuration.
// They live together because none of them is about the C++ model.

func TestNotFoundErrorWithoutCause(t *testing.T) {
	err := &NotFoundError{Paths: []string{"/one", "/two"}}
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	assert.Contains(t, err.Error(), "install libclang-dev")
	assert.Contains(t, err.Error(), "/one, /two")
}

func TestLoadFailsWhenEveryCandidateIsBroken(t *testing.T) {
	// A candidate that exists but is not libclang must be reported as "not
	// found", with the binding error as the cause.
	compiler := ccCompiler(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "fake.c")
	require.NoError(t, os.WriteFile(source, []byte(fakeLibclangSource()), 0o644))
	fake := filepath.Join(dir, "libfake.so")
	output, err := command(t, compiler, "-shared", "-fPIC", "-o", fake, source)
	require.NoErrorf(t, err, "building the fake library: %s", output)

	withCandidates(t, []string{filepath.Join(dir, "absent.so"), fake})
	resetLibclangCache(t)

	_, err = Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	var notFound *NotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.NotNil(t, notFound.Cause, "a candidate that exists but fails to bind must be the cause")
}

func TestLoadWhenNoCandidateExistsAtAll(t *testing.T) {
	withCandidates(t, []string{filepath.Join(t.TempDir(), "absent.so")})
	resetLibclangCache(t)

	_, err := Load()
	require.Error(t, err)
	var notFound *NotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Nil(t, notFound.Cause, "nothing existed, so there is nothing to blame")
}

func TestParseTranslationUnitWithNullIndex(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	// A translation unit needs an index; simulate libclang handing us none.
	// The struct is used in place (it holds a mutex, so it must not be copied).
	saved := library.f.createIndex
	library.f.createIndex = func(excludeDeclsFromPCH, displayDiagnostics int32) uintptr { return 0 }
	t.Cleanup(func() { library.f.createIndex = saved })
	_, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "null index")
}

func TestVisitWithUnregisteredKey(t *testing.T) {
	// The C trampoline can be entered with a cookie nobody registered (a late
	// callback after a visit was cancelled); that must stop the traversal
	// instead of dereferencing anything.
	assert.Equal(t, visitBreak, dispatchVisit(CXCursor{}, CXCursor{}, nil))
	assert.Equal(t, visitBreak, dispatchVisit(CXCursor{}, CXCursor{}, unsafePointerOf(&visitKey{})))
}

func TestVisitOnClosedLibclang(t *testing.T) {
	library := skipWithoutLibclang(t)
	path := library.Path()
	second, err := LoadPath(path)
	require.NoError(t, err)
	require.NoError(t, second.Close())

	err = second.visit(CXCursor{}, func(cursor, parent CXCursor) int32 { return visitContinue })
	assert.ErrorIs(t, err, ErrClosed)
}

func TestSymbolsAfterLibclangClosed(t *testing.T) {
	library := skipWithoutLibclang(t)
	path := library.Path()
	dir := copyFixture(t)
	second, err := LoadPath(path)
	require.NoError(t, err)
	tu, err := second.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), []string{"-std=c++17", "-I" + dir})
	require.NoError(t, err)
	// The trampoline refuses to run through a closed handle; Symbols reports
	// nothing rather than a crash or a stale answer.
	require.NoError(t, second.Close())
	assert.Empty(t, tu.Symbols())
}

func TestQualify(t *testing.T) {
	parent := func(kind int32, spelling string) parentName {
		return parentName{kind: kind, spelling: spelling}
	}
	tu := parent(cursorTranslationUnit, "/tmp/mathx.cpp")

	for _, tc := range []struct {
		name     string
		spelling string
		parents  []parentName
		want     string
		wantOK   bool
	}{
		{
			name:     "free function",
			spelling: "make",
			parents:  []parentName{parent(cursorNamespace, "mathx"), tu},
			want:     "mathx::make",
			wantOK:   true,
		},
		{
			name:     "method",
			spelling: "norm",
			parents:  []parentName{parent(cursorClassDecl, "Vector"), parent(cursorNamespace, "mathx"), tu},
			want:     "mathx::Vector::norm",
			wantOK:   true,
		},
		{
			name:     "global function",
			spelling: "main",
			parents:  []parentName{tu},
			want:     "main",
			wantOK:   true,
		},
		{
			name:     "empty spellings are dropped from the chain",
			spelling: "f",
			parents:  []parentName{parent(cursorLinkageSpec, ""), parent(cursorNamespace, "ns"), tu},
			want:     "ns::f",
			wantOK:   true,
		},
		{
			name:    "unnamed entity",
			parents: []parentName{tu},
			wantOK:  false,
		},
		{
			name:     "anonymous namespace",
			spelling: "helper",
			parents:  []parentName{parent(cursorNamespace, ""), tu},
			wantOK:   false,
		},
		{
			name:     "chain that never reaches the translation unit",
			spelling: "f",
			parents:  []parentName{parent(cursorNamespace, "ns")},
			wantOK:   false,
		},
		{
			name:     "invalid parent cursor",
			spelling: "f",
			parents:  []parentName{parent(cursorInvalidFile, "")},
			want:     "f",
			wantOK:   true,
		},
	} {
		got, ok := qualify(tc.spelling, tc.parents)
		assert.Equal(t, tc.wantOK, ok, tc.name)
		assert.Equal(t, tc.want, got, tc.name)
	}
}

func TestProjectOpenWithoutLibclang(t *testing.T) {
	dir := copyFixture(t)
	resetLibclangCache(t)
	t.Setenv("CPPGO_LIBCLANG", filepath.Join(dir, "absent.so"))

	_, err := Open(Options{Header: filepath.Join(dir, "mathx.hpp"), Args: []string{"-std=c++17", "-I" + dir}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
}

func TestProjectOpenWithUnparseableHeader(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	_, err := Open(Options{
		Header: filepath.Join(dir, "mathx.hpp"),
		Args:   []string{"-std=c++99"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing")
}

func TestProjectMissingExplicitSource(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	_, err := Open(Options{
		Header:  filepath.Join(dir, "mathx.hpp"),
		Args:    []string{"-std=c++17", "-I" + dir},
		Sources: []string{filepath.Join(dir, "absent.cpp")},
	})
	require.Error(t, err)
}

func TestProjectSourceDiscoveryFails(t *testing.T) {
	// A source outside the header's directory is discovered through the
	// compilation database, and asking for its flags must fail if its own
	// .clangd is malformed.
	skipWithoutLibclang(t)
	compiler := cxxCompiler(t)
	root := copyFixture(t)
	src := filepath.Join(root, "impl")
	require.NoError(t, os.Mkdir(src, 0o755))
	data, err := os.ReadFile(filepath.Join(root, "mathx.cpp"))
	require.NoError(t, err)
	moved := filepath.Join(src, "mathx.cpp")
	require.NoError(t, os.WriteFile(moved, data, 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, "mathx.cpp")))
	require.NoError(t, os.WriteFile(filepath.Join(root, "compile_commands.json"),
		[]byte(`[{"directory":"`+src+`","file":"`+moved+`","command":"`+compileCommandFor(compiler, src)+`"}]`), 0o644))
	writeClangd(t, src, "CompileFlags: [this is: not, valid: yaml\n")

	_, err = Open(Options{Header: filepath.Join(root, "mathx.hpp")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".clangd")
}

func TestProjectSourcesFromCompilationDatabase(t *testing.T) {
	// The definition lives outside the header's directory, so only the
	// compilation database can reveal it.
	skipWithoutLibclang(t)
	compiler := cxxCompiler(t)
	root := copyFixture(t)
	src := filepath.Join(root, "impl")
	require.NoError(t, os.Mkdir(src, 0o755))
	data, err := os.ReadFile(filepath.Join(root, "mathx.cpp"))
	require.NoError(t, err)
	moved := filepath.Join(src, "mathx.cpp")
	require.NoError(t, os.WriteFile(moved, data, 0o644))
	require.NoError(t, os.Remove(filepath.Join(root, "mathx.cpp")))
	// Two entries, one for a header (not a source) and one for the real source.
	require.NoError(t, os.WriteFile(filepath.Join(root, "compile_commands.json"), []byte(
		`[{"directory":"`+root+`","file":"`+filepath.Join(root, "mathx.hpp")+`","command":"`+compiler+` -std=c++17 -I`+root+` -c mathx.hpp"},
		  {"directory":"`+src+`","file":"`+moved+`","command":"`+compileCommandFor(compiler, root)+`"}]`), 0o644))

	project, err := Open(Options{Header: filepath.Join(root, "mathx.hpp")})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()

	require.Len(t, project.MainFiles, 2)
	assert.Equal(t, filepath.Clean(moved), project.MainFiles[1])
	norm, ok := project.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.True(t, norm.HasOutOfLineBody)
}

func TestProjectCloseWithoutLibrary(t *testing.T) {
	project := &Project{}
	require.NoError(t, project.Close(), "a partially built project must close cleanly")
}

func TestReadDatabaseErrors(t *testing.T) {
	_, err := readDatabase(t.TempDir())
	assert.Error(t, err, "a directory is not a compilation database")
	_, err = readDatabase(filepath.Join(t.TempDir(), "absent.json"))
	assert.Error(t, err)
}

func TestReadClangdError(t *testing.T) {
	_, err := readClangd(t.TempDir())
	assert.Error(t, err)
}

func TestIncludesUnreadableFile(t *testing.T) {
	// A directory passes the stat check and fails the read.
	assert.False(t, includes(t.TempDir(), "mathx.hpp"))
}

func TestDatabaseEntrySelection(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	source := filepath.Join(dir, "mathx.cpp")
	header := filepath.Join(dir, "mathx.hpp")

	// Two entries, so the single-entry shortcut does not apply: the source's
	// own entry must be found, and the header must borrow a source that
	// includes it.
	database := filepath.Join(dir, "compile_commands.json")
	require.NoError(t, os.WriteFile(database, []byte(
		`[{"directory":"`+dir+`","file":"`+filepath.Join(dir, "other.cpp")+`","command":"`+compiler+` -DOTHER=1 -c other.cpp"},
		  {"directory":"`+dir+`","file":"`+source+`","command":"`+compileCommandFor(compiler, dir)+`"}]`), 0o644))

	entry, err := databaseEntry(database, source)
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, source, entry.path())

	entry, err = databaseEntry(database, header)
	require.NoError(t, err)
	require.NotNil(t, entry, "the header borrows the flags of a source that includes it")
	assert.Equal(t, source, entry.path())

	// A header entry in the database is skipped by the includes scan (it is not
	// a source), and no entry matches a header nobody includes.
	require.NoError(t, os.WriteFile(database, []byte(
		`[{"directory":"`+dir+`","file":"`+header+`","command":"`+compiler+` -c mathx.hpp"},
		  {"directory":"`+dir+`","file":"`+filepath.Join(dir, "other.cpp")+`","command":"`+compiler+` -DOTHER=1 -c other.cpp"}]`), 0o644))
	orphan := filepath.Join(dir, "orphan.hpp")
	entry, err = databaseEntry(database, orphan)
	require.NoError(t, err)
	assert.Nil(t, entry)
}

func TestDiscoverSourceWithOwnEntry(t *testing.T) {
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	source := filepath.Join(dir, "mathx.cpp")
	writeDatabase(t, dir, compiler)

	flags, err := Discover(DiscoverOptions{File: source})
	require.NoError(t, err)
	assert.Equal(t, SourceCompileDatabase, flags.Source)
	assert.Contains(t, flags.Args, "-std=c++17")
	// A source keeps its own language: no -x c++-header.
	assert.NotContains(t, flags.Args, "c++-header")
}

func TestSplitCommandQuotedEscape(t *testing.T) {
	got, err := splitCommand(`g++ -DNAME="a\"b" -c x.cpp`)
	require.NoError(t, err)
	assert.Equal(t, []string{"g++", `-DNAME=a"b`, "-c", "x.cpp"}, got)
}

func TestYamlStringListRejectsSequenceOfMappings(t *testing.T) {
	dir := copyFixture(t)
	writeClangd(t, dir, `
CompileFlags:
  Add:
    - flag: value
`)
	_, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	assert.Error(t, err)
}

func TestDiscoverBrokenCompilationDatabaseFromClangd(t *testing.T) {
	dir := copyFixture(t)
	writeClangd(t, dir, `
CompileFlags:
  CompilationDatabase: nowhere
`)
	_, err := Discover(DiscoverOptions{File: filepath.Join(dir, "mathx.hpp")})
	assert.Error(t, err)
}
