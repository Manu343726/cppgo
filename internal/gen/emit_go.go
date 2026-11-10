package gen

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// emitGo renders zz_cppgo.gen.go: the type-safe API, the generated Open, and
// the declarations of the assembly trampolines.
func (m *packageModel) emitGo() ([]byte, error) {
	var b strings.Builder
	b.WriteString(GeneratedBanner + "\n//\n//\t" + m.cfg.invocation() + "\n\n")
	fmt.Fprintf(&b, "package %s\n\n", m.name)

	usesUnsafe := m.usesUnsafe()
	b.WriteString("import (\n\t\"fmt\"\n")
	if m.hasTemplates() {
		b.WriteString("\t\"hash/fnv\"\n\t\"reflect\"\n\t\"strings\"\n")
	}
	if m.libraryName != "" {
		// Where the user's library is loaded from is a filesystem question at
		// load time: beside the shim, or wherever the deployment points.
		b.WriteString("\t\"os\"\n\t\"path/filepath\"\n")
	}
	b.WriteString("\t\"sync\"\n")
	if usesUnsafe {
		b.WriteString("\t\"unsafe\"\n")
	}
	fmt.Fprintf(&b, "\n\t%q\n)\n\n", CppgoImportPath)

	m.emitOpen(&b)
	if m.usesBool() {
		b.WriteString(`
// cppgoBool converts a Go bool to the int64 the trampoline passes; the C++
// side reads the low byte.
func cppgoBool(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
`)
	}
	if m.usesCString {
		b.WriteString(`
// cppgoCString copies s into a NUL-terminated byte slice for a C++ char *
// parameter, and returns the pointer and the slice to keep alive. A Go string
// is immutable and not NUL-terminated, so the callee never sees Go's bytes.
func cppgoCString(s string) (unsafe.Pointer, []byte) {
	cppgoBytes := make([]byte, len(s)+1)
	copy(cppgoBytes, s)
	return unsafe.Pointer(&cppgoBytes[0]), cppgoBytes
}

// cppgoGoString copies a NUL-terminated C string into a Go string. The pointer
// belongs to the library and is only read here, before the call returns.
func cppgoGoString(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}
`)
	}

	for _, record := range m.records {
		m.emitRecord(&b, record)
	}
	for _, enum := range m.enums {
		m.emitEnum(&b, enum)
	}
	m.emitArgumentBlocks(&b)
	m.emitTemplates(&b)
	m.emitPackageFunctions(&b)
	m.emitTrampolineDeclarations(&b)
	return gofmtSource(b.String())
}

// usesUnsafe reports whether the generated file mentions unsafe.Pointer or
// unsafe.Sizeof.
func (m *packageModel) usesUnsafe() bool {
	if m.hasTemplates() {
		// A generic call hands out-pointers to the frame, which is unsafe
		// whatever the instantiation.
		return true
	}
	if m.usesCString {
		return true
	}
	for _, record := range m.records {
		if record.Mirrored {
			return true
		}
	}
	for _, binding := range m.bindings {
		for _, arg := range binding.Wire {
			if arg.slotType == "unsafe.Pointer" {
				return true
			}
		}
	}
	return false
}

// usesBool reports whether any parameter or result is a C++ bool.
func (m *packageModel) usesBool() bool {
	for _, binding := range m.bindings {
		if binding.Result != nil && binding.Result.Kind == goBool {
			return true
		}
		for _, param := range binding.Params {
			if param.Type.Kind == goBool {
				return true
			}
		}
	}
	return false
}

