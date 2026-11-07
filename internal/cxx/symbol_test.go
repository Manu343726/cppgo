package cxx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// headerSymbols parses the fixture header on its own and returns the symbols
// indexed by qualified name. It is the "headers only, no sources" case: every
// member is a declaration and nothing has a body.
func headerSymbols(t *testing.T) map[string]Symbol {
	t.Helper()
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })

	out := make(map[string]Symbol)
	for _, sym := range tu.Symbols() {
		out[sym.QualifiedName] = sym
	}
	return out
}

func TestSymbolsFromHeaderOnly(t *testing.T) {
	symbols := headerSymbols(t)

	// The mangled names are libclang's, never assembled by hand.
	assert.Equal(t, "_ZNK5mathx6Vector4normEv", symbols["mathx::Vector::norm"].Mangled)
	assert.Equal(t, "_ZN5mathx6VectorC1Edd", symbols["mathx::Vector::Vector"].Mangled)
	assert.Equal(t, "_ZN5mathx6VectorD1Ev", symbols["mathx::Vector::~Vector"].Mangled)
	assert.Equal(t, "_ZNK5mathx6Vector3dotEPKdm", symbols["mathx::Vector::dot"].Mangled)
	assert.Equal(t, "_ZNK5mathx6Vector1xEv", symbols["mathx::Vector::x"].Mangled)
	assert.Equal(t, "_ZNK5mathx5Shape4areaEv", symbols["mathx::Shape::area"].Mangled)

	// A header on its own declares but never defines: hasOutOfLineBody is
	// false everywhere, which is exactly why a Project parses the sources too.
	for name, sym := range symbols {
		assert.False(t, sym.HasOutOfLineBody, "%s must not claim a body from a declaration", name)
		assert.False(t, sym.HasExportedSymbol(), name)
	}

	// The inline accessors are reported as inline-only.
	assert.False(t, symbols["mathx::Vector::x"].HasOutOfLineBody)
	assert.False(t, symbols["mathx::Point::Point"].HasOutOfLineBody)
	assert.False(t, symbols["mathx::Point::value"].HasOutOfLineBody)
}

func TestSymbolQualifiers(t *testing.T) {
	symbols := headerSymbols(t)

	norm := symbols["mathx::Vector::norm"]
	assert.Equal(t, Method, norm.Kind)
	assert.True(t, norm.IsConst, "a const member function carries K in the mangling")
	assert.False(t, norm.IsVirtual)
	assert.False(t, norm.IsStatic)
	assert.True(t, norm.IsConst && norm.Mangled == "_ZNK5mathx6Vector4normEv")

	dims := symbols["mathx::Vector::dims"]
	assert.Equal(t, StaticMethod, dims.Kind)
	assert.True(t, dims.IsStatic, "a static member needs no this")
	assert.False(t, dims.IsConst)

	scale := symbols["mathx::Vector::scale"]
	assert.True(t, scale.IsVirtual, "virtual dispatch goes through the vtable")

	ctor := symbols["mathx::Vector::Vector"]
	assert.Equal(t, Constructor, ctor.Kind)
	assert.Equal(t, TypeVoid, ctor.Result.Kind)
	assert.False(t, ctor.HasSret)

	dtor := symbols["mathx::Shape::~Shape"]
	assert.Equal(t, Destructor, dtor.Kind)
	assert.True(t, dtor.IsVirtual, "a virtual destructor is worth reporting")

	// A pure virtual has no body at all: declared, never defined.
	area := symbols["mathx::Shape::area"]
	assert.True(t, area.IsVirtual)
	assert.False(t, area.HasOutOfLineBody)
	assert.False(t, area.HasExportedSymbol())

	// Internal linkage: `static` at namespace scope.
	counter := symbols["mathx::internal_counter"]
	assert.Equal(t, FreeFunc, counter.Kind)
	assert.True(t, counter.IsStatic, "a static free function has internal linkage")

	make := symbols["mathx::make"]
	assert.Equal(t, FreeFunc, make.Kind)
	assert.False(t, make.IsStatic)
}

