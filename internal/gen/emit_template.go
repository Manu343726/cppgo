package gen

import (
	"fmt"
	"strings"
)

// hasTemplates reports whether the package exposes a generic API, which decides
// the two imports and the runtime helpers the generated file needs.
func (m *packageModel) hasTemplates() bool { return len(m.templates) > 0 }

// emitTemplates writes the generic API: one generic Go type or function per
// C++ template pattern, plus the runtime helpers the two-phase dispatch uses.
// Nothing here knows a concrete instantiation, so the file compiles before
// `cppgo build` has scanned a program.
func (m *packageModel) emitTemplates(b *strings.Builder) {
	if !m.hasTemplates() {
		return
	}
	b.WriteString(templateHelpers)
	if m.hasDimensions() {
		b.WriteString(dimensionHelpers)
	}
	for _, tmpl := range m.templates {
		if tmpl.IsFunction {
			m.emitTemplateFunction(b, tmpl)
			continue
		}
		m.emitTemplateType(b, tmpl)
	}
}

// templateHelpers is the generated half of the two-phase flow: the
// instantiation key, the thunk naming, the resolution that names the missing
// `template ...;` line, and the argument marshaling a generic call needs
// because the slot types are only known at the instantiation.
const templateHelpers = `
// cppgoTemplateKey is the instantiation key of one type argument: the Go
// type's string form, which is what cppgo build collects when it scans a
// program.
func cppgoTemplateKey[T any]() string {
	return reflect.TypeOf((*T)(nil)).Elem().String()
}

// cppgoTemplateHash is the generator's FNV-1a identifier. It has to stay
// byte-identical to the generator's shortHash: the shim's thunk names are
// computed here, at the instantiation, not looked up in a table.
func cppgoTemplateHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%012x", h.Sum64()&0xffffffffffff)
}

// cppgoTemplateSymbol is the shim symbol of one binding of one instantiation.
func cppgoTemplateSymbol(tmpl, binding, key string) string {
	return "cppgo_tmpl_" + cppgoTemplateHash(tmpl+"|"+binding+"|"+key)
}

// cppgoTemplateLine is the explicit instantiation that would make the
// specialization exist, which is the line the failure names.
func cppgoTemplateLine(tmpl, cxxArgs string) string {
	return "template struct ::" + tmpl + "<" + cxxArgs + ">;"
}

// cppgoTemplateFunc resolves one instantiation's thunk and caches it. A
// missing thunk is the contract's failure mode: the Go program instantiated a
// type the shim has no C++ counterpart for, and the error names the line to
// add, never a nil call.
func cppgoTemplateFunc(symbol, what, line string) (uintptr, error) {
	cppgoMu.Lock()
	addr, ok := cppgoTemplateSymbols[symbol]
	cppgoMu.Unlock()
	if ok {
		return addr, nil
	}
	if err := cppgoOpen(""); err != nil {
		return 0, err
	}
	cppgoMu.Lock()
	shim := cppgoLoaded.shim
	cppgoMu.Unlock()
	got, err := shim.Symbol(symbol)
	if err != nil {
		return 0, fmt.Errorf("cppgo: %s has no C++ instantiation in the shim: add %q to the header and run ` + "`cppgo build -scan <package>`" + `: %w", what, line, err)
	}
	cppgoMu.Lock()
	cppgoTemplateSymbols[symbol] = got
	cppgoMu.Unlock()
	return got, nil
}

// cppgoTemplateLayout resolves an instantiation's sizeof and alignof thunks,
// which is how the generic constructor allocates the object without knowing
// the type's size at generation time.
func cppgoTemplateLayout(tmpl, key, what, line string) (uintptr, uintptr, error) {
	size, err := cppgoTemplateFunc(cppgoTemplateSymbol(tmpl, "size", key), what, line)
	if err != nil {
		return 0, 0, err
	}
	align, err := cppgoTemplateFunc(cppgoTemplateSymbol(tmpl, "align", key), what, line)
	if err != nil {
		return 0, 0, err
	}
	var sizeFrame, alignFrame cppgo.Args
	cppgo.CallReentrant(size, &sizeFrame)
	cppgo.CallReentrant(align, &alignFrame)
	return sizeFrame.Ret, alignFrame.Ret, nil
}

// cppgoTemplateFrameSet fills the next argument slot of a generic call. The
// register bank depends on the concrete type argument, so it is decided here,
// at the instantiation, from reflect's view of the value.
func cppgoTemplateFrameSet(frame *cppgo.Args, ints, floats *int, value any) error {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Bool:
		if rv.Bool() {
			frame.I[*ints] = 1
		}
		*ints = *ints + 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		frame.I[*ints] = uintptr(rv.Int())
		*ints = *ints + 1
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		frame.I[*ints] = uintptr(rv.Uint())
		*ints = *ints + 1
	case reflect.Float32, reflect.Float64:
		frame.F[*floats] = rv.Float()
		*floats = *floats + 1
	case reflect.Pointer, reflect.UnsafePointer:
		frame.I[*ints] = uintptr(rv.Pointer())
		*ints = *ints + 1
	default:
		return fmt.Errorf("cppgo: the template argument %s is not carried yet; instantiate with a fixed-width scalar", rv.Type())
	}
	return nil
}

// cppgoTemplateSymbols caches the thunk addresses one instantiation resolved.
// A generated package opens one shim, so the cache is package state exactly
// like cppgoLoaded.
var cppgoTemplateSymbols = map[string]uintptr{}

// cppgoCxxType names the C++ type of one Go type argument, for the line the
// missing-instantiation error prints.
func cppgoCxxType(goType string) (string, bool) {
	switch goType {
	case "bool":
		return "bool", true
	case "int8":
		return "std::int8_t", true
	case "uint8":
		return "std::uint8_t", true
	case "int16":
		return "std::int16_t", true
	case "uint16":
		return "std::uint16_t", true
	case "int32":
		return "std::int32_t", true
	case "uint32":
		return "std::uint32_t", true
	case "int64":
		return "std::int64_t", true
	case "uint64":
		return "std::uint64_t", true
	case "float32":
		return "float", true
	case "float64":
		return "double", true
	}
	if strings.HasPrefix(goType, "*") {
		if elem, ok := cppgoCxxType(goType[1:]); ok {
			return elem + " *", true
		}
	}
	return "", false
}

`