// emitOpen writes the generated entry point and the symbol table Open resolves.
//
// Two objects are loaded, in this order: the user's library, which carries the
// direct symbols, and the shim cppgo compiled, which carries the thunks. The
// shim names the user's library in its DT_NEEDED entry, by soname and never by
// the path the build found it at. Opening the shim therefore brings the library
// in as well, and loading the library first is what makes the name resolve to
// the object the Go side already holds.
//
// The generated name is never a build path. The default deployment keeps the
// two shared objects together: the user's library beside the shim, found through
// $ORIGIN and through the directory of the path Open was given. A deployment
// with another layout sets CppgoLibraryPath or CPPGO_LIBRARY_PATH.
func (m *packageModel) emitOpen(b *strings.Builder) {
	b.WriteString(`
// Open loads the cppgo-built shim and, when the package has one, the user's
// library it was linked against, then resolves every symbol this package binds.
//
// path names the shim cppgo compiled beside this package. A path of "" uses the
// name the package was generated with. It is safe to call Open more than once;
// the first successful call wins.
func Open(path string) error { return cppgoOpen(path) }
`)

	if m.libraryName != "" {
		b.WriteString(`
// CppgoLibraryPath overrides where the user's library is loaded from, for a
// deployment that does not keep it beside the shim. It must be set before the
// first Open: the two objects are loaded once, and a later change is not
// observed.
//
// The shim names the library by its soname, so a library that declares one is
// found wherever this points. A library built without a soname is loaded from
// the path here, and the shim still needs the loader to find it by name: keep
// it beside the shim, or put its directory on LD_LIBRARY_PATH.
var CppgoLibraryPath string
`)
	}

	b.WriteString(`
const (
	// cppgoShimName is the shim cppgo compiled beside this package.
	cppgoShimName = `)
	fmt.Fprintf(b, "%q\n", ShimLibraryName(m.name))
	if m.libraryName != "" {
		b.WriteString(`	// cppgoUserLibrary is the user's library by the name the shim names in its
	// DT_NEEDED entry: its soname, or its file name when it declares none. It is
	// never the path the package was generated at.
	cppgoUserLibrary = `)
		fmt.Fprintf(b, "%q\n", m.libraryName)
		b.WriteString(`	// cppgoLibraryPathEnv is the environment override, for a deployment that
	// cannot set CppgoLibraryPath before Open.
	cppgoLibraryPathEnv = "CPPGO_LIBRARY_PATH"
`)
	}
	b.WriteString(`)

// cppgoLibraries are the two objects a generated call reaches: the shim cppgo
// built, and the user's library it was linked against.
type cppgoLibraries struct {
	user *cppgo.Library
	shim *cppgo.Library
}

var (
	cppgoMu      sync.Mutex
	cppgoLoaded  *cppgoLibraries
	cppgoOpenErr error
)

// Resolved by cppgoOpen before any binding runs.
var (
`)
	for _, binding := range m.bindings {
		fmt.Fprintf(b, "\t// %s\n\t%s uintptr\n", binding.Symbol, binding.SymbolVar)
	}
	if m.usesFree {
		b.WriteString("\t// cppgo_free\n\tcppgoFreeSym uintptr\n")
	}
	b.WriteString(")\n\n")

	b.WriteString(`// cppgoOpen loads the two objects and resolves every symbol the generated
// bindings need. A missing thunk means the shim cppgo compiled is not the one
// this package was generated from, and the message says how to rebuild it.
func cppgoOpen(path string) error {
	cppgoMu.Lock()
	defer cppgoMu.Unlock()
	if cppgoLoaded != nil {
		return nil
	}
	if path == "" && cppgoOpenErr != nil {
		return cppgoOpenErr
	}
	if path == "" {
		path = cppgoShimName
	}
	loaded, err := cppgoLoad(path)
	if err != nil {
		cppgoOpenErr = err
		return err
	}
	cppgoLoaded = loaded
	cppgoOpenErr = nil
	return nil
}

`)

	if m.libraryName != "" {
		b.WriteString(`// cppgoUserLibraryPath is where the user's library is loaded from:
// CPPGO_LIBRARY_PATH, then CppgoLibraryPath, then the directory the shim was
// found in (the default deployment keeps the two objects together), then the
// recorded name for the dynamic loader's own search.
func cppgoUserLibraryPath(shimPath string) string {
	if path := os.Getenv(cppgoLibraryPathEnv); path != "" {
		return path
	}
	if CppgoLibraryPath != "" {
		return CppgoLibraryPath
	}
	if dir := filepath.Dir(shimPath); dir != "" && dir != "." {
		beside := filepath.Join(dir, cppgoUserLibrary)
		if _, err := os.Stat(beside); err == nil {
			return beside
		}
	}
	return cppgoUserLibrary
}

`)
	}
	b.WriteString(`// cppgoLoad opens the shim and resolves every symbol from the object that
// carries it.
func cppgoLoad(shimPath string) (*cppgoLibraries, error) {
	loaded := &cppgoLibraries{}
`)
	if m.libraryName != "" {
		b.WriteString(`	// The shim names the user's library in its DT_NEEDED entry, so opening the
	// shim has already brought it in. A deployment that keeps the library
	// somewhere else also gets an explicit handle, which is what a direct
	// symbol is resolved from; a handle is not required, because the shim's own
	// dependency graph covers the same symbols.
	if user, err := cppgo.Open(cppgoUserLibraryPath(shimPath)); err == nil {
		loaded.user = user
	}
`)
	}
	b.WriteString(`	shim, err := cppgo.Open(shimPath)
	if err != nil {
		return nil, fmt.Errorf("cppgo: open the shim %s: %w", shimPath, err)
	}
	loaded.shim = shim
	for _, symbol := range []struct {
		name string
		addr *uintptr
		shim bool
	}{
`)
	for _, binding := range m.bindings {
		fmt.Fprintf(b, "\t\t{name: %q, addr: &%s, shim: %t},\n", binding.Symbol, binding.SymbolVar, binding.Carrier.shimCarried())
	}
	if m.usesFree {
		b.WriteString("\t\t{name: \"cppgo_free\", addr: &cppgoFreeSym, shim: true},\n")
	}
	b.WriteString(`	} {
		lib := loaded.user
		if symbol.shim || lib == nil {
			lib = loaded.shim
		}
		addr, err := lib.Symbol(symbol.name)
		if err != nil {
			cppgoUnload(loaded)
			if symbol.shim {
				err = fmt.Errorf("cppgo: %w: the shim cppgo compiled is stale; run cppgo gen again", err)
			}
			return nil, err
		}
		*symbol.addr = addr
	}
	return loaded, nil
}

// cppgoUnload closes whatever a failed load opened.
func cppgoUnload(loaded *cppgoLibraries) {
	if loaded.user != nil {
		_ = loaded.user.Close()
	}
	if loaded.shim != nil {
		_ = loaded.shim.Close()
	}
}

// cppgoCallError reads the message a thunk stored and wraps it in the error the
// binding returns.
func cppgoCallError(symbol string) error {
	cppgoMu.Lock()
	shim := cppgoLoaded.shim
	cppgoMu.Unlock()
	err := cppgo.LastError(shim)
	if err == nil {
		err = fmt.Errorf("the call failed without a message")
	}
	return &cppgo.CallError{Symbol: symbol, Err: err}
}

// cppgoCloseError is the destructor's error: nil when the destructor stored no
// message, a CallError when it did.
func cppgoCloseError(symbol string) error {
	cppgoMu.Lock()
	shim := cppgoLoaded.shim
	cppgoMu.Unlock()
	if err := cppgo.LastError(shim); err != nil {
		return &cppgo.CallError{Symbol: symbol, Err: err}
	}
	return nil
}
`)
}

// emitRecord writes one record's Go type and its members.
func (m *packageModel) emitRecord(b *strings.Builder, record *recordModel) {
	b.WriteString("\n")
	if record.Mirrored {
		m.emitMirroredType(b, record)
	} else {
		m.emitOpaqueType(b, record)
	}
	for _, ctor := range record.Ctors {
		m.emitConstructor(b, record, ctor)
	}
	if record.Mirrored {
		m.emitMirroredConstructors(b, record)
	}
	if record.Clone != nil {
		m.emitClone(b, record, record.Clone)
	}
	if record.Dtor != nil {
		m.emitClose(b, record, record.Dtor)
	} else if !record.Mirrored {
		m.emitStorageOnlyClose(b, record)
	}
	for _, method := range record.Methods {
		m.emitCall(b, record, method)
	}
}

// emitMirroredType writes the Go struct whose layout is the C++ layout, and the
// compile-time assertions that prove it.
func (m *packageModel) emitMirroredType(b *strings.Builder, record *recordModel) {
	sort.Slice(record.Fields, func(i, j int) bool { return record.Fields[i].Offset < record.Fields[j].Offset })
	var offsets []string
	for _, field := range record.Fields {
		offsets = append(offsets, fmt.Sprintf("%s at offset %d", field.GoName, field.Offset))
	}
	fmt.Fprintf(b, "%s\n", docBlock(record.GoName, fmt.Sprintf("%s is %s: a POD, %d bytes, %s.", record.GoName, record.CxxName, record.Size, strings.Join(offsets, ", ")), record.Comment))
	fmt.Fprintf(b, "type %s struct {\n", record.GoName)
	for _, field := range record.Fields {
		if doc := commentLines(field.Comment); doc != "" {
			fmt.Fprintf(b, "%s", indentLines(doc, "\t"))
		}
		fmt.Fprintf(b, "\t%s %s\n", field.GoName, field.Type.Go)
	}
	b.WriteString("}\n\n")
	b.WriteString("// The C++ layout is proven at compile time: a wrong size, alignment or\n// field offset does not compile.\n")
	fmt.Fprintf(b, "var _ [%d]byte = [unsafe.Sizeof(%s{})]byte{}\n", record.Size, record.GoName)
	fmt.Fprintf(b, "var _ [%d]byte = [unsafe.Alignof(%s{})]byte{}\n", record.Align, record.GoName)
	for _, field := range record.Fields {
		fmt.Fprintf(b, "var _ [%d]byte = [unsafe.Offsetof(%s{}.%s)]byte{}\n", field.Offset, record.GoName, field.GoName)
	}
}

// destroyNote is what Close does to the object, stated for the record the
// sentence is written about. A record that declares a destructor has Close run
// it; one that declares none releases the storage and runs nothing, because
// there is no destructor to call.
func destroyNote(record *recordModel) string {
	if record.Dtor != nil {
		return "Close destroys it"
	}
	return "Close releases the storage and runs no C++ destructor"
}

