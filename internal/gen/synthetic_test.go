package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// The predicates and small helpers in this file take a model value, not a
// header: a record, a parameter, a symbol. They are exercised here with values
// built by hand, which is the only way to reach the shapes a C++ library does
// not happen to declare and the emitter still has to answer.

// emptyBuilder is a builder with the maps every predicate reads and nothing
// else.
func emptyBuilder(records ...cxx.Record) *builder {
	b := &builder{
		records:         map[string]*recordModel{},
		enums:           map[string]*enumModel{},
		rawRecords:      records,
		selectedSymbols: map[string]bool{},
		refusalReasons:  map[string]string{},
		skipReasons:     map[string]string{},
		skipClasses:     map[string]string{},
	}
	b.mapper = &mapper{records: b.records, enums: b.enums}
	return b
}

// scalarField is a public scalar field at an offset.
func scalarField(name string, param cxx.Param, offset int64) cxx.Field {
	return cxx.Field{Name: name, Type: param, Access: cxx.AccessPublic, OffsetBytes: offset}
}

// TestMirrorableRecordRejections covers every reason a record is not mirrored
// and takes the opaque path instead: a base the flattening cannot reproduce, an
// array Go cannot express, and a nested record that is not mirrorable itself.
func TestMirrorableRecordRejections(t *testing.T) {
	b := emptyBuilder()
	intField := scalarField("value", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}, 0)
	pod := cxx.Record{QualifiedName: "t::Pod", SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true, Fields: []cxx.Field{intField}}
	require.True(t, b.mirrorable(pod))

	virtualBase := pod
	virtualBase.QualifiedName = "t::VirtualBase"
	virtualBase.Bases = []cxx.Base{{QualifiedName: "t::Base", IsVirtual: true}}
	assert.False(t, b.mirrorable(virtualBase), "a virtual base's offset is not part of the layout")

	shiftedBase := pod
	shiftedBase.QualifiedName = "t::ShiftedBase"
	shiftedBase.Bases = []cxx.Base{{QualifiedName: "t::Base", OffsetBytes: 4}}
	assert.False(t, b.mirrorable(shiftedBase), "a base that does not start the record cannot be flattened")

	for _, tc := range []struct {
		name  string
		field cxx.Field
	}{
		{"array without a length", scalarField("a", cxx.Param{Kind: cxx.TypeArray}, 0)},
		{"array without an element", scalarField("a", cxx.Param{Kind: cxx.TypeArray, ArraySize: 2}, 0)},
		{"array of a record", scalarField("a", cxx.Param{Kind: cxx.TypeArray, ArraySize: 2, Element: &cxx.Param{Kind: cxx.TypeRecord, RecordName: "t::Pod"}}, 0)},
		{"pointer", scalarField("p", cxx.Param{Kind: cxx.TypePointer, SizeBytes: 8}, 0)},
		{"nested record the model does not know", scalarField("r", cxx.Param{Kind: cxx.TypeRecord, RecordName: "t::Missing"}, 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := cxx.Record{QualifiedName: "t::Rejected", SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true,
				Fields: []cxx.Field{tc.field}}
			assert.False(t, b.mirrorable(rec))
		})
	}

	// An array of scalars is a Go array, and the size check proves the layout.
	array := cxx.Record{QualifiedName: "t::Array", SizeBytes: 16, AlignBytes: 8, LayoutKnown: true, IsPOD: true,
		Fields: []cxx.Field{scalarField("values", cxx.Param{Kind: cxx.TypeArray, ArraySize: 2,
			Element: &cxx.Param{Kind: cxx.TypeFloat, ClangKind: "Double", SizeBytes: 8}}, 0)}}
	require.True(t, b.mirrorable(array))
}

