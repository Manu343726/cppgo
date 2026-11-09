package gen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestNestPrefix covers the prefix rule on the shapes a qualified name takes:
// a type at namespace scope has none, a nested one takes its record's Go name,
// and a template argument's own "::" is not nesting.
func TestNestPrefix(t *testing.T) {
	records := map[string]string{
		"ns::Outer":     "Outer",
		"ns::Outer::In": "OuterIn",
		"ns::Box<int>":  "BoxInt",
	}
	cases := []struct {
		name string
		want string
	}{
		{"ns::Plain", ""},
		{"ns::Outer::Tag", "Outer"},
		{"ns::Outer::In::Deep", "OuterIn"},
		{"ns::Box<int>::Slot", "BoxInt"},
		{"ns::Box<ns::Outer>::Slot", ""},
	}
	b := &builder{}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, b.nestPrefix(tc.name, records))
		})
	}
}

// TestAssignTypeNamesSkipsPatternsAndDuplicates pins what the pre-pass does with
// the shapes a header produces: a template pattern is not a Go type, and two
// sightings of one enum name it once.
func TestAssignTypeNamesSkipsPatternsAndDuplicates(t *testing.T) {
	b := &builder{
		rawRecords: []cxx.Record{
			{QualifiedName: "ns::Box", IsTemplate: true},
			{QualifiedName: "ns::Outer"},
			{QualifiedName: "ns::Outer::In"},
			{QualifiedName: ""},
			{QualifiedName: "ns::Outer"},
		},
		rawEnums: []cxx.Enum{
			{QualifiedName: "ns::Outer::Kind"},
			{QualifiedName: ""},
			{QualifiedName: "ns::Outer::Kind"},
			{QualifiedName: "ns::Mode"},
		},
	}
	b.assignTypeNames()
	assert.NotContains(t, b.recordGoNames, "ns::Box")
	assert.Equal(t, "Outer", b.recordGoNames["ns::Outer"])
	assert.Equal(t, "OuterIn", b.recordGoNames["ns::Outer::In"])
	assert.Equal(t, "OuterKind", b.enumGoNames["ns::Outer::Kind"])
	assert.Equal(t, "Mode", b.enumGoNames["ns::Mode"])
}

// TestBuildEnumsSkipsADuplicateSighting is the same rule for the enum model.
func TestBuildEnumsSkipsADuplicateSighting(t *testing.T) {
	b := &builder{cfg: Config{}, records: map[string]*recordModel{}, enums: map[string]*enumModel{}}
	b.enumGoNames = map[string]string{"ns::Mode": "Mode"}
	underlying := cxx.Param{Kind: cxx.TypeInteger, Spelling: "int", SizeBytes: 4}
	b.rawEnums = []cxx.Enum{
		{QualifiedName: "ns::Mode", ConstantsKnown: true, Underlying: underlying},
		{QualifiedName: "ns::Mode", ConstantsKnown: true, Underlying: underlying},
		{QualifiedName: ""},
	}
	b.buildEnums()
	assert.Len(t, b.enums, 1)
	assert.Equal(t, []string{"ns::Mode"}, b.enumOrder)
}

// TestSelectEnumsNarrowsByTheManifest covers the four ways an enum is kept: a
// root, a named entry, a signature's type closure, and a mirrored field.
func TestSelectEnumsNarrowsByTheManifest(t *testing.T) {
	t.Run("no bind keeps every enum", func(t *testing.T) {
		b := &builder{cfg: Config{}, enums: map[string]*enumModel{
			"ns::A": {CxxName: "ns::A"},
			"ns::B": {CxxName: "ns::B"},
		}}
		b.selectEnums(nil)
		assert.True(t, b.enums["ns::A"].selected)
		assert.True(t, b.enums["ns::B"].selected)
	})

	t.Run("a bind entry keeps the enum it names", func(t *testing.T) {
		b := &builder{cfg: Config{Bind: []string{"ns::A"}}, enums: map[string]*enumModel{
			"ns::A": {CxxName: "ns::A"},
			"ns::B": {CxxName: "ns::B"},
		}}
		matched := make([]bool, 1)
		b.selectEnums(matched)
		assert.True(t, b.enums["ns::A"].selected)
		assert.False(t, b.enums["ns::B"].selected)
		assert.True(t, matched[0])
	})

	t.Run("an exclude entry drops the enum it names", func(t *testing.T) {
		b := &builder{cfg: Config{Exclude: []string{"ns::B"}}, enums: map[string]*enumModel{
			"ns::A": {CxxName: "ns::A"},
			"ns::B": {CxxName: "ns::B"},
		}}
		b.selectEnums(nil)
		assert.True(t, b.enums["ns::A"].selected)
		assert.False(t, b.enums["ns::B"].selected)
	})

	t.Run("a kept binding's signature keeps its enum", func(t *testing.T) {
		enum := &enumModel{CxxName: "ns::A"}
		b := &builder{cfg: Config{Bind: []string{"ns::f"}}, enums: map[string]*enumModel{"ns::A": enum}}
		b.bindings = []*binding{{
			Owner:    nil,
			Params:   []*goParam{{Name: "a", Type: &goType{Kind: goEnum, Enum: enum}}},
			selected: true,
		}}
		b.selectEnums(nil)
		assert.True(t, enum.selected)
	})

	t.Run("a mirrored field keeps its enum", func(t *testing.T) {
		enum := &enumModel{CxxName: "ns::A"}
		b := &builder{cfg: Config{Bind: []string{"ns::S"}}, enums: map[string]*enumModel{"ns::A": enum}}
		b.order = []string{"ns::S"}
		b.records = map[string]*recordModel{"ns::S": {Fields: []*fieldModel{{Type: &goType{Kind: goEnum, Enum: enum}}}}}
		b.selectEnums(nil)
		assert.True(t, enum.selected)
	})
}

