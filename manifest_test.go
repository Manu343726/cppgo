package cppgo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
)

// TestManifestZeroValue measures the rule the type documents: the zero value is
// empty, it describes no package, and reading it neither panics nor invents
// data.
func TestManifestZeroValue(t *testing.T) {
	var m cppgo.Manifest

	assert.Zero(t, m)
	assert.Equal(t, 0, m.Schema)
	assert.Empty(t, m.GeneratorVersion)
	assert.Empty(t, m.ClangVersion)
	assert.Empty(t, m.CompilerVersion)
	assert.Nil(t, m.Libraries)
	assert.Nil(t, m.Headers)
	assert.Nil(t, m.Flags)
	assert.Nil(t, m.Bindings)
	assert.Nil(t, m.Types)
	assert.Nil(t, m.Constructs.Classes)
	assert.Nil(t, m.Constructs.Structs)
	assert.Nil(t, m.Constructs.Functions)
	assert.Nil(t, m.Constructs.Templates)
	assert.Nil(t, m.Constructs.Instantiations)

	// The zero enum values are the documented defaults, not an accident of
	// numbering.
	assert.Equal(t, cppgo.PackagingUnspecified, cppgo.Packaging(0))
	assert.Equal(t, cppgo.FlagSourceUnknown, cppgo.FlagSource(0))
	assert.Equal(t, cppgo.CarrierDirect, cppgo.Carrier(0))
	assert.Equal(t, cppgo.WireNone, cppgo.Wire(0))
	assert.Equal(t, cppgo.ErrorNone, cppgo.ErrorConvention(0))
	assert.Equal(t, cppgo.TypeOther, cppgo.TypeKind(0))
}

