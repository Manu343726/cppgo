package cxx

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordFixture parses the mathx header on its own and returns the translation
// unit, so a test can read both the record model and the symbol model from the
// same parse.
func recordFixture(t *testing.T) *TU {
	t.Helper()
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"),
		[]string{"-std=c++17", "-I" + dir, "-x", "c++-header"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })
	return tu
}

// recordByName is the record map of the fixture, keyed by qualified name.
func recordByName(t *testing.T) map[string]Record {
	t.Helper()
	out := make(map[string]Record)
	for _, record := range recordFixture(t).Records() {
		out[record.QualifiedName] = record
	}
	return out
}

func TestRecordLayout(t *testing.T) {
	records := recordByName(t)

	for _, tc := range []struct {
		name       string
		kind       RecordKind
		size       int64
		align      int64
		pod        bool
		trivial    bool
		polymorph  bool
		abstract   bool
		commentary string
	}{
		{"mathx::Vector", Class, 24, 8, false, false, true, false, "a virtual method adds a vptr and makes it polymorphic"},
		{"mathx::Point", Class, 4, 4, false, true, false, false, "user constructor: not POD, still trivial for calls"},
		{"mathx::Tiny", Struct, 4, 4, false, false, false, false, "declared destructor"},
		{"mathx::Pair", Struct, 16, 8, true, true, false, false, "the register-friendly aggregate"},
		{"mathx::Shape", Class, 16, 8, false, false, true, true, "virtual destructor and a pure virtual"},
		{"mathx::Point2D", Struct, 16, 8, true, true, false, false, "a mirrorable aggregate"},
		{"mathx::Segment", Struct, 40, 8, true, true, false, false, "nested PODs compose"},
		{"mathx::Flags", Struct, 8, 4, true, true, false, false, "bitfields"},
		{"mathx::Packed", Struct, 9, 1, true, true, false, false, "packing is a layout Go cannot reproduce"},
		{"mathx::Mixed", Struct, 12, 4, false, true, false, false, "members of different access levels are not standard-layout"},
		{"mathx::Value", Union, 4, 4, true, true, false, false, "a union"},
		{"mathx::Level1", Struct, 4, 4, false, false, false, false, "triviality propagates through bases"},
		{"mathx::SmallCopy", Struct, 4, 4, false, false, false, false, "user copy constructor"},
		{"mathx::SmallMember", Struct, 4, 4, false, false, false, false, "a non-trivial member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, ok := records[tc.name]
			require.True(t, ok, "%s must be in the model", tc.name)
			assert.Equal(t, tc.kind, record.Kind, "kind")
			assert.Equal(t, tc.size, record.SizeBytes, "sizeof")
			assert.Equal(t, tc.align, record.AlignBytes, "alignof")
			assert.Equal(t, tc.pod, record.IsPOD, "POD")
			assert.Equal(t, tc.trivial, record.IsTrivial, "trivial for calls (%s)", tc.commentary)
			assert.Equal(t, tc.polymorph, record.IsPolymorphic, "polymorphic")
			assert.Equal(t, tc.abstract, record.IsAbstract, "abstract")
			assert.False(t, record.IsTemplate, "a definition is not a template")
			assert.NotEmpty(t, record.DeclFile, "the definition has a location")
			assert.Positive(t, record.DeclLine)
		})
	}
}

func TestRecordFields(t *testing.T) {
	records := recordByName(t)

	for _, tc := range []struct {
		record string
		fields []Field
	}{
		{"mathx::Vector", []Field{
			{Name: "x_", Access: AccessPrivate, OffsetBytes: 8},
			{Name: "y_", Access: AccessPrivate, OffsetBytes: 16},
		}},
		{"mathx::Pair", []Field{
			{Name: "a", Access: AccessPublic, OffsetBytes: 0},
			{Name: "b", Access: AccessPublic, OffsetBytes: 8},
		}},
		{"mathx::Segment", []Field{
			{Name: "from", Access: AccessPublic, OffsetBytes: 0},
			{Name: "to", Access: AccessPublic, OffsetBytes: 16},
			{Name: "id", Access: AccessPublic, OffsetBytes: 32},
		}},
		{"mathx::Mixed", []Field{
			{Name: "visible", Access: AccessPublic, OffsetBytes: 0},
			{Name: "inherited", Access: AccessProtected, OffsetBytes: 4},
			{Name: "hidden", Access: AccessPrivate, OffsetBytes: 8},
		}},
		{"mathx::Value", []Field{
			{Name: "i", Access: AccessPublic, OffsetBytes: 0},
			{Name: "f", Access: AccessPublic, OffsetBytes: 0},
		}},
	} {
		t.Run(tc.record, func(t *testing.T) {
			record := records[tc.record]
			require.Len(t, record.Fields, len(tc.fields))
			for i, want := range tc.fields {
				got := record.Fields[i]
				assert.Equal(t, want.Name, got.Name, "field %d name", i)
				assert.Equal(t, want.Access, got.Access, "field %s access", want.Name)
				assert.Equal(t, want.OffsetBytes, got.OffsetBytes, "field %s offset", want.Name)
				assert.False(t, got.IsBitField, "field %s is not a bitfield", want.Name)
				assert.Zero(t, got.BitWidth, "field %s width", want.Name)
			}
		})
	}
}