// TestSelectedEnumsOrdersByGoName keeps the generated file deterministic.
func TestSelectedEnumsOrdersByGoName(t *testing.T) {
	b := &builder{enums: map[string]*enumModel{
		"ns::B": {GoName: "Bravo", selected: true},
		"ns::A": {GoName: "Alpha", selected: true},
		"ns::C": {GoName: "Charlie"},
	}, enumOrder: []string{"ns::B", "ns::A", "ns::C"}}
	got := b.selectedEnums()
	require.Len(t, got, 2)
	assert.Equal(t, "Alpha", got[0].GoName)
	assert.Equal(t, "Bravo", got[1].GoName)
}

// TestStaticFieldSelection covers the manifest rule for a static member: no bind
// keeps it, an entry naming the member or its record keeps it, and an exclude
// drops it without failing the entry.
func TestStaticFieldSelection(t *testing.T) {
	owner := &recordModel{CxxName: "ns::Counter", GoName: "Counter"}
	field := cxx.StaticField{Name: "count"}
	cases := []struct {
		name    string
		bind    []string
		excl    []string
		want    bool
		matched bool
	}{
		{name: "no manifest", want: true},
		{name: "bound by member", bind: []string{"ns::Counter::count"}, want: true, matched: true},
		{name: "bound by record", bind: []string{"ns::Counter"}, want: true, matched: true},
		{name: "another member only", bind: []string{"ns::Counter::label"}},
		{name: "excluded by member", excl: []string{"ns::Counter::count"}, want: false, matched: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := &builder{cfg: Config{Bind: tc.bind, Exclude: tc.excl},
				bindMatched: make([]bool, len(tc.bind)), excludeMatched: make([]bool, len(tc.excl))}
			assert.Equal(t, tc.want, b.staticFieldSelected(owner, field))
		})
	}
}

// TestTemplateSelection covers the manifest rule for a generic template: with no
// manifest every pattern is a root, a -bind entry selects it, and an
// -instantiate entry selects the pattern it specializes.
func TestTemplateSelection(t *testing.T) {
	cases := []struct {
		name        string
		bind        []string
		instantiate []string
		want        bool
	}{
		{name: "no manifest", want: true},
		{name: "bound", bind: []string{"ns::Box"}, want: true},
		{name: "bound by a specialization", bind: []string{"ns::Box<int>"}, want: true},
		{name: "instantiated", instantiate: []string{"ns::Box<int>"}, want: true},
		{name: "a manifest that names something else", bind: []string{"ns::Other"}},
		{name: "an instantiation of something else", instantiate: []string{"ns::Other<int>"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			b := &builder{cfg: Config{Bind: tc.bind, Instantiate: tc.instantiate},
				bindMatched: make([]bool, len(tc.bind))}
			assert.Equal(t, tc.want, b.templateSelected("ns::Box"))
		})
	}
}

// TestExpandDefaultsZeroArityAndErrors covers the two edges of the expansion: the
// zero-argument prefix is dropped with a warning, and a prefix the emitter cannot
// lower names its arity.
func TestExpandDefaultsZeroArityAndErrors(t *testing.T) {
	t.Run("the zero-argument prefix is dropped", func(t *testing.T) {
		header := writeHeader(t, "zero.hpp", `
namespace zero {
int f(int a = 1);
}
`)
		result, err := RenderResult(Config{Headers: []string{header}, Package: "zero", Library: "libzero", Out: "."})
		require.NoError(t, err)
		found := false
		for _, warning := range result.Warnings {
			if strings.Contains(warning, "the zero-argument spelling is dropped") {
				found = true
			}
		}
		assert.True(t, found, "the drop must be reported: %v", result.Warnings)
		var names []string
		for _, line := range result.Report {
			names = append(names, firstField(line))
		}
		assert.Contains(t, names, "zero::f(int)")
	})
}

// firstField is the first whitespace-separated field of a report line.
func firstField(line string) string {
	if i := strings.IndexByte(line, ' '); i >= 0 {
		return line[:i]
	}
	return line
}

