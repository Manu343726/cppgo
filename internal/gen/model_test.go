package gen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestMathxModel asserts the model the emitter builds for the canonical
// fixture: which records are mirrored, which bindings exist, and which carrier
// each one got and why.
func TestMathxModel(t *testing.T) {
	model, err := build(mathxConfig())
	require.NoError(t, err)

	require.Len(t, model.records, 2)
	point := findRecord(t, model, "Point")
	assert.True(t, point.Mirrored)
	assert.Equal(t, int64(16), point.Size)
	assert.Equal(t, int64(8), point.Align)
	require.Len(t, point.Fields, 2)
	assert.Equal(t, "X", point.Fields[0].GoName)
	assert.Equal(t, int64(0), point.Fields[0].Offset)
	assert.Equal(t, "Y", point.Fields[1].GoName)
	assert.Equal(t, int64(8), point.Fields[1].Offset)
	assert.True(t, point.DefaultConstructible)
	assert.True(t, point.CopyConstructible)
	assert.Nil(t, point.Dtor)

	vector := findRecord(t, model, "Vector")
	assert.False(t, vector.Mirrored)
	assert.Equal(t, int64(16), vector.Size)
	assert.Equal(t, int64(8), vector.Align)
	require.Len(t, vector.Ctors, 1)
	require.NotNil(t, vector.Clone)
	require.NotNil(t, vector.Dtor)
	assert.Len(t, vector.Methods, 4)

	require.Len(t, model.bindings, 9)

	newVector := findBinding(t, model, "NewVector")
	assert.Equal(t, thunk, newVector.Carrier)
	assert.True(t, newVector.Status)
	assert.True(t, newVector.ErrorResult)
	assert.Equal(t, "not noexcept: an exception would unwind into Go", newVector.Reason)
	require.Len(t, newVector.Wire, 3)
	assert.Equal(t, "this", newVector.Wire[0].label)
	assert.Equal(t, "float64", newVector.Wire[1].slotType)

	clone := findBinding(t, model, "Clone")
	assert.True(t, clone.IsClone)
	assert.Empty(t, clone.Params, "the copy source is the receiver, not a Go parameter")
	require.Len(t, clone.Wire, 2)

	closeBinding := findBinding(t, model, "Close")
	assert.True(t, closeBinding.IsDestructor)
	assert.True(t, closeBinding.ErrorResult)

	norm := findBinding(t, model, "Norm")
	assert.Equal(t, thunk, norm.Carrier)
	assert.True(t, norm.Status)
	require.NotNil(t, norm.Out)
	assert.Equal(t, "float64", norm.OutLocal)
	require.Len(t, norm.Wire, 2)
	assert.Equal(t, "out", norm.Wire[1].label)

	scale := findBinding(t, model, "Scale")
	assert.Equal(t, direct, scale.Carrier)
	assert.False(t, scale.Status)
	assert.False(t, scale.ErrorResult)
	assert.Equal(t, "_ZN5mathx6Vector5scaleEd", scale.Symbol)
	assert.Empty(t, scale.Thunk)
	require.Len(t, scale.Slots, 3)
	assert.Equal(t, 24, scale.Frame)
	assert.Equal(t, "DI", scale.Slots[1].reg)
	assert.Equal(t, "X0", scale.Slots[2].reg)

	dot := findBinding(t, model, "Dot")
	require.Len(t, dot.Params, 1)
	assert.Equal(t, "ys", dot.Params[0].Name)
	assert.True(t, dot.Params[0].Slice)
	assert.Equal(t, "[]float64", dot.Params[0].Type.Go)

	saxpy := findBinding(t, model, "Saxpy")
	assert.Equal(t, thunk, saxpy.Carrier)
	require.Len(t, saxpy.Params, 3)
	assert.Equal(t, []string{"a", "x", "y"}, []string{saxpy.Params[0].Name, saxpy.Params[1].Name, saxpy.Params[2].Name})
	require.Len(t, saxpy.Params[1].Group, 2, "x and y share the one length parameter")
	require.Len(t, saxpy.Wire, 4)
	assert.Equal(t, "uint64", saxpy.Wire[3].slotType)

	dims := findBinding(t, model, "VectorDims")
	assert.True(t, dims.IsStatic)
	assert.Equal(t, thunk, dims.Carrier)
	require.NotNil(t, dims.Result)
	assert.Equal(t, "int32", dims.Result.Go)

	assert.Empty(t, model.verified)
	assert.Len(t, model.report(), 9)
	assert.Contains(t, strings.Join(model.report(), "\n"), "mathx::Vector::scale(double) noexcept")
}