// TestNaturalLayoutRejectsPadding covers the records whose C++ layout is not the
// one Go would give the struct: a field the width table cannot size, and a field
// at an offset the natural rule would not choose.
func TestNaturalLayoutRejectsPadding(t *testing.T) {
	b := emptyBuilder()
	unsized := cxx.Record{QualifiedName: "t::Unsized", SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true,
		Fields: []cxx.Field{scalarField("value", cxx.Param{Kind: cxx.TypeOther, Spelling: "t::Unknown"}, 0)}}
	assert.False(t, b.naturalLayout(unsized), "a field the width table cannot size has no natural layout")

	natural := cxx.Record{QualifiedName: "t::Natural", SizeBytes: 8, AlignBytes: 4, LayoutKnown: true, IsPOD: true,
		Fields: []cxx.Field{
			scalarField("a", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}, 0),
			scalarField("b", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}, 4),
		}}
	assert.True(t, b.naturalLayout(natural), "two four-byte fields at 0 and 4 are the natural layout")

	// A record whose size is not a multiple of its alignment has padding Go
	// would not produce.
	padded := natural
	padded.SizeBytes = 12
	assert.False(t, b.naturalLayout(padded))

	misaligned := natural
	misaligned.QualifiedName = "t::Misaligned"
	misaligned.Fields = []cxx.Field{
		scalarField("a", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}, 0),
		scalarField("b", cxx.Param{Kind: cxx.TypeFloat, ClangKind: "Double", SizeBytes: 8}, 4),
	}
	assert.False(t, b.naturalLayout(misaligned), "a double at offset 4 is a packed member")
}

// TestFieldWidthAndMirrorType covers the two mappings a mirrored field goes
// through: its width for the layout proof, and its Go type.
func TestFieldWidthAndMirrorType(t *testing.T) {
	b := emptyBuilder(cxx.Record{QualifiedName: "t::Inner", SizeBytes: 4, AlignBytes: 4, LayoutKnown: true, IsPOD: true})

	for _, tc := range []struct {
		name  string
		field cxx.Field
		size  int64
		align int64
	}{
		{"bool", scalarField("b", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1}, 0), 1, 1},
		{"array of a scalar", scalarField("a", cxx.Param{Kind: cxx.TypeArray, ArraySize: 3,
			Element: &cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}}, 0), 12, 4},
		{"array without an element", scalarField("a", cxx.Param{Kind: cxx.TypeArray}, 0), -1, 1},
		{"array of an unsized element", scalarField("a", cxx.Param{Kind: cxx.TypeArray, ArraySize: 2,
			Element: &cxx.Param{Kind: cxx.TypeInteger}}, 0), -1, 1},
		{"nested record", scalarField("r", cxx.Param{Kind: cxx.TypeRecord, RecordName: "t::Inner"}, 0), 4, 4},
		{"unknown record", scalarField("r", cxx.Param{Kind: cxx.TypeRecord, RecordName: "t::Missing"}, 0), -1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			size, align := b.fieldWidth(tc.field)
			assert.Equal(t, tc.size, size)
			assert.Equal(t, tc.align, align)
		})
	}

	mirrored := b.mirrorFieldType(cxx.Param{Kind: cxx.TypeArray, ArraySize: 2,
		Element: &cxx.Param{Kind: cxx.TypeFloat, ClangKind: "Double", SizeBytes: 8}})
	assert.Equal(t, "[2]float64", mirrored.Go)
	require.NotNil(t, mirrored.Elem)
	assert.Equal(t, "float64", mirrored.Elem.Go)

	assert.Equal(t, "bool", b.mirrorFieldType(cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Bool", SizeBytes: 1}).Go)
}

// TestStdTypeNameAliases covers the typedef a library writes for a standard type:
// cv::String is std::string, and the canonical record name is the only place the
// fact survives. A pointer to one is not the same question.
func TestStdTypeNameAliases(t *testing.T) {
	alias := cxx.Param{
		Kind:       cxx.TypeRecord,
		Spelling:   "String",
		ClangKind:  "Record",
		RecordName: "std::__cxx11::basic_string<char, std::char_traits<char>, std::allocator<char>>",
		SizeBytes:  32,
	}
	name, ok := stdTypeName(alias)
	require.True(t, ok)
	assert.Equal(t, "std::string", name)

	reference := cxx.Param{Kind: cxx.TypePointer, Spelling: "const String &", ClangKind: "LValueReference",
		SizeBytes: 8, Pointee: &alias}
	name, ok = stdTypeName(reference)
	require.True(t, ok)
	assert.Equal(t, "std::string", name)

	vector := cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<uchar>", ClangKind: "Record",
		RecordName: "std::vector<unsigned char, std::allocator<unsigned char>>"}
	name, ok = stdTypeName(vector)
	require.True(t, ok)
	assert.Equal(t, "std::vector<unsigned char>", name)

	scalar := cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::size_t", ClangKind: "ULong"}
	_, ok = stdTypeName(scalar)
	assert.False(t, ok, "a scalar alias is not a standard-library value the table crosses")

	plain := cxx.Param{Kind: cxx.TypeRecord, Spelling: "t::Value", ClangKind: "Record", RecordName: "t::Value"}
	_, ok = stdTypeName(plain)
	assert.False(t, ok)
}