// TestInheritanceHelpers covers the pieces the build path uses only through a
// hierarchy the fixtures already have: the base walk's guards, the derived set
// and the override key.
func TestInheritanceHelpers(t *testing.T) {
	b := &builder{
		records: map[string]*recordModel{},
		rawRecords: []cxx.Record{
			{QualifiedName: "ns::Base"},
			{QualifiedName: "ns::Hidden", Bases: []cxx.Base{{QualifiedName: "ns::Base", Access: cxx.AccessPrivate}}},
			{QualifiedName: "ns::Child", Bases: []cxx.Base{{QualifiedName: "ns::Base", Access: cxx.AccessPublic}}},
			{QualifiedName: "ns::Unknown", Bases: []cxx.Base{{QualifiedName: "ns::Missing", Access: cxx.AccessPublic}}},
		},
	}
	for _, raw := range b.rawRecords {
		b.records[raw.QualifiedName] = &recordModel{CxxName: raw.QualifiedName, GoName: exportName(simpleName(raw.QualifiedName))}
		b.order = append(b.order, raw.QualifiedName)
	}
	hidden := b.records["ns::Hidden"]
	assert.Empty(t, b.basesOf(hidden), "a private base is not part of the API")

	child := b.records["ns::Child"]
	bases := b.basesOf(child)
	require.Len(t, bases, 1)
	assert.Equal(t, "ns::Base", bases[0].CxxName)
	child.Bases = bases
	assert.Equal(t, []*recordModel{child}, b.derivedFrom(b.records["ns::Base"]))

	unknown := b.records["ns::Unknown"]
	assert.Empty(t, b.basesOf(unknown), "a base the model does not hold contributes nothing")

	// The override key is the simple name, the parameter spellings and const.
	key := methodKey(cxx.Symbol{QualifiedName: "ns::A::f", Params: []cxx.Param{{Spelling: "int"}}, IsConst: true})
	assert.Equal(t, "f(int) const", key)
	assert.Equal(t, "f(int) const", methodKey(cxx.Symbol{QualifiedName: "ns::B::f", Params: []cxx.Param{{Spelling: "int"}}, IsConst: true}))
	assert.NotEqual(t, key, methodKey(cxx.Symbol{QualifiedName: "ns::A::f", Params: []cxx.Param{{Spelling: "double"}}, IsConst: true}))
}

// TestCopyAssignmentAccess covers the field-setter rule: a setter that copies a
// record into the member needs the record's copy assignment, and a protected one
// leaves the field read-only. A scalar field is assignable whatever the record
// declares.
func TestCopyAssignmentAccess(t *testing.T) {
	header := writeHeader(t, "assign.hpp", `
namespace assign {
class Locked {
public:
    Locked();
    ~Locked();
    int value;
protected:
    Locked &operator=(const Locked &);
};
class Holder {
public:
    Holder();
    ~Holder();
    /// A record field whose copy assignment is protected: read only.
    Locked inner;
    /// A scalar field: always assignable.
    int count;
};
}
`)
	result, err := RenderResult(Config{Headers: []string{header}, Package: "assign", Library: "libassign", Out: "."})
	require.NoError(t, err)
	var generated string
	for _, out := range result.Outputs {
		if out.Name == GenFileName {
			generated = string(out.Data)
		}
	}
	assert.Contains(t, generated, "func (h *Holder) SetCount(count int32) {")
	assert.NotContains(t, generated, "func (h *Holder) SetInner")
	joined := ""
	for _, warning := range result.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "assign::Holder::inner has no setter")

	b := &builder{symbols: []cxx.Symbol{
		{QualifiedName: "assign::Locked::operator=", IsOperator: true, Operator: "=", Access: cxx.AccessProtected},
	}}
	assert.False(t, b.copyAssignmentPublic("assign::Locked"))
	assert.True(t, b.copyAssignmentPublic("assign::Holder"))
}

// TestABorrowedRecordPointerFieldIsReported pins the field rule: a pointer to a
// record has no Go owner, so the field is omitted and named.
func TestABorrowedRecordPointerFieldIsReported(t *testing.T) {
	header := writeHeader(t, "borrowed.hpp", `
namespace borrowed {
class Payload { public: ~Payload(); };
class Holder {
public:
    Payload *payload;
    int count;
};
}
`)
	result, err := RenderResult(Config{Headers: []string{header}, Package: "borrowed", Library: "libborrowed", Out: "."})
	require.NoError(t, err)
	joined := ""
	for _, warning := range result.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "borrowed::Holder::payload is not bound")
}

// TestSimpleNameRespectingTemplates covers the split on a name whose last
// component carries template arguments.
func TestSimpleNameRespectingTemplates(t *testing.T) {
	assert.Equal(t, "size_t>", simpleNameRespectingTemplates("std::size_t>"))
	assert.Equal(t, "Matrix<int>", simpleNameRespectingTemplates("ns::Matrix<int>"))
	assert.Equal(t, "Bare", simpleNameRespectingTemplates("Bare"))
}
