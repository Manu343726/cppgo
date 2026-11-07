package cxx

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openFixture opens the mathx fixture as a project, with the flags a real
// project would pass.
func openFixture(t *testing.T) *Project {
	t.Helper()
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	project, err := Open(Options{
		Header: filepath.Join(dir, "mathx.hpp"),
		Args:   []string{"-std=c++17", "-I" + dir},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })
	return project
}

func TestProjectDiscoversIncludingSource(t *testing.T) {
	project := openFixture(t)

	require.Len(t, project.MainFiles, 2, "the header and the source that includes it")
	assert.Equal(t, project.Header, project.MainFiles[0])
	assert.Equal(t, "mathx.cpp", filepath.Base(project.MainFiles[1]))
	assert.Contains(t, project.Flags.Args, "-std=c++17")
	assert.Equal(t, SourceArgs, project.Flags.Source)
}

func TestProjectFindsBodiesAndManglings(t *testing.T) {
	project := openFixture(t)

	norm, ok := project.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.Equal(t, "_ZNK5mathx6Vector4normEv", norm.Mangled)
	assert.True(t, norm.HasOutOfLineBody, "norm is defined in mathx.cpp, outside the class")
	assert.True(t, norm.HasExportedSymbol())
	assert.Equal(t, Method, norm.Kind)
	assert.True(t, norm.IsConst)
	assert.Equal(t, "mathx.cpp", filepath.Base(norm.DefFile))
	assert.Positive(t, norm.DefLine)

	ctor, ok := project.Lookup("mathx::Vector::Vector")
	require.True(t, ok)
	assert.True(t, ctor.HasOutOfLineBody, "the constructor is defined out of line")
	assert.Equal(t, "_ZN5mathx6VectorC1Edd", ctor.Mangled)

	dtor, ok := project.Lookup("mathx::Vector::~Vector")
	require.True(t, ok)
	assert.True(t, dtor.HasOutOfLineBody)

	// The inline accessor: the whole point of this package.
	accessor, ok := project.Lookup("mathx::Vector::x")
	require.True(t, ok)
	assert.Equal(t, "_ZNK5mathx6Vector1xEv", accessor.Mangled)
	assert.False(t, accessor.HasOutOfLineBody, "x() is defined inside the class definition: no symbol exists")
	assert.False(t, accessor.HasExportedSymbol())

	// An inline constructor, the other shape that must be refused.
	point, ok := project.Lookup("mathx::Point::Point")
	require.True(t, ok)
	assert.False(t, point.HasOutOfLineBody)
	assert.False(t, point.HasExportedSymbol())

	// A pure virtual has no definition anywhere.
	area, ok := project.Lookup("mathx::Shape::area")
	require.True(t, ok)
	assert.False(t, area.HasOutOfLineBody)
	assert.True(t, area.IsVirtual)

	// A definition out of line but with internal linkage exports nothing.
	counter, ok := project.Lookup("mathx::internal_counter")
	require.True(t, ok)
	assert.True(t, counter.HasOutOfLineBody)
	assert.True(t, counter.IsStatic)
	assert.False(t, counter.HasExportedSymbol())

	// Free functions from the .cpp are found too.
	makeSym, ok := project.Lookup("mathx::make")
	require.True(t, ok)
	assert.True(t, makeSym.HasOutOfLineBody)
	assert.True(t, makeSym.HasSret)
}

func TestProjectMissingBodies(t *testing.T) {
	project := openFixture(t)
	missing := project.MissingBodies()
	require.NotEmpty(t, missing)

	names := make([]string, 0, len(missing))
	for _, sym := range missing {
		names = append(names, sym.QualifiedName)
	}
	sort.Strings(names)
	assert.Equal(t, []string{
		"mathx::Point::Point",
		"mathx::Point::value",
		"mathx::Shape::area",
		"mathx::Vector::x",
		"mathx::Vector::y",
	}, names)
}