func TestRecordFieldTypes(t *testing.T) {
	records := recordByName(t)

	segment := records["mathx::Segment"]
	assert.Equal(t, TypeRecord, segment.Fields[0].Type.Kind)
	assert.Equal(t, "mathx::Point2D", segment.Fields[0].Type.RecordName)
	assert.Equal(t, int64(16), segment.Fields[0].Type.SizeBytes)

	id := segment.Fields[2].Type
	assert.Equal(t, TypeInteger, id.Kind)
	assert.Equal(t, int64(4), id.SizeBytes)
	assert.False(t, id.Unsigned)
	assert.Equal(t, "Int", id.ClangKind)

	// A pointer field names its pointee even when the pointee is incomplete.
	usesForward := records["mathx::UsesForward"]
	require.Len(t, usesForward.Fields, 1)
	pointer := usesForward.Fields[0].Type
	assert.Equal(t, TypePointer, pointer.Kind)
	require.NotNil(t, pointer.Pointee)
	assert.Equal(t, "mathx::Forward", pointer.Pointee.RecordName)
	assert.Less(t, pointer.Pointee.SizeBytes, int64(0), "a forward declaration has no size")
	assert.Equal(t, TypeRecord, pointer.Pointee.Kind)
}

func TestRecordBitfields(t *testing.T) {
	flags := recordByName(t)["mathx::Flags"]
	require.Len(t, flags.Fields, 3)

	low := flags.Fields[0]
	assert.True(t, low.IsBitField)
	assert.Equal(t, 3, low.BitWidth)
	assert.Equal(t, int64(0), low.OffsetBytes)

	high := flags.Fields[1]
	assert.True(t, high.IsBitField)
	assert.Equal(t, 5, high.BitWidth)
	assert.Equal(t, int64(0), high.OffsetBytes, "a bitfield offset is truncated to the byte it starts in")

	ordinary := flags.Fields[2]
	assert.False(t, ordinary.IsBitField)
	assert.Equal(t, int64(4), ordinary.OffsetBytes)
}

func TestPackedRecordLayoutIsReadRaw(t *testing.T) {
	// The model reports packing exactly as the compiler laid it out; the
	// generator is what refuses to mirror it.
	packed := recordByName(t)["mathx::Packed"]
	require.Len(t, packed.Fields, 2)
	assert.Equal(t, int64(0), packed.Fields[0].OffsetBytes)
	assert.Equal(t, int64(1), packed.Fields[1].OffsetBytes, "a packed double starts at offset 1")
	assert.Equal(t, int64(9), packed.SizeBytes)
	assert.Equal(t, int64(1), packed.AlignBytes)
}

func TestRecordBases(t *testing.T) {
	records := recordByName(t)

	level1 := records["mathx::Level1"]
	require.Len(t, level1.Bases, 1)
	assert.Equal(t, "mathx::Level2", level1.Bases[0].QualifiedName)
	assert.Equal(t, AccessPublic, level1.Bases[0].Access, "a struct inherits publicly by default")
	assert.False(t, level1.Bases[0].IsVirtual)
	assert.Equal(t, int64(0), level1.Bases[0].OffsetBytes)

	// The whole chain is in the model, not only the first link.
	for _, name := range []string{"mathx::Level2", "mathx::Level3", "mathx::Level4", "mathx::Level5", "mathx::Level6"} {
		assert.Contains(t, records, name)
	}

	virtualBase := records["mathx::SmallVirtualBase"]
	require.Len(t, virtualBase.Bases, 1)
	assert.True(t, virtualBase.Bases[0].IsVirtual)
	assert.True(t, virtualBase.IsPolymorphic, "a virtual base makes the record polymorphic")
}

