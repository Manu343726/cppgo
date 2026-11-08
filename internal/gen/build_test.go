package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindCompiler covers the three answers of the resolution rule: -cxx when
// it names a program, the compiler's own name when -cxx names nothing, and the
// two fallbacks in order when nothing was named.
func TestFindCompiler(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not in PATH")
	}

	path, err := findCompiler("g++")
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(path), "the resolved compiler is a path, not a name: %s", path)

	_, err = findCompiler("cppgo-no-such-compiler")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-cxx")

	path, err = findCompiler("")
	require.NoError(t, err)
	assert.Contains(t, filepath.Base(path), "++")
}

// TestFindCompilerWithoutAny covers the machine that has no C++ compiler: the
// error names what to install, because the shim is C++ and cppgo compiles it.
func TestFindCompilerWithoutAny(t *testing.T) {
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	_, err := findCompiler("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no C++ compiler found")
	assert.Contains(t, err.Error(), "g++")
}

// TestCompileFlags strips what belongs to a compile-only invocation and keeps
// what the shim needs, and adds the standard when the project named none.
func TestCompileFlags(t *testing.T) {
	model := &packageModel{
		flags: []string{
			"-x", "c++-header", "-std=c++20", "-I/tmp/project", "-c", "-o", "out.o",
			"-MF", "dep.d", "-MD", "-MMD", "-fsyntax-only", "-DFEATURE=1", "-O3",
		},
		headers: []string{filepath.Join("testdata", "mathx", "vector.hpp")},
	}
	args := model.compileFlags("/scratch")
	for _, dropped := range []string{"c++-header", "-c", "out.o", "dep.d", "-MD", "-MMD", "-fsyntax-only"} {
		assert.NotContains(t, args, dropped)
	}
	for _, kept := range []string{"-std=c++20", "-I/tmp/project", "-DFEATURE=1", "-O3"} {
		assert.Contains(t, args, kept)
	}
	assert.Contains(t, args, "/scratch")
	assert.NotContains(t, args, "-std=c++17", "the project's standard wins")

	bare := &packageModel{flags: []string{"-I/tmp/project"}}
	assert.Contains(t, bare.compileFlags("/scratch"), "-std=c++17",
		"a project with no standard gets C++17, which the generated shim is written for")
}

// TestOptimisationAndStandard cover the two "already chosen?" questions.
func TestOptimisationAndStandard(t *testing.T) {
	assert.True(t, hasOptimisation([]string{"-O2"}))
	assert.True(t, hasOptimisation([]string{"-g", "-O0"}))
	assert.False(t, hasOptimisation([]string{"-g", "-Wall"}))

	assert.True(t, hasLanguageStandard([]string{"-std=gnu++20"}))
	assert.False(t, hasLanguageStandard([]string{"-stdlib=libstdc++"}))
}

// TestLinkableLibrary: only a file the linker can be handed is a library input,
// and it is handed over as a directory plus a file name so the DT_NEEDED entry
// is the soname rather than a build path. A bare loader name is not linkable.
func TestLinkableLibrary(t *testing.T) {
	_, _, ok := linkableLibrary("")
	assert.False(t, ok)
	_, _, ok = linkableLibrary("libmathx")
	assert.False(t, ok)
	_, _, ok = linkableLibrary(filepath.Join(t.TempDir(), "absent.so"))
	assert.False(t, ok)

	dir := t.TempDir()
	real := filepath.Join(dir, "libmathx.so")
	require.NoError(t, os.WriteFile(real, []byte("\x7fELF"), 0o644))
	libraryDir, base, ok := linkableLibrary(real)
	require.True(t, ok)
	assert.Equal(t, dir, libraryDir)
	assert.Equal(t, "libmathx.so", base)
}

// TestFirstDiagnostic parses the shapes g++ and clang++ print, and reports nil
// for output that carries no location (a link error).
func TestFirstDiagnostic(t *testing.T) {
	cases := []struct {
		name   string
		output string
		file   string
		line   int
		msg    string
	}{
		{
			name:   "g++",
			output: "/tmp/x/zz_cppgo_shim.cpp:31:10: error: use of undeclared identifier 'x'",
			file:   "/tmp/x/zz_cppgo_shim.cpp", line: 31, msg: "use of undeclared identifier 'x'",
		},
		{
			name:   "clang++ fatal",
			output: "shim.cpp:7:1: fatal error: 'nope.hpp' file not found",
			file:   "shim.cpp", line: 7, msg: "'nope.hpp' file not found",
		},
		{
			name:   "a warning is not an error",
			output: "shim.cpp:7:1: warning: unused variable",
		},
		{
			name:   "a link failure has no location",
			output: "/usr/bin/ld: undefined reference to `foo'",
		},
		{
			name:   "empty",
			output: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := firstDiagnostic([]byte(tc.output))
			if tc.file == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tc.file, got.file)
			assert.Equal(t, tc.line, got.line)
			assert.Equal(t, tc.msg, got.message)
		})
	}
}

// TestFirstLine is the fallback for output with no diagnostic in it.
func TestFirstLine(t *testing.T) {
	assert.Equal(t, "undefined reference", firstLine("\n\n  undefined reference\nmore"))
	assert.Equal(t, "no output", firstLine("\n \n"))
}