func TestProjectLookupsAndMangledLookup(t *testing.T) {
	project := openFixture(t)

	overloads := project.Lookups("mathx::add")
	require.Len(t, overloads, 2)
	assert.Equal(t, "_ZN5mathx3addEii", overloads[0].Mangled)
	assert.Equal(t, "_ZN5mathx3addEdd", overloads[1].Mangled)

	intAdd, ok := project.LookupMangled("_ZN5mathx3addEii")
	require.True(t, ok)
	assert.Equal(t, "int(int, int)", intAdd.Signature())

	doubleAdd, ok := project.LookupMangled("_ZN5mathx3addEdd")
	require.True(t, ok)
	assert.Equal(t, "double(double, double)", doubleAdd.Signature())

	_, ok = project.LookupMangled("_ZN5mathx3addEvv")
	assert.False(t, ok)
	_, ok = project.Lookup("mathx::nope")
	assert.False(t, ok)

	// Symbols returns a copy: a caller cannot corrupt the project's state.
	symbols := project.Symbols()
	require.NotEmpty(t, symbols)
	symbols[0].Mangled = "clobbered"
	assert.NotEqual(t, "clobbered", project.Symbols()[0].Mangled)
}

func TestProjectSymbolsAreUnique(t *testing.T) {
	project := openFixture(t)
	seen := map[string]string{}
	for _, sym := range project.Symbols() {
		if sym.Mangled == "" {
			continue
		}
		if previous, ok := seen[sym.Mangled]; ok {
			t.Fatalf("%s and %s share the mangled name %s", previous, sym.QualifiedName, sym.Mangled)
		}
		seen[sym.Mangled] = sym.QualifiedName
	}
	// The header and the source both declare mathx::Vector::norm; the merge must
	// have produced one symbol, not two.
	count := 0
	for _, sym := range project.Symbols() {
		if sym.QualifiedName == "mathx::Vector::norm" {
			count++
		}
	}
	assert.Equal(t, 1, count)
}

func TestProjectExplicitSources(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	// Put the definition somewhere the discovery would not look.
	deep := filepath.Join(dir, "src", "impl")
	require.NoError(t, os.MkdirAll(deep, 0o755))
	data, err := os.ReadFile(filepath.Join(dir, "mathx.cpp"))
	require.NoError(t, err)
	moved := filepath.Join(deep, "other.cpp")
	require.NoError(t, os.WriteFile(moved, data, 0o644))
	require.NoError(t, os.Remove(filepath.Join(dir, "mathx.cpp")))

	project, err := Open(Options{
		Header:  filepath.Join(dir, "mathx.hpp"),
		Args:    []string{"-std=c++17", "-I" + dir},
		Sources: []string{moved},
	})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()
	require.Len(t, project.MainFiles, 2)
	assert.Equal(t, filepath.Clean(moved), project.MainFiles[1])

	norm, ok := project.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.True(t, norm.HasOutOfLineBody)
}

func TestProjectSourceAsHeader(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	project, err := Open(Options{
		Header: filepath.Join(dir, "mathx.cpp"),
		Args:   []string{"-std=c++17", "-I" + dir},
	})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()
	assert.Equal(t, []string{filepath.Join(dir, "mathx.cpp")}, project.MainFiles,
		"a source is its own translation unit")
	norm, ok := project.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.True(t, norm.HasOutOfLineBody)
}

func TestProjectCapsSources(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	// Twelve translation units including the header: discovery must stop at
	// maxSources instead of parsing all of them.
	for i := 0; i < 12; i++ {
		name := filepath.Join(dir, "extra"+string(rune('a'+i))+".cpp")
		require.NoError(t, os.WriteFile(name, []byte("#include \"mathx.hpp\"\n"), 0o644))
	}
	project, err := Open(Options{
		Header: filepath.Join(dir, "mathx.hpp"),
		Args:   []string{"-std=c++17", "-I" + dir},
	})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()
	assert.LessOrEqual(t, len(project.MainFiles), maxSources+1, "the header plus at most maxSources sources")
	assert.Greater(t, len(project.MainFiles), 2)
}

func TestProjectErrors(t *testing.T) {
	skipWithoutLibclang(t)
	_, err := Open(Options{})
	assert.Error(t, err, "no header")

	_, err = Open(Options{Header: filepath.Join(t.TempDir(), "absent.hpp")})
	assert.Error(t, err)

	dir := copyFixture(t)
	_, err = Open(Options{Header: filepath.Join(dir, "mathx.hpp"), CompileCommands: filepath.Join(dir, "absent.json")})
	assert.Error(t, err)
}

