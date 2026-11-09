package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// templateParamKind is what a C++ template parameter is.
type templateParamKind uint8

const (
	// templateParamType is `typename T`: a Go type parameter.
	templateParamType templateParamKind = iota
	// templateParamDimension is `int N`: a Go type parameter constrained by the
	// generated phantom dimension type, because Go has no value parameters.
	templateParamDimension
)

// templateParam is one parameter of a template pattern, as the generic Go
// declaration spells it.
type templateParam struct {
	// Name is the C++ parameter, e.g. "T".
	Name string
	// GoName is the identifier the generic Go declaration uses.
	GoName string
	// Kind is what the parameter is.
	Kind templateParamKind
}

// templateModel is one class template pattern: the generic API emitted for it
// and the bindings the two-phase flow instantiates. It is never a record the
// concrete API classifies, because a pattern has no layout and no symbol.
type templateModel struct {
	// GoName is the generic Go type name, e.g. "Box".
	GoName string
	// CxxName is the qualified C++ template name, e.g. "tmpl::Box".
	CxxName string
	// Kind is the keyword the template was declared with, which the shim's
	// explicit instantiation repeats.
	Kind cxx.RecordKind
	// Params are the template's type parameters, in declaration order.
	Params []templateParam
	// Ctors, Dtor, Methods and Statics are the pattern's public members, with
	// their dependent types already spelled as the generic parameters.
	Ctors   []*binding
	Dtor    *binding
	Methods []*binding
	Statics []*binding
	// IsFunction is true for a free function template, whose binding is Func.
	IsFunction bool
	Func       *binding
}

// bindings returns every instantiable binding of the template in a stable
// order. The two-phase shim emits a thunk per entry per instantiation.
func (t *templateModel) bindings() []*binding {
	if t.IsFunction {
		if t.Func == nil {
			return nil
		}
		return []*binding{t.Func}
	}
	out := make([]*binding, 0, len(t.Ctors)+len(t.Methods)+len(t.Statics)+1)
	out = append(out, t.Ctors...)
	out = append(out, t.Methods...)
	out = append(out, t.Statics...)
	if t.Dtor != nil {
		out = append(out, t.Dtor)
	}
	return out
}

// paramMap is what the type mapper uses to recognize a dependent type: the
// parameter's written name and libclang's indexed spelling both map to the Go
// name the generic declaration uses.
func (t *templateModel) paramMap() map[string]string {
	if len(t.Params) == 0 {
		return nil
	}
	m := make(map[string]string, 2*len(t.Params))
	for i, param := range t.Params {
		if param.Kind != templateParamType {
			continue
		}
		m[param.Name] = param.GoName
		m[fmt.Sprintf("type-parameter-0-%d", i)] = param.GoName
	}
	return m
}

// goParams renders the generic type-parameter list: "[T any]".
func (t *templateModel) goParams() string {
	parts := make([]string, len(t.Params))
	for i, param := range t.Params {
		constraint := "any"
		if param.Kind == templateParamDimension {
			constraint = "cppgoDimension"
		}
		parts[i] = param.GoName + " " + constraint
	}
	return strings.Join(parts, ", ")
}