// TestTypeFixtureModel exercises the wider type map and the record shapes.
func TestTypeFixtureModel(t *testing.T) {
	model, err := build(fixtureConfig("types", "types.hpp"))
	require.NoError(t, err)

	pod := findRecord(t, model, "Pod")
	assert.True(t, pod.Mirrored)
	assert.Len(t, pod.Fields, 5)

	empty := findRecord(t, model, "Empty")
	assert.False(t, empty.Mirrored, "an empty C++ struct is one byte, a Go struct is zero")
	assert.Nil(t, empty.Dtor, "an empty record declares no destructor")

	noDtor := findRecord(t, model, "NoDtor")
	assert.False(t, noDtor.Mirrored)
	assert.Nil(t, noDtor.Dtor)
	require.Len(t, noDtor.Ctors, 1)
	assert.Equal(t, direct, noDtor.Ctors[0].Carrier, "a noexcept constructor is called directly")
	assert.False(t, noDtor.Ctors[0].Status)

	implicit := findRecord(t, model, "Implicit")
	require.Len(t, implicit.Ctors, 1, "a default-constructible record gets New<Type>")
	assert.Equal(t, "types::Implicit::Implicit", implicit.Ctors[0].sym.QualifiedName)

	// Every noexcept scalar function is direct, including the pointer result,
	// the bool result and the slice shapes.
	for _, name := range []string{"Negate", "AddI8", "AddU8", "AddI16", "AddU16", "AddI32", "AddU32", "AddI64", "AddU64", "AddF32", "AddF64", "PointerIdentity", "Count", "AddInto", "Read", "SumPod"} {
		binding := findBinding(t, model, name)
		assert.Equal(t, direct, binding.Carrier, name)
		assert.False(t, binding.ErrorResult, name)
	}
	assert.Equal(t, goBool, findBinding(t, model, "Negate").Result.Kind)
	assert.Equal(t, goPointer, findBinding(t, model, "PointerIdentity").Result.Kind)
	assert.Equal(t, "uint32", findBinding(t, model, "Count").Result.Go)

	// AddInto has two slices sharing one length and no error result: its length
	// mismatch is a panic, which the emitter has to render.
	require.Len(t, findBinding(t, model, "AddInto").Params, 2)
	assert.Len(t, findBinding(t, model, "AddInto").Params[0].Group, 2)
}

// TestOverloadNames checks the suffix rule on constructors, methods and free
// functions.
func TestOverloadNames(t *testing.T) {
	model, err := build(fixtureConfig("overloads", "overloads.hpp"))
	require.NoError(t, err)

	names := map[string]bool{}
	for _, binding := range model.bindings {
		names[binding.GoName] = true
	}
	for _, want := range []string{"NewThing", "NewThingInt32", "Value", "ValueInt32", "Reset", "ResetInt32", "AddInt32Int32", "AddFloat64Float64", "Close"} {
		assert.True(t, names[want], "%s must be generated", want)
	}
	assert.False(t, names["Value_2"])
}

// TestSelectionBindAndExclude exercises the manifest's root selection.
func TestSelectionBindAndExclude(t *testing.T) {
	cases := []struct {
		name     string
		bind     []string
		exclude  []string
		wantFns  []string
		notFns   []string
		wantRecs []string
	}{
		{
			name:     "bind a class",
			bind:     []string{"mathx::Vector"},
			wantFns:  []string{"NewVector", "Norm", "Scale", "VectorDims"},
			notFns:   []string{"Saxpy"},
			wantRecs: []string{"Vector"},
		},
		{
			name:     "bind a free function",
			bind:     []string{"mathx::saxpy"},
			wantFns:  []string{"Saxpy"},
			notFns:   []string{"NewVector", "Norm"},
			wantRecs: nil,
		},
		{
			name:     "bind a member",
			bind:     []string{"mathx::Vector::norm"},
			wantFns:  []string{"Norm"},
			notFns:   []string{"X", "Scale", "Saxpy"},
			wantRecs: []string{"Vector"},
		},
		{
			name:     "bind a record with no members",
			bind:     []string{"mathx::Point"},
			wantFns:  nil,
			wantRecs: []string{"Point"},
		},
		{
			name:     "exclude a function",
			exclude:  []string{"mathx::saxpy"},
			wantFns:  []string{"Norm", "X"},
			notFns:   []string{"Saxpy"},
			wantRecs: []string{"Point", "Vector"},
		},
		{
			name:     "exclude a class",
			exclude:  []string{"mathx::Vector"},
			wantFns:  []string{"Saxpy"},
			notFns:   []string{"NewVector", "Norm"},
			wantRecs: []string{"Point"},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := mathxConfig()
			cfg.Bind = tc.bind
			cfg.Exclude = tc.exclude
			model, err := build(cfg)
			require.NoError(t, err)
			names := map[string]bool{}
			for _, binding := range model.bindings {
				names[binding.GoName] = true
			}
			for _, want := range tc.wantFns {
				assert.True(t, names[want], "%s must be bound", want)
			}
			for _, unwanted := range tc.notFns {
				assert.False(t, names[unwanted], "%s must not be bound", unwanted)
			}
			var records []string
			for _, record := range model.records {
				records = append(records, record.GoName)
			}
			assert.ElementsMatch(t, tc.wantRecs, records)
		})
	}
}

