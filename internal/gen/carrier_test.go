package gen

import (
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestOutOfLineIsDefinitionOrDynsym pins the two halves of the rule: the header
// saw a body, or the library exports the mangled name. A declaration the header
// never defines is out of line only when the evidence is in the .dynsym, which
// is the rule v3 had and v4 keeps.
func TestOutOfLineIsDefinitionOrDynsym(t *testing.T) {
	b := &builder{exported: map[string]bool{"_Z3foov": true}}
	cases := []struct {
		name string
		sym  cxx.Symbol
		want bool
	}{
		{"a header that defines the body", cxx.Symbol{HasOutOfLineBody: true}, true},
		{"a declaration the library exports", cxx.Symbol{Mangled: "_Z3foov"}, true},
		{"a declaration the library does not export", cxx.Symbol{Mangled: "_Z3barv"}, false},
		{"a declaration with no mangled name", cxx.Symbol{}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, b.outOfLine(tc.sym))
		})
	}

	// With no library the table is nil and only the header's own evidence
	// counts, which is the header-only case.
	empty := &builder{}
	assert.True(t, empty.outOfLine(cxx.Symbol{HasOutOfLineBody: true}))
	assert.False(t, empty.outOfLine(cxx.Symbol{Mangled: "_Z3foov"}))
	assert.NoError(t, empty.crossCheck(), "no symbol table, nothing to cross-check")
}

// TestLoadExportedsSkipsWhatIsNotAFile covers the two -library values that
// contribute no symbol table: a bare loader name and a path that does not
// exist. Neither is an error: a header-only library has no object at all.
func TestLoadExportedsSkipsWhatIsNotAFile(t *testing.T) {
	for _, library := range []string{"", "libmathx", filepath.Join(t.TempDir(), "absent.so")} {
		library := library
		t.Run(library, func(t *testing.T) {
			b := &builder{cfg: Config{Library: library}}
			require.NoError(t, b.loadExporteds())
			assert.Nil(t, b.exported)
		})
	}
}

// TestDirectCarrierNeedsTheDynamicSymbol is the measured half: the fixture's
// out-of-line noexcept method is direct only once the library is in the run,
// because the header only declares it.
func TestDirectCarrierNeedsTheDynamicSymbol(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	if _, err := cxx.Load(); err != nil {
		t.Skipf("libclang is not available: %v", err)
	}

	lib := filepath.Join(t.TempDir(), "libmathx.so")
	output, err := exec.Command(gxx, "-std=c++17", "-O1", "-fPIC", "-shared",
		"testdata/mathx/vector.cpp", "-o", lib).CombinedOutput()
	require.NoErrorf(t, err, "g++ failed:\n%s", output)

	header, err := filepath.Abs("testdata/mathx/vector.hpp")
	require.NoError(t, err)
	cfg := mathxConfig()
	cfg.Headers = []string{header}
	cfg.Library = lib

	model, err := build(cfg)
	require.NoError(t, err)

	scale := findBinding(t, model, "Scale")
	assert.Equal(t, direct, scale.Carrier)
	assert.Equal(t, "_ZN5mathx6Vector5scaleEd", scale.Symbol)
	assert.Contains(t, scale.Reason, "noexcept, out of line", "the header defines scale out of line")

	// The inline accessor is a shim binding: the header has a body, so a
	// trampoline cannot be the carrier for it (this one is also not noexcept,
	// which is checked first).
	inline := findBinding(t, model, "X")
	assert.Equal(t, thunk, inline.Carrier)
	assert.Contains(t, inline.Reason, "not noexcept")

	assert.Equal(t, "libmathx.so", model.verified)
}

// TestCarrierFromDynsymOnly covers the case the ELF tier exists for: a
// declaration whose body the header does not show, noexcept and register
// representable, is direct exactly when the library exports its mangled name.
func TestCarrierFromDynsymOnly(t *testing.T) {
	cases := []struct {
		name     string
		exported map[string]bool
		want     carrier
		reason   string
	}{
		{"the library exports it", map[string]bool{"_Z3foov": true}, direct, "the library exports the symbol"},
		{"the library does not", map[string]bool{}, thunk, "inline body"},
		{"no library at all", nil, thunk, "inline body"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := &builder{exported: tc.exported}
			binding := &binding{sym: cxx.Symbol{
				QualifiedName:    "ns::foo",
				Mangled:          "_Z3foov",
				IsNoExcept:       true,
				HasOutOfLineBody: false,
			}}
			b.chooseCarrier(binding)
			assert.Equal(t, tc.want, binding.Carrier)
			assert.Contains(t, binding.Reason, tc.reason)
		})
	}
}

// TestCrossCheckRejectsAMissingDirectSymbol closes the loop: the model keeps a
// direct binding when the symbol table says it is there, and refuses when it
// says otherwise.
func TestCrossCheckRejectsAMissingDirectSymbol(t *testing.T) {
	b := &builder{
		cfg:      Config{Library: "libmissing.so"},
		exported: map[string]bool{},
		bindings: []*binding{{
			sym:     cxx.Symbol{QualifiedName: "mathx::scale", Mangled: "_ZN5mathx5scaleEd"},
			Carrier: direct,
			Symbol:  "_ZN5mathx5scaleEd",
		}},
	}
	err := b.crossCheck()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not exported by libmissing.so")

	b.exported["_ZN5mathx5scaleEd"] = true
	require.NoError(t, b.crossCheck())
	assert.Equal(t, "libmissing.so", b.verified)
}
