package gen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// testBuilder is a builder with one opaque record and no headers, for the
// guards that only a hand-built model can reach.
func testBuilder() *builder {
	owner := &recordModel{GoName: "Owner", CxxName: "ns::Owner", Size: 4, Align: 4}
	b := &builder{
		cfg:     Config{Package: "p", Library: "l"},
		records: map[string]*recordModel{"ns::Owner": owner},
		mapper:  &mapper{records: map[string]*recordModel{"ns::Owner": owner}},
	}
	b.order = []string{"ns::Owner"}
	b.rawRecords = []cxx.Record{{QualifiedName: "ns::Owner", SizeBytes: 4, AlignBytes: 4}}
	return b
}

// TestBuildBindingsGuards covers the declarations buildBindings has to skip or
// refuse before a header can reach them.
func TestBuildBindingsGuards(t *testing.T) {
	t.Run("member of an unmodelled record", func(t *testing.T) {
		b := testBuilder()
		b.symbols = []cxx.Symbol{{
			QualifiedName: "ns::Missing::method",
			Parent:        "ns::Missing",
			Kind:          cxx.Method,
			Access:        cxx.AccessPublic,
		}}
		b.buildBindings()
		assert.Empty(t, b.bindings)
	})

	t.Run("method with an unsupported parameter", func(t *testing.T) {
		b := testBuilder()
		b.symbols = []cxx.Symbol{{
			QualifiedName: "ns::Owner::bad",
			Parent:        "ns::Owner",
			Kind:          cxx.Method,
			Access:        cxx.AccessPublic,
			Params:        []cxx.Param{{Kind: cxx.TypeOther, Spelling: "__int128"}},
		}}
		b.buildBindings()
		assert.Empty(t, b.bindings)
		require.Len(t, b.warnings, 1)
		assert.Contains(t, b.warnings[0], "ns::Owner::bad")
		assert.Contains(t, b.warnings[0], "__int128")
	})

	t.Run("constructor with an unsupported parameter", func(t *testing.T) {
		b := testBuilder()
		b.symbols = []cxx.Symbol{{
			QualifiedName: "ns::Owner::Owner",
			Parent:        "ns::Owner",
			Kind:          cxx.Constructor,
			Access:        cxx.AccessPublic,
			Params:        []cxx.Param{{Kind: cxx.TypeOther, Spelling: "__int128"}},
		}}
		b.buildBindings()
		assert.Empty(t, b.bindings)
		require.Len(t, b.warnings, 1)
		assert.Contains(t, b.warnings[0], "ns::Owner::Owner")
		assert.Contains(t, b.warnings[0], "__int128")
	})

	t.Run("copy constructor with an unsupported parameter", func(t *testing.T) {
		b := testBuilder()
		b.symbols = []cxx.Symbol{{
			QualifiedName:     "ns::Owner::Owner",
			Parent:            "ns::Owner",
			Kind:              cxx.Constructor,
			Access:            cxx.AccessPublic,
			IsCopyConstructor: true,
			Params:            []cxx.Param{{Kind: cxx.TypeOther, Spelling: "__int128"}},
		}}
		b.buildBindings()
		assert.Empty(t, b.bindings)
		require.Len(t, b.warnings, 1)
		assert.Contains(t, b.warnings[0], "__int128")
	})
}

// TestRawRecordUnknown checks the lookup of a record that is not in the model.
func TestRawRecordUnknown(t *testing.T) {
	b := testBuilder()
	assert.Equal(t, cxx.Record{}, b.rawRecord("ns::Nope"))
}

// TestUsesHelpers checks the import decisions the generated file makes.
func TestUsesHelpers(t *testing.T) {
	t.Run("mirrored record needs unsafe", func(t *testing.T) {
		m := &packageModel{records: []*recordModel{{Mirrored: true}}}
		assert.True(t, m.usesUnsafe())
	})
	t.Run("wire pointer needs unsafe", func(t *testing.T) {
		m := &packageModel{bindings: []*binding{{Wire: []*wireArg{{slotType: "unsafe.Pointer"}}}}}
		assert.True(t, m.usesUnsafe())
	})
	t.Run("scalars need no unsafe", func(t *testing.T) {
		m := &packageModel{bindings: []*binding{{Wire: []*wireArg{{slotType: "int64"}}}}}
		assert.False(t, m.usesUnsafe())
	})
	t.Run("no bool", func(t *testing.T) {
		m := &packageModel{bindings: []*binding{{Params: []*goParam{{Type: &goType{Kind: goInteger}}}}}}
		assert.False(t, m.usesBool())
	})
	t.Run("bool result", func(t *testing.T) {
		m := &packageModel{bindings: []*binding{{Result: &goType{Kind: goBool}}}}
		assert.True(t, m.usesBool())
	})
	t.Run("bool parameter", func(t *testing.T) {
		m := &packageModel{bindings: []*binding{{Params: []*goParam{{Type: &goType{Kind: goBool}}}}}}
		assert.True(t, m.usesBool())
	})
}

// TestEmitMirroredConstructorsEdges covers the two promises a mirrored record
// may not make: default construction and copying.
func TestEmitMirroredConstructorsEdges(t *testing.T) {
	var b strings.Builder
	m := &packageModel{}
	m.emitMirroredConstructors(&b, &recordModel{GoName: "Plain"})
	assert.Empty(t, b.String(), "a record that is neither constructible nor copyable gets neither")

	b.Reset()
	m.emitMirroredConstructors(&b, &recordModel{GoName: "OnlyNew", DefaultConstructible: true})
	assert.Contains(t, b.String(), "func NewOnlyNew() (*OnlyNew, error)")
	assert.NotContains(t, b.String(), "Clone")
}