// TestManifestConstructed builds a manifest that uses every carrier, wire, type
// kind, flag source, packaging policy and error convention, and reads it back
// field by field.
func TestManifestConstructed(t *testing.T) {
	m := cppgo.Manifest{
		Schema:           cppgo.ManifestSchemaVersion,
		GeneratorVersion: "v1.2.3",
		ClangVersion:     "Debian clang version 22.1.8",
		CompilerVersion:  "g++ (GCC) 15.2.1 20250705",
		Libraries: []cppgo.LibraryInfo{
			{
				Soname:    "libmathx.so.1",
				Path:      "/build/libmathx.so.1",
				SHA256:    "7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730",
				Embedded:  true,
				Packaging: cppgo.PackagingExceptRuntime,
				Dependencies: []cppgo.Dependency{
					{Soname: "libhelper.so.1", Path: "/build/libhelper.so.1", Embedded: true},
					{Soname: "libstdc++.so.6", Path: "/usr/lib/libstdc++.so.6", Runtime: true, Needs: []string{"libc.so.6", "libm.so.6"}},
				},
			},
			{
				Soname:    "libc.so.6",
				Path:      "/usr/lib/libc.so.6",
				Packaging: cppgo.PackagingLibraryOnly,
			},
			{
				Soname:    "libheader_only.so.0",
				Packaging: cppgo.PackagingUnspecified,
			},
		},
		Headers: []cppgo.Header{
			{
				Path:         "/project/include/mathx.hpp",
				RelativePath: "include/mathx.hpp",
				SHA256:       "f7d2a2a4d1a9b7c1c8e6e0e4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5b4",
			},
			{Path: "/project/include/extra.hpp", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"},
		},
		Flags: []cppgo.Flag{
			{Value: "-std=c++20", Source: cppgo.FlagSourceCompileCommands, SourcePath: "/project/build/compile_commands.json", Order: 0},
			{Value: "-I/project/include", Source: cppgo.FlagSourceClangd, SourcePath: "/project/.clangd", Order: 1},
			{Value: "-DNDEBUG", Source: cppgo.FlagSourceOverride, Order: 2},
			{Value: "-std=c++17", Source: cppgo.FlagSourceAssumed, Order: 3},
			{Value: "-Wall", Source: cppgo.FlagSourceUnknown, Order: 4},
		},
		Bindings: []cppgo.Binding{
			{
				CppName:      "mathx::add",
				Symbol:       "_ZN5mathx3addEii",
				Carrier:      cppgo.CarrierDirect,
				GoName:       "Add",
				GoSignature:  "func Add(a int32, b int32) int32",
				CppSignature: "mathx::add(int, int)",
				Params: []cppgo.Param{
					{Name: "a", CppType: "int", GoType: "int32", Wire: cppgo.WireInteger},
					{Name: "b", CppType: "int", GoType: "int32", Wire: cppgo.WireInteger},
				},
				Results: []cppgo.Param{
					{Name: "", CppType: "int", GoType: "int32", Wire: cppgo.WireInteger},
				},
				Error: cppgo.ErrorNone,
			},
			{
				CppName:      "mathx::Vector::norm",
				Symbol:       "cppgo_thunk_0123456789ab",
				Carrier:      cppgo.CarrierThunk,
				GoName:       "Norm",
				GoSignature:  "func (v *Vector) Norm() float64",
				CppSignature: "mathx::Vector::norm() const",
				Results: []cppgo.Param{
					{Name: "", CppType: "double", GoType: "float64", Wire: cppgo.WireFloat},
				},
				Error: cppgo.ErrorNone,
			},
			{
				CppName:      "mathx::transform",
				Symbol:       "cppgo_thunk_ba9876543210",
				Carrier:      cppgo.CarrierReentrant,
				GoName:       "Transform",
				GoSignature:  "func Transform(xs []float64, f func(float64) float64) (bool, error)",
				CppSignature: "mathx::transform(double const*, unsigned long, std::function<double (double)>)",
				Params: []cppgo.Param{
					{Name: "xs", CppType: "const double *", GoType: "[]float64", Wire: cppgo.WirePointerLength},
					{Name: "f", CppType: "std::function<double (double)>", GoType: "func(float64) float64", Wire: cppgo.WirePointer},
				},
				Results: []cppgo.Param{
					{Name: "", CppType: "double", GoType: "float64", Wire: cppgo.WireIndirect},
				},
				Error: cppgo.ErrorStatus,
			},
			{
				CppName:      "mathx::open",
				Symbol:       "_ZN5mathx4openEPKc",
				Carrier:      cppgo.CarrierDirect,
				GoName:       "Open",
				GoSignature:  "func Open(path string) (File, error)",
				CppSignature: "mathx::open(char const*)",
				Params: []cppgo.Param{
					{Name: "path", CppType: "const char *", GoType: "string", Wire: cppgo.WirePointerLength},
				},
				Results: []cppgo.Param{
					{Name: "", CppType: "mathx::File", GoType: "*File", Wire: cppgo.WirePointer},
				},
				Error: cppgo.ErrorException,
			},
		},
		Types: []cppgo.Type{
			{CppType: "mathx::Axis", GoType: "Axis", Kind: cppgo.TypeEnum},
			{CppType: "mathx::Point", GoType: "Point", Kind: cppgo.TypeMirror},
			{CppType: "mathx::Vector", GoType: "*Vector", Kind: cppgo.TypeOpaque},
			{CppType: "mathx::Vector::const_iterator", GoType: "*ConstIterator", Kind: cppgo.TypeView},
			{CppType: "std::string", GoType: "string", Kind: cppgo.TypeContainer},
			{CppType: "mathx::Box<T>", GoType: "Box[T]", Kind: cppgo.TypeTemplate},
			{CppType: "int", GoType: "int32", Kind: cppgo.TypeScalar},
			{CppType: "mathx::detail::Tag", GoType: "struct{}", Kind: cppgo.TypeOther},
		},
		Constructs: cppgo.Constructs{
			Classes:        []string{"mathx::Vector"},
			Structs:        []string{"mathx::Point"},
			Functions:      []string{"mathx::add", "mathx::open"},
			Templates:      []string{"mathx::Box"},
			Instantiations: []string{"mathx::Box<int>"},
		},
	}

	assert.Equal(t, cppgo.ManifestSchemaVersion, m.Schema)
	assert.Equal(t, 1, cppgo.ManifestSchemaVersion)

	require.Len(t, m.Libraries, 3)
	assert.Equal(t, "libmathx.so.1", m.Libraries[0].Soname)
	assert.Equal(t, "/build/libmathx.so.1", m.Libraries[0].Path)
	assert.Equal(t, "7d865e959b2466918c9863afca942d0fb89d7c9ac0c99bafc3749504ded97730", m.Libraries[0].SHA256)
	assert.True(t, m.Libraries[0].Embedded)
	assert.Equal(t, cppgo.PackagingExceptRuntime, m.Libraries[0].Packaging)
	require.Len(t, m.Libraries[0].Dependencies, 2)
	assert.Equal(t, cppgo.Dependency{Soname: "libhelper.so.1", Path: "/build/libhelper.so.1", Embedded: true}, m.Libraries[0].Dependencies[0])
	assert.True(t, m.Libraries[0].Dependencies[1].Runtime)
	assert.False(t, m.Libraries[0].Dependencies[1].Embedded)
	assert.Equal(t, []string{"libc.so.6", "libm.so.6"}, m.Libraries[0].Dependencies[1].Needs)
	assert.Equal(t, cppgo.PackagingLibraryOnly, m.Libraries[1].Packaging)
	assert.Empty(t, m.Libraries[2].Path)
	assert.Empty(t, m.Libraries[2].SHA256)
	assert.False(t, m.Libraries[2].Embedded)

	require.Len(t, m.Headers, 2)
	assert.Equal(t, "/project/include/mathx.hpp", m.Headers[0].Path)
	assert.Equal(t, "include/mathx.hpp", m.Headers[0].RelativePath)
	assert.NotEmpty(t, m.Headers[0].SHA256)
	assert.Empty(t, m.Headers[1].RelativePath)

	require.Len(t, m.Flags, 5)
	for i, flag := range m.Flags {
		assert.Equal(t, i, flag.Order)
	}
	assert.Equal(t, cppgo.FlagSourceCompileCommands, m.Flags[0].Source)
	assert.Equal(t, "/project/build/compile_commands.json", m.Flags[0].SourcePath)
	assert.Equal(t, cppgo.FlagSourceClangd, m.Flags[1].Source)
	assert.Equal(t, "/project/.clangd", m.Flags[1].SourcePath)
	assert.Equal(t, cppgo.FlagSourceOverride, m.Flags[2].Source)
	assert.Empty(t, m.Flags[2].SourcePath)
	assert.Equal(t, cppgo.FlagSourceAssumed, m.Flags[3].Source)
	assert.Empty(t, m.Flags[3].SourcePath)

	require.Len(t, m.Bindings, 4)
	direct := m.Bindings[0]
	assert.Equal(t, "mathx::add", direct.CppName)
	assert.Equal(t, "_ZN5mathx3addEii", direct.Symbol)
	assert.Equal(t, cppgo.CarrierDirect, direct.Carrier)
	assert.Equal(t, "Add", direct.GoName)
	assert.Equal(t, "func Add(a int32, b int32) int32", direct.GoSignature)
	assert.Equal(t, "mathx::add(int, int)", direct.CppSignature)
	assert.Equal(t, []cppgo.Param{
		{Name: "a", CppType: "int", GoType: "int32", Wire: cppgo.WireInteger},
		{Name: "b", CppType: "int", GoType: "int32", Wire: cppgo.WireInteger},
	}, direct.Params)
	assert.Equal(t, cppgo.WireInteger, direct.Results[0].Wire)
	assert.Equal(t, cppgo.ErrorNone, direct.Error)

	assert.Equal(t, cppgo.CarrierThunk, m.Bindings[1].Carrier)
	assert.Equal(t, "cppgo_thunk_0123456789ab", m.Bindings[1].Symbol)
	assert.Equal(t, cppgo.WireFloat, m.Bindings[1].Results[0].Wire)

	reentrant := m.Bindings[2]
	assert.Equal(t, cppgo.CarrierReentrant, reentrant.Carrier)
	assert.Equal(t, cppgo.WirePointerLength, reentrant.Params[0].Wire)
	assert.Equal(t, cppgo.WireIndirect, reentrant.Results[0].Wire)
	assert.Equal(t, cppgo.ErrorStatus, reentrant.Error)

	assert.Equal(t, cppgo.ErrorException, m.Bindings[3].Error)

	require.Len(t, m.Types, 8)
	assert.Equal(t, []cppgo.TypeKind{
		cppgo.TypeEnum,
		cppgo.TypeMirror,
		cppgo.TypeOpaque,
		cppgo.TypeView,
		cppgo.TypeContainer,
		cppgo.TypeTemplate,
		cppgo.TypeScalar,
		cppgo.TypeOther,
	}, []cppgo.TypeKind{
		m.Types[0].Kind,
		m.Types[1].Kind,
		m.Types[2].Kind,
		m.Types[3].Kind,
		m.Types[4].Kind,
		m.Types[5].Kind,
		m.Types[6].Kind,
		m.Types[7].Kind,
	})

	assert.Equal(t, []string{"mathx::Vector"}, m.Constructs.Classes)
	assert.Equal(t, []string{"mathx::Point"}, m.Constructs.Structs)
	assert.Equal(t, []string{"mathx::add", "mathx::open"}, m.Constructs.Functions)
	assert.Equal(t, []string{"mathx::Box"}, m.Constructs.Templates)
	assert.Equal(t, []string{"mathx::Box<int>"}, m.Constructs.Instantiations)
}

// TestManifestIsAPlainValue measures that a Manifest carries no state behind
// its fields: a copy keeps the struct's own values, and both values read the
// same slices.
func TestManifestIsAPlainValue(t *testing.T) {
	original := cppgo.Manifest{
		Schema:           cppgo.ManifestSchemaVersion,
		GeneratorVersion: "v1.2.3",
		Libraries:        []cppgo.LibraryInfo{{Soname: "libmathx.so.1"}},
		Bindings: []cppgo.Binding{{
			CppName: "mathx::add",
			GoName:  "Add",
		}},
		Types: []cppgo.Type{{CppType: "int", GoType: "int32", Kind: cppgo.TypeScalar}},
		Constructs: cppgo.Constructs{
			Functions: []string{"mathx::add"},
		},
	}

	copied := original
	copied.Schema = 0
	copied.GeneratorVersion = ""
	copied.Libraries = append(copied.Libraries, cppgo.LibraryInfo{Soname: "libother.so.1"})

	assert.Equal(t, cppgo.ManifestSchemaVersion, original.Schema)
	assert.Equal(t, "v1.2.3", original.GeneratorVersion)

	require.Len(t, copied.Libraries, 2)
	assert.Equal(t, "libmathx.so.1", copied.Libraries[0].Soname)

	// Both copies read the same binding, type and construct slices.
	require.Len(t, copied.Bindings, 1)
	assert.Equal(t, "mathx::add", copied.Bindings[0].CppName)
	require.Len(t, copied.Types, 1)
	assert.Equal(t, cppgo.TypeScalar, copied.Types[0].Kind)
	require.Len(t, copied.Constructs.Functions, 1)
	assert.Equal(t, "mathx::add", copied.Constructs.Functions[0])
}

// TestManifestEnumStrings measures every value of every enum, including an
// out-of-range value, so the printed form of a manifest is stable.
func TestManifestEnumStrings(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"packaging unspecified", cppgo.PackagingUnspecified.String(), "unspecified"},
		{"packaging library only", cppgo.PackagingLibraryOnly.String(), "library"},
		{"packaging library and dependencies", cppgo.PackagingLibraryAndDependencies.String(), "library+dependencies"},
		{"packaging except runtime", cppgo.PackagingExceptRuntime.String(), "except-runtime"},
		{"packaging all", cppgo.PackagingAll.String(), "all"},
		{"packaging out of range", cppgo.Packaging(99).String(), "unspecified"},

		{"flag source unknown", cppgo.FlagSourceUnknown.String(), "unknown"},
		{"flag source compile commands", cppgo.FlagSourceCompileCommands.String(), "compile_commands.json"},
		{"flag source clangd", cppgo.FlagSourceClangd.String(), ".clangd"},
		{"flag source override", cppgo.FlagSourceOverride.String(), "override"},
		{"flag source assumed", cppgo.FlagSourceAssumed.String(), "assumed"},
		{"flag source out of range", cppgo.FlagSource(99).String(), "unknown"},

		{"carrier direct", cppgo.CarrierDirect.String(), "direct"},
		{"carrier thunk", cppgo.CarrierThunk.String(), "thunk"},
		{"carrier reentrant", cppgo.CarrierReentrant.String(), "reentrant"},
		{"carrier out of range", cppgo.Carrier(99).String(), "direct"},

		{"wire none", cppgo.WireNone.String(), "none"},
		{"wire integer", cppgo.WireInteger.String(), "integer"},
		{"wire float", cppgo.WireFloat.String(), "float"},
		{"wire pointer", cppgo.WirePointer.String(), "pointer"},
		{"wire pointer and length", cppgo.WirePointerLength.String(), "pointer+length"},
		{"wire indirect", cppgo.WireIndirect.String(), "indirect"},
		{"wire out of range", cppgo.Wire(99).String(), "none"},

		{"error none", cppgo.ErrorNone.String(), "none"},
		{"error exception", cppgo.ErrorException.String(), "exception"},
		{"error code", cppgo.ErrorCode.String(), "error_code"},
		{"error expected", cppgo.ErrorExpected.String(), "expected"},
		{"error optional", cppgo.ErrorOptional.String(), "optional"},
		{"error status", cppgo.ErrorStatus.String(), "status"},
		{"error errno", cppgo.ErrorErrno.String(), "errno"},
		{"error out param", cppgo.ErrorOutParam.String(), "out_param"},
		{"error custom", cppgo.ErrorCustom.String(), "custom"},
		{"error variant", cppgo.ErrorVariant.String(), "variant"},
		{"error out of range", cppgo.ErrorConvention(99).String(), "none"},

		{"type other", cppgo.TypeOther.String(), "other"},
		{"type scalar", cppgo.TypeScalar.String(), "scalar"},
		{"type enum", cppgo.TypeEnum.String(), "enum"},
		{"type mirror", cppgo.TypeMirror.String(), "mirror"},
		{"type opaque", cppgo.TypeOpaque.String(), "opaque"},
		{"type view", cppgo.TypeView.String(), "view"},
		{"type container", cppgo.TypeContainer.String(), "container"},
		{"type template", cppgo.TypeTemplate.String(), "template"},
		{"type out of range", cppgo.TypeKind(99).String(), "other"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
		})
	}
}