// hasDimensions reports whether any generic declaration has a non-type
// parameter, which is what makes the package declare the phantom dimension
// types once.
func (m *packageModel) hasDimensions() bool {
	for _, tmpl := range m.templates {
		if tmpl.hasDimensions() {
			return true
		}
	}
	return false
}

// dimensionHelpers is the phantom side of a non-type template parameter. Go
// has no value parameters, so a fixed dimension is a type whose only job is to
// name the C++ value; Matrix[float64, D4, D4] is the generic form of
// Matrix<double, 4, 4>. Dynamic is the contract's runtime dimension (-1).
const dimensionHelpers = `
// cppgoDimension is the constraint of a non-type template parameter: a
// phantom type that names the C++ value a fixed dimension instantiates.
type cppgoDimension interface {
	cppgoDimension() string
}

// cppgoDimensionCxx is the C++ value of one dimension type.
func cppgoDimensionCxx[D cppgoDimension]() string {
	var dimension D
	return dimension.cppgoDimension()
}

type D1 struct{}

func (D1) cppgoDimension() string { return "1" }

type D2 struct{}

func (D2) cppgoDimension() string { return "2" }

type D3 struct{}

func (D3) cppgoDimension() string { return "3" }

type D4 struct{}

func (D4) cppgoDimension() string { return "4" }

type D5 struct{}

func (D5) cppgoDimension() string { return "5" }

type D6 struct{}

func (D6) cppgoDimension() string { return "6" }

type D7 struct{}

func (D7) cppgoDimension() string { return "7" }

type D8 struct{}

func (D8) cppgoDimension() string { return "8" }

// Dynamic is the runtime dimension: Eigen::Dynamic's -1.
type Dynamic struct{}

func (Dynamic) cppgoDimension() string { return "-1" }

`