// emitOpaqueType writes the Go value that owns C++ storage.
func (m *packageModel) emitOpaqueType(b *strings.Builder, record *recordModel) {
	fmt.Fprintf(b, "%s\n", docBlock(record.GoName, fmt.Sprintf("%s is %s (sizeof %d). The object belongs to this Go value: obj is where it lives, %s, and a value obtained by value is a copy the shim constructed here.", record.GoName, record.CxxName, record.Size, destroyNote(record)), record.Comment))
	fmt.Fprintf(b, "type %s struct {\n\tobj *cppgo.Object\n\t// view is the address of this value inside the object, 0 for the object itself.\n\taddr uintptr\n}\n\n", record.GoName)
	fmt.Fprintf(b, `// cppgoAddr is the object address handed to C++, or 0 for a nil or closed
// value.
func (%s *%s) cppgoAddr() uintptr {
	if %s == nil || %s.obj == nil {
		return 0
	}
	if %s.addr != 0 {
		return %s.addr
	}
	return %s.obj.Addr()
}
`, receiverName(record.GoName), record.GoName, receiverName(record.GoName), receiverName(record.GoName), receiverName(record.GoName), receiverName(record.GoName), receiverName(record.GoName))
}

// emitMirroredConstructors writes New<Type>() and Clone() for a record whose Go
// value is its C layout: both are ordinary Go operations.
func (m *packageModel) emitMirroredConstructors(b *strings.Builder, record *recordModel) {
	if !record.DefaultConstructible {
		return
	}
	fmt.Fprintf(b, "\n// New%s returns a zero %s.\n", record.GoName, record.GoName)
	fmt.Fprintf(b, "func New%s() (*%s, error) { return &%s{}, nil }\n", record.GoName, record.GoName, record.GoName)
	if !record.CopyConstructible {
		return
	}
	recv := receiverName(record.GoName)
	fmt.Fprintf(b, "\n// Clone copies the value.\n")
	fmt.Fprintf(b, "func (%s *%s) Clone() (*%s, error) {\n", recv, record.GoName, record.GoName)
	fmt.Fprintf(b, "\tif %s == nil {\n\t\treturn nil, cppgo.ErrClosed\n\t}\n", recv)
	fmt.Fprintf(b, "\tclone := *%s\n\treturn &clone, nil\n}\n", recv)
}

// emitConstructor writes one factory constructor.
func (m *packageModel) emitConstructor(b *strings.Builder, record *recordModel, ctor *binding) {
	ctx := callContext{thisExpr: "obj.Addr()", keepThis: "obj"}
	doc := fmt.Sprintf("New%s constructs a %s.", strings.TrimPrefix(ctor.GoName, "New"), record.CxxName)
	m.emitBindingDoc(b, ctor, doc)
	fmt.Fprintf(b, "func %s(%s) (*%s, error) {\n", ctor.GoName, goParams(ctor.Params), record.GoName)
	for _, decl := range ctor.stdDecls {
		fmt.Fprintf(b, "\t%s\n", decl)
	}
	b.WriteString("\tif err := cppgoOpen(\"\"); err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\tcppgo.ProbeStack()\n")
	hasDtor := record.Dtor != nil
	if hasDtor {
		b.WriteString("\tconstructed := false\n")
	}
	b.WriteString("\tvar obj *cppgo.Object\n")
	if hasDtor {
		fmt.Fprintf(b, "\tobj = cppgo.NewObject(%d, %d, func() {\n", record.Size, record.Align)
		b.WriteString("\t\tif !constructed {\n\t\t\treturn\n\t\t}\n")
		b.WriteString("\t\tcppgo.ProbeStack()\n")
		fmt.Fprintf(b, "\t\t%s(%s, obj.Addr())\n", record.Dtor.Trampoline, record.Dtor.SymbolVar)
		b.WriteString("\t})\n")
	} else {
		fmt.Fprintf(b, "\tobj = cppgo.NewObject(%d, %d, nil)\n", record.Size, record.Align)
	}
	for _, assignment := range m.blockAssignments(ctor, ctx) {
		fmt.Fprintf(b, "\t%s\n", assignment)
	}
	call := ctor.Trampoline + "(" + strings.Join(m.callArgs(ctor, ctx), ", ") + ")"
	if ctor.Status {
		fmt.Fprintf(b, "\tif cppgoStatus := %s; cppgoStatus != 0 {\n", call)
		b.WriteString("\t\tobj.Release()\n")
		fmt.Fprintf(b, "\t\treturn nil, cppgoCallError(%q)\n\t}\n", ctor.sym.QualifiedName)
	} else {
		fmt.Fprintf(b, "\t%s\n", call)
	}
	if len(ctor.cstringKeeps) > 0 {
		fmt.Fprintf(b, "\tcppgo.KeepAlive(%s)\n", strings.Join(ctor.cstringKeeps, ", "))
	}
	if hasDtor {
		b.WriteString("\tconstructed = true\n")
	}
	fmt.Fprintf(b, "\treturn &%s{obj: obj}, nil\n}\n", record.GoName)
}

// emitClone writes the copy constructor binding as Clone().
func (m *packageModel) emitClone(b *strings.Builder, record *recordModel, clone *binding) {
	recv := receiverName(record.GoName)
	ctx := callContext{thisExpr: "obj.Addr()", keepThis: "obj", recvExpr: recv + ".cppgoAddr()", recvKeep: recv}
	m.emitBindingDoc(b, clone, clone.GoName+" copies the object through its copy constructor.")
	fmt.Fprintf(b, "func (%s *%s) %s() (*%s, error) {\n", recv, record.GoName, clone.GoName, record.GoName)
	fmt.Fprintf(b, "\tif %s == nil || %s.obj == nil {\n\t\treturn nil, cppgo.ErrClosed\n\t}\n", recv, recv)
	b.WriteString("\tif err := cppgoOpen(\"\"); err != nil {\n\t\treturn nil, err\n\t}\n")
	b.WriteString("\tcppgo.ProbeStack()\n")
	hasDtor := record.Dtor != nil
	if hasDtor {
		b.WriteString("\tconstructed := false\n")
	}
	b.WriteString("\tvar obj *cppgo.Object\n")
	if hasDtor {
		fmt.Fprintf(b, "\tobj = cppgo.NewObject(%d, %d, func() {\n", record.Size, record.Align)
		b.WriteString("\t\tif !constructed {\n\t\t\treturn\n\t\t}\n")
		b.WriteString("\t\tcppgo.ProbeStack()\n")
		fmt.Fprintf(b, "\t\t%s(%s, obj.Addr())\n", record.Dtor.Trampoline, record.Dtor.SymbolVar)
		b.WriteString("\t})\n")
	} else {
		fmt.Fprintf(b, "\tobj = cppgo.NewObject(%d, %d, nil)\n", record.Size, record.Align)
	}
	// A copy constructor takes one argument — the object being copied — so it
	// can never need an argument block.
	call := clone.Trampoline + "(" + strings.Join(m.callArgs(clone, ctx), ", ") + ")"
	if clone.Status {
		fmt.Fprintf(b, "\tcppgoStatus := %s\n", call)
		m.emitKeepAlive(b, clone, ctx)
		b.WriteString("\tif cppgoStatus != 0 {\n")
		b.WriteString("\t\tobj.Release()\n")
		fmt.Fprintf(b, "\t\treturn nil, cppgoCallError(%q)\n\t}\n", clone.sym.QualifiedName)
	} else {
		fmt.Fprintf(b, "\t%s\n", call)
		m.emitKeepAlive(b, clone, ctx)
	}
	if hasDtor {
		b.WriteString("\tconstructed = true\n")
	}
	fmt.Fprintf(b, "\treturn &%s{obj: obj}, nil\n}\n", record.GoName)
}