// typeParams renders the type parameters as a use: "[T]".
func (t *templateModel) typeParams() string {
	parts := make([]string, len(t.Params))
	for i, param := range t.Params {
		parts[i] = param.GoName
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// declTypeParams renders the type parameters of a declaration: "[T any]".
func (t *templateModel) declTypeParams() string {
	return "[" + t.goParams() + "]"
}

// hasDimensions reports whether any parameter is a non-type dimension, which
// is what makes the generated package declare the phantom dimension types.
func (t *templateModel) hasDimensions() bool {
	for _, param := range t.Params {
		if param.Kind == templateParamDimension {
			return true
		}
	}
	return false
}

// dimensionValue reads the C++ value of a phantom dimension type: D4 is 4 and
// Dynamic is -1, the value the contract's Matrix API uses for a runtime
// dimension. The name may be qualified by the generated package.
func dimensionValue(name string) (string, bool) {
	base := name
	if i := strings.LastIndex(base, "."); i >= 0 {
		base = base[i+1:]
	}
	if base == "Dynamic" {
		return "-1", true
	}
	if len(base) < 2 || base[0] != 'D' || len(base) > 2 {
		return "", false
	}
	if base[1] < '1' || base[1] > '8' {
		return "", false
	}
	return base[1:], true
}

// templateArgument is one concrete argument of one instantiation: the Go
// spelling the scan collected and the C and C++ spellings the shim needs.
type templateArgument struct {
	// Go is the Go type's string form, the instantiation key.
	Go string
	// C is the parameter type in the shim's extern "C" prototype.
	C string
	// Cxx is the type the thunk casts to.
	Cxx string
	// Type is the concrete Go shape of the argument.
	Type *goType
}

// templateInstantiation is one concrete specialization the Go program uses.
type templateInstantiation struct {
	// Template is the pattern it instantiates.
	Template *templateModel
	// Key is the scan's identity: the Go type arguments joined by commas.
	Key string
	// Args are the concrete arguments, one per template parameter.
	Args []templateArgument
}

// cxxName is the instantiated C++ type, without leading ::, e.g.
// "tmpl::Box<std::int32_t>".
func (i *templateInstantiation) cxxName() string {
	parts := make([]string, len(i.Args))
	for j, arg := range i.Args {
		parts[j] = arg.Cxx
	}
	return i.Template.CxxName + "<" + strings.Join(parts, ", ") + ">"
}

// explicitInstantiation is the line the shim adds to make the specialization
// exist, and the line the load error tells the user to add.
func (i *templateInstantiation) explicitInstantiation() string {
	keyword := "struct"
	if i.Template.Kind == cxx.Class {
		keyword = "class"
	}
	return "template " + keyword + " ::" + i.cxxName() + ";"
}

// buildTemplates turns every template pattern into a generic model. It is the
// first phase of the two-phase flow: nothing here knows a concrete argument,
// and the output compiles on its own.
func (b *builder) buildTemplates() error {
	b.templateByName = make(map[string]*templateModel)
	for _, rec := range b.rawRecords {
		if !rec.IsTemplate {
			continue
		}
		tmpl, err := b.newTemplate(rec)
		if err != nil {
			return err
		}
		b.templates = append(b.templates, tmpl)
		b.templateByName[rec.QualifiedName] = tmpl
	}
	for _, sym := range b.symbols {
		if !sym.IsTemplate || sym.Kind != cxx.FreeFunc {
			continue
		}
		if len(sym.TemplateParameters) == 0 {
			continue
		}
		tmpl, err := b.newTemplateFunction(sym)
		if err != nil {
			return err
		}
		b.templates = append(b.templates, tmpl)
		b.templateByName[tmpl.CxxName] = tmpl
	}
	sort.Slice(b.templates, func(i, j int) bool { return b.templates[i].CxxName < b.templates[j].CxxName })
	seen := make(map[string]string, len(b.templates))
	for _, tmpl := range b.templates {
		if other, taken := seen[tmpl.GoName]; taken {
			return fmt.Errorf("cppgo: %s and %s both map to the Go name %s; rename one with -rename", other, tmpl.CxxName, tmpl.GoName)
		}
		seen[tmpl.GoName] = tmpl.CxxName
	}
	return nil
}

// templateParams converts a C++ parameter list into Go type parameters. A
// parameter this build cannot express is a generation error, not an omission.
func templateParams(params []cxx.TemplateParameter) ([]templateParam, error) {
	out := make([]templateParam, 0, len(params))
	for _, param := range params {
		name := param.Name
		if name == "" {
			name = fmt.Sprintf("P%d", len(out))
		}
		switch param.Kind {
		case cxx.TemplateParameterType:
			out = append(out, templateParam{Name: name, GoName: name, Kind: templateParamType})
		case cxx.TemplateParameterNonType:
			if param.Type.Kind != cxx.TypeInteger || param.Type.SizeBytes <= 0 {
				return nil, pending(
					fmt.Sprintf("the non-type template parameter %s is not an integer", name),
					"expose the value as an integral template parameter at the C++ boundary")
			}
			out = append(out, templateParam{Name: name, GoName: name, Kind: templateParamDimension})
		default:
			return nil, pending(
				fmt.Sprintf("the %s template parameter %s is not a type or an integer", param.Kind, name),
				"expose a template-template parameter as a concrete type at the C++ boundary")
		}
	}
	if len(out) == 0 {
		return nil, pending("a class template with no parameters is not a generic type", "name a concrete instantiation with -instantiate")
	}
	return out, nil
}

// newTemplate builds the generic model of one class template pattern.
func (b *builder) newTemplate(rec cxx.Record) (*templateModel, error) {
	params, err := templateParams(rec.TemplateParameters)
	if err != nil {
		return nil, recordError(rec, err)
	}
	tmpl := &templateModel{
		GoName:  exportName(simpleName(rec.QualifiedName)),
		CxxName: rec.QualifiedName,
		Kind:    rec.Kind,
		Params:  params,
	}
	for _, field := range rec.Fields {
		if field.Access != cxx.AccessPublic {
			continue
		}
		return nil, recordError(rec, pending(
			fmt.Sprintf("the public field %s of a template is not emitted by this build yet", field.Name),
			"make the field private and expose an accessor in C++, or bind the instantiation with -instantiate"))
	}
	for _, sym := range b.symbols {
		if sym.Parent != rec.QualifiedName || !sym.IsTemplate {
			continue
		}
		if sym.Access != cxx.AccessPublic {
			continue
		}
		if len(sym.TemplateParameters) > 0 {
			return nil, symbolError(sym, pending(
				fmt.Sprintf("the member function template %s is not emitted by this build yet", sym.QualifiedName),
				"expose the concrete instantiation as a non-template member"))
		}
		if sym.IsOperator || sym.IsConversion {
			return nil, symbolError(sym, pending(
				"operators and conversion operators are not emitted by this build yet",
				"give it a named member function"))
		}
		if b.namesNestedType(sym) {
			// A member whose signature names a type declared inside the
			// template has one answer per instantiation, so it cannot be part
			// of the generic API. The concrete specialization carries it.
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s is not part of the generic API: its signature names a type declared inside %s, and each instantiation has its own; the specialization carries it",
				displaySignature(sym), rec.QualifiedName))
			continue
		}
		switch sym.Kind {
		case cxx.Constructor:
			if sym.IsDeleted || sym.IsCopyConstructor || sym.IsMoveConstructor {
				continue
			}
		case cxx.Destructor:
			if sym.IsDeleted {
				continue
			}
		case cxx.Method:
			if sym.IsPureVirtual || sym.IsDeleted {
				continue
			}
		case cxx.StaticMethod:
			if sym.IsDeleted {
				continue
			}
		}
		binding, err := b.newTemplateBinding(sym, tmpl)
		if err != nil {
			return nil, symbolError(sym, err)
		}
		switch sym.Kind {
		case cxx.Constructor:
			tmpl.Ctors = append(tmpl.Ctors, binding)
		case cxx.Destructor:
			tmpl.Dtor = binding
		case cxx.Method:
			tmpl.Methods = append(tmpl.Methods, binding)
		case cxx.StaticMethod:
			tmpl.Statics = append(tmpl.Statics, binding)
		}
	}
	tmpl.assignConstructorNames()
	if tmpl.Dtor != nil {
		tmpl.Dtor.GoName = "Close"
	}
	for _, method := range tmpl.Methods {
		method.GoName = exportName(simpleName(method.sym.QualifiedName))
	}
	for _, static := range tmpl.Statics {
		static.GoName = tmpl.GoName + exportName(simpleName(static.sym.QualifiedName))
	}
	sort.Slice(tmpl.Ctors, func(i, j int) bool { return tmpl.Ctors[i].GoName < tmpl.Ctors[j].GoName })
	sort.Slice(tmpl.Methods, func(i, j int) bool { return tmpl.Methods[i].GoName < tmpl.Methods[j].GoName })
	return tmpl, nil
}