// emitTemplateKeyFunctions writes the per-template key and C++ argument
// helpers: the join of every type parameter, in declaration order.
func (m *packageModel) emitTemplateKeyFunctions(b *strings.Builder, tmpl *templateModel) {
	fmt.Fprintf(b, "// cppgo%sKey is the instantiation key of %s.\n", tmpl.GoName, tmpl.CxxName)
	fmt.Fprintf(b, "func cppgo%sKey%s() string {\n", tmpl.GoName, tmpl.declTypeParams())
	fmt.Fprintf(b, "\treturn %s\n}\n\n", strings.Join(templateKeys(tmpl), ` + "," + `))
	fmt.Fprintf(b, "// cppgo%sArgs names the C++ type arguments of %s.\n", tmpl.GoName, tmpl.CxxName)
	fmt.Fprintf(b, "func cppgo%sArgs%s() (string, bool) {\n", tmpl.GoName, tmpl.declTypeParams())
	b.WriteString("\tparts := make([]string, 0, " + itoa(len(tmpl.Params)) + ")\n")
	keys := templateKeys(tmpl)
	for i, param := range tmpl.Params {
		if param.Kind == templateParamDimension {
			fmt.Fprintf(b, "\tparts = append(parts, cppgoDimensionCxx[%s]())\n", param.GoName)
			continue
		}
		fmt.Fprintf(b, "\targ, ok := cppgoCxxType(%s)\n", keys[i])
		b.WriteString("\tif !ok {\n")
		fmt.Fprintf(b, "\t\treturn %s, false\n\t}\n", keys[i])
		b.WriteString("\tparts = append(parts, arg)\n")
	}
	b.WriteString("\treturn strings.Join(parts, \", \"), true\n}\n\n")
}

// templateKeys are the key expressions of one template's type parameters.
func templateKeys(tmpl *templateModel) []string {
	keys := make([]string, len(tmpl.Params))
	for i, param := range tmpl.Params {
		keys[i] = "cppgoTemplateKey[" + param.GoName + "]()"
	}
	return keys
}

// emitTemplateKeyPrologue writes the key, the C++ argument list and the shim
// line every generic wrapper starts from.
func (m *packageModel) emitTemplateKeyPrologue(b *strings.Builder, tmpl *templateModel) {
	fmt.Fprintf(b, "\tkey := cppgo%sKey%s()\n", tmpl.GoName, tmpl.typeParams())
	fmt.Fprintf(b, "\tcxxArgs, _ := cppgo%sArgs%s()\n", tmpl.GoName, tmpl.typeParams())
	fmt.Fprintf(b, "\twhat := %q + cxxArgs + %q\n", tmpl.CxxName+"<", ">")
	fmt.Fprintf(b, "\tline := cppgoTemplateLine(%q, cxxArgs)\n", tmpl.CxxName)
}

// emitTemplateType writes one generic Go type, its constructors, Close and
// methods.
func (m *packageModel) emitTemplateType(b *strings.Builder, tmpl *templateModel) {
	recv := receiverName(tmpl.GoName)
	fmt.Fprintf(b, "\n// %s is %s<%s>: the generic API. The concrete specializations are the ones\n", tmpl.GoName, tmpl.CxxName, paramNames(tmpl))
	fmt.Fprintf(b, "// the program instantiates; `cppgo build` collects them and emits the shim.\n")
	fmt.Fprintf(b, "type %s[%s] struct {\n\tobj *cppgo.Object\n}\n\n", tmpl.GoName, tmpl.goParams())
	fmt.Fprintf(b, "// cppgoAddr is the object address handed to C++, or 0 for a nil or closed value.\n")
	fmt.Fprintf(b, "func (%s *%s%s) cppgoAddr() uintptr {\n", recv, tmpl.GoName, tmpl.typeParams())
	fmt.Fprintf(b, "\tif %s == nil || %s.obj == nil {\n\t\treturn 0\n\t}\n\treturn %s.obj.Addr()\n}\n\n", recv, recv, recv)
	m.emitTemplateKeyFunctions(b, tmpl)
	for _, ctor := range tmpl.Ctors {
		m.emitTemplateConstructor(b, tmpl, ctor)
	}
	if tmpl.Dtor != nil {
		m.emitTemplateClose(b, tmpl)
	} else {
		fmt.Fprintf(b, `// Close releases the object storage.
func (%s *%s%s) Close() error {
	if %s == nil || %s.obj == nil {
		return cppgo.ErrClosed
	}
	obj := %s.obj
	%s.obj = nil
	obj.Release()
	return nil
}

`, recv, tmpl.GoName, tmpl.typeParams(), recv, recv, recv, recv)
	}
	for _, method := range tmpl.Methods {
		m.emitTemplateMethod(b, tmpl, method)
	}
	for _, static := range tmpl.Statics {
		m.emitTemplateStatic(b, tmpl, static)
	}
}