// TestSelectionUnmatchedEntries checks that a typo cannot silently shrink the
// API.
func TestSelectionUnmatchedEntries(t *testing.T) {
	cfg := mathxConfig()
	cfg.Bind = []string{"mathx::Nope"}
	_, err := build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-bind mathx::Nope")

	cfg = mathxConfig()
	cfg.Exclude = []string{"mathx::Nope"}
	_, err = build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-exclude mathx::Nope")
}

// TestTypeClosureKeepsReferredRecords checks that binding only a function that
// takes an opaque record still generates the record's type.
func TestTypeClosureKeepsReferredRecords(t *testing.T) {
	cfg := fixtureConfig("types", "types.hpp")
	cfg.Bind = []string{"types::read"}
	model, err := build(cfg)
	require.NoError(t, err)
	require.Len(t, model.records, 1)
	assert.Equal(t, "Opaque", model.records[0].GoName)
	require.Len(t, model.bindings, 1)
	assert.Equal(t, "Read", model.bindings[0].GoName)
}

// TestRenames checks the manifest's rename rule, by qualified name and by full
// signature.
func TestRenames(t *testing.T) {
	cfg := mathxConfig()
	cfg.Rename = []string{"mathx::Vector::norm=Norm2"}
	model, err := build(cfg)
	require.NoError(t, err)
	assert.NotNil(t, findBinding(t, model, "Norm2"))

	cfg = mathxConfig()
	cfg.Rename = []string{"mathx::Vector::norm() const=Norm3"}
	model, err = build(cfg)
	require.NoError(t, err)
	assert.NotNil(t, findBinding(t, model, "Norm3"))
}

// TestRenameErrors covers every way a rename entry can be wrong.
func TestRenameErrors(t *testing.T) {
	cases := []struct {
		name   string
		rename string
		want   string
	}{
		{"malformed", "mathx::Vector::norm", "must be <qualified signature>=<GoName>"},
		{"empty name", "=Norm", "must be <qualified signature>=<GoName>"},
		{"empty target", "mathx::Vector::norm=", "must be <qualified signature>=<GoName>"},
		{"invalid target", "mathx::Vector::norm=9Norm", "not a valid Go identifier"},
		{"names nothing", "mathx::Nope=Norm", "names nothing"},
		{"ambiguous", "mathx::Vector::Vector=Maker", "names 2 declarations"},
		{"collides", "mathx::Vector::norm=X", "both map to Go name X"},
		{"reserved Open", "mathx::saxpy=Open", "generated entry point"},
		{"reserved Close", "mathx::Vector::norm=Close", "every record's destructor"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := mathxConfig()
			cfg.Rename = []string{tc.rename}
			_, err := build(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestDuplicateRecordNames checks that two records in different namespaces that
// map to one Go type are an error naming the rename that fixes it.
func TestDuplicateRecordNames(t *testing.T) {
	cfg := Config{
		Headers: []string{
			"testdata/duplicate/a.hpp",
			"testdata/duplicate/b.hpp",
		},
		Package: "dup",
		Library: "libdup",
		Out:     ".",
	}
	_, err := build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "two records map to Go type Dup")
}

// TestChooseCarrierReasons unit-tests every carrier reason, including the ones
// a header cannot reach because the result type is rejected first.
func TestChooseCarrierReasons(t *testing.T) {
	b := &builder{}
	cases := []struct {
		name string
		sym  cxx.Symbol
		want carrier
	}{
		{"template", cxx.Symbol{IsTemplate: true}, thunk},
		{"no symbol", cxx.Symbol{}, thunk},
		{"virtual", cxx.Symbol{Mangled: "m", IsNoExcept: true, HasOutOfLineBody: true, IsVirtual: true}, thunk},
		{"throws", cxx.Symbol{Mangled: "m", HasOutOfLineBody: true}, thunk},
		{"inline", cxx.Symbol{Mangled: "m", IsNoExcept: true}, thunk},
		{"sret", cxx.Symbol{Mangled: "m", IsNoExcept: true, HasOutOfLineBody: true, HasSret: true}, thunk},
		{"direct", cxx.Symbol{Mangled: "m", IsNoExcept: true, HasOutOfLineBody: true}, direct},
	}
	for _, tc := range cases {
		binding := &binding{sym: tc.sym}
		b.chooseCarrier(binding)
		assert.Equal(t, tc.want, binding.Carrier, tc.name)
		assert.NotEmpty(t, binding.Reason, tc.name)
	}
}