// assignConstructorNames names a class template's constructors: the one with
// the most parameters keeps New<Type>, a zero-argument overload becomes
// New<Type>Default, and a suffix that two overloads share is disambiguated by
// an index rather than by silently dropping one.
func (t *templateModel) assignConstructorNames() {
	sort.Slice(t.Ctors, func(i, j int) bool {
		left, right := t.Ctors[i], t.Ctors[j]
		if len(left.Params) != len(right.Params) {
			return len(left.Params) > len(right.Params)
		}
		return displaySignature(left.sym) < displaySignature(right.sym)
	})
	used := make(map[string]bool, len(t.Ctors))
	for i, ctor := range t.Ctors {
		name := "New" + t.GoName
		if i > 0 {
			suffix := templateParamSuffix(ctor.Params)
			if suffix == "" {
				suffix = "Default"
			}
			name += suffix
		}
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s%d", base, n)
		}
		used[name] = true
		ctor.GoName = name
	}
}

// templateParamSuffix names an overloaded constructor from its parameter
// types, the same rule the concrete API uses.
func templateParamSuffix(params []*goParam) string {
	var b strings.Builder
	for _, param := range params {
		b.WriteString(typeSuffix(param.Type.Go))
	}
	return b.String()
}

// newTemplateFunction builds the generic model of a free function template.
func (b *builder) newTemplateFunction(sym cxx.Symbol) (*templateModel, error) {
	params, err := templateParams(sym.TemplateParameters)
	if err != nil {
		return nil, symbolError(sym, err)
	}
	tmpl := &templateModel{
		GoName:     exportName(simpleName(sym.QualifiedName)),
		CxxName:    sym.QualifiedName,
		Params:     params,
		IsFunction: true,
	}
	binding, err := b.newTemplateBinding(sym, tmpl)
	if err != nil {
		return nil, symbolError(sym, err)
	}
	binding.GoName = tmpl.GoName
	binding.IsStatic = true
	tmpl.Func = binding
	return tmpl, nil
}