// TestCanonicalStdNameShapes covers the canonical spellings the table normalises
// and the one it passes through: a vector's allocator argument is dropped, a
// three-argument vector is not a vector the table knows.
func TestCanonicalStdNameShapes(t *testing.T) {
	for _, tc := range []struct {
		record string
		want   string
	}{
		{"std::__cxx11::basic_string<char, std::char_traits<char>, std::allocator<char>>", "std::string"},
		{"std::basic_string_view<char>", "std::string_view"},
		{"std::vector<int, std::allocator<int>>", "std::vector<int>"},
		{"std::vector<int>", "std::vector<int>"},
		{"std::vector<int, A, B>", "std::vector<int, A, B>"},
		{"std::optional<int>", "std::optional<int>"},
		{"std::array<int, 4>", "std::array<int, 4>"},
		{"std::unique_ptr<int>", "std::unique_ptr<int>"},
	} {
		got, ok := canonicalStdName(tc.record)
		require.True(t, ok, tc.record)
		assert.Equal(t, tc.want, got, tc.record)
	}
	_, ok := canonicalStdName("t::Value")
	assert.False(t, ok)
}

// TestMapPointerVoidConst covers the two spellings of an untyped pointer: the
// caller's buffer is writable, and a library's constant blob is not.
func TestMapPointerVoidConst(t *testing.T) {
	m := testMapper()
	writable, err := m.mapPointer(pointerParam(cxx.Param{Kind: cxx.TypeVoid, Spelling: "void", ClangKind: "Void"}, "void *", "Pointer"))
	require.NoError(t, err)
	assert.Equal(t, goPointer, writable.Kind)
	assert.Equal(t, "unsafe.Pointer", writable.Go)
	assert.Equal(t, "void *", writable.C)

	constant, err := m.mapPointer(pointerParam(
		cxx.Param{Kind: cxx.TypeVoid, Spelling: "const void", ClangKind: "Void", IsConst: true}, "const void *", "Pointer"))
	require.NoError(t, err)
	assert.Equal(t, "unsafe.Pointer", constant.Go)
	assert.Equal(t, "const void *", constant.C)
}

// TestSkippedWithoutAMap covers the collector's guard: a builder that does not
// collect skips (a unit-level model, not a run) is not a crash.
func TestSkippedWithoutAMap(t *testing.T) {
	b := &builder{}
	b.skipped(cxx.Symbol{QualifiedName: "t::T::~T"}, "not-needed", "reason")
	assert.Nil(t, b.skipReasons)
}

// TestConvertUnsafePointerLocal covers the out-pointer conversion of an untyped
// pointer result: it is already Go's untyped pointer, so there is no type to
// cast it to.
func TestConvertUnsafePointerLocal(t *testing.T) {
	assert.Equal(t, "unsafe.Pointer(cppgoResult)", convertFromLocal(&goType{Kind: goPointer, Go: "unsafe.Pointer"}, "cppgoResult"))
	assert.Equal(t, "(*uint8)(cppgoResult)", convertFromLocal(&goType{Kind: goPointer, Go: "*uint8"}, "cppgoResult"))
	assert.Equal(t, "cppgoResult != 0", convertFromLocal(&goType{Kind: goBool, Go: "bool"}, "cppgoResult"))
}

// TestTemplateEntrySelected covers the two ways a manifest entry selects a
// template pattern, and the answer when it selects nothing.
func TestTemplateEntrySelected(t *testing.T) {
	b := emptyBuilder()
	b.cfg.Bind = []string{"t::Box", "t::Fixed<double>"}
	assert.True(t, b.templateEntrySelected(cxx.Symbol{QualifiedName: "t::Box", IsTemplate: true}))
	assert.True(t, b.templateEntrySelected(cxx.Symbol{QualifiedName: "t::Box::get", Parent: "t::Box", IsTemplate: true}),
		"naming the record selects its member templates")
	assert.True(t, b.templateEntrySelected(cxx.Symbol{QualifiedName: "t::Fixed", IsTemplate: true}),
		"a specialization entry names the pattern it is built from")
	assert.False(t, b.templateEntrySelected(cxx.Symbol{QualifiedName: "t::Other", IsTemplate: true}))

	all := emptyBuilder()
	assert.True(t, all.templateEntrySelected(cxx.Symbol{QualifiedName: "t::Anything", IsTemplate: true}),
		"with no -bind every template is selected")
}