// emitClose writes the destructor method.
func (m *packageModel) emitClose(b *strings.Builder, record *recordModel, dtor *binding) {
	recv := receiverName(record.GoName)
	m.emitBindingDoc(b, dtor, "Close destroys the object. It returns cppgo.ErrClosed when the receiver has no live object.")
	fmt.Fprintf(b, "func (%s *%s) Close() error {\n", recv, record.GoName)
	fmt.Fprintf(b, "\tif %s == nil || %s.obj == nil {\n\t\treturn cppgo.ErrClosed\n\t}\n", recv, recv)
	fmt.Fprintf(b, "\tobj := %s.obj\n\t%s.obj = nil\n\tobj.Release()\n", recv, recv)
	fmt.Fprintf(b, "\treturn cppgoCloseError(%q)\n}\n", dtor.sym.QualifiedName)
}

// emitStorageOnlyClose writes Close for an opaque record that declares no
// destructor: only the storage is released.
func (m *packageModel) emitStorageOnlyClose(b *strings.Builder, record *recordModel) {
	recv := receiverName(record.GoName)
	fmt.Fprintf(b, `
// Close releases the object storage. It returns cppgo.ErrClosed when the
// receiver has no live object.
func (%s *%s) Close() error {
	if %s == nil || %s.obj == nil {
		return cppgo.ErrClosed
	}
	obj := %s.obj
	%s.obj = nil
	obj.Release()
	return nil
}
`, recv, record.GoName, recv, recv, recv, recv)
}

// emitPackageFunctions writes the static members and free functions.
func (m *packageModel) emitPackageFunctions(b *strings.Builder) {
	var funcs []*binding
	for _, binding := range m.bindings {
		if binding.Owner == nil || binding.IsStatic {
			funcs = append(funcs, binding)
		}
	}
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].GoName < funcs[j].GoName })
	for _, binding := range funcs {
		m.emitCall(b, nil, binding)
	}
}

// callContext carries the expressions a call needs that depend on the binding's
// Go receiver.
type callContext struct {
	thisExpr string
	keepThis string
	recvExpr string
	recvKeep string
}

// callArgs renders the trampoline arguments, including the implicit this and a
// clone's hidden source.
func (m *packageModel) callArgs(binding *binding, ctx callContext) []string {
	return append([]string{binding.SymbolVar}, m.wireExprs(binding, ctx)...)
}

// wireExprs renders the argument expressions in wire order, with the implicit
// this and a clone's hidden source filled in from the Go receiver.
func (m *packageModel) wireExprs(binding *binding, ctx callContext) []string {
	exprs := make([]string, 0, len(binding.Wire))
	cloneSource := binding.IsClone
	for _, arg := range binding.Wire {
		switch {
		case arg.label == "this":
			exprs = append(exprs, ctx.thisExpr)
		case cloneSource:
			exprs = append(exprs, ctx.recvExpr)
			cloneSource = false
		default:
			exprs = append(exprs, arg.expr)
		}
	}
	return exprs
}

// emitReentrant writes a call that enters C through runtime.cgocall: every Go
// function the signature takes is registered in the callback table first, the
// argument frame is filled from the wire words, the call is made, and the
// recorded panic, the status and the result are read back in that order.
func (m *packageModel) emitReentrant(b *strings.Builder, binding *binding, ctx callContext) {
	for _, arg := range binding.Wire {
		if arg.callable != nil {
			m.emitCallbackRegistration(b, binding, arg)
		}
	}
	if binding.Out != nil && binding.outPointer {
		fmt.Fprintf(b, "\tcppgoResult := new(%s)\n", binding.OutLocal)
	}
	b.WriteString("\tvar cppgoFrame cppgo.Args\n")
	// The frame is filled from the same expressions the NOSPLIT trampoline
	// would load, in wire order, into the two register banks.
	exprs := m.wireExprs(binding, ctx)
	ints, floats := 0, 0
	for i, arg := range binding.Wire {
		if arg.class == classFloat {
			fmt.Fprintf(b, "\tcppgoFrame.F[%d] = float64(%s)\n", floats, exprs[i])
			floats++
			continue
		}
		fmt.Fprintf(b, "\tcppgoFrame.I[%d] = uintptr(%s)\n", ints, exprs[i])
		ints++
	}
	fmt.Fprintf(b, "\tcppgo.CallReentrant(%s, &cppgoFrame)\n", binding.SymbolVar)
	m.emitKeepAlive(b, binding, ctx)
	b.WriteString("\tif cppgoPanic := cppgo.TakeCallbackPanic(); cppgoPanic != nil {\n\t\tpanic(cppgoPanic.Value)\n\t}\n")
	if !binding.Status {
		switch {
		case binding.Out != nil:
			fmt.Fprintf(b, "\treturn %s\n", convertResult(binding, "*cppgoResult"))
		case binding.Result != nil:
			fmt.Fprintf(b, "\treturn %s\n", reentrantResult(binding))
		}
		return
	}
	b.WriteString("\tif cppgoStatus := int32(cppgoFrame.Ret); cppgoStatus != 0 {\n")
	fmt.Fprintf(b, "\t\treturn %scppgoCallError(%q)\n\t}\n", errorZeroPrefix(binding), binding.sym.QualifiedName)
	// A status thunk with a value always carries it through the out-pointer the
	// caller owns, so what is left is that pointer or no result at all.
	if binding.Out != nil {
		fmt.Fprintf(b, "\treturn %s, nil\n", convertResult(binding, "*cppgoResult"))
		return
	}
	b.WriteString("\treturn nil\n")
}

