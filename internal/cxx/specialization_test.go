package cxx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpecializationMirrorsWhenLayoutIsKnown parses a header with no system
// includes, the case where libclang completes the instantiation: the
// specialization must be a normal mirrorable record.
func TestSpecializationMirrorsWhenLayoutIsKnown(t *testing.T) {
	skipWithoutLibclang(t)
	header := fixturePath(t, "spec", "mirror.hpp")
	project, err := Open(Options{
		Header: header,
		Args:   []string{"-std=c++17", "-I" + filepath.Dir(header)},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	box, ok := project.Record("spec::Box<int>")
	require.True(t, ok)
	assert.True(t, box.IsTemplateSpecialization)
	assert.True(t, box.LayoutKnown, "libclang completed this instantiation")
	assert.Equal(t, int64(4), box.SizeBytes)
	assert.Equal(t, int64(4), box.AlignBytes)
	assert.True(t, box.IsDefaultConstructible)
	assert.True(t, box.IsCopyConstructible)

	require.Len(t, box.Fields, 1)
	assert.Equal(t, "value", box.Fields[0].Name)
	assert.Equal(t, TypeInteger, box.Fields[0].Type.Kind)
	assert.Equal(t, int64(4), box.Fields[0].Type.SizeBytes)
	assert.Equal(t, int64(0), box.Fields[0].OffsetBytes)

	require.Len(t, box.Members, 1)
	assert.Equal(t, "spec::Box<int>::get", box.Members[0].QualifiedName)
	assert.Equal(t, Method, box.Members[0].Kind)
	assert.Equal(t, "int", box.Members[0].Result.Spelling)

	holder, ok := project.Record("spec::Holder")
	require.True(t, ok)
	require.Len(t, holder.Fields, 1)
	assert.Equal(t, int64(0), holder.Fields[0].OffsetBytes)
}

// TestSpecializationLayoutContract pins the layout contract for a specialisation
// parsed with the system headers available. libclang either completes the
// instantiation or defers it, depending on the standard headers the environment
// ships, so the test does not pick one outcome: the specialisation always
// carries its fields and members, a known layout has real size/align/offsets,
// and an unknown one is explicitly -1.
func TestSpecializationLayoutContract(t *testing.T) {
	tu := recordFixture(t)

	box, ok := tu.Record("mathx::Box<int>")
	require.True(t, ok)
	assert.True(t, box.IsTemplateSpecialization)
	assertLayoutIsContractual(t, box)
	assert.True(t, box.IsDefaultConstructible)
	assert.True(t, box.IsCopyConstructible)

	require.Len(t, box.Fields, 1, "the fields are reachable whichever layout branch the environment takes")
	field := box.Fields[0]
	assert.Equal(t, "value", field.Name)
	assert.Equal(t, TypeInteger, field.Type.Kind)
	assert.Equal(t, int64(4), field.Type.SizeBytes, "int is 4 bytes on the x86-64 System V ABI")

	if box.LayoutKnown {
		// The pinned CI image resolves <cstddef>, so this is its branch:
		// Box<int> is one int field, with no padding.
		assert.Equal(t, int64(4), box.SizeBytes, "sizeof(Box<int>) is sizeof(int)")
		assert.Equal(t, int64(4), box.AlignBytes, "alignof(Box<int>) is alignof(int)")
		assert.Equal(t, int64(0), field.OffsetBytes, "the single field starts at offset 0")
	} else {
		// A bare checkout whose system headers do not resolve takes this
		// branch: the layout is absent and the model says so instead of
		// guessing a size.
		assert.Equal(t, int64(-1), box.SizeBytes)
		assert.Equal(t, int64(-1), box.AlignBytes)
		assert.Equal(t, int64(-1), field.OffsetBytes, "a deferred layout reports no offset")
	}

	var memberNames []string
	var get Symbol
	for _, member := range box.Members {
		memberNames = append(memberNames, member.QualifiedName)
		if member.QualifiedName == "mathx::Box<int>::get" {
			get = member
		}
	}
	assert.ElementsMatch(t, []string{
		"mathx::Box<int>::get",
		"mathx::Box<int>::operator int",
	}, memberNames, "the members are reachable whichever layout branch the environment takes")
	require.NotEmpty(t, get.QualifiedName, "the instantiation carries its members")
	assert.Equal(t, "int", get.Result.Spelling)
	assert.Empty(t, get.Mangled, "a template instantiation is carried by a thunk")
}

func TestSpecializationConstructorsAndMethods(t *testing.T) {
	tu := recordFixture(t)

	pair, ok := tu.Record("mathx::Pair2<int>")
	require.True(t, ok)
	require.Len(t, pair.Fields, 2)
	assert.Equal(t, "first", pair.Fields[0].Name)
	assert.Equal(t, TypeInteger, pair.Fields[0].Type.Kind)
	assert.Equal(t, "second", pair.Fields[1].Name)
	assert.False(t, pair.IsDefaultConstructible, "its only constructor takes two arguments")
	assert.True(t, pair.IsCopyConstructible)

	var names []string
	for _, member := range pair.Members {
		names = append(names, member.QualifiedName)
	}
	assert.ElementsMatch(t, []string{
		"mathx::Pair2<int>::Pair2",
		"mathx::Pair2<int>::~Pair2",
		"mathx::Pair2<int>::sum",
	}, names)

	ctor := pair.Members[0]
	require.Equal(t, Constructor, ctor.Kind)
	require.Len(t, ctor.Params, 2)
	assert.Equal(t, TypeInteger, ctor.Params[0].Kind)
	assert.Equal(t, int64(4), ctor.Params[0].SizeBytes)
	assert.Equal(t, "int", ctor.Params[1].Spelling)

	dtor := pair.Members[1]
	assert.Equal(t, Destructor, dtor.Kind)
	assert.Equal(t, TypeVoid, dtor.Result.Kind)

	sum := pair.Members[2]
	assert.Equal(t, "int", sum.Result.Spelling)
	assert.Empty(t, sum.Params)
}

func TestSpecializationCopyConstructibility(t *testing.T) {
	tu := recordFixture(t)

	noCopy, ok := tu.Record("mathx::NoCopy<int>")
	require.True(t, ok)
	assert.True(t, noCopy.IsDefaultConstructible, "the default constructor is = default")
	assert.False(t, noCopy.IsCopyConstructible, "the copy constructor is = delete")
	require.Len(t, noCopy.Members, 2)
	assert.False(t, noCopy.Members[0].IsDeleted)
	assert.True(t, noCopy.Members[1].IsDeleted)
}

func TestSpecializationsReachedThroughAFieldType(t *testing.T) {
	tu := recordFixture(t)

	// BoxHolder's field is Box<double>: libclang emits no cursor for that
	// implicit instantiation, so the model builds it from the field's type.
	boxDouble, ok := tu.Record("mathx::Box<double>")
	require.True(t, ok)
	assert.Equal(t, "mathx::Box", boxDouble.TemplateName)
	require.Len(t, boxDouble.TemplateArguments, 1)
	assert.Equal(t, "double", boxDouble.TemplateArguments[0].Spelling)
	require.Len(t, boxDouble.Fields, 1)
	assert.Equal(t, TypeFloat, boxDouble.Fields[0].Type.Kind)

	// A non-type argument survives through the type spelling.
	fixed, ok := tu.Record("mathx::Fixed<double,4>")
	require.True(t, ok)
	require.Len(t, fixed.TemplateArguments, 2)
	assert.Equal(t, TemplateArgumentType, fixed.TemplateArguments[0].Kind)
	assert.Equal(t, "double", fixed.TemplateArguments[0].Spelling)
	assert.Equal(t, TemplateArgumentIntegral, fixed.TemplateArguments[1].Kind)
	assert.Equal(t, "4", fixed.TemplateArguments[1].Spelling)
	assert.Equal(t, int64(4), fixed.TemplateArguments[1].Value)
	require.Len(t, fixed.Fields, 1)
	assert.Equal(t, "double[4]", fixed.Fields[0].Type.Spelling, "the bound is substituted")
	assert.True(t, fixed.IsDefaultConstructible, "an array of doubles is trivially constructible")
	assert.True(t, fixed.IsCopyConstructible)
	assert.Equal(t, int64(4), fixed.Fields[0].Type.ArraySize)
	assert.Equal(t, TypeFloat, fixed.Fields[0].Type.Element.Kind, "the dependent element is resolved")

	// A parameter pack is one argument whose spelling is the whole list.
	pack, ok := tu.Record("mathx::Pack<int,double>")
	require.True(t, ok)
	require.Len(t, pack.TemplateArguments, 1)
	assert.Equal(t, TemplateArgumentPack, pack.TemplateArguments[0].Kind)
	assert.Equal(t, "int,double", pack.TemplateArguments[0].Spelling)
}

func TestProjectMembersIncludesSpecializationMembers(t *testing.T) {
	project := openFixture(t)

	var names []string
	for _, member := range project.Members("mathx::Pair2<int>") {
		names = append(names, member.QualifiedName)
	}
	assert.ElementsMatch(t, []string{
		"mathx::Pair2<int>::Pair2",
		"mathx::Pair2<int>::~Pair2",
		"mathx::Pair2<int>::sum",
	}, names)
}

func TestSpecializationBasesAndVirtuality(t *testing.T) {
	tu := recordFixture(t)

	derived, ok := tu.Record("mathx::DerivedBox<int>")
	require.True(t, ok)
	require.Len(t, derived.Bases, 1)
	assert.Equal(t, "mathx::Box<int>", derived.Bases[0].QualifiedName, "the base name is substituted")
	assert.Equal(t, int64(-1), derived.Bases[0].OffsetBytes, "the instantiated layout is not available")
	assert.False(t, derived.HasVirtualBase)
	require.Len(t, derived.Fields, 1)
	assert.Equal(t, "extra", derived.Fields[0].Name)

	virtual, ok := tu.Record("mathx::VirtualBox<int>")
	require.True(t, ok)
	assert.True(t, virtual.HasVirtualFunction)
	assert.True(t, virtual.IsPolymorphic)
	var virtualMethods []string
	for _, member := range virtual.Members {
		if member.IsVirtual {
			virtualMethods = append(virtualMethods, member.QualifiedName)
		}
	}
	assert.ElementsMatch(t, []string{
		"mathx::VirtualBox<int>::~VirtualBox",
		"mathx::VirtualBox<int>::get",
	}, virtualMethods)
}

func TestSpecializationRecordFieldType(t *testing.T) {
	tu := recordFixture(t)

	wrapper, ok := tu.Record("mathx::Wrapper<int>")
	require.True(t, ok)
	require.Len(t, wrapper.Fields, 1)
	inner := wrapper.Fields[0].Type
	assert.Equal(t, "Box<int>", inner.Spelling, "a dependent record spelling is substituted too")
	// libclang classifies a dependent specialization as an unexposed type, so
	// the model keeps it as written; the emitter maps it by that spelling.
	assert.Equal(t, TypeOther, inner.Kind)
	assert.Empty(t, inner.RecordName)
}

func TestBaseTypeName(t *testing.T) {
	for _, tc := range []struct {
		spelling string
		want     string
	}{
		{"T", "T"},
		{"const T", "T"},
		{"volatile T", "T"},
		{"const volatile T", "T"},
		{"struct T", "T"},
		{"class T", "T"},
		{"T *", "T *"},
		{"", ""},
	} {
		assert.Equal(t, tc.want, baseTypeName(tc.spelling), tc.spelling)
	}
}

func TestTypeFieldOffsetWithoutName(t *testing.T) {
	library := skipWithoutLibclang(t)
	assert.Equal(t, int64(-1), library.typeFieldOffset(CXType{}, ""))
	assert.Empty(t, library.fillArgumentSpellings(nil, CXType{}), "an invalid type spells no arguments")
}

func TestFullExplicitSpecialization(t *testing.T) {
	tu := recordFixture(t)

	full, ok := tu.Record("mathx::Spec<int>")
	require.True(t, ok)
	assert.False(t, full.Synthesized, "a full explicit specialization has its own definition")
	assert.True(t, full.LayoutKnown)
	assert.Equal(t, int64(4), full.SizeBytes)
	require.Len(t, full.Fields, 1)
	assert.Equal(t, "special", full.Fields[0].Name, "the explicit body's field, not the pattern's")
	assert.Equal(t, TypeInteger, full.Fields[0].Type.Kind)
	assert.Empty(t, full.Members, "the body's members are project symbols, with their manglings")
	member, ok := tu.Lookup("mathx::Spec<int>::get")
	require.True(t, ok)
	assert.Equal(t, Method, member.Kind)

	// The primary pattern's body is not part of the specialization.
	for _, field := range full.Fields {
		assert.NotEqual(t, "value", field.Name)
	}

	kind, ok := tu.Enum("mathx::Spec<int>::Kind")
	require.True(t, ok)
	assert.True(t, kind.ConstantsKnown)
	require.Len(t, kind.Constants, 1)
	assert.Equal(t, int64(42), kind.Constants[0].Value, "the explicit body's initializer, not the pattern's 1")

	_, ok = tu.Enum("mathx::Spec::Kind")
	assert.False(t, ok, "the primary pattern's nested enum is not a concrete type")
}

func TestPartialSpecialization(t *testing.T) {
	tu := recordFixture(t)

	partial, ok := tu.Record("mathx::Spec<int*>")
	require.True(t, ok)
	assert.True(t, partial.Synthesized, "a partial instantiation has no definition cursor")
	require.Len(t, partial.Fields, 1)
	assert.Equal(t, "pointer", partial.Fields[0].Name)
	assert.Equal(t, TypePointer, partial.Fields[0].Type.Kind)
	require.NotNil(t, partial.Fields[0].Type.Pointee)
	assert.Equal(t, TypeInteger, partial.Fields[0].Type.Pointee.Kind)

	pointee := mustMember(t, tu, "mathx::Spec<int*>::pointee")
	assert.Equal(t, TypeInteger, pointee.Result.Kind, "the partial's T is int")
	assert.True(t, pointee.IsTemplate == false)

	kind, ok := tu.Enum("mathx::Spec<int*>::Kind")
	require.True(t, ok)
	assert.Equal(t, int64(7), kind.Constants[0].Value, "the partial's initializer")

	// A non-matching instantiation falls back to the primary.
	fallback, ok := tu.Record("mathx::Spec<double>")
	require.True(t, ok)
	require.Len(t, fallback.Fields, 1)
	assert.Equal(t, "value", fallback.Fields[0].Name)
	assert.Equal(t, TypeFloat, fallback.Fields[0].Type.Kind)
	doubleKind, ok := tu.Enum("mathx::Spec<double>::Kind")
	require.True(t, ok)
	assert.Equal(t, int64(1), doubleKind.Constants[0].Value, "the primary's initializer")
}

func mustMember(t *testing.T, tu *TU, name string) Symbol {
	t.Helper()
	for _, record := range tu.Records() {
		for _, member := range record.Members {
			if member.QualifiedName == name {
				return member
			}
		}
	}
	t.Fatalf("%s is not a member of any record", name)
	return Symbol{}
}

func TestFullExplicitMemberHasItsOwnMangling(t *testing.T) {
	project := openFixture(t)
	member, ok := project.Lookup("mathx::Spec<int>::get")
	require.True(t, ok)
	assert.NotEmpty(t, member.Mangled, "the explicit body's member is a real symbol")
	assert.True(t, member.Parent == "mathx::Spec<int>")
	assert.False(t, member.IsTemplate)
}