// TestTemplateSelectedNamesTheSpecialization covers the record form of the same
// question: a concrete type named with -instantiate is part of the API, and the
// generic pattern behind it is not.
func TestTemplateSelectedNamesTheSpecialization(t *testing.T) {
	b := emptyBuilder()
	b.cfg.Instantiate = []string{"t::Fixed<int>", "t::Box"}
	b.instantiateMatched = make([]bool, len(b.cfg.Instantiate))
	b.bindMatched = make([]bool, 1)
	assert.True(t, b.templateSelected("t::Box"))
	assert.True(t, b.templateSelected("t::Fixed"), "a specialization entry selects the pattern it is built from")
	assert.False(t, b.templateSelected("t::Other"))
	b.cfg.Bind = []string{"t::Fixed"}
	b.bindMatched = make([]bool, 1)
	assert.True(t, b.templateSelected("t::Fixed"), "-bind names the pattern")
	assert.True(t, b.bindMatched[0], "the entry that selected the pattern is marked matched")
}

// TestCoverageEntriesClassifications covers the report's entry builder with a
// model built by hand: a declaration with no location, a refusal without a
// recorded reason, a skip, and a declaration the manifest did not name.
func TestCoverageEntriesClassifications(t *testing.T) {
	b := emptyBuilder()
	refused := cxx.Symbol{QualifiedName: "t::refused", DeclFile: "t/t.hpp"}
	selected := cxx.Symbol{QualifiedName: "t::selected", DeclFile: "t/t.hpp"}
	skipped := cxx.Symbol{QualifiedName: "t::skipped", DeclFile: "t/t.hpp"}
	b.symbols = []cxx.Symbol{
		{QualifiedName: "cv::segmentation::Thing::apply", Parent: "cv::segmentation::Thing"},
		refused,
		selected,
		skipped,
		{QualifiedName: "t::unselected", DeclFile: "t/t.hpp"},
	}
	b.selectedSymbols[symbolKey(refused)] = true
	b.selectedSymbols[symbolKey(selected)] = true
	b.refusalReasons[symbolKey(selected)] = "a void pointer has no Go value"
	b.skipReasons[symbolKey(skipped)] = "the member is deleted"
	b.skipClasses[symbolKey(skipped)] = "not-needed"

	entries := b.coverageEntries()
	require.Len(t, entries, 5)

	find := func(prefix string) coverageEntry {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Declaration, prefix) {
				return entry
			}
		}
		t.Fatalf("the report has no entry for %s: %v", prefix, entries)
		return coverageEntry{}
	}
	assert.Equal(t, "segmentation", find("cv::segmentation::Thing::apply").Module,
		"a declaration with no location is attributed to its namespace")
	refusedEntry := find("t::refused")
	assert.Equal(t, "refused", refusedEntry.Kind)
	assert.Equal(t, "not lowered: the declaration was selected and produced no binding", refusedEntry.Reason)
	assert.Equal(t, "other", refusedEntry.Class)
	selectedEntry := find("t::selected")
	assert.Equal(t, "refused", selectedEntry.Kind)
	assert.Equal(t, "a void pointer has no Go value", selectedEntry.Reason)
	assert.Equal(t, "untyped-pointer", selectedEntry.Class)
	skippedEntry := find("t::skipped")
	assert.Equal(t, "skipped", skippedEntry.Kind)
	assert.Equal(t, "not-needed", skippedEntry.Class)
	assert.Equal(t, "the member is deleted", skippedEntry.Reason)
	assert.Equal(t, "not-selected", find("t::unselected").Kind)
}