func TestRecordTemplates(t *testing.T) {
	records := recordByName(t)

	box, ok := records["mathx::Box"]
	require.True(t, ok, "a template pattern is part of the model, so it can be reported")
	assert.True(t, box.IsTemplate)
	assert.Equal(t, Struct, box.Kind, "clang_getTemplateCursorKind says how the template was declared")
	assert.Equal(t, int64(-1), box.SizeBytes, "a template has no layout")
	assert.Equal(t, int64(-1), box.AlignBytes)
	require.Len(t, box.TemplateParameters, 1)
	assert.Equal(t, TemplateParameter{Name: "T", Kind: TemplateParameterType}, box.TemplateParameters[0])
	require.Len(t, box.Fields, 1, "the pattern's fields are visible even though its layout is not")
	assert.Equal(t, "value", box.Fields[0].Name)
	assert.Equal(t, int64(-1), box.Fields[0].OffsetBytes)
}

func TestRecordLookups(t *testing.T) {
	tu := recordFixture(t)

	vector, ok := tu.Record("mathx::Vector")
	require.True(t, ok)
	assert.Equal(t, int64(24), vector.SizeBytes, "a vptr and two doubles")

	_, ok = tu.Record("mathx::DoesNotExist")
	assert.False(t, ok)

	all := tu.Records()
	require.NotEmpty(t, all)
	assert.Equal(t, all[0].QualifiedName, tu.Records()[0].QualifiedName, "Records returns a copy in a stable order")
}

func TestEnumModel(t *testing.T) {
	tu := recordFixture(t)

	axis, ok := tu.Enum("mathx::Axis")
	require.True(t, ok)
	assert.True(t, axis.IsScoped)
	assert.Equal(t, TypeInteger, axis.Underlying.Kind)
	assert.Equal(t, int64(1), axis.Underlying.SizeBytes)
	assert.True(t, axis.Underlying.Unsigned, "unsigned char")
	require.Len(t, axis.Constants, 2)
	assert.Equal(t, "X", axis.Constants[0].Name)
	assert.Equal(t, int64(0), axis.Constants[0].Value)
	assert.Equal(t, "Y", axis.Constants[1].Name)
	assert.Equal(t, int64(1), axis.Constants[1].Value)
	assert.True(t, axis.Constants[1].IsUnsigned)

	mode, ok := tu.Enum("mathx::Mode")
	require.True(t, ok)
	assert.False(t, mode.IsScoped)
	assert.Equal(t, int64(4), mode.Underlying.SizeBytes)
	require.Len(t, mode.Constants, 3)
	assert.Equal(t, int64(5), mode.Constants[1].Value)
	assert.Equal(t, "ModeMax", mode.Constants[2].Name)
	assert.Equal(t, int64(10), mode.Constants[2].Value)

	_, ok = tu.Enum("mathx::DoesNotExist")
	assert.False(t, ok)
	assert.NotEmpty(t, tu.Enums())
}

func TestRecordKindStrings(t *testing.T) {
	for _, tc := range []struct {
		kind RecordKind
		want string
	}{
		{Class, "class"},
		{Struct, "struct"},
		{Union, "union"},
		{RecordKind(99), "unknown record kind"},
	} {
		assert.Equal(t, tc.want, tc.kind.String())
	}
}

func TestAccessStrings(t *testing.T) {
	for _, tc := range []struct {
		access Access
		want   string
	}{
		{AccessNone, "none"},
		{AccessPublic, "public"},
		{AccessProtected, "protected"},
		{AccessPrivate, "private"},
		{Access(99), "none"},
	} {
		assert.Equal(t, tc.want, tc.access.String())
	}
}