// TestCompileErrorAttributesTheBinding maps the compiler's output back to the
// declaration: a line inside a thunk names it, a line in a header cannot and
// falls back to the generated file, and an unparseable failure reports the
// compiler's own line.
func TestCompileErrorAttributesTheBinding(t *testing.T) {
	build := &shimBuild{
		Compiler: "g++",
		Dir:      t.TempDir(),
		Command:  []string{"g++", "-shared", "-o", "lib.so", "zz_cppgo_shim.cpp"},
	}
	model := &packageModel{
		spans: []thunkSpan{{start: 10, end: 20, symbol: "ns::thunk_owner"}},
	}

	err := model.compileError(build, []byte("zz_cppgo_shim.cpp:14:3: error: boom"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zz_cppgo_shim.cpp:14")
	assert.Contains(t, err.Error(), "ns::thunk_owner")
	assert.Contains(t, err.Error(), "boom")
	assert.Contains(t, err.Error(), "g++ -shared -o lib.so zz_cppgo_shim.cpp",
		"the failing command is part of the message")

	err = model.compileError(build, []byte("include/analytic.hpp:9:2: error: inline body"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "analytic.hpp:9")
	assert.Contains(t, err.Error(), "zz_cppgo_shim.cpp", "no binding could be attributed")

	err = model.compileError(build, []byte("/usr/bin/ld: cannot find -lnope"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot find -lnope")
	assert.Contains(t, err.Error(), "compiler: g++")
}

// TestCommandIsEmptyWithoutABuild covers the model that never compiled a shim,
// which is every Render-only test.
func TestCommandIsEmptyWithoutABuild(t *testing.T) {
	assert.Nil(t, (&packageModel{}).command())
	assert.Equal(t, []string{"g++"}, (&packageModel{build: &shimBuild{Command: []string{"g++"}}}).command())
}

// TestBuildShimWithoutACompiler reports the missing compiler instead of writing
// half an artifact.
func TestBuildShimWithoutACompiler(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	model := &packageModel{cfg: Config{Package: "x"}}
	_, err := model.buildShim(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no C++ compiler found")
}

// TestBuildShimCommand checks the argv the compile runs: the language reset
// before the linker input, the rpath, the output name, and the user's library
// when -library names a file.
func TestBuildShimCommand(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not in PATH")
	}
	header, err := filepath.Abs("testdata/mathx/vector.hpp")
	require.NoError(t, err)

	compile := func(t *testing.T, library string) *shimBuild {
		t.Helper()
		dir := t.TempDir()
		// The two shim sources are what Render writes; a trivially valid
		// translation unit is enough to exercise the argv.
		require.NoError(t, os.MkdirAll(filepath.Join(dir, ShimDirName), 0o755))
		require.NoError(t, os.WriteFile(ShimPath(dir, ShimHeaderFileName),
			[]byte("#pragma once\nextern \"C\" const char *cppgo_last_error(void);\n"), 0o644))
		require.NoError(t, os.WriteFile(ShimPath(dir, ShimSourceFileName),
			[]byte("#include \"zz_cppgo_shim.h\"\nextern \"C\" const char *cppgo_last_error(void) { return \"\"; }\n"), 0o644))
		model := &packageModel{
			cfg:     Config{Package: "mathx", Headers: []string{header}, Library: library, Out: dir},
			name:    "mathx",
			headers: []string{header},
			flags:   []string{"-std=c++17"},
		}
		build, err := model.buildShim(dir)
		require.NoErrorf(t, err, "the shim must compile:\n%s", strings.Join(build.Command, " "))
		require.FileExists(t, build.Path)
		return build
	}

	t.Run("no library", func(t *testing.T) {
		build := compile(t, "")
		assert.Contains(t, build.Command, "-Wl,-rpath,$ORIGIN")
		assert.Contains(t, build.Command, "-x")
		assert.NotContains(t, build.Command, "libuser.so", "no library input without -library")
		assert.Equal(t, ShimLibraryName("mathx"), filepath.Base(build.Path))
	})

	t.Run("a library file", func(t *testing.T) {
		dir := t.TempDir()
		source := filepath.Join(dir, "user.cpp")
		require.NoError(t, os.WriteFile(source, []byte("int mathx_dummy = 0;\n"), 0o644))
		library := filepath.Join(dir, "libuser.so")
		output, err := exec.Command("g++", "-shared", "-fPIC", source, "-o", library).CombinedOutput()
		require.NoErrorf(t, err, "g++ failed:\n%s", output)

		build := compile(t, library)
		assert.Contains(t, build.Command, "-x")
		assert.Contains(t, build.Command, "none")
		// The library is named, not pathed: the linker records its soname (or
		// its file name) as DT_NEEDED, so no build directory reaches the shim.
		assert.Contains(t, build.Command, "-L"+filepath.Dir(library))
		assert.Contains(t, build.Command, "-l:"+filepath.Base(library))
		assert.NotContains(t, build.Command, library)
	})
}

// TestWriteOutputsErrors covers the writer's two failures: an output directory
// that is a file, and a generated name that is a directory.
func TestWriteOutputsErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	err := writeOutputs(filepath.Join(file, "out"), []Output{{Name: GenFileName, Data: []byte("x")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating output directory")

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ShimDirName, ShimSourceFileName), 0o755))
	err = writeOutputs(dir, []Output{{Name: ShimSourceFileName, Subdir: ShimDirName, Data: []byte("x")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ShimSourceFileName)

	// A subdirectory that is a file: the shim's own directory cannot be
	// created, and the error names the path.
	blocked := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(blocked, ShimDirName), []byte("x"), 0o644))
	err = writeOutputs(blocked, []Output{{Name: ShimSourceFileName, Subdir: ShimDirName, Data: []byte("x")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), ShimDirName)
}