// emitCallbackRegistration writes one Go function into the callback table. The
// closure marshals the C argument frame to the Go signature and the Go result
// back to the one result register the ABI carries.
func (m *packageModel) emitCallbackRegistration(b *strings.Builder, binding *binding, arg *wireArg) {
	index := 0
	for _, other := range binding.Wire {
		if other == arg {
			break
		}
		if other.callable != nil {
			index++
		}
	}
	callable := arg.callable
	args := make([]string, len(callable.Params))
	ints, floats := 0, 0
	for i, param := range callable.Params {
		switch param.Kind {
		case goFloat:
			args[i] = fmt.Sprintf("cppgoFrame.F[%d]", floats)
			floats++
		case goBool:
			args[i] = fmt.Sprintf("cppgoFrame.I[%d] != 0", ints)
			ints++
		default:
			args[i] = fmt.Sprintf("%s(cppgoFrame.I[%d])", param.Go, ints)
			ints++
		}
	}
	invoke := arg.callbackParam + "(" + strings.Join(args, ", ") + ")"
	fmt.Fprintf(b, "\tcppgoCallback%d, cppgoCallbackErr := cppgo.RegisterCallback(func(cppgoFrame *cppgo.Args) {\n", index)
	// The registration's own error is handled before the call; a nil function
	// is the only way a fresh registration fails, and the table is 64 slots.
	switch {
	case callable.Result == nil:
		fmt.Fprintf(b, "\t\t%s\n", invoke)
	case callable.Result.Kind == goBool:
		fmt.Fprintf(b, "\t\tif %s {\n\t\t\tcppgoFrame.Ret = 1\n\t\t}\n", invoke)
	case callable.Result.Kind == goFloat:
		fmt.Fprintf(b, "\t\tcppgoFrame.RetF = %s\n", invoke)
	case unsignedType(callable.Result):
		fmt.Fprintf(b, "\t\tcppgoFrame.Ret = uintptr(%s)\n", invoke)
	default:
		fmt.Fprintf(b, "\t\tcppgoFrame.Ret = uintptr(int64(%s))\n", invoke)
	}
	fmt.Fprintf(b, "\t})\n")
	if binding.ErrorResult {
		fmt.Fprintf(b, "\tif cppgoCallbackErr != nil {\n\t\treturn %scppgoCallbackErr\n\t}\n", errorZeroPrefix(binding))
	} else {
		b.WriteString("\tif cppgoCallbackErr != nil {\n\t\tpanic(cppgoCallbackErr)\n\t}\n")
	}
	fmt.Fprintf(b, "\tdefer cppgo.ReleaseCallback(cppgoCallback%d)\n", index)
}

// unsignedType reports whether a mapped integer has no sign bit to extend. The
// Go name is the evidence: a template argument's element type carries no
// libclang parameter, so its width and signedness are the mapped spelling.
func unsignedType(t *goType) bool { return strings.HasPrefix(t.Go, "uint") }

// reentrantResult reads a value result out of the argument frame: the INTEGER
// result from RAX and the floating one from XMM0.
func reentrantResult(binding *binding) string {
	result := binding.Result
	switch result.Kind {
	case goBool:
		// bool returns in AL; the rest of RAX is not part of the value.
		return "cppgoFrame.Ret&0xff != 0"
	case goFloat:
		return "cppgoFrame.RetF"
	case goPointer:
		return "(*" + strings.TrimPrefix(result.Go, "*") + ")(unsafe.Pointer(cppgoFrame.Ret))"
	default:
		return result.Go + "(cppgoFrame.Ret)"
	}
}

// emitCall writes one function or method wrapper.
func (m *packageModel) emitCall(b *strings.Builder, record *recordModel, binding *binding) {
	recv := ""
	ctx := callContext{}
	if record != nil {
		recv = receiverName(record.GoName)
		if record.Mirrored {
			ctx = callContext{thisExpr: "uintptr(unsafe.Pointer(" + recv + "))", keepThis: recv}
		} else {
			ctx = callContext{thisExpr: recv + ".cppgoAddr()", keepThis: recv}
		}
	}
	m.emitBindingDoc(b, binding, docLine(record, binding))
	if record != nil {
		fmt.Fprintf(b, "func (%s *%s) %s(%s)%s {\n", recv, record.GoName, binding.GoName, goParams(binding.Params), goResults(binding))
		fmt.Fprintf(b, "\tif %s == nil", recv)
		if !record.Mirrored {
			fmt.Fprintf(b, " || %s.obj == nil", recv)
		}
		b.WriteString(" {\n")
		if binding.ErrorResult {
			fmt.Fprintf(b, "\t\treturn %scppgo.ErrClosed\n", errorZeroPrefix(binding))
			b.WriteString("\t}\n")
		} else {
			fmt.Fprintf(b, "\t\tpanic(%q)\n\t}\n", "cppgo: "+displaySignature(binding.sym)+" on a closed "+record.GoName)
		}
	} else {
		fmt.Fprintf(b, "func %s(%s)%s {\n", binding.GoName, goParams(binding.Params), goResults(binding))
	}
	// A length parameter shared by several slices is one C++ argument: the Go
	// caller must not hand over differently sized arrays.
	m.emitLengthChecks(b, binding)
	m.emitByValueNilChecks(b, binding)

	for _, decl := range binding.stdDecls {
		fmt.Fprintf(b, "\t%s\n", decl)
	}
	if err := cppgoOpenCode(binding, "\t"); err != "" {
		b.WriteString(err)
	}
	b.WriteString("\tcppgo.ProbeStack()\n")
	for _, assignment := range m.blockAssignments(binding, ctx) {
		fmt.Fprintf(b, "\t%s\n", assignment)
	}
	if binding.Carrier == reentrant {
		m.emitReentrant(b, binding, ctx)
		b.WriteString("}\n")
		return
	}
	call := binding.Trampoline + "(" + strings.Join(m.callArgs(binding, ctx), ", ") + ")"
	switch {
	case binding.Status:
		// A status thunk delivers its value through the out-pointer and, when
		// the binding has one, reports the failure as an error. A status thunk
		// always has an error result: it exists because the call can throw.
		if binding.OutObject != nil {
			m.emitObjectResult(b, binding, call, ctx)
			break
		}
		if binding.Out != nil {
			fmt.Fprintf(b, "\tvar cppgoResult %s\n", binding.OutLocal)
		}
		fmt.Fprintf(b, "\tcppgoStatus := %s\n", call)
		m.emitKeepAlive(b, binding, ctx)
		fmt.Fprintf(b, "\tif cppgoStatus != 0 {\n\t\treturn %scppgoCallError(%q)\n\t}\n",
			errorZeroPrefix(binding), binding.sym.QualifiedName)
		m.emitStdOutCopies(b, binding)
		switch {
		case binding.StdResult:
			m.emitStdReturn(b, binding)
		case binding.ViewResult:
			m.emitViewReturn(b, binding, record, recv, "cppgoResult")
		case binding.Result != nil:
			fmt.Fprintf(b, "\treturn %s, nil\n", convertResult(binding, "cppgoResult"))
		default:
			b.WriteString("\treturn nil\n")
		}
	case binding.ViewResult:
		// A borrowed record travels through the out-pointer when the call can
		// fail and in the result register when it cannot; either way the Go
		// side turns the address into a view of the receiver.
		if binding.Out != nil {
			fmt.Fprintf(b, "\tvar cppgoResult %s\n", binding.OutLocal)
			fmt.Fprintf(b, "\t%s\n", call)
			m.emitKeepAlive(b, binding, ctx)
			m.emitViewReturn(b, binding, record, recv, "cppgoResult")
			break
		}
		fmt.Fprintf(b, "\tcppgoRet := %s\n", call)
		m.emitKeepAlive(b, binding, ctx)
		m.emitViewReturn(b, binding, record, recv, "cppgoRet")
	case binding.Out != nil:
		// A value the thunk writes into Go storage; a call that cannot fail has
		// no error to report, so the result is the signature's only return.
		fmt.Fprintf(b, "\tvar cppgoResult %s\n", binding.OutLocal)
		fmt.Fprintf(b, "\t%s\n", call)
		m.emitKeepAlive(b, binding, ctx)
		fmt.Fprintf(b, "\treturn %s\n", convertResult(binding, "cppgoResult"))
	case binding.Result != nil:
		// A thunk that cannot throw returns the value directly, and a direct
		// call has no error channel at all.
		fmt.Fprintf(b, "\tcppgoRet := %s\n", call)
		m.emitKeepAlive(b, binding, ctx)
		fmt.Fprintf(b, "\treturn %s\n", convertResult(binding, "cppgoRet"))
	default:
		fmt.Fprintf(b, "\t%s\n", call)
		m.emitKeepAlive(b, binding, ctx)
	}
	b.WriteString("}\n")
}