func TestMergeRecord(t *testing.T) {
	into := Record{
		QualifiedName: "mathx::Point2D",
		SizeBytes:     -1,
		AlignBytes:    -1,
		IsTemplate:    true,
	}
	from := Record{
		QualifiedName: "mathx::Point2D",
		SizeBytes:     16,
		AlignBytes:    8,
		IsPOD:         true,
		IsTrivial:     true,
		IsTemplate:    false,
		Fields:        []Field{{Name: "x"}},
		Bases:         []Base{{QualifiedName: "mathx::Base"}},
		DeclFile:      "mathx.hpp",
		DeclLine:      4,
	}
	mergeRecord(&into, from)
	assert.Equal(t, int64(16), into.SizeBytes)
	assert.Equal(t, int64(8), into.AlignBytes)
	assert.True(t, into.IsPOD)
	assert.True(t, into.IsTrivial)
	assert.False(t, into.IsTemplate, "a layout-bearing sighting wins over the pattern")
	assert.Len(t, into.Fields, 1)
	assert.Len(t, into.Bases, 1)
	assert.Equal(t, "mathx.hpp", into.DeclFile)
	assert.Equal(t, 4, into.DeclLine)

	// A second layout-bearing sighting changes nothing, and a sighting with no
	// location never overwrites the one that had it.
	mergeRecord(&into, Record{SizeBytes: 8, Fields: []Field{{Name: "y"}}, DeclFile: "other.hpp", DeclLine: 9})
	assert.Equal(t, int64(16), into.SizeBytes)
	assert.Len(t, into.Fields, 1)
	assert.Equal(t, "mathx.hpp", into.DeclFile)
}

func TestParentOf(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"mathx::Vector::norm", "mathx::Vector"},
		{"mathx::add", "mathx"},
		{"add", ""},
		{"", ""},
	} {
		assert.Equal(t, tc.want, parentOf(tc.name), tc.name)
	}
}

func TestIsTemplateParent(t *testing.T) {
	assert.True(t, isTemplateParent(cursorFunctionTemplate))
	assert.True(t, isTemplateParent(cursorClassTemplate))
	assert.True(t, isTemplateParent(cursorClassTemplatePartialSpecialization))
	assert.False(t, isTemplateParent(cursorClassDecl))
}

func TestUnnamedTypesAreDropped(t *testing.T) {
	tu := recordFixture(t)

	for _, record := range tu.Records() {
		assert.NotEmpty(t, record.QualifiedName, "an unnamed record has no name to bind under and must not be invented")
		assert.NotContains(t, record.QualifiedName, "(unnamed", "a synthesized name embeds a source path")
	}
	for _, enum := range tu.Enums() {
		assert.NotEmpty(t, enum.QualifiedName, "an unnamed enum is dropped for the same reason")
		assert.NotContains(t, enum.QualifiedName, "(unnamed", "a synthesized name embeds a source path")
	}
	assert.GreaterOrEqual(t, len(tu.Enums()), 2, "the named enums, including nested ones, are in the model")

	// A type in an anonymous namespace has no qualified name; the base is
	// still readable through its spelling, and the generator refuses it later.
	derived, ok := tu.Record("mathx::DerivedFromHidden")
	require.True(t, ok)
	require.Len(t, derived.Bases, 1)
	assert.Equal(t, "Hidden", derived.Bases[0].QualifiedName)

	// A field of unnamed record type has no name either: the spelling exists,
	// and the generator refuses the field instead of emitting a path.
	withAnonymous, ok := tu.Record("mathx::WithAnonymous")
	require.True(t, ok)
	require.Len(t, withAnonymous.Fields, 1)
	assert.Empty(t, withAnonymous.Fields[0].Type.RecordName)
	assert.Equal(t, TypeRecord, withAnonymous.Fields[0].Type.Kind)
	assert.Contains(t, withAnonymous.Fields[0].Type.Spelling, "unnamed")
}

func TestSymbolMergeKey(t *testing.T) {
	// A mangled name is the identity when there is one.
	assert.Equal(t, "_ZN5mathx3addEii", symbolMergeKey(Symbol{Mangled: "_ZN5mathx3addEii"}))

	// A template pattern has none; its qualified name and signature keep the
	// same declaration from being counted once per translation unit.
	pattern := Symbol{QualifiedName: "mathx::max", IsTemplate: true, Params: []Param{{Spelling: "T"}}}
	assert.Equal(t, "template:mathx::max|void(T)", symbolMergeKey(pattern))

	// An ordinary declaration with no mangled name has no identity to merge on.
	assert.Empty(t, symbolMergeKey(Symbol{QualifiedName: "mathx::nothing"}))
}