// namesNestedType reports whether a declaration's signature names a record or
// enum the generated API does not hold — which, inside a template pattern, is a
// type declared inside the template itself: libclang spells it against the
// pattern, and every instantiation has its own.
func (b *builder) namesNestedType(sym cxx.Symbol) bool {
	var unknown func(p cxx.Param) bool
	unknown = func(p cxx.Param) bool {
		if p.RecordName == "" {
			if p.Pointee != nil {
				return unknown(*p.Pointee)
			}
			return false
		}
		if _, ok := b.records[p.RecordName]; ok {
			return false
		}
		if _, ok := b.enums[p.RecordName]; ok {
			return false
		}
		return true
	}
	if unknown(sym.Result) {
		return true
	}
	for _, param := range sym.Params {
		if unknown(param) {
			return true
		}
	}
	return false
}

// newTemplateBinding computes one generic binding. Only a bare type parameter,
// a pointer to one, and void are carried by this build: every other dependent
// type needs machinery the two-phase emitters do not have yet, and that is a
// named generation error rather than a silently absent method.
func (b *builder) newTemplateBinding(sym cxx.Symbol, tmpl *templateModel) (*binding, error) {
	mapper := &mapper{records: b.records, templateParams: tmpl.paramMap()}
	binding := &binding{
		sym:           sym,
		Template:      tmpl,
		ErrorResult:   true,
		This:          sym.Kind == cxx.Method || sym.Kind == cxx.Constructor || sym.Kind == cxx.Destructor,
		IsConstructor: sym.Kind == cxx.Constructor,
		IsDestructor:  sym.Kind == cxx.Destructor,
		IsStatic:      sym.Kind == cxx.StaticMethod || sym.Kind == cxx.FreeFunc,
	}
	taken := map[string]bool{}
	if binding.This {
		taken[receiverName(tmpl.GoName)] = true
	}
	for i, param := range sym.Params {
		mapped, err := mapper.mapType(param)
		if err != nil {
			return nil, fmt.Errorf("parameter %s: %w", param.Name, err)
		}
		if err := checkGenericCarried(mapped, param); err != nil {
			return nil, err
		}
		name := uniqueName(paramName(param.Name, i), taken)
		binding.Params = append(binding.Params, &goParam{Name: name, Type: mapped})
	}
	if !binding.IsConstructor && !binding.IsDestructor {
		result, err := mapper.mapType(sym.Result)
		if err != nil {
			return nil, fmt.Errorf("result: %w", err)
		}
		if result.Kind != goVoid {
			if err := checkGenericCarried(result, sym.Result); err != nil {
				return nil, fmt.Errorf("result: %w", err)
			}
			binding.Result = result
		}
	}
	binding.ID = shortHash(displaySignature(sym))
	return binding, nil
}