// emitBindingDoc writes the description and the one evidence line every binding
// carries. The C++ doc comment is the Go doc comment; the generated contract
// sentence leads when the header wrote none, or when its comment does not open
// with the Go identifier.
func (m *packageModel) emitBindingDoc(b *strings.Builder, binding *binding, description string) {
	if note := byValueNote(binding); note != "" {
		description += " " + note
	}
	fmt.Fprintf(b, "\n%s\n", docBlock(binding.GoName, description, binding.sym.Comment))
	mangled := binding.sym.Mangled
	if mangled == "" {
		mangled = "synthesized"
	}
	evidence := "//\n// C++: " + m.declaration(binding) + ", " + mangled
	evidence += " (" + binding.Carrier.String()
	if binding.Carrier.shimCarried() {
		evidence += " " + binding.Thunk
	}
	evidence += ", " + binding.Reason + ")"
	if m.verified != "" {
		evidence += ", verified against " + m.verified
	}
	fmt.Fprintf(b, "%s\n", evidence)
}

// declaration is the C++ declaration the evidence line names. A static data
// member is not a function, so it is spelled as the member itself.
func (m *packageModel) declaration(binding *binding) string {
	if binding.staticField == nil {
		return displaySignature(binding.sym)
	}
	if binding.staticWrite {
		return binding.sym.QualifiedName + " = <value>"
	}
	return binding.sym.QualifiedName
}

// docLine is the generated description of a call binding.
func docLine(record *recordModel, binding *binding) string {
	name := simpleName(binding.sym.QualifiedName)
	if binding.memberField != nil {
		if binding.fieldWrite {
			return fmt.Sprintf("%s sets %s.", binding.GoName, binding.sym.QualifiedName)
		}
		return fmt.Sprintf("%s reads %s.", binding.GoName, binding.sym.QualifiedName)
	}
	if binding.staticField != nil {
		if binding.staticWrite {
			return fmt.Sprintf("%s sets %s.", binding.GoName, binding.sym.QualifiedName)
		}
		return fmt.Sprintf("%s reads %s.", binding.GoName, binding.sym.QualifiedName)
	}
	if record != nil {
		return fmt.Sprintf("%s calls %s.", binding.GoName, record.CxxName+"::"+name)
	}
	return fmt.Sprintf("%s calls %s.", binding.GoName, binding.sym.QualifiedName)
}

// emitByValueNilChecks refuses a nil value where the C++ side takes the record
// itself. The wire word for such an argument is the object's address, and a nil
// object has none: passing 0 would hand the shim a null pointer to copy from.
// Every such binding carries an error channel (its copy runs in the shim, which
// catches), so the refusal is an error and never a panic.
func (m *packageModel) emitByValueNilChecks(b *strings.Builder, binding *binding) {
	for _, param := range binding.Params {
		if !param.Type.ByValue {
			continue
		}
		message := "cppgo: " + displaySignature(binding.sym) + ": the argument " + param.Name + " is passed by value, and a nil " + param.Type.Go + " has no C++ object to copy"
		fmt.Fprintf(b, "\tif %s == nil {\n", param.Name)
		fmt.Fprintf(b, "\t\treturn %sfmt.Errorf(%q)\n", errorZeroPrefix(binding), message)
		b.WriteString("\t}\n")
	}
}

// byValueNote is the ownership sentence every binding that crosses a record by
// value carries. It is the whole rule, stated where the caller reads it: who
// allocated the object, who destroys it, and what the copy does not promise.
func byValueNote(binding *binding) string {
	var notes []string
	if binding.OutObject != nil {
		// The record's own declaration decides the sentence: Close runs the
		// destructor when the record declares one, and releases the storage
		// alone when it does not.
		closeNote := "Close runs the C++ destructor once"
		if binding.OutObject.Dtor == nil {
			closeNote = "Close releases the storage and runs no C++ destructor"
		}
		notes = append(notes, "The returned "+binding.OutObject.GoName+" is Go-owned: the shim constructs it from the callee's result, "+closeNote+", and the type's own copy semantics decide what the copy shares with its source.")
	}
	for _, param := range binding.Params {
		if !param.Type.ByValue {
			continue
		}
		notes = append(notes, "The value of "+param.Name+" is copied into the call by the shim, so the callee never holds the Go object; the caller keeps ownership and closes it.")
	}
	for _, param := range binding.Params {
		if param.Type.CString {
			notes = append(notes, "The string "+param.Name+" crosses as a NUL-terminated copy: the callee reads it during the call and must not retain it, and the empty string is a pointer to a zero byte, never a null pointer.")
		}
	}
	if binding.Result != nil && binding.Result.CString {
		notes = append(notes, "The result is copied out of the library's own C string before the call returns, so the Go string owns its bytes.")
	}
	return strings.Join(notes, " ")
}

// emitLengthChecks writes the pre-call length comparison for every slice group
// that feeds one C++ length parameter.
func (m *packageModel) emitLengthChecks(b *strings.Builder, binding *binding) {
	seen := map[string]bool{}
	for _, param := range binding.Params {
		if len(param.Group) < 2 || seen[param.Group[0].Name] {
			continue
		}
		seen[param.Group[0].Name] = true
		first := param.Group[0].Name
		var conditions []string
		printed := []string{"len(" + first + ")"}
		verbs := []string{"%d"}
		for _, other := range param.Group[1:] {
			conditions = append(conditions, fmt.Sprintf("len(%s) != len(%s)", first, other.Name))
			printed = append(printed, "len("+other.Name+")")
			verbs = append(verbs, "%d")
		}
		message := "cppgo: " + binding.sym.QualifiedName + ": the slices must have the same length, got " + strings.Join(verbs, " and ")
		fmt.Fprintf(b, "\tif %s {\n", strings.Join(conditions, " || "))
		if binding.ErrorResult {
			fmt.Fprintf(b, "\t\treturn %sfmt.Errorf(%q, %s)\n", errorZeroPrefix(binding), message, strings.Join(printed, ", "))
		} else {
			fmt.Fprintf(b, "\t\tpanic(fmt.Sprintf(%q, %s))\n", message, strings.Join(printed, ", "))
		}
		b.WriteString("\t}\n")
	}
}