// paramNames renders the C++ template parameters for a doc comment.
func paramNames(tmpl *templateModel) string {
	names := make([]string, len(tmpl.Params))
	for i, param := range tmpl.Params {
		names[i] = param.Name
	}
	return strings.Join(names, ", ")
}

// emitTemplateConstructor writes New<Type>[T...].
func (m *packageModel) emitTemplateConstructor(b *strings.Builder, tmpl *templateModel, ctor *binding) {
	fmt.Fprintf(b, "// %s constructs a %s<%s>.\n", ctor.GoName, tmpl.CxxName, paramNames(tmpl))
	line := "//\n// C++: " + displaySignature(ctor.sym) + ", two-phase template"
	fmt.Fprintf(b, "%s\n", line)
	fmt.Fprintf(b, "func %s%s(%s) (*%s%s, error) {\n", ctor.GoName, tmpl.declTypeParams(), goParams(ctor.Params), tmpl.GoName, tmpl.typeParams())
	m.emitTemplateKeyPrologue(b, tmpl)
	b.WriteString("\tsize, align, err := cppgoTemplateLayout(")
	fmt.Fprintf(b, "%q, key, what, line)\n", tmpl.CxxName)
	b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	dtor := ""
	if tmpl.Dtor != nil {
		fmt.Fprintf(b, "\tdtor, err := cppgoTemplateFunc(cppgoTemplateSymbol(%q, %q, key), what, line)\n", tmpl.CxxName, tmpl.Dtor.ID)
		b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
		dtor = "dtor"
	}
	fmt.Fprintf(b, "\tfn, err := cppgoTemplateFunc(cppgoTemplateSymbol(%q, %q, key), what, line)\n", tmpl.CxxName, ctor.ID)
	b.WriteString("\tif err != nil {\n\t\treturn nil, err\n\t}\n")
	if dtor != "" {
		b.WriteString("\tconstructed := false\n")
	}
	b.WriteString("\tvar obj *cppgo.Object\n")
	if dtor != "" {
		b.WriteString("\tobj = cppgo.NewObject(size, align, func() {\n")
		b.WriteString("\t\tif !constructed {\n\t\t\treturn\n\t\t}\n")
		b.WriteString("\t\tvar cppgoFrame cppgo.Args\n")
		b.WriteString("\t\tcppgoFrame.I[0] = obj.Addr()\n")
		b.WriteString("\t\tcppgo.CallReentrant(dtor, &cppgoFrame)\n")
		b.WriteString("\t})\n")
	} else {
		b.WriteString("\tobj = cppgo.NewObject(size, align, nil)\n")
	}
	b.WriteString("\tvar cppgoFrame cppgo.Args\n\tints, floats := 0, 0\n\t_ = floats\n")
	b.WriteString("\tcppgoFrame.I[ints] = obj.Addr()\n\tints = ints + 1\n")
	for _, param := range ctor.Params {
		fmt.Fprintf(b, "\tif err := cppgoTemplateFrameSet(&cppgoFrame, &ints, &floats, %s); err != nil {\n\t\tobj.Release()\n\t\treturn nil, err\n\t}\n", param.Name)
	}
	b.WriteString("\tcppgo.CallReentrant(fn, &cppgoFrame)\n")
	b.WriteString("\tif cppgoStatus := int32(cppgoFrame.Ret); cppgoStatus != 0 {\n\t\tobj.Release()\n")
	fmt.Fprintf(b, "\t\treturn nil, cppgoCallError(%q)\n\t}\n", ctor.sym.QualifiedName)
	if dtor != "" {
		b.WriteString("\tconstructed = true\n")
	}
	fmt.Fprintf(b, "\treturn &%s%s{obj: obj}, nil\n}\n\n", tmpl.GoName, tmpl.typeParams())
}