func TestProjectCloseTwice(t *testing.T) {
	skipWithoutLibclang(t)
	dir := copyFixture(t)
	project, err := Open(Options{Header: filepath.Join(dir, "mathx.hpp"), Args: []string{"-std=c++17", "-I" + dir}})
	require.NoError(t, err)
	require.NoError(t, project.Close())
	assert.Error(t, project.Close())
}

// TestSymbolsAgainstNm is the cross-check the whole package exists for: every
// symbol the model says has an out-of-line body must appear in the dynamic
// symbol table of the compiled library, bit for bit, and every symbol it says
// does not must be absent.
func TestSymbolsAgainstNm(t *testing.T) {
	skipWithoutLibclang(t)
	compiler := cxxCompiler(t)
	dir := copyFixture(t)
	library := buildFixtureLibrary(t, dir, compiler)

	// Ground truth, twice: our own ELF reader, and nm's output.
	fromELF, err := DefinedDynamicSymbols(library)
	require.NoError(t, err)
	fromNm := nmDefinedSymbols(t, library)
	assert.Equal(t, fromNm, fromELF, "the ELF reader must agree with nm -D --defined-only")
	exported := make(map[string]bool, len(fromELF))
	for _, name := range fromELF {
		exported[name] = true
	}

	project, err := Open(Options{Header: filepath.Join(dir, "mathx.hpp"), Args: []string{"-std=c++17", "-I" + dir}})
	require.NoError(t, err)
	defer func() { _ = project.Close() }()

	// Only the symbols that belong to this fixture; the library also carries
	// template instantiations and the C++ runtime.
	isFixture := func(sym Symbol) bool {
		return strings.Contains(sym.QualifiedName, "mathx::") || strings.HasPrefix(sym.QualifiedName, "internal_counter")
	}
	checked := 0
	for _, sym := range project.Symbols() {
		if !isFixture(sym) || sym.Mangled == "" {
			continue
		}
		if sym.QualifiedName == "mathx::Shape::Shape" {
			// Known and documented exception: for an abstract class libclang
			// names the complete-object constructor (C1) and the compiler may
			// emit only the base-object one (C2) - clang++ does, g++ emits both.
			// Which variant exists is a property of the compiler, not of the
			// model, so assert the invariant that holds either way rather than
			// pinning one toolchain's choice. See Symbol.HasExportedSymbol.
			assert.True(t, exported["_ZN5mathx5ShapeC1Ei"] || exported["_ZN5mathx5ShapeC2Ei"],
				"an abstract class must export at least one constructor variant")
			continue
		}
		checked++
		if sym.HasExportedSymbol() {
			assert.True(t, exported[sym.Mangled],
				"%s (%s) is reported as defined out of line but the library does not export it",
				sym.QualifiedName, sym.Mangled)
		} else {
			assert.False(t, exported[sym.Mangled],
				"%s (%s) is reported as having no out-of-line body but the library exports it",
				sym.QualifiedName, sym.Mangled)
		}
	}
	assert.Positive(t, checked)

	// The three assertions that matter, stated one by one.
	norm, ok := project.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.True(t, exported[norm.Mangled])
	assert.True(t, exported["_ZNK5mathx6Vector4normEv"])

	accessor, ok := project.Lookup("mathx::Vector::x")
	require.True(t, ok)
	assert.False(t, accessor.HasOutOfLineBody)
	assert.False(t, exported[accessor.Mangled], "an inline accessor has no dynamic symbol at all")

	virtual, ok := project.Lookup("mathx::Vector::scale")
	require.True(t, ok)
	assert.True(t, virtual.IsVirtual)
	assert.True(t, exported[virtual.Mangled])
}

// nmDefinedSymbols runs nm and parses the same lines a human would read.
func nmDefinedSymbols(t *testing.T, library string) []string {
	t.Helper()
	if _, err := exec.LookPath("nm"); err != nil {
		t.Skip("nm is not installed")
	}
	output, err := exec.Command("nm", "-D", "--defined-only", library).Output()
	require.NoError(t, err)
	names := make([]string, 0, 64)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := fields[len(fields)-1]
		// nm appends the ELF symbol version ("clang_getClangVersion@@LLVM_13");
		// debug/elf reports the bare name, which is what dlsym resolves.
		if at := strings.IndexByte(name, '@'); at >= 0 {
			name = name[:at]
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