// emitObjectResult writes the delivery of a record Go cannot mirror that the
// callee returns by value. The object is Go's: its storage is allocated here,
// the shim constructs the value into it through the out-pointer, and the
// destructor the record declares runs at Close, once, through the same
// trampoline Close uses. A call that failed constructed nothing, so the release
// closure is told so and does not run a destructor over raw storage.
func (m *packageModel) emitObjectResult(b *strings.Builder, binding *binding, call string, ctx callContext) {
	record := binding.OutObject
	b.WriteString("\tvar cppgoObject *cppgo.Object\n")
	if record.Dtor != nil {
		b.WriteString("\tconstructed := false\n")
		fmt.Fprintf(b, "\tcppgoObject = cppgo.NewObject(%d, %d, func() {\n", record.Size, record.Align)
		b.WriteString("\t\tif !constructed {\n\t\t\treturn\n\t\t}\n")
		b.WriteString("\t\tcppgo.ProbeStack()\n")
		fmt.Fprintf(b, "\t\t%s(%s, cppgoObject.Addr())\n", record.Dtor.Trampoline, record.Dtor.SymbolVar)
		b.WriteString("\t})\n")
	} else {
		// A record with no declared destructor needs none: the storage is the
		// whole object, and nothing has to be undone when the call fails, so the
		// guard flag the destructor's closure uses is not declared.
		fmt.Fprintf(b, "\tcppgoObject = cppgo.NewObject(%d, %d, nil)\n", record.Size, record.Align)
	}
	fmt.Fprintf(b, "\tcppgoStatus := %s\n", call)
	m.emitKeepAlive(b, binding, ctx)
	fmt.Fprintf(b, "\tif cppgoStatus != 0 {\n\t\tcppgoObject.Release()\n\t\treturn nil, cppgoCallError(%q)\n\t}\n", binding.sym.QualifiedName)
	if record.Dtor != nil {
		b.WriteString("\tconstructed = true\n")
	}
	fmt.Fprintf(b, "\treturn &%s{obj: cppgoObject}, nil\n", record.GoName)
}

// emitViewReturn turns the address C++ returned into a view of the receiver:
// the same object, one more Go-side reference, at the address the C++ cast
// produced. That is what makes a method returning `T&` safe to bind: the object
// lives until the last view is closed, and no address is handed out that
// nothing owns.
func (m *packageModel) emitViewReturn(b *strings.Builder, binding *binding, record *recordModel, recv, local string) {
	// The view's Go type is the type the declaration returns: a field accessor
	// returns the field's record, not the receiver's.
	viewRecord := record
	if binding.Result != nil && binding.Result.Record != nil {
		viewRecord = binding.Result.Record
	}
	b.WriteString("\t// A borrowed record result is a view: the receiver's object, one more reference.\n")
	fmt.Fprintf(b, "\tif %s == nil {\n", local)
	switch {
	case binding.dynamicCast:
		// A dynamic_cast that produced null: the object is not this type.
		b.WriteString("\t\treturn nil, false\n\t}\n")
	case binding.ErrorResult:
		b.WriteString("\t\treturn nil, nil\n\t}\n")
	default:
		b.WriteString("\t\treturn nil\n\t}\n")
	}
	fmt.Fprintf(b, "\t%s.obj.Retain()\n", recv)
	switch {
	case binding.dynamicCast:
		fmt.Fprintf(b, "\treturn &%s{obj: %s.obj, addr: uintptr(%s)}, true\n", viewRecord.GoName, recv, local)
	case binding.ErrorResult:
		fmt.Fprintf(b, "\treturn &%s{obj: %s.obj, addr: uintptr(%s)}, nil\n", viewRecord.GoName, recv, local)
	default:
		fmt.Fprintf(b, "\treturn &%s{obj: %s.obj, addr: uintptr(%s)}\n", viewRecord.GoName, recv, local)
	}
}

// emitKeepAlive pins every Go allocation the call handed to C++.
func (m *packageModel) emitKeepAlive(b *strings.Builder, binding *binding, ctx callContext) {
	var names []string
	if binding.This && ctx.keepThis != "" {
		names = append(names, ctx.keepThis)
	}
	cloneSource := binding.IsClone
	for _, arg := range binding.Wire {
		if arg.label == "this" {
			continue
		}
		if cloneSource {
			cloneSource = false
			names = append(names, ctx.recvKeep)
			continue
		}
		if arg.keep != "" {
			names = append(names, arg.keep)
		}
	}
	// The values packed into the block are Go memory C++ reads after the call
	// has started, so they are pinned for as long as the block lives.
	for _, arg := range binding.Block {
		if arg.keep != "" && arg.label != "this" {
			names = append(names, arg.keep)
		}
	}
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(b, "\tcppgo.KeepAlive(%s)\n", strings.Join(names, ", "))
}

// blockAssignments is the Go code that fills an over-full call's argument
// block: one field per C++ argument, in declaration order, written with the
// expression the register version would have passed. The implicit this and a
// clone's hidden source come from the Go receiver, exactly as the wire version
// fills them.
func (m *packageModel) blockAssignments(binding *binding, ctx callContext) []string {
	if len(binding.Block) == 0 {
		return nil
	}
	out := make([]string, 0, len(binding.Block)+1)
	out = append(out, "var cppgoBlock "+binding.blockType())
	for _, arg := range binding.Block {
		expr := arg.expr
		if arg.label == "this" {
			// The receiver is the one value that comes from the Go side rather
			// than from a parameter.
			expr = ctx.thisExpr
		}
		out = append(out, "cppgoBlock."+arg.cName+" = "+expr)
	}
	return out
}

// emitArgumentBlocks writes the struct each over-full call packs its arguments
// into. The C++ side declares the same fields, of the same widths, in the same
// order, and asserts sizeof and every offset: one layout, checked by both
// compilers.
func (m *packageModel) emitArgumentBlocks(b *strings.Builder) {
	for _, binding := range m.bindings {
		if len(binding.Block) == 0 {
			continue
		}
		fmt.Fprintf(b, "\n// %s is the argument block of %s: the %d argument words the\n// register file cannot hold, in declaration order. The shim declares the same\n// fields and asserts every offset.\n",
			binding.blockType(), displaySignature(binding.sym), len(binding.Block))
		fmt.Fprintf(b, "type %s struct {\n", binding.blockType())
		for _, arg := range binding.Block {
			fmt.Fprintf(b, "\t%s %s\n", arg.cName, arg.slotType)
		}
		b.WriteString("}\n")
		fmt.Fprintf(b, "var _ [%d]byte = [unsafe.Sizeof(%s{})]byte{}\n", binding.BlockSize, binding.blockType())
		for i, arg := range binding.Block {
			fmt.Fprintf(b, "var _ [%d]byte = [unsafe.Offsetof(%s{}.%s)]byte{}\n", binding.BlockOffsets[i], binding.blockType(), arg.cName)
		}
	}
}