// emitTemplateClose writes Close for a template that declares a destructor.
func (m *packageModel) emitTemplateClose(b *strings.Builder, tmpl *templateModel) {
	recv := receiverName(tmpl.GoName)
	fmt.Fprintf(b, "// Close destroys the %s. It returns cppgo.ErrClosed when the receiver has no live object.\n", tmpl.GoName)
	fmt.Fprintf(b, "func (%s *%s%s) Close() error {\n", recv, tmpl.GoName, tmpl.typeParams())
	fmt.Fprintf(b, "\tif %s == nil || %s.obj == nil {\n\t\treturn cppgo.ErrClosed\n\t}\n", recv, recv)
	fmt.Fprintf(b, "\tobj := %s.obj\n\t%s.obj = nil\n\tobj.Release()\n", recv, recv)
	fmt.Fprintf(b, "\treturn cppgoCloseError(%q)\n}\n\n", tmpl.Dtor.sym.QualifiedName)
}

// emitTemplateMethod writes one generic method.
func (m *packageModel) emitTemplateMethod(b *strings.Builder, tmpl *templateModel, method *binding) {
	recv := receiverName(tmpl.GoName)
	fmt.Fprintf(b, "// %s calls %s.\n", method.GoName, method.sym.QualifiedName)
	fmt.Fprintf(b, "//\n// C++: %s, two-phase template\n", displaySignature(method.sym))
	results := templateResults(method)
	fmt.Fprintf(b, "func (%s *%s%s) %s(%s)%s {\n", recv, tmpl.GoName, tmpl.typeParams(), method.GoName, goParams(method.Params), results)
	m.emitTemplateClosedCheck(b, method, recv+" == nil || "+recv+".obj == nil")
	m.emitTemplateZero(b, method)
	m.emitTemplateKeyPrologue(b, tmpl)
	fmt.Fprintf(b, "\tfn, err := cppgoTemplateFunc(cppgoTemplateSymbol(%q, %q, key), what, line)\n", tmpl.CxxName, method.ID)
	m.emitTemplateErrorReturn(b, method)
	m.emitTemplateCallBody(b, method, fmt.Sprintf("%s.obj.Addr()", recv))
	fmt.Fprintf(b, "}\n\n")
}

// emitTemplateStatic writes one static member of a class template as a
// package function, the same shape the concrete API gives a static member.
func (m *packageModel) emitTemplateStatic(b *strings.Builder, tmpl *templateModel, static *binding) {
	fmt.Fprintf(b, "\n// %s calls %s.\n", static.GoName, static.sym.QualifiedName)
	fmt.Fprintf(b, "//\n// C++: %s, two-phase template\n", displaySignature(static.sym))
	fmt.Fprintf(b, "func %s%s(%s)%s {\n", static.GoName, tmpl.declTypeParams(), goParams(static.Params), templateResults(static))
	m.emitTemplateZero(b, static)
	m.emitTemplateKeyPrologue(b, tmpl)
	fmt.Fprintf(b, "\tfn, err := cppgoTemplateFunc(cppgoTemplateSymbol(%q, %q, key), what, line)\n", tmpl.CxxName, static.ID)
	m.emitTemplateErrorReturn(b, static)
	m.emitTemplateCallBody(b, static, "")
	fmt.Fprintf(b, "}\n\n")
}