func TestSymbolSignatures(t *testing.T) {
	symbols := headerSymbols(t)

	dot := symbols["mathx::Vector::dot"]
	require.Len(t, dot.Params, 2)
	assert.Equal(t, "const double *", dot.Params[0].Spelling)
	assert.Equal(t, TypePointer, dot.Params[0].Kind)
	assert.True(t, dot.Params[0].Ptr)
	assert.False(t, dot.Params[0].Float, "a pointer to a double travels in an integer register")
	assert.Equal(t, int64(8), dot.Params[0].SizeBytes)
	assert.Equal(t, "Pointer", dot.Params[0].ClangKind)

	assert.Equal(t, "std::size_t", dot.Params[1].Spelling)
	assert.Equal(t, TypeInteger, dot.Params[1].Kind, "typedefs are resolved before classification")
	assert.Equal(t, "ULong", dot.Params[1].ClangKind)
	assert.Equal(t, int64(8), dot.Params[1].SizeBytes)
	assert.False(t, dot.Params[1].Ptr)

	assert.Equal(t, TypeFloat, dot.Result.Kind)
	assert.True(t, dot.Result.Float)
	assert.False(t, dot.HasSret)
	assert.Equal(t, "double(const double *, std::size_t) const", dot.Signature())

	isAxis := symbols["mathx::is_axis"]
	require.Len(t, isAxis.Params, 2)
	assert.Equal(t, "Axis", isAxis.Params[0].Spelling)
	assert.Equal(t, TypeEnum, isAxis.Params[0].Kind)
	assert.Equal(t, int64(1), isAxis.Params[0].SizeBytes)
	assert.Equal(t, TypeInteger, isAxis.Params[1].Kind)
	assert.Equal(t, "Bool", isAxis.Params[1].ClangKind)
	assert.Equal(t, TypeInteger, isAxis.Result.Kind)

	pick := symbols["mathx::Vector::pick"]
	require.Len(t, pick.Params, 1)
	assert.Equal(t, TypeOther, pick.Params[0].Kind, "a pointer to member is neither an integer nor a pointer")
	assert.Equal(t, "MemberPointer", pick.Params[0].ClangKind)

	name := symbols["mathx::Vector::name"]
	assert.Equal(t, TypePointer, name.Result.Kind)
	assert.Equal(t, "const char *", name.Result.Spelling)

	// Constructors have no result; the model says void rather than guessing.
	assert.Equal(t, "void(double, double)", symbols["mathx::Vector::Vector"].Signature())
}

func TestSymbolSret(t *testing.T) {
	symbols := headerSymbols(t)

	// More than two eightbytes: SysV memory class, hidden pointer in RDI.
	makeSym := symbols["mathx::make"]
	assert.Equal(t, TypeRecord, makeSym.Result.Kind)
	assert.True(t, makeSym.HasSret, "Vector is larger than 16 bytes and polymorphic")

	// Four bytes with a user-provided destructor: small, but non-trivial for
	// the purposes of calls, so it comes back through the hidden pointer too.
	tiny := symbols["mathx::make_tiny"]
	assert.True(t, tiny.HasSret, "Tiny declares a destructor: the ABI returns it indirectly")

	// 16 bytes and trivially copyable: comes back in XMM0:XMM1.
	pair := symbols["mathx::make_pair"]
	assert.Equal(t, int64(16), pair.Result.SizeBytes)
	assert.False(t, pair.HasSret, "a trivially copyable aggregate of two eightbytes returns in registers")

	// The same rule through five levels of base classes.
	assert.True(t, symbols["mathx::make_deep"].HasSret)

	assert.False(t, symbols["mathx::add"].HasSret)

	// Every remaining reason for an indirect return, on records that are small
	// enough to fit in registers.
	assert.True(t, symbols["mathx::make_small_virtual"].HasSret, "a polymorphic class is non-trivial for calls")
	assert.True(t, symbols["mathx::make_small_copy"].HasSret, "a user-provided copy constructor makes it indirect")
	assert.True(t, symbols["mathx::make_small_move"].HasSret, "a user-provided move constructor makes it indirect")
	assert.True(t, symbols["mathx::make_small_virtual_base"].HasSret, "a virtual base makes it indirect")
	assert.True(t, symbols["mathx::make_small_member"].HasSret, "a non-trivial member makes it indirect")
}

func TestLookupByNameAndMangling(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	defer func() { _ = tu.Close() }()

	sym, ok := tu.Lookup("mathx::Vector::norm")
	require.True(t, ok)
	assert.Equal(t, "_ZNK5mathx6Vector4normEv", sym.Mangled)

	byMangled, ok := tu.LookupMangled("_ZNK5mathx6Vector4normEv")
	require.True(t, ok)
	assert.Equal(t, "mathx::Vector::norm", byMangled.QualifiedName)

	_, ok = tu.Lookup("mathx::Vector::absent")
	assert.False(t, ok)
	_, ok = tu.LookupMangled("_ZNK5mathx6Vector9absentEv")
	assert.False(t, ok)

	// Overloads share a qualified name and are told apart by mangling.
	overloads := tu.Lookups("mathx::add")
	require.Len(t, overloads, 2)
	assert.Equal(t, "_ZN5mathx3addEii", overloads[0].Mangled)
	assert.Equal(t, "_ZN5mathx3addEdd", overloads[1].Mangled)
	first, ok := tu.Lookup("mathx::add")
	require.True(t, ok)
	assert.Equal(t, overloads[0].Mangled, first.Mangled)
	assert.Empty(t, tu.Lookups("mathx::nothing"))
}