// TestGofmtSourceError checks that a formatting failure is reported instead of
// writing a file Go cannot compile.
func TestGofmtSourceError(t *testing.T) {
	_, err := gofmtSource("package p\nfunc (")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "formatting generated Go source")
}

// TestRenderModelEmitterErrors covers renderModel's two failure returns: a Go
// file that does not format, and a shim whose thunk cannot be expressed.
func TestRenderModelEmitterErrors(t *testing.T) {
	t.Run("invalid package name", func(t *testing.T) {
		m := &packageModel{name: "9not-a-package", cfg: Config{Package: "9not-a-package"}}
		_, err := renderModel(m)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "formatting generated Go source")
	})

	t.Run("thunk over an unknown record", func(t *testing.T) {
		owner := &recordModel{GoName: "T", CxxName: "ns::T", Size: 4, Align: 4}
		m := &packageModel{
			name:    "p",
			cfg:     Config{Package: "p", Library: "libp"},
			mapper:  &mapper{records: map[string]*recordModel{"ns::T": owner}},
			records: []*recordModel{owner},
		}
		m.bindings = []*binding{{
			sym:         cxx.Symbol{QualifiedName: "ns::T::m", Kind: cxx.Method},
			Owner:       owner,
			GoName:      "M",
			Carrier:     thunk,
			Status:      true,
			ErrorResult: true,
			ID:          "abcdefabcdef",
			Symbol:      "cppgo_thunk_abcdefabcdef",
			Thunk:       "cppgo_thunk_abcdefabcdef",
			SymbolVar:   "cppgoSym_abcdefabcdef",
			Trampoline:  "cppgoCall_abcdefabcdef",
			This:        true,
			Wire: []*wireArg{
				{label: "this", slotType: "uintptr", class: classInt, size: 8, cType: "void *", cName: "self"},
				{label: "a1", slotType: "uintptr", class: classInt, size: 8, expr: "uintptr(0)", cType: "const void *", cName: "ghost",
					param: cxx.Param{Kind: cxx.TypePointer, Spelling: "Ghost *", Ptr: true,
						Pointee: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Ghost"}}},
			},
			Slots: []*slot{
				{label: "fn", goType: "uintptr", class: classInt, size: 8},
				{label: "this", goType: "uintptr", class: classInt, size: 8},
				{label: "a1", goType: "uintptr", class: classInt, size: 8},
				{label: "ret", goType: "int32", class: classInt, size: 4},
			},
			Frame: 28,
		}}
		_, err := renderModel(m)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "record pointer the model does not know")

		// The same model exercises emitShim's own error return and shimBody's.
		_, _, _, err = m.emitShim()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "record pointer the model does not know")
	})
}

// TestCrossCheckErrors covers the two ways the ELF cross-check fails: a library
// that is not an ELF, and an ELF that does not export a direct binding.
func TestCrossCheckErrors(t *testing.T) {
	t.Run("not an ELF", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not.so")
		require.NoError(t, os.WriteFile(path, []byte("this is not an object file"), 0o644))
		cfg := mathxConfig()
		cfg.Library = path
		_, err := build(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading dynamic symbols")
	})

	t.Run("missing direct symbol", func(t *testing.T) {
		gxx, err := exec.LookPath("g++")
		if err != nil {
			t.Skip("g++ is not in PATH")
		}
		dir := t.TempDir()
		source := filepath.Join(dir, "empty.cpp")
		require.NoError(t, os.WriteFile(source, []byte("int unrelated() { return 0; }\n"), 0o644))
		lib := filepath.Join(dir, "libempty.so")
		output, err := exec.Command(gxx, "-std=c++17", "-fPIC", "-shared", "-o", lib, source).CombinedOutput()
		require.NoErrorf(t, err, "g++ failed:\n%s", output)

		cfg := mathxConfig()
		cfg.Library = lib
		_, err = build(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not exported by")
	})
}

// TestGenerateFailurePaths covers Generate's error and writer paths.
func TestGenerateFailurePaths(t *testing.T) {
	t.Run("build error", func(t *testing.T) {
		require.Error(t, Generate(Config{}))
	})

	t.Run("default output directory", func(t *testing.T) {
		cfg := mathxConfig()
		cfg.Out = ""
		cfg.Check = true
		err := Generate(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "out of date")
	})

	t.Run("warnings and report writers", func(t *testing.T) {
		var warnings, report bytes.Buffer
		cfg := mathxConfig()
		cfg.Out = t.TempDir()
		cfg.Warnings = &warnings
		cfg.Report = &report
		require.NoError(t, Generate(cfg))
		assert.Contains(t, warnings.String(), "cppgo: warning:")
		assert.Contains(t, report.String(), "thunk")
	})
}

// TestShimCompileIsTheGate is the proof that an uncompilable thunk never
// reaches disk: the instantiation's member body does not compile for int, and
// the generator says so, naming the binding and the compiler's own line. The
// gate is the C++ compiler, not a re-parse: v4 compiles the shim itself, so a
// diagnostic is the compiler's and a thunk that would fail at the first call
// cannot be written.
func TestShimCompileIsTheGate(t *testing.T) {
	if _, err := exec.LookPath("g++"); err != nil {
		t.Skip("g++ is not in PATH")
	}
	out := filepath.Join(t.TempDir(), "out")
	cfg := fixtureConfig("badshim", "badshim.hpp")
	cfg.Instantiate = []string{"bad::Bad<int>"}
	cfg.Out = out
	cfg.Library = ""
	err := Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "badshim.hpp:")
	assert.Contains(t, err.Error(), "the generated shim does not compile")
	assert.Contains(t, err.Error(), "bad::Bad<int>::get")
	_, statErr := os.Stat(out)
	assert.True(t, os.IsNotExist(statErr), "a failed generation must not create the output directory")
}