// emitTrampolineDeclarations declares every assembly trampoline.
func (m *packageModel) emitTrampolineDeclarations(b *strings.Builder) {
	b.WriteString("\n// Assembly trampolines. Each one reads its arguments, switches to a 16-byte\n// aligned stack for the C call, restores the Go stack pointer and only then\n// stores the result. They must not be called directly.\n")
	if m.usesFree {
		b.WriteString("\n// cppgoFree is the NOSPLIT trampoline for cppgo_free.\nfunc cppgoFree(fn uintptr, block unsafe.Pointer)\n")
	}
	for _, binding := range m.bindings {
		if binding.Carrier == reentrant {
			continue
		}
		b.WriteString("\n")
		fmt.Fprintf(b, "// %s is the NOSPLIT trampoline for %s.\n", binding.Trampoline, binding.Symbol)
		parts := make([]string, 0, len(binding.Slots))
		result := ""
		for _, slot := range binding.Slots {
			if slot.label == "ret" {
				result = " " + slot.goType
				continue
			}
			parts = append(parts, slot.label+" "+slot.goType)
		}
		fmt.Fprintf(b, "func %s(%s)%s\n", binding.Trampoline, strings.Join(parts, ", "), result)
	}
}

// goParams renders a Go parameter list.
func goParams(params []*goParam) string {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = param.Name + " " + param.Type.Go
		if param.StdOut {
			parts[i] = param.Name + " *" + param.Type.Go
		}
	}
	return strings.Join(parts, ", ")
}

// goResults renders a Go result list.
func goResults(binding *binding) string {
	switch {
	case binding.dynamicCast:
		// A derived view reports whether the object really is that type.
		return " (" + binding.Result.Go + ", bool)"
	case binding.Result != nil && binding.ErrorResult:
		return " (" + binding.Result.Go + ", error)"
	case binding.Result != nil:
		return " " + binding.Result.Go
	case binding.ErrorResult:
		return " error"
	default:
		return ""
	}
}

// errorZeroPrefix is the zero value a failed error-returning binding reports,
// plus a comma: every one of them has an error as its last result.
func errorZeroPrefix(binding *binding) string {
	if binding.Result == nil {
		return ""
	}
	return zeroValue(binding.Result) + ", "
}

// zeroValue is the Go zero of a result type.
func zeroValue(t *goType) string {
	switch t.Kind {
	case goBool:
		return "false"
	case goPointer, goOptional:
		return "nil"
	case goString:
		return `""`
	case goSlice:
		return "nil"
	case goArray, goRecord:
		return t.Go + "{}"
	case goEnum:
		return "0"
	default:
		return "0"
	}
}

// emitStdOutCopies copies a standard-library parameter the callee replaced back
// into the caller's slice and releases the block the thunk allocated. The
// caller's slice keeps its capacity, so the copy is one append over its own
// storage when it fits.
func (m *packageModel) emitStdOutCopies(b *strings.Builder, binding *binding) {
	for _, arg := range binding.allArgs() {
		if !arg.stdOutCopy {
			continue
		}
		name := arg.stdGoName
		fmt.Fprintf(b, "\tif %s != 0 {\n\t\t*%s = append((*%s)[:0], unsafe.Slice((*%s)(%s), int(%s))...)\n\t} else {\n\t\t*%s = (*%s)[:0]\n\t}\n",
			arg.stdOutSize, name, name, arg.stdElem.Go, arg.stdOutData, arg.stdOutSize, name, name)
		fmt.Fprintf(b, "\tcppgoFree(cppgoFreeSym, %s)\n", arg.stdOutData)
	}
}

// emitStdReturn copies a standard-library result out of the C++ copy and
// releases it. The bytes are copied before cppgo_free runs, so no C++ memory
// escapes the thunk.
func (m *packageModel) emitStdReturn(b *strings.Builder, binding *binding) {
	result := binding.Result
	switch result.Std {
	case stdString, stdStringView:
		b.WriteString("\tvar cppgoResult string\n")
		b.WriteString("\tif cppgoSize != 0 {\n\t\tcppgoResult = string(unsafe.Slice((*byte)(cppgoData), int(cppgoSize)))\n\t}\n")
		b.WriteString("\tcppgoFree(cppgoFreeSym, cppgoData)\n")
		b.WriteString("\treturn cppgoResult, nil\n")
	case stdVector, stdSpan:
		fmt.Fprintf(b, "\tvar cppgoResult %s\n", result.Go)
		fmt.Fprintf(b, "\tif cppgoSize != 0 {\n\t\tcppgoResult = append(cppgoResult, unsafe.Slice((*%s)(cppgoData), int(cppgoSize))...)\n\t}\n", result.Elem.Go)
		b.WriteString("\tcppgoFree(cppgoFreeSym, cppgoData)\n")
		b.WriteString("\treturn cppgoResult, nil\n")
	case stdArray:
		b.WriteString("\treturn cppgoResult, nil\n")
	case stdOptional:
		b.WriteString("\tif cppgoHas != 0 {\n\t\treturn &cppgoResult, nil\n\t}\n\treturn nil, nil\n")
	}
}

// convertResult turns the register or out-pointer value into the Go result.
func convertResult(binding *binding, local string) string {
	result := binding.Result
	if binding.Status {
		return convertFromLocal(result, local)
	}
	if result.CString {
		// The thunk wrote the library's `const char *`; the wrapper copies the
		// bytes into a Go string before the call returns.
		return "cppgoGoString(" + local + ")"
	}
	switch result.Kind {
	case goBool:
		return local + "&0xff != 0"
	case goPointer:
		if result.Go == "unsafe.Pointer" {
			// An untyped C pointer is already Go's untyped pointer; there is no
			// type to cast it to.
			return "unsafe.Pointer(" + local + ")"
		}
		return "(*" + strings.TrimPrefix(result.Go, "*") + ")(" + local + ")"
	case goInteger, goEnum:
		if result.Go == "int64" || result.Go == "uint64" {
			return local
		}
		return result.Go + "(" + local + ")"
	default:
		return local
	}
}

// convertFromLocal converts an out-pointer local, whose type is the exact C
// width, to the Go result.
func convertFromLocal(result *goType, local string) string {
	if result.CString {
		// A C string result is copied out of the library's pointer before the
		// call returns, whether it arrived through the result register or an
		// out-pointer.
		return "cppgoGoString(" + local + ")"
	}
	switch result.Kind {
	case goBool:
		return local + " != 0"
	case goPointer:
		if result.Go == "unsafe.Pointer" {
			return "unsafe.Pointer(" + local + ")"
		}
		return "(*" + strings.TrimPrefix(result.Go, "*") + ")(" + local + ")"
	default:
		return local
	}
}

// cppgoOpenCode is the prologue every wrapper runs: it loads the library and,
// when the binding has no error result, panics because a failed load is a
// configuration error the caller cannot handle.
func cppgoOpenCode(binding *binding, indent string) string {
	if binding.ErrorResult {
		return indent + "if err := cppgoOpen(\"\"); err != nil {\n" + indent + "\treturn " + errorZeroPrefix(binding) + "err\n" + indent + "}\n"
	}
	return indent + "if err := cppgoOpen(\"\"); err != nil {\n" + indent + "\tpanic(err)\n" + indent + "}\n"
}

// gofmtSource formats generated Go source.
func gofmtSource(src string) ([]byte, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("formatting generated Go source: %w", err)
	}
	return out, nil
}
