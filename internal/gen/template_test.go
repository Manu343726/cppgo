package gen

import (
	"go/token"
	"go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// boxConfig is the two-phase fixture.
func boxConfig() Config { return fixtureConfig("box", "box.hpp") }

// findTemplate returns the template with the given C++ name.
func findTemplate(t *testing.T, model *packageModel, name string) *templateModel {
	t.Helper()
	for _, tmpl := range model.templates {
		if tmpl.CxxName == name {
			return tmpl
		}
	}
	require.FailNowf(t, "template not found", "%s is not in the model", name)
	return nil
}

// findTemplateBinding returns the binding with the given Go name in a pattern.
func findTemplateBinding(t *testing.T, tmpl *templateModel, name string) *binding {
	t.Helper()
	for _, binding := range tmpl.bindings() {
		if binding.GoName == name {
			return binding
		}
	}
	require.FailNowf(t, "binding not found", "%s is not in %s", name, tmpl.CxxName)
	return nil
}

// TestTemplateModel checks the generic model phase one builds: the type
// parameters, the substituted signatures and the member groups.
func TestTemplateModel(t *testing.T) {
	model, err := build(boxConfig())
	require.NoError(t, err)
	require.Len(t, model.templates, 6)
	assert.Empty(t, model.instantiations, "phase one knows no concrete type")

	box := findTemplate(t, model, "box::Box")
	assert.Equal(t, "Box", box.GoName)
	assert.Equal(t, cxx.Class, box.Kind)
	require.Len(t, box.Params, 1)
	assert.Equal(t, templateParam{Name: "T", GoName: "T", Kind: templateParamType}, box.Params[0])
	require.Len(t, box.Ctors, 2)
	assert.Equal(t, "NewBox", box.Ctors[0].GoName, "the constructor with parameters keeps the bare name")
	assert.Equal(t, "NewBoxDefault", box.Ctors[1].GoName)
	require.NotNil(t, box.Dtor)
	assert.Equal(t, "Close", box.Dtor.GoName)
	assert.Len(t, box.Methods, 6)
	require.Len(t, box.Statics, 1)
	assert.Equal(t, "BoxIdentity", box.Statics[0].GoName)
	assert.Len(t, box.bindings(), 10)

	echo := findTemplateBinding(t, box, "Echo")
	require.Len(t, echo.Params, 1)
	assert.Equal(t, goPointer, echo.Params[0].Type.Kind)
	assert.Equal(t, "*T", echo.Params[0].Type.Go)
	require.NotNil(t, echo.Result)
	assert.Equal(t, "*T", echo.Result.Go)

	plain := findTemplate(t, model, "box::Plain")
	assert.Nil(t, plain.Dtor, "Plain declares no destructor, so Close releases storage only")

	deleted := findTemplate(t, model, "box::Deleted")
	assert.Empty(t, deleted.Ctors, "a deleted constructor is not a factory")
	assert.Nil(t, deleted.Dtor, "a deleted destructor is not Close")
	assert.Empty(t, deleted.Methods, "a deleted method has no address to call")
	assert.Empty(t, deleted.Statics, "a deleted static has no address to call")

	abstract := findTemplate(t, model, "box::Abstract")
	assert.Empty(t, abstract.Methods, "a pure virtual has no address to call")
	require.NotNil(t, abstract.Dtor)

	matrix := findTemplate(t, model, "box::Matrix")
	require.Len(t, matrix.Params, 3)
	assert.Equal(t, templateParamType, matrix.Params[0].Kind)
	assert.Equal(t, templateParamDimension, matrix.Params[1].Kind)
	assert.Equal(t, templateParamDimension, matrix.Params[2].Kind)
	assert.True(t, matrix.hasDimensions())
	assert.Equal(t, "T any, R cppgoDimension, C cppgoDimension", matrix.goParams())
	assert.Equal(t, map[string]string{"T": "T", "type-parameter-0-0": "T"}, matrix.paramMap(),
		"a dimension parameter is not a type the member mapper substitutes")
	assert.Len(t, matrix.Ctors, 1)
	assert.Len(t, matrix.Statics, 1)

	twice := findTemplate(t, model, "box::twice")
	assert.True(t, twice.IsFunction)
	require.NotNil(t, twice.Func)
	assert.Equal(t, "T", twice.Func.Result.Go)
	assert.Equal(t, "box::twice(T)", displaySignature(twice.Func.sym))

	assert.Equal(t, "[T any]", box.declTypeParams())
	assert.Equal(t, "[T]", box.typeParams())
	assert.Equal(t, "T", paramNames(box))
	assert.Equal(t, map[string]string{"T": "T", "type-parameter-0-0": "T"}, box.paramMap())
}

// TestTemplateNoParameters covers the two ways a template model refuses input
// it cannot turn into a generic declaration.
func TestTemplateNoParameters(t *testing.T) {
	_, err := templateParams(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no parameters")

	_, err = templateParams([]cxx.TemplateParameter{{Name: "N", Kind: cxx.TemplateParameterNonType,
		Type: cxx.Param{Kind: cxx.TypeFloat, SizeBytes: 8}}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an integer")

	_, err = templateParams([]cxx.TemplateParameter{{Name: "C", Kind: cxx.TemplateParameterTemplate}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a type or an integer")
}

// TestTemplateParameterDefaults covers the anonymous-parameter fallback and the
// empty model edge cases.
func TestTemplateParameterDefaults(t *testing.T) {
	params, err := templateParams([]cxx.TemplateParameter{{Kind: cxx.TemplateParameterType}})
	require.NoError(t, err)
	require.Len(t, params, 1)
	assert.Equal(t, "P0", params[0].GoName)

	empty := &templateModel{}
	assert.Empty(t, empty.bindings())
	assert.Nil(t, empty.paramMap())
	assert.Equal(t, "[]", empty.typeParams())
	assert.Equal(t, "[]", empty.declTypeParams())
}

// TestTemplateGoOutput checks the generic API the golden file carries: a type
// that compiles without a concrete instantiation, constructors whose overloads
// are named, a pointer parameter, a static member and a free function template.
func TestTemplateGoOutput(t *testing.T) {
	gen := renderMap(t, boxConfig())[GenFileName]
	for _, want := range []string{
		"type Box[T any] struct {",
		"func NewBox[T any](value T) (*Box[T], error)",
		"func NewBoxDefault[T any]() (*Box[T], error)",
		"func (b *Box[T]) Close() error",
		"func (b *Box[T]) Get() (T, error)",
		"func (b *Box[T]) Set(value T) error",
		"func (b *Box[T]) Echo(value *T) (*T, error)",
		"func BoxIdentity[T any](value T) (T, error)",
		"type Plain[T any] struct {",
		"func NewPlain[T any](value T) (*Plain[T], error)",
		"func Twice[T any](arg0 T) (T, error)",
		"type Matrix[T any, R cppgoDimension, C cppgoDimension] struct {",
		"func NewMatrix[T any, R cppgoDimension, C cppgoDimension](fill T) (*Matrix[T, R, C], error)",
		"func MatrixRows[T any, R cppgoDimension, C cppgoDimension]() (int32, error)",
		"type D4 struct{}",
		"func (D4) cppgoDimension() string { return \"4\" }",
		"func cppgoDimensionCxx[D cppgoDimension]() string",
		"func cppgoTemplateKey[T any]() string",
		"func cppgoTemplateFunc(symbol, what, line string) (uintptr, error)",
		"cppgoTemplateSymbols = map[string]uintptr{}",
	} {
		assert.Contains(t, gen, want)
	}
	// Plain declares no destructor: Close is the storage-only form.
	plainClose := gen[strings.Index(gen, "func (p *Plain[T]) Close() error"):]
	plainClose = plainClose[:strings.Index(plainClose, "\n}\n")]
	assert.Contains(t, plainClose, "return nil")
	assert.NotContains(t, plainClose, "cppgoCloseError")
}

// TestTemplateShimOutput checks phase two's translation unit: the explicit
// instantiations the scan collected, the size and alignment thunks the generic
// constructor needs, and one thunk per member with the concrete C++ types.
func TestTemplateShimOutput(t *testing.T) {
	cfg := boxConfig()
	cfg.Instantiation = []Instantiation{
		{Template: "box::Box", Args: []string{"int32"}},
		{Template: "box::Plain", Args: []string{"float64"}},
		{Template: "box::twice", Args: []string{"int64"}},
		{Template: "box::Matrix", Args: []string{"float64", "box.D4", "box.D4"}},
	}
	shim := renderMap(t, cfg)[ShimSourceFileName]
	for _, want := range []string{
		"template class ::box::Box<std::int32_t>;",
		"template class ::box::Plain<double>;",
		"template class ::box::Matrix<double, 4, 4>;",
		"return sizeof(::box::Box<std::int32_t>);",
		"return alignof(::box::Plain<double>);",
		"static_cast<const ::box::Box<std::int32_t> *>",
		"->echo(value)",
		"->echo(*value)",
		"::box::Box<std::int32_t>::identity(value)",
		"::box::twice<std::int64_t>(arg0)",
	} {
		// The reference/pointer call forms are asserted separately below.
		switch want {
		case "->echo(value)", "->echo(*value)":
			continue
		}
		assert.Contains(t, shim, want)
	}
	assert.Contains(t, shim, "->echo(value)")
	assert.NotContains(t, shim, "->echo(*value)", "a T* parameter is passed as a pointer")
	assert.NotContains(t, shim, "template class ::box::twice", "a function template is instantiated implicitly")
	assert.NotContains(t, shim, "sizeof(::box::twice")
}

// TestTemplateShimThunkNamesAreStable checks the FNV identity the generated Go
// recomputes: the same three inputs always name the same thunk.
func TestTemplateShimThunkNamesAreStable(t *testing.T) {
	first := templateSymbolName("box::Box", "abcdefabcdef", "int32")
	assert.Equal(t, first, templateSymbolName("box::Box", "abcdefabcdef", "int32"))
	assert.NotEqual(t, first, templateSymbolName("box::Box", "abcdefabcdef", "int64"))
	assert.NotEqual(t, first, templateSymbolName("box::Box", "123456123456", "int32"))
	assert.True(t, strings.HasPrefix(first, "cppgo_tmpl_"))
}

// TestTemplateInstantiationLines covers the class/struct keyword and the
// synthesized C++ name.
func TestTemplateInstantiationLines(t *testing.T) {
	inst := &templateInstantiation{
		Template: &templateModel{GoName: "Num", CxxName: "ns::Num", Kind: cxx.Struct},
		Key:      "int32",
		Args:     []templateArgument{{Go: "int32", C: "int32_t", Cxx: "std::int32_t"}},
	}
	assert.Equal(t, "ns::Num<std::int32_t>", inst.cxxName())
	assert.Equal(t, "template struct ::ns::Num<std::int32_t>;", inst.explicitInstantiation())
	inst.Template.Kind = cxx.Class
	assert.Equal(t, "template class ::ns::Num<std::int32_t>;", inst.explicitInstantiation())
}

// TestConcreteTemplateArguments covers every scalar the Go side accepts and the
// refusal that names the shape a non-scalar needs.
func TestConcreteTemplateArguments(t *testing.T) {
	cases := []struct {
		goType string
		c      string
		cxx    string
	}{
		{"bool", "bool", "bool"},
		{"int8", "int8_t", "std::int8_t"},
		{"uint8", "uint8_t", "std::uint8_t"},
		{"int16", "int16_t", "std::int16_t"},
		{"uint16", "uint16_t", "std::uint16_t"},
		{"int32", "int32_t", "std::int32_t"},
		{"uint32", "uint32_t", "std::uint32_t"},
		{"int64", "int64_t", "std::int64_t"},
		{"uint64", "uint64_t", "std::uint64_t"},
		{"float32", "float", "float"},
		{"float64", "double", "double"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.goType, func(t *testing.T) {
			arg, err := concreteTemplateArgument(tc.goType, templateParam{Kind: templateParamType})
			require.NoError(t, err)
			assert.Equal(t, tc.goType, arg.Go)
			assert.Equal(t, tc.c, arg.C)
			assert.Equal(t, tc.cxx, arg.Cxx)
			require.NotNil(t, arg.Type)
		})
	}

	pointer, err := concreteTemplateArgument("*int32", templateParam{Kind: templateParamType})
	require.NoError(t, err)
	assert.Equal(t, "int32_t *", pointer.C)
	assert.Equal(t, "std::int32_t *", pointer.Cxx)
	assert.Equal(t, goPointer, pointer.Type.Kind)

	_, err = concreteTemplateArgument("mypkg.Named", templateParam{Kind: templateParamType})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a scalar")

	_, err = concreteTemplateArgument("*mypkg.Named", templateParam{Kind: templateParamType})
	require.Error(t, err)

	_, ok := concreteScalar("int")
	assert.False(t, ok, "int is not a fixed-width Go spelling")
}

// TestConcreteType covers the substitution of one generic type: a bare
// parameter, void, a pointer, and the two failures the emitter must name.
func TestConcreteType(t *testing.T) {
	tmpl := &templateModel{CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	inst := &templateInstantiation{
		Template: tmpl, Key: "int32",
		Args: []templateArgument{{Go: "int32", C: "int32_t", Cxx: "std::int32_t",
			Type: &goType{Kind: goInteger, Go: "int32", C: "int32_t", Cxx: "std::int32_t"}}},
	}
	param := &goType{Kind: goTypeParam, Go: "T", ParamName: "T"}
	resolved, err := concreteType(param, inst)
	require.NoError(t, err)
	assert.Equal(t, "int32", resolved.Go)

	void, err := concreteType(&goType{Kind: goVoid}, inst)
	require.NoError(t, err)
	assert.Equal(t, goVoid, void.Kind)

	pointer, err := concreteType(&goType{Kind: goPointer, Go: "*T", Elem: param}, inst)
	require.NoError(t, err)
	assert.Equal(t, "*int32", pointer.Go)
	assert.Equal(t, "int32_t *", pointer.C)

	_, err = concreteType(&goType{Kind: goTypeParam, Go: "U", ParamName: "U"}, inst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no concrete argument")

	_, err = concreteType(&goType{Kind: goPointer, Go: "*U", Elem: &goType{Kind: goTypeParam, ParamName: "U"}}, inst)
	require.Error(t, err)

	_, err = concreteType(&goType{Kind: goSlice, Go: "[]T"}, inst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot substitute")
}

// TestCheckGenericCarried covers the dependent types the generic emitters
// carry and the one they refuse.
func TestCheckGenericCarried(t *testing.T) {
	param := cxx.Param{Spelling: "T"}
	require.NoError(t, checkGenericCarried(&goType{Kind: goVoid}, param))
	require.NoError(t, checkGenericCarried(&goType{Kind: goTypeParam}, param))
	require.NoError(t, checkGenericCarried(&goType{Kind: goPointer, Elem: &goType{Kind: goTypeParam}}, param))
	err := checkGenericCarried(&goType{Kind: goSlice, Go: "[]T"}, param)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependent type")
}

// TestBuildInstantiations checks the second phase's validation: a name that is
// not a template, a wrong argument count, a non-scalar argument, and dedup.
func TestBuildInstantiations(t *testing.T) {
	cases := []struct {
		name string
		inst []Instantiation
		want string
	}{
		{"unknown template", []Instantiation{{Template: "box::Nope", Args: []string{"int32"}}}, "not a template"},
		{"arity", []Instantiation{{Template: "box::Box", Args: []string{"int32", "int32"}}}, "takes 1 template arguments"},
		{"non-scalar", []Instantiation{{Template: "box::Box", Args: []string{"pkg.Named"}}}, "not a scalar"},
		{"bad dimension", []Instantiation{{Template: "box::Matrix", Args: []string{"float64", "box.D9", "box.D9"}}}, "not one of the generated dimension types"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cfg := boxConfig()
			cfg.Instantiation = tc.inst
			_, err := build(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	cfg := boxConfig()
	cfg.Instantiation = []Instantiation{
		{Template: "box::Box", Args: []string{"int32"}},
		{Template: "box::Box", Args: []string{"int32"}},
	}
	model, err := build(cfg)
	require.NoError(t, err)
	assert.Len(t, model.instantiations, 1, "the same instantiation is emitted once")

	assert.Equal(t, "int32,float64", instantiationKey([]templateArgument{{Go: "int32"}, {Go: "float64"}}))
}

// TestTemplateDuplicateGoName checks two C++ templates that would be one Go
// identifier: the error names both, never one silently replacing the other.
func TestTemplateDuplicateGoName(t *testing.T) {
	cfg := fixtureConfig("errors", "tempduplicate.hpp")
	_, err := build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both map to the Go name")
}

// TestTemplateParamSuffix checks the overload naming rule the constructors
// share with the concrete API.
func TestTemplateParamSuffix(t *testing.T) {
	assert.Equal(t, "T", templateParamSuffix([]*goParam{{Type: &goType{Go: "T"}}}))
	assert.Equal(t, "Int32Float64", templateParamSuffix([]*goParam{
		{Type: &goType{Go: "int32"}}, {Type: &goType{Go: "float64"}},
	}))
}

// TestGeneratedTemplate covers the scanner's position check directly: an
// object outside the generated package is not a template of ours even when its
// name matches.
func TestGeneratedTemplate(t *testing.T) {
	names := map[string]string{"Box": "box::Box"}
	_, ok := generatedTemplate(nil, token.NewFileSet(), "", names)
	assert.False(t, ok)

	pkg := types.NewPackage("cppgo-e2e/box", "box")
	noPkg := types.NewTypeName(token.NoPos, nil, "Box", nil)
	_, ok = generatedTemplate(noPkg, token.NewFileSet(), "", names)
	assert.False(t, ok, "an object with no package is not a generated template")

	other := types.NewTypeName(token.NoPos, pkg, "Other", nil)
	_, ok = generatedTemplate(other, token.NewFileSet(), "", names)
	assert.False(t, ok)

	fset := token.NewFileSet()
	file := fset.AddFile("/tmp/generated/box/zz_cppgo.gen.go", -1, 10)
	inside := types.NewTypeName(file.Pos(1), pkg, "Box", nil)
	template, ok := generatedTemplate(inside, fset, "/tmp/generated/box", names)
	require.True(t, ok)
	assert.Equal(t, "box::Box", template)

	_, ok = generatedTemplate(inside, fset, "/tmp/elsewhere", names)
	assert.False(t, ok, "the declaration must be inside the output directory")

	assert.True(t, withinDir("/tmp/generated/box/zz.go", "/tmp/generated/box"))
	assert.False(t, withinDir("/tmp/generated/box2/zz.go", "/tmp/generated/box"))
}

// TestTemplateModelErrors covers the model construction paths a fixture cannot
// reach: a binding whose parameter the generic emitter cannot carry, and a
// nil-function template.
func TestTemplateModelErrors(t *testing.T) {
	owner := &recordModel{GoName: "Owner", CxxName: "ns::Owner", Size: 4, Align: 4}
	b := &builder{
		cfg:     Config{Package: "p"},
		records: map[string]*recordModel{"ns::Owner": owner},
	}
	tmpl := &templateModel{GoName: "Box", CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	bad := cxx.Symbol{
		QualifiedName: "ns::Box::bad",
		Kind:          cxx.Method,
		Parent:        "ns::Box",
		Params:        []cxx.Param{{Spelling: "std::vector<T>"}},
	}
	_, err := b.newTemplateBinding(bad, tmpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "std::vector<T>")

	missingResult := cxx.Symbol{
		QualifiedName: "ns::Box::missing",
		Kind:          cxx.Method,
		Parent:        "ns::Box",
		Result:        cxx.Param{Spelling: "__int128"},
	}
	_, err = b.newTemplateBinding(missingResult, tmpl)
	require.Error(t, err)

	empty := &templateModel{GoName: "Fn", CxxName: "ns::fn", IsFunction: true}
	assert.Empty(t, empty.bindings())
}

// TestAssignConstructorNames covers the overload rule: the widest constructor
// keeps the bare name, a zero-argument one gets Default, and a shared suffix is
// disambiguated instead of two functions sharing one identifier.
func TestAssignConstructorNames(t *testing.T) {
	ctor := func(param string) *binding {
		return &binding{
			sym: cxx.Symbol{QualifiedName: "ns::Pair::Pair", Kind: cxx.Constructor,
				Params: []cxx.Param{{Spelling: param}}},
			Params: []*goParam{{Type: &goType{Go: param}}},
		}
	}
	tmpl := &templateModel{GoName: "Pair", Ctors: []*binding{ctor("T *"), ctor("T"), ctor("T")}}
	tmpl.assignConstructorNames()
	assert.Equal(t, []string{"NewPair", "NewPairT", "NewPairT2"}, []string{tmpl.Ctors[0].GoName, tmpl.Ctors[1].GoName, tmpl.Ctors[2].GoName})

	zero := &binding{sym: cxx.Symbol{QualifiedName: "ns::Pair::Pair", Kind: cxx.Constructor}}
	one := ctor("T")
	tmpl = &templateModel{GoName: "Pair", Ctors: []*binding{zero, one}}
	tmpl.assignConstructorNames()
	assert.Equal(t, "NewPair", tmpl.Ctors[0].GoName)
	assert.Equal(t, "NewPairDefault", tmpl.Ctors[1].GoName)
}

// TestTemplateBindingGenericErrors covers the dependent types that map onto a
// slice: they are refused with the parameter named, never emitted as a call
// whose slot types the shim cannot know.
func TestTemplateBindingGenericErrors(t *testing.T) {
	b := &builder{cfg: Config{Package: "p"}, records: map[string]*recordModel{}}
	tmpl := &templateModel{GoName: "Box", CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	param := cxx.Symbol{
		QualifiedName: "ns::Box::bag", Kind: cxx.Method, Parent: "ns::Box",
		Params: []cxx.Param{{Kind: cxx.TypeRecord, Spelling: "std::vector<T>"}},
	}
	_, err := b.newTemplateBinding(param, tmpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependent type")

	result := cxx.Symbol{
		QualifiedName: "ns::Box::bag", Kind: cxx.Method, Parent: "ns::Box",
		Result: cxx.Param{Kind: cxx.TypeRecord, Spelling: "std::vector<T>"},
	}
	_, err = b.newTemplateBinding(result, tmpl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dependent type")
}

// TestTemplateThunkErrors covers the two substitutions a thunk cannot make: a
// parameter and a result the generic emitters do not carry.
func TestTemplateThunkErrors(t *testing.T) {
	tmpl := &templateModel{GoName: "Box", CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	inst := &templateInstantiation{Template: tmpl, Key: "int32",
		Args: []templateArgument{{Go: "int32", C: "int32_t", Cxx: "std::int32_t", Type: &goType{Kind: goInteger, Go: "int32"}}}}
	m := &packageModel{}
	badParam := &binding{sym: cxx.Symbol{QualifiedName: "ns::Box::bad", Kind: cxx.Method},
		Params: []*goParam{{Name: "values", Type: &goType{Kind: goSlice, Go: "[]T"}}}}
	_, err := m.templateThunk(badParam, inst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameter values")

	badResult := &binding{sym: cxx.Symbol{QualifiedName: "ns::Box::bad", Kind: cxx.Method},
		Result: &goType{Kind: goSlice, Go: "[]T"}}
	_, err = m.templateThunk(badResult, inst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "result")
}

// TestEmitTemplateShimError checks the shim's own error return: a binding it
// cannot express stops the generation instead of writing a thunk with no call.
func TestEmitTemplateShimError(t *testing.T) {
	tmpl := &templateModel{GoName: "Box", CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	bad := &binding{sym: cxx.Symbol{QualifiedName: "ns::Box::bad", Kind: cxx.Method},
		Params: []*goParam{{Name: "values", Type: &goType{Kind: goSlice, Go: "[]T"}}}}
	tmpl.Methods = []*binding{bad}
	inst := &templateInstantiation{Template: tmpl, Key: "int32",
		Args: []templateArgument{{Go: "int32", C: "int32_t", Cxx: "std::int32_t", Type: &goType{Kind: goInteger, Go: "int32"}}}}
	m := &packageModel{instantiations: []*templateInstantiation{inst}}
	var h, s strings.Builder
	var spans []thunkSpan
	err := m.emitTemplateShim(&h, &s, &spans)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parameter values")
}

// TestConcreteTypePointerEdges covers the pointer substitution's const
// qualification and the pointer whose element is not a template parameter.
func TestConcreteTypePointerEdges(t *testing.T) {
	tmpl := &templateModel{CxxName: "ns::Box", Params: []templateParam{{Name: "T", GoName: "T"}}}
	inst := &templateInstantiation{Template: tmpl,
		Args: []templateArgument{{Go: "int32", C: "int32_t", Cxx: "std::int32_t",
			Type: &goType{Kind: goInteger, Go: "int32", C: "int32_t", Cxx: "std::int32_t"}}}}
	pointer := &goType{Kind: goPointer, Go: "*T", Elem: &goType{Kind: goTypeParam, ParamName: "T"}, Const: true}
	resolved, err := concreteType(pointer, inst)
	require.NoError(t, err)
	assert.Equal(t, "const int32_t *", resolved.C)
	assert.Equal(t, "const std::int32_t *", resolved.Cxx)

	_, err = concreteType(&goType{Kind: goPointer, Go: "*int32", Elem: &goType{Kind: goInteger, Go: "int32"}}, inst)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot substitute")
}

// TestLoadInstantiationsNameCollision checks the scan's symbol map: two
// templates that would share a generated name are an error, not an overwrite.
func TestLoadInstantiationsNameCollision(t *testing.T) {
	b := &builder{cfg: Config{Package: "p", Scan: []string{"./unused"}}, records: map[string]*recordModel{}}
	b.templates = []*templateModel{
		{GoName: "A", CxxName: "ns::A", Ctors: []*binding{{GoName: "B"}}},
		{GoName: "B", CxxName: "ns::B"},
	}
	err := b.loadInstantiations()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both map to the Go name")
}

// TestDimensionValue covers the phantom dimension spelling: D1..D8 are the
// fixed values the generic API declares, Dynamic is -1, and anything else is
// refused rather than turned into a C++ integer.
func TestDimensionValue(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		ok    bool
	}{
		{"D4", "4", true},
		{"box.D4", "4", true},
		{"Dynamic", "-1", true},
		{"box.Dynamic", "-1", true},
		{"D9", "", false},
		{"D0", "", false},
		{"DP", "", false},
		{"", "", false},
		{"D", "", false},
		{"named.Distance", "", false},
	} {
		value, ok := dimensionValue(tc.name)
		assert.Equal(t, tc.ok, ok, tc.name)
		assert.Equal(t, tc.value, value, tc.name)
	}
}