// checkGenericCarried refuses a dependent type the two-phase emitters do not
// carry, naming the declaration and the fix.
func checkGenericCarried(mapped *goType, param cxx.Param) error {
	switch mapped.Kind {
	case goVoid, goTypeParam, goInteger, goFloat, goBool:
		// A concrete scalar needs no substitution, and a template parameter
		// is the instantiation's to fill.
		return nil
	case goPointer:
		if mapped.Elem != nil && mapped.Elem.Kind == goTypeParam {
			return nil
		}
	}
	return pending(
		fmt.Sprintf("%q is a dependent type the generic API does not carry yet", param.Spelling),
		"take the type parameter by value, by pointer or by reference at the C++ boundary")
}

// loadInstantiations runs the second phase's collection when -scan names a
// program, and validates whatever list the configuration carries.
func (b *builder) loadInstantiations() error {
	if len(b.cfg.Scan) > 0 {
		names := make(map[string]string, 2*len(b.templates))
		for _, tmpl := range b.templates {
			// The generic type or function is one instantiation point, and a
			// class template's constructors are the other: a program writes
			// NewBox[int32], and that is the concrete type the shim needs.
			candidates := []string{tmpl.GoName}
			for _, ctor := range tmpl.Ctors {
				candidates = append(candidates, ctor.GoName)
			}
			for _, static := range tmpl.Statics {
				candidates = append(candidates, static.GoName)
			}
			for _, name := range candidates {
				if other, taken := names[name]; taken && other != tmpl.CxxName {
					return fmt.Errorf("cppgo: %s and %s both map to the Go name %s; rename one with -rename", other, tmpl.CxxName, name)
				}
				names[name] = tmpl.CxxName
			}
		}
		found, err := scanInstantiations(b.cfg.ScanDir, b.cfg.Scan, b.cfg.Out, names)
		if err != nil {
			return err
		}
		b.cfg.Instantiation = found
	}
	return b.buildInstantiations()
}

// buildInstantiations validates the scan's result against the patterns and
// turns it into the concrete arguments the shim emits for.
func (b *builder) buildInstantiations() error {
	if len(b.cfg.Instantiation) == 0 {
		return nil
	}
	seen := map[string]bool{}
	for _, entry := range b.cfg.Instantiation {
		tmpl := b.templateByName[entry.Template]
		if tmpl == nil {
			return fmt.Errorf("cppgo: the two-phase scan found %s, which is not a template this header declares", entry.Template)
		}
		if len(entry.Args) != len(tmpl.Params) {
			return fmt.Errorf("cppgo: %s takes %d template arguments, and the scan found %d", entry.Template, len(tmpl.Params), len(entry.Args))
		}
		args := make([]templateArgument, len(entry.Args))
		for i, arg := range entry.Args {
			concrete, err := concreteTemplateArgument(arg, tmpl.Params[i])
			if err != nil {
				return fmt.Errorf("cppgo: %s argument %d: %w", entry.Template, i+1, err)
			}
			args[i] = concrete
		}
		key := instantiationKey(args)
		identity := entry.Template + "|" + key
		if seen[identity] {
			continue
		}
		seen[identity] = true
		b.instantiations = append(b.instantiations, &templateInstantiation{Template: tmpl, Key: key, Args: args})
	}
	sort.Slice(b.instantiations, func(i, j int) bool {
		left, right := b.instantiations[i], b.instantiations[j]
		if left.Template.CxxName != right.Template.CxxName {
			return left.Template.CxxName < right.Template.CxxName
		}
		return left.Key < right.Key
	})
	return nil
}

// instantiationKey is the identity the Go side computes at runtime: the Go
// spellings of the arguments joined by commas.
func instantiationKey(args []templateArgument) string {
	parts := make([]string, len(args))
	for i, arg := range args {
		parts[i] = arg.Go
	}
	return strings.Join(parts, ",")
}