// TestStripDocLineShapes covers the comment shapes the doc normaliser reads: a
// block comment with and without a doc marker, a line comment, and the
// continuation star.
func TestStripDocLineShapes(t *testing.T) {
	for _, tc := range []struct {
		line string
		want string
	}{
		{"/** A doc line. */", "A doc line."},
		{"/*! A Qt doc line. */", "A Qt doc line."},
		{"/* plain block */", "plain block"},
		{"/// three slashes", "three slashes"},
		{"//! qt line", "qt line"},
		{"// two slashes", "two slashes"},
		{"* starred", "starred"},
		{"*no-space", "no-space"},
		{"*", ""},
		{"*/", ""},
		{"", ""},
	} {
		assert.Equal(t, tc.want, stripDocLine(tc.line), tc.line)
	}
}

// TestSplitRecordInstantiationEmptyArgument covers the one argument list the
// record parser refuses: an empty component between two commas.
func TestSplitRecordInstantiationEmptyArgument(t *testing.T) {
	_, _, ok := splitRecordInstantiation("t::Fixed<int,,double>")
	assert.False(t, ok)
	_, _, ok = splitRecordInstantiation("t::Fixed<>")
	assert.False(t, ok)
}

// TestManifestRequestErrors covers the manifest mistakes the two request paths
// report: an entry that names no instantiation at all, and an entry whose type
// argument is not a scalar the model can build.
func TestManifestRequestErrors(t *testing.T) {
	cfg := fixtureConfig("instantiate", "instantiate.hpp")
	cfg.Bind = []string{"ins::Box"}
	cfg.Instantiate = []string{"ins::Box"}
	_, err := Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no instantiation")

	cfg.Instantiate = []string{"ins::Box::get<>()"}
	_, err = Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "type argument list is empty")
}

// TestGenerateWritesTheCoverageReport covers the file the manifest asks for: the
// report is one of the generated outputs, so it is written beside the package and
// a check compares it.
func TestGenerateWritesTheCoverageReport(t *testing.T) {
	dir := t.TempDir()
	cfg := fixtureConfig("types", "types.hpp")
	cfg.Out = dir
	cfg.Coverage = filepath.Join(dir, "coverage.txt")
	require.NoError(t, Generate(cfg))

	report, err := os.ReadFile(filepath.Join(dir, "coverage.txt"))
	require.NoError(t, err)
	assert.Contains(t, string(report), "# cppgo coverage: package types")

	// The same configuration in check mode is clean, because the two runs write
	// the same bytes; a report edited by hand is what fails.
	cfg.Check = true
	require.NoError(t, Generate(cfg))
	stale := append(report, []byte("# edited\n")...)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "coverage.txt"), stale, 0o644))
	err = Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coverage.txt")
}

// TestGenerateReportsAScratchFailure covers the one directory the generator
// cannot create: when TMPDIR names a missing directory, the run fails before it
// writes anything.
func TestGenerateReportsAScratchFailure(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	t.Setenv("TMPDIR", missing)
	for _, variable := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(variable, missing)
	}
	dir := t.TempDir()
	cfg := fixtureConfig("types", "types.hpp")
	cfg.Out = dir
	err := Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shim build directory")
	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Empty(t, entries, "a generation that cannot build its shim writes nothing")
}