func TestInternalLinkageIsNotListedByName(t *testing.T) {
	symbols := headerSymbols(t)
	// The anonymous-namespace helper has no name a linker could find, so it is
	// not part of the model at all.
	for name := range symbols {
		assert.NotContains(t, name, "hidden_helper")
		assert.NotContains(t, name, "anonymous")
	}
}

func TestCursorDisplayName(t *testing.T) {
	// The qualified name is built from spellings, because that is what a C++
	// programmer writes; clang_getCursorDisplayName is the other rendering
	// libclang offers, and it is part of the binding surface, so prove it
	// arrives intact rather than as a mangled CXString.
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	defer func() { _ = tu.Close() }()

	var displayNames []string
	require.NoError(t, library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		switch library.f.getCursorKind(cursor) {
		case cursorNamespace, cursorClassDecl, cursorStructDecl:
			return visitRecurse
		case cursorFunctionDecl, cursorCXXMethod, cursorConstructor, cursorDestructor:
			displayNames = append(displayNames, library.str(library.f.getCursorDisplayName(cursor)))
			return visitContinue
		default:
			return visitContinue
		}
	}))
	// The display name is the signature, not the qualified name: libclang
	// renders "norm()" where the model needs "mathx::Vector::norm".
	assert.Contains(t, displayNames, "make(double, double)")
	assert.Contains(t, displayNames, "norm()")
	// libclang qualifies a member-pointer parameter differently across
	// versions: clang 22 renders "pick(int Vector::*)" and clang 19 renders
	// "pick(int mathx::Vector::*)". Match the shape, not the spelling.
	assert.True(t, anyDisplayNameMatches(displayNames, "pick(int", "Vector::*)"),
		"no display name looks like a member-pointer parameter: %v", displayNames)
}

func TestSignatureWithoutSpelling(t *testing.T) {
	// A zero Symbol has no result spelling; the signature still has to read
	// like C++ rather than like an empty string.
	assert.Equal(t, "void()", Symbol{}.Signature())
	assert.Equal(t, "void(int) const", Symbol{Params: []Param{{Spelling: "int"}}, IsConst: true}.Signature())
}

func TestSymbolStrings(t *testing.T) {
	for _, tc := range []struct {
		kind Kind
		want string
	}{
		{FreeFunc, "free function"},
		{StaticMethod, "static method"},
		{Method, "method"},
		{Constructor, "constructor"},
		{Destructor, "destructor"},
		{Kind(99), "unknown kind"},
	} {
		assert.Equal(t, tc.want, tc.kind.String())
	}
}

func TestSymbolHasExportedSymbol(t *testing.T) {
	// An out-of-line body is not enough: a namespace-scope static function has
	// one and still exports nothing.
	assert.True(t, Symbol{HasOutOfLineBody: true, Kind: Method}.HasExportedSymbol())
	assert.True(t, Symbol{HasOutOfLineBody: true, Kind: StaticMethod}.HasExportedSymbol())
	assert.False(t, Symbol{HasOutOfLineBody: false, Kind: Method}.HasExportedSymbol())
	assert.False(t, Symbol{HasOutOfLineBody: true, Kind: FreeFunc, IsStatic: true}.HasExportedSymbol())
}

func TestMergeInto(t *testing.T) {
	into := Symbol{QualifiedName: "mathx::add", Mangled: "_ZN5mathx3addEii", Kind: FreeFunc}
	from := Symbol{
		QualifiedName:    "mathx::add",
		Mangled:          "_ZN5mathx3addEii",
		Kind:             FreeFunc,
		HasOutOfLineBody: true,
		DefFile:          "/src/mathx.cpp",
		DefLine:          42,
		Params:           []Param{{Spelling: "int"}},
		Result:           Param{Spelling: "int", Kind: TypeInteger},
		DeclFile:         "/src/mathx.hpp",
		DeclLine:         7,
	}
	mergeInto(&into, from)
	assert.True(t, into.HasOutOfLineBody)
	assert.Equal(t, "/src/mathx.cpp", into.DefFile)
	assert.Equal(t, 42, into.DefLine)
	assert.Equal(t, "/src/mathx.hpp", into.DeclFile)
	require.Len(t, into.Params, 1)
	assert.Equal(t, "int", into.Result.Spelling)

	// A second declaration must not clear a definition, and must not overwrite
	// what the first sighting already knows.
	mergeInto(&into, Symbol{Mangled: from.Mangled, DefFile: "/elsewhere.hpp", DefLine: 3})
	assert.True(t, into.HasOutOfLineBody)
	assert.Equal(t, "/src/mathx.cpp", into.DefFile)
	assert.Len(t, into.Params, 1)
}

// anyDisplayNameMatches reports whether some display name contains all the
// given fragments. It exists so a test can assert the shape of a libclang
// rendering without pinning the qualification rules of one clang version.
func anyDisplayNameMatches(names []string, fragments ...string) bool {
	for _, n := range names {
		all := true
		for _, f := range fragments {
			if !strings.Contains(n, f) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}