func TestMultipleInheritanceAndVirtualBases(t *testing.T) {
	records := recordByName(t)

	both := records["mathx::Both"]
	assert.True(t, both.IsPolymorphic, "Left's virtual function puts a vtable in Both")
	assert.False(t, both.HasVirtualFunction, "Both declares no virtual function itself")
	assert.False(t, both.HasVirtualBase)
	require.Len(t, both.Bases, 2)
	assert.Equal(t, "mathx::Left", both.Bases[0].QualifiedName)
	assert.Equal(t, int64(0), both.Bases[0].OffsetBytes)
	assert.Equal(t, "mathx::Right", both.Bases[1].QualifiedName)
	assert.Equal(t, int64(8), both.Bases[1].OffsetBytes, "the second base is adjusted by 8 bytes")

	left := records["mathx::Left"]
	assert.True(t, left.HasVirtualFunction)
	assert.True(t, left.IsPolymorphic)

	derived := records["mathx::VDerived"]
	assert.True(t, derived.HasVirtualBase)
	assert.True(t, derived.IsPolymorphic)
	require.Len(t, derived.Bases, 1)
	assert.True(t, derived.Bases[0].IsVirtual)
	assert.Equal(t, "mathx::VBase", derived.Bases[0].QualifiedName)
	assert.Equal(t, int64(8), derived.Bases[0].OffsetBytes, "the virtual base offset in this class's layout")
}

func TestRecordLayoutKnown(t *testing.T) {
	records := recordByName(t)

	assert.True(t, records["mathx::Pair"].LayoutKnown)
	assert.True(t, records["mathx::Flags"].LayoutKnown, "bitfields are laid out; they are merely not mirrorable")
	assert.False(t, records["mathx::Box"].LayoutKnown, "a template pattern has no layout")

	// Box<int> is the instantiation libclang may or may not complete, depending
	// on whether the environment's <cstddef> resolves; both branches are valid,
	// and each must have the shape assertLayoutIsContractual pins.
	box := records["mathx::Box<int>"]
	assertLayoutIsContractual(t, box)
	require.Len(t, box.Fields, 1, "the fields are reachable on both branches")
	assert.Equal(t, "value", box.Fields[0].Name)

	// BoxHolder's only field is a Box<double>. Its layout is trustworthy
	// exactly when the member's type was resolved; a deferred member layout
	// makes the holder untrustworthy, and an untrusted size is -1, not a guess.
	holder := records["mathx::BoxHolder"]
	member := records["mathx::Box<double>"]
	assertLayoutIsContractual(t, holder)
	assertLayoutIsContractual(t, member)
	require.Len(t, holder.Fields, 1, "the fields are reachable on both branches")
	if member.LayoutKnown {
		assert.True(t, holder.LayoutKnown, "a fully laid-out member lets the holder's layout be mirrored")
		assert.Equal(t, int64(8), holder.SizeBytes, "sizeof(BoxHolder) is sizeof(Box<double>): one double, 8 bytes")
		assert.Equal(t, int64(8), holder.AlignBytes, "alignof(BoxHolder) is alignof(double)")
	} else {
		assert.False(t, holder.LayoutKnown, "a member whose layout is deferred makes the record untrustworthy")
		assert.Equal(t, int64(-1), holder.SizeBytes, "an untrusted size is reported as unknown")
		assert.Equal(t, int64(-1), holder.AlignBytes)
	}
}

func TestMergeRecordCarriesHierarchyAndTemplateData(t *testing.T) {
	into := Record{QualifiedName: "mathx::Box<int>", SizeBytes: -1, AlignBytes: -1}
	from := Record{
		QualifiedName:            "mathx::Box<int>",
		IsTemplateSpecialization: true,
		TemplateName:             "mathx::Box",
		TemplateArguments:        []TemplateArgument{{Kind: TemplateArgumentType, Spelling: "int"}},
		HasVirtualFunction:       true,
		HasVirtualBase:           true,
		IsPolymorphic:            true,
		SizeBytes:                4,
		AlignBytes:               4,
		LayoutKnown:              true,
	}
	mergeRecord(&into, from)
	assert.True(t, into.IsTemplateSpecialization)
	assert.Equal(t, "mathx::Box", into.TemplateName)
	assert.Len(t, into.TemplateArguments, 1)
	assert.True(t, into.HasVirtualFunction)
	assert.True(t, into.HasVirtualBase)
	assert.True(t, into.IsPolymorphic)
	assert.Equal(t, int64(4), into.SizeBytes)
	assert.True(t, into.LayoutKnown)

	mergeRecord(&into, Record{HasVirtualBase: false, HasVirtualFunction: false, IsPolymorphic: false})
	assert.True(t, into.HasVirtualFunction, "a sighting must not clear a flag")
	assert.True(t, into.HasVirtualBase)
}