// concreteTemplateArgument maps one Go type argument onto the C and C++ types
// the shim uses. A non-scalar argument is refused with the shape that would
// make it representable, never guessed.
func concreteTemplateArgument(name string, param templateParam) (templateArgument, error) {
	if param.Kind == templateParamDimension {
		value, ok := dimensionValue(name)
		if !ok {
			return templateArgument{}, pending(
				fmt.Sprintf("the dimension argument %s is not one of the generated dimension types", name),
				"use D1..D8 or Dynamic, the phantom types the generic API declares")
		}
		return templateArgument{Go: name, C: value, Cxx: value}, nil
	}
	if scalar, ok := concreteScalar(name); ok {
		return scalar, nil
	}
	if strings.HasPrefix(name, "*") {
		elem, err := concreteTemplateArgument(name[1:], param)
		if err != nil {
			return templateArgument{}, err
		}
		return templateArgument{
			Go:  name,
			C:   elem.C + " *",
			Cxx: elem.Cxx + " *",
			Type: &goType{Kind: goPointer, Go: name, C: elem.C + " *",
				Cxx: elem.Cxx + " *", Elem: elem.Type},
		}, nil
	}
	return templateArgument{}, pending(
		fmt.Sprintf("the Go type %s is not a scalar the two-phase flow carries yet", name),
		"instantiate the template with a fixed-width Go scalar at the C++ boundary")
}

// concreteScalar maps a Go scalar spelling onto the C and C++ types the shim
// uses for one template argument.
func concreteScalar(name string) (templateArgument, bool) {
	if name == "bool" {
		return templateArgument{Go: name, C: "bool", Cxx: "bool",
			Type: &goType{Kind: goBool, Go: "bool", C: "bool", Cxx: "bool"}}, true
	}
	for _, width := range []struct {
		bits     int64
		unsigned bool
	}{
		{8, false}, {16, false}, {32, false}, {64, false},
		{8, true}, {16, true}, {32, true}, {64, true},
	} {
		goName, cName, _ := integerType(width.bits/8, width.unsigned)
		if goName != name {
			continue
		}
		return templateArgument{Go: name, C: cName, Cxx: "std::" + cName,
			Type: &goType{Kind: goInteger, Go: goName, C: cName, Cxx: "std::" + cName}}, true
	}
	for _, float := range []struct {
		goName string
		cName  string
	}{
		{"float32", "float"}, {"float64", "double"},
	} {
		if float.goName != name {
			continue
		}
		return templateArgument{Go: name, C: float.cName, Cxx: float.cName,
			Type: &goType{Kind: goFloat, Go: name, C: float.cName, Cxx: float.cName}}, true
	}
	return templateArgument{}, false
}

// concreteType substitutes an instantiation's arguments into one generic type.
func concreteType(mapped *goType, inst *templateInstantiation) (*goType, error) {
	switch mapped.Kind {
	case goVoid, goInteger, goFloat, goBool:
		return mapped, nil
	case goTypeParam:
		for i, param := range inst.Template.Params {
			if param.Name == mapped.ParamName {
				return inst.Args[i].Type, nil
			}
		}
		return nil, fmt.Errorf("cppgo: the template parameter %s has no concrete argument", mapped.ParamName)
	case goPointer:
		if mapped.Elem == nil || mapped.Elem.Kind != goTypeParam {
			break
		}
		for i, param := range inst.Template.Params {
			if param.Name != mapped.Elem.ParamName {
				continue
			}
			arg := inst.Args[i]
			out := &goType{
				Kind:   goPointer,
				Go:     "*" + arg.Go,
				C:      arg.C + " *",
				Cxx:    arg.Cxx + " *",
				Elem:   arg.Type,
				Record: arg.Type.Record,
				Const:  mapped.Const,
			}
			if mapped.Const {
				out.C = "const " + out.C
				out.Cxx = "const " + out.Cxx
			}
			return out, nil
		}
		return nil, fmt.Errorf("cppgo: the template parameter %s has no concrete argument", mapped.Elem.ParamName)
	}
	return nil, pending(
		fmt.Sprintf("%q is a generic type the shim cannot substitute yet", mapped.Go),
		"take a bare type parameter at the C++ boundary")
}