// TestScanReportsAMissingPackage covers the scan's first failure: a pattern that
// matches no Go package is reported with the pattern, not an empty instantiation
// list.
func TestScanReportsAMissingPackage(t *testing.T) {
	_, err := scanInstantiations(".", []string{"./does-not-exist"}, ".", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

// TestAssignNamesReportsTheOperatorFailure covers the naming error as it leaves
// assignNames: an operator the table does not name stops the run with the
// declaration and the rename that resolves it.
func TestAssignNamesReportsTheOperatorFailure(t *testing.T) {
	b := emptyBuilder()
	b.bindings = []*binding{{sym: cxx.Symbol{QualifiedName: "t::T::operator->*", IsOperator: true, Operator: "->*"}}}
	err := b.assignNames()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside the naming table")
}

// TestResolveCollisionsOrdering covers the two comparisons the collision pass
// sorts a group by: a declared member before the generated copy constructor, and
// the signature when neither the clone flag nor the constness decides it.
func TestResolveCollisionsOrdering(t *testing.T) {
	b := emptyBuilder()
	clone := &binding{sym: cxx.Symbol{QualifiedName: "t::T::T"}, IsClone: true, GoName: "Clone"}
	method := &binding{sym: cxx.Symbol{QualifiedName: "t::T::clone"}, GoName: "Clone"}
	alpha := &binding{sym: cxx.Symbol{QualifiedName: "t::T::a"}, GoName: "Same"}
	beta := &binding{sym: cxx.Symbol{QualifiedName: "t::T::b"}, GoName: "Same"}
	b.bindings = []*binding{clone, method, alpha, beta}
	b.resolveCollisions()

	assert.Equal(t, "Clone", method.GoName, "the declared member keeps its name")
	assert.Equal(t, "Copy", clone.GoName)
	assert.Equal(t, "Same", alpha.GoName, "the first of the group keeps the name")
	assert.NotEqual(t, "Same", beta.GoName, "the second is told apart by its signature")
}

// TestCollisionNameFallsBackToTheHash covers the last resort of the naming rule:
// a non-const member whose parameter suffix and Const form are both taken takes
// a short hash of its declaration.
func TestCollisionNameFallsBackToTheHash(t *testing.T) {
	b := emptyBuilder()
	binding := &binding{sym: cxx.Symbol{QualifiedName: "t::T::m"}, Params: []*goParam{{Type: &goType{Go: "int32"}}}}
	binding.GoName = "M"
	taken := map[string]bool{"M": true, "MInt32": true}
	name := b.collisionName(binding, taken)
	assert.True(t, strings.HasPrefix(name, "MInt32"), name)
	assert.Len(t, name, len("MInt32")+6, "the fallback appends a six-character hash")
}

// TestTemplateSuffixWithoutAScalar covers a template argument the scalar table
// does not know: the suffix is made from the spelling, so the name is still
// readable and deterministic.
func TestTemplateSuffixWithoutAScalar(t *testing.T) {
	b := emptyBuilder()
	name := b.templateSuffix(&binding{templateArgs: []string{"t::Thing"}})
	assert.Equal(t, "TThing", name)
}

// TestStdTypeNameThroughAnAliasTheTableKeeps covers the second half of the alias
// rule: a canonical name the model reads from the declaration rather than from
// the spelling (std::optional) is resolved through the record name too.
func TestStdTypeNameThroughAnAliasTheTableKeeps(t *testing.T) {
	alias := cxx.Param{Kind: cxx.TypeRecord, Spelling: "Opt", ClangKind: "Record",
		RecordName: "std::optional<int>", SizeBytes: 8}
	name, ok := stdTypeName(alias)
	require.True(t, ok)
	assert.Equal(t, "std::optional<int>", name)

	pointer := cxx.Param{Kind: cxx.TypePointer, Spelling: "const Opt &", ClangKind: "LValueReference",
		SizeBytes: 8, Pointee: &alias}
	name, ok = stdTypeName(pointer)
	require.True(t, ok)
	assert.Equal(t, "std::optional<int>", name)
}

// TestScanOfAnEmptyDirectory covers the pattern that matches no package at all:
// the scan reports it instead of returning an empty instantiation list.
func TestScanOfAnEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module empty\n\ngo 1.25.0\n"), 0o644))
	_, err := scanInstantiations(dir, []string{"./..."}, dir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "matches no Go package")
}

// TestBuildRecordsKeepsTheCompleteSpecialization covers the two sightings of one
// specialization: the partial record libclang reports from a type reference has
// no layout, and the record built from the pattern does. The complete one wins
// whichever order they arrive in, and the order list keeps one entry.
func TestBuildRecordsKeepsTheCompleteSpecialization(t *testing.T) {
	partial := cxx.Record{QualifiedName: "t::Fixed<int>", IsTemplateSpecialization: true, SizeBytes: -1, AlignBytes: -1}
	complete := cxx.Record{QualifiedName: "t::Fixed<int>", IsTemplateSpecialization: true, SizeBytes: 4, AlignBytes: 4,
		LayoutKnown: true, IsPOD: true, Fields: []cxx.Field{scalarField("value", cxx.Param{Kind: cxx.TypeInteger, ClangKind: "Int", SizeBytes: 4}, 0)}}

	for _, order := range [][]cxx.Record{{partial, complete}, {complete, partial}} {
		b := emptyBuilder()
		b.rawRecords = order
		b.instantiated = map[string]bool{"t::Fixed<int>": true}
		b.recordGoNames = map[string]string{"t::Fixed<int>": "FixedInt"}
		b.buildRecords()
		require.Len(t, b.order, 1, "the order list keeps one entry")
		model, ok := b.records["t::Fixed<int>"]
		require.True(t, ok)
		assert.Equal(t, int64(4), model.Size, "the complete record wins")
		assert.True(t, model.Mirrored)
	}
}

// TestAliasReadsALibraryTemplateAsTheStandardOne covers -alias at the type
// map: OpenCV's cv::Ptr<T> is std::shared_ptr<T>, and the model reads the
// library's spelling as the standard shape. The declaration then takes the
// result branch it belongs to — a carried result, not an unknown record.
func TestAliasReadsALibraryTemplateAsTheStandardOne(t *testing.T) {
	aliased := cxx.Param{Kind: cxx.TypeRecord, ClangKind: "Record", Spelling: "Ptr<ORB>", RecordName: "cv::Ptr<cv::ORB>"}
	_, ok := stdTypeNameWith(aliased, nil)
	assert.False(t, ok, "without the alias the spelling is a record the table does not know")

	name, ok := stdTypeNameWith(aliased, map[string]string{"cv::Ptr": "std::shared_ptr"})
	require.True(t, ok)
	assert.Equal(t, "std::shared_ptr<ORB>", name)
}

// TestAliasEntryGrammar covers the malformed forms of -alias.
func TestAliasEntryGrammar(t *testing.T) {
	for _, bad := range []string{"cv::Ptr", "=std::shared_ptr", "cv::Ptr="} {
		_, err := Render(Config{Headers: []string{"x"}, Package: "x", Alias: []string{bad}})
		require.Error(t, err, bad)
		assert.Contains(t, err.Error(), "-alias")
	}
}

// TestExpandAlias covers the two spellings an alias is written with: the
// qualified one a manifest writes, and the unqualified one a declaration inside
// the alias's own namespace uses.
func TestExpandAlias(t *testing.T) {
	aliases := map[string]string{"cv::Ptr": "std::shared_ptr"}
	assert.Equal(t, "std::shared_ptr<cv::ORB>", expandAlias("cv::Ptr<cv::ORB>", aliases))
	assert.Equal(t, "std::shared_ptr<ORB>", expandAlias("Ptr<ORB>", aliases))
	assert.Equal(t, "std::vector<int>", expandAlias("std::vector<int>", aliases))
	assert.Equal(t, "Ptr", expandAlias("Ptr", aliases), "no bracket, no template")
}

// TestSymbolKeyCollapsesTwoManglings covers the identity a constructor has: it
// is mangled twice (C1 and C2: complete and base object), and one declaration
// is one binding, not two thunks that share a trampoline name.
func TestSymbolKeyCollapsesTwoManglings(t *testing.T) {
	complete := cxx.Symbol{QualifiedName: "ns::T::T", Kind: cxx.Constructor, Mangled: "_ZN2ns1TC1Ev",
		Params: []cxx.Param{{Kind: cxx.TypeInteger, Spelling: "int", SizeBytes: 4}}}
	base := complete
	base.Mangled = "_ZN2ns1TC2Ev"
	assert.Equal(t, symbolKey(complete), symbolKey(base))

	overload := complete
	overload.Params = []cxx.Param{{Kind: cxx.TypeFloat, Spelling: "double", SizeBytes: 8}}
	assert.NotEqual(t, symbolKey(complete), symbolKey(overload), "two signatures are two declarations")
}

// TestLibrariesAreEveryLinkedObject covers -library repeated: the shim links
// every shared object a slice crosses modules through, and the first is the one
// the package loads.
func TestLibrariesAreEveryLinkedObject(t *testing.T) {
	cfg := Config{Library: "liba.so", Libraries: []string{"libb.so", "", "liba.so", "libc.so"}}
	assert.Equal(t, []string{"liba.so", "libb.so", "libc.so"}, cfg.libraries())

	empty := Config{}
	assert.Empty(t, empty.libraries())
}

// TestShimArgCastsACharBuffer covers a one-byte integer pointer: the wire names
// it with the fixed-width type the thunk prototype uses, and the declaration
// says char, so the call casts.
func TestShimArgCastsACharBuffer(t *testing.T) {
	m := &packageModel{cfg: Config{}}
	expr, err := m.shimArg(cxx.Param{Kind: cxx.TypePointer, Ptr: true, Spelling: "const char *",
		Pointee: &cxx.Param{Kind: cxx.TypeInteger, Spelling: "char", SizeBytes: 1}}, "data")
	require.NoError(t, err)
	assert.Equal(t, "reinterpret_cast<const char *>(data)", expr)

	expr, err = m.shimArg(cxx.Param{Kind: cxx.TypePointer, Ptr: true, Spelling: "const int *",
		Pointee: &cxx.Param{Kind: cxx.TypeInteger, Spelling: "int", SizeBytes: 4}}, "data")
	require.NoError(t, err)
	assert.Equal(t, "data", expr)
}

// TestProtectedNestedRecordIsReported covers a nested type of a class that is
// not public inside it: no thunk can spell the name, so the type is reported
// and the rest of the class binds.
func TestProtectedNestedRecordIsReported(t *testing.T) {
	header := filepath.Join(t.TempDir(), "nested.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace ns {
class Owner {
public:
    Owner() noexcept;
    int value() const noexcept;
protected:
    class Hidden {
    public:
        Hidden() noexcept;
    };
};
}
`), 0o644))
	rendered, err := RenderResult(Config{Headers: []string{header}, Package: "ns", Library: "libns", Out: "."})
	require.NoError(t, err)
	assert.Contains(t, joinWarnings(rendered.Warnings), "ns::Owner::Hidden is not part of the generated API")
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func NewOwner()", "the class itself still binds")
	assert.NotContains(t, goFile, "type Hidden", "the protected nested type is not emitted")
}

// TestProxyOfANonContainerIsTheInvocationsMistake covers the second -proxy
// mistake: the declaration's parameter is adapted to a type the model does not
// read as a container.
func TestProxyOfANonContainerIsTheInvocationsMistake(t *testing.T) {
	header := filepath.Join(t.TempDir(), "proxy2.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace ns {
class Proxy {
public:
    Proxy() noexcept;
};
void sink(const Proxy &p) noexcept;
}
`), 0o644))
	_, err := Render(Config{
		Headers: []string{header}, Package: "ns", Library: "libns", Out: ".",
		Proxy: []string{"ns::sink@0=int"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not map as a container")
}

// TestManifestTypeAdaptationsAreParsedDuringARun covers the two manifest flags
// that adapt the type map, at the point a run reads them: -alias builds the
// replacement table before the header is loaded, and a malformed -proxy entry
// stops the run with its own message.
func TestManifestTypeAdaptationsAreParsedDuringARun(t *testing.T) {
	cfg := fixtureConfig("containers", "containers.hpp")
	cfg.Alias = []string{" cv::Ptr = std::shared_ptr "}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err, "a well-formed alias does not disturb a slice that does not use it")
	assert.NotEmpty(t, rendered.Outputs)

	cfg.Proxy = []string{"containers::total@0"}
	_, err = Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-proxy")
	assert.Contains(t, err.Error(), "write it as <declaration>@<index>=<C++ type>")
}

// TestTemplateSuffixNamesEveryArgumentShape covers the suffix a concrete
// instantiation's Go name carries: a record's Go name, a scalar's Go type, and
// the spelling itself for the two shapes that have neither — an argument the
// model cannot map, and one that is not a scalar spelling at all.
func TestTemplateSuffixNamesEveryArgumentShape(t *testing.T) {
	point := &recordModel{GoName: "PointInt", CxxName: "cv::Point_<int>"}
	b := &builder{mapper: &mapper{records: map[string]*recordModel{"cv::Point_<int>": point}}}

	assert.Equal(t, "",
		b.templateSuffix(&binding{}), "a declaration with no template arguments has no suffix")
	assert.Equal(t, "PointInt",
		b.templateSuffix(&binding{templateArgs: []string{"cv::Point_<int>"}}),
		"a record argument is named after the Go type")
	assert.Equal(t, "Float64",
		b.templateSuffix(&binding{templateArgs: []string{"double"}}),
		"a scalar argument is named after its Go type")
	// long double is a scalar spelling the type map refuses, so the suffix is
	// the spelling rather than a Go type the package does not have.
	assert.Equal(t, "LongDouble",
		b.templateSuffix(&binding{templateArgs: []string{"long double"}}))
	assert.Equal(t, "SomeSpelling",
		b.templateSuffix(&binding{templateArgs: []string{"some_spelling"}}),
		"a spelling that is not a scalar type is exported as written")
}