// emitTemplateFunction writes one free function template.
func (m *packageModel) emitTemplateFunction(b *strings.Builder, tmpl *templateModel) {
	fn := tmpl.Func
	m.emitTemplateKeyFunctions(b, tmpl)
	fmt.Fprintf(b, "\n// %s calls %s.\n", fn.GoName, fn.sym.QualifiedName)
	fmt.Fprintf(b, "//\n// C++: %s, two-phase template\n", displaySignature(fn.sym))
	fmt.Fprintf(b, "func %s%s(%s)%s {\n", fn.GoName, tmpl.declTypeParams(), goParams(fn.Params), templateResults(fn))
	m.emitTemplateZero(b, fn)
	m.emitTemplateKeyPrologue(b, tmpl)
	fmt.Fprintf(b, "\tfn, err := cppgoTemplateFunc(cppgoTemplateSymbol(%q, %q, key), what, line)\n", tmpl.CxxName, fn.ID)
	m.emitTemplateErrorReturn(b, fn)
	m.emitTemplateCallBody(b, fn, "")
	fmt.Fprintf(b, "}\n\n")
}

// emitTemplateClosedCheck writes the zero-value guard of a generic method.
func (m *packageModel) emitTemplateClosedCheck(b *strings.Builder, binding *binding, condition string) {
	fmt.Fprintf(b, "\tif %s {\n", condition)
	if binding.Result != nil {
		fmt.Fprintf(b, "\t\tvar cppgoZero %s\n\t\treturn cppgoZero, cppgo.ErrClosed\n", binding.Result.Go)
	} else {
		b.WriteString("\t\treturn cppgo.ErrClosed\n")
	}
	b.WriteString("\t}\n")
}

// emitTemplateZero declares the zero result a failed generic call returns.
func (m *packageModel) emitTemplateZero(b *strings.Builder, binding *binding) {
	if binding.Result != nil {
		fmt.Fprintf(b, "\tvar cppgoZero %s\n", binding.Result.Go)
	}
}

// emitTemplateErrorReturn writes the error branch of a thunk resolution.
func (m *packageModel) emitTemplateErrorReturn(b *strings.Builder, binding *binding) {
	b.WriteString("\tif err != nil {\n")
	if binding.Result != nil {
		b.WriteString("\t\treturn cppgoZero, err\n")
	} else {
		b.WriteString("\t\treturn err\n")
	}
	b.WriteString("\t}\n")
}

// templateResults renders a generic signature's result list.
func templateResults(binding *binding) string {
	if binding.Result != nil {
		return " (" + binding.Result.Go + ", error)"
	}
	return " error"
}

// emitTemplateCallBody writes the frame, the call, the status check and the
// result.
func (m *packageModel) emitTemplateCallBody(b *strings.Builder, binding *binding, this string) {
	b.WriteString("\tvar cppgoFrame cppgo.Args\n\tints, floats := 0, 0\n\t_ = floats\n")
	if this != "" {
		b.WriteString("\tcppgoFrame.I[ints] = " + this + "\n\tints = ints + 1\n")
	}
	if binding.Result != nil {
		b.WriteString("\tvar cppgoResult ")
		b.WriteString(binding.Result.Go)
		b.WriteString("\n\tcppgoFrame.I[ints] = uintptr(unsafe.Pointer(&cppgoResult))\n\tints = ints + 1\n")
	}
	for _, param := range binding.Params {
		fmt.Fprintf(b, "\tif err := cppgoTemplateFrameSet(&cppgoFrame, &ints, &floats, %s); err != nil {\n", param.Name)
		if binding.Result != nil {
			b.WriteString("\t\treturn cppgoZero, err\n")
		} else {
			b.WriteString("\t\treturn err\n")
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("\tcppgo.CallReentrant(fn, &cppgoFrame)\n")
	if binding.Result != nil {
		b.WriteString("\tcppgo.KeepAlive(&cppgoResult)\n")
	}
	b.WriteString("\tif cppgoStatus := int32(cppgoFrame.Ret); cppgoStatus != 0 {\n")
	if binding.Result != nil {
		fmt.Fprintf(b, "\t\treturn cppgoZero, cppgoCallError(%q)\n", binding.sym.QualifiedName)
	} else {
		fmt.Fprintf(b, "\t\treturn cppgoCallError(%q)\n", binding.sym.QualifiedName)
	}
	b.WriteString("\t}\n")
	if binding.Result != nil {
		b.WriteString("\treturn cppgoResult, nil\n")
	} else {
		b.WriteString("\treturn nil\n")
	}
}
