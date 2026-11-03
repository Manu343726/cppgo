package gen

import (
	"fmt"
	"go/format"
	"strings"
)

// emitGo renders zz_cppgo.gen.go: the idiomatic wrappers, the lazy library
// binding and the declarations of the assembly trampolines.
func emitGo(pkg *packageInfo, sigs []*signature) ([]byte, error) {
	var b strings.Builder
	b.WriteString(GeneratedBanner + "\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg.Name)

	usesUnsafe, usesBool := false, false
	needsAlloc, needsRelease := false, false
	for _, sig := range sigs {
		if sig.usesUnsafe() {
			usesUnsafe = true
		}
		for _, p := range sig.fn.Params {
			if p.Info.kind == kindBool {
				usesBool = true
			}
		}
		if sig.abi {
			switch sig.fn.Kind {
			case bindCtor:
				needsAlloc = true
				usesUnsafe = true
			case bindDtor:
				needsRelease = true
			}
		}
	}

	b.WriteString("import (\n\t\"fmt\"\n\t\"sync\"\n")
	if usesUnsafe {
		b.WriteString("\t\"unsafe\"\n")
	}
	fmt.Fprintf(&b, "\n\t%q\n)\n\n", CppgoImportPath)

	b.WriteString(`// CppgoLibraryPath overrides the shared library opened by this package's
// generated bindings. It is read once, on the first call; leave it empty to
// use the name given to the //cppgo:library directive.
var CppgoLibraryPath string

`)
	fmt.Fprintf(&b, "// cppgoLibraryName is the library named by //cppgo:library.\nconst cppgoLibraryName = %q\n\n", pkg.Library)

	b.WriteString("var (\n\tcppgoOnce sync.Once\n\tcppgoLib  *cppgo.Library\n\tcppgoErr  error\n\n")
	for _, f := range pkg.Funcs {
		fmt.Fprintf(&b, "\t// %s is the resolved address of the %q symbol.\n", f.symVar(), f.lookupName())
		fmt.Fprintf(&b, "\t%s uintptr\n", f.symVar())
	}
	b.WriteString(")\n\n")

	b.WriteString(`// cppgoEnsure opens the shared library and resolves every symbol the
// generated bindings need. It runs at most once; later calls report the
// result of the first attempt.
func cppgoEnsure() error {
	cppgoOnce.Do(func() {
		path := cppgoLibraryName
		if CppgoLibraryPath != "" {
			path = CppgoLibraryPath
		}
		lib, err := cppgo.Open(path)
		if err != nil {
			cppgoErr = fmt.Errorf("cppgo: opening %s: %w", path, err)
			return
		}
		for _, sym := range []struct {
			name string
			addr *uintptr
		}{
`)
	for _, f := range pkg.Funcs {
		fmt.Fprintf(&b, "\t\t\t{name: %q, addr: &%s},\n", f.lookupName(), f.symVar())
	}
	b.WriteString(`		} {
			addr, err := lib.Symbol(sym.name)
			if err != nil {
				_ = lib.Close()
				cppgoErr = fmt.Errorf("cppgo: %s: %w", path, err)
				return
			}
			*sym.addr = addr
		}
		cppgoLib = lib
	})
	return cppgoErr
}
`)

	if needsAlloc || needsRelease {
		b.WriteString(abiObjectRegistry)
	}
	if needsAlloc {
		b.WriteString(abiAllocHelper)
	}
	if needsRelease {
		b.WriteString(abiReleaseHelper)
	}
	if usesBool {
		b.WriteString(`
// cppgoBoolInt64 converts a bool to the int32_t the C ABI expects.
func cppgoBoolInt64(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
`)
	}

	for _, sig := range sigs {
		if sig.abi {
			emitABIWrapper(&b, sig, pkg.abi.source)
			continue
		}
		emitWrapper(&b, sig)
	}

	b.WriteString("\n// Assembly trampolines. Each one saves the Go stack pointer, switches to an\n")
	b.WriteString("// aligned scratch stack for the C call, restores it, and only then stores the\n")
	b.WriteString("// result (the result slot is addressed relative to SP).\n")
	for _, sig := range sigs {
		f := sig.fn
		b.WriteString("\n")
		if sig.abi {
			fmt.Fprintf(&b, "// %s is the NOSPLIT trampoline for the C++ symbol %q.\n", f.trampName(), f.Symbol)
		} else {
			fmt.Fprintf(&b, "// %s is the NOSPLIT trampoline for the C function %q.\n", f.trampName(), f.Symbol)
		}
		fmt.Fprintf(&b, "// It must not be called directly: use %s.\n", f.Name)
		b.WriteString("//\n//go:noescape\n")
		fmt.Fprintf(&b, "func %s(%s)%s\n", f.trampName(), trampolineParams(sig), trampolineResult(sig))
	}

	return gofmtSource(b.String())
}

// abiObjectRegistry is the storage table the direct ABI mode needs when a
// package binds a constructor or a destructor: the handle a constructor returns
// is the address of Go memory, which the garbage collector cannot see through a
// cppgo.Handle, so the buffer is held here until the destructor releases it.
const abiObjectRegistry = `
// cppgoObjects keeps the storage of every object a bound constructor created
// reachable for as long as the object exists. A cppgo.Handle is a uintptr, so
// no finaliser or pointer map can do it: the destructor drops the entry.
var (
	cppgoObjectsMu sync.Mutex
	cppgoObjects   = map[cppgo.Handle][]byte{}
)
`

// abiAllocHelper allocates the storage a constructor writes into.
const abiAllocHelper = `
// cppgoAlloc returns the address of a zeroed buffer of size bytes whose start
// is aligned to align, and records it so the handle stays valid until
// cppgoRelease drops it.
func cppgoAlloc(size, align int) cppgo.Handle {
	buf := make([]byte, size+align)
	offset := 0
	if align > 1 {
		offset = (align - int(uintptr(unsafe.Pointer(&buf[0]))%uintptr(align))) % align
	}
	handle := cppgo.Handle(uintptr(unsafe.Pointer(&buf[offset])))
	cppgoObjectsMu.Lock()
	cppgoObjects[handle] = buf
	cppgoObjectsMu.Unlock()
	return handle
}
`

// abiReleaseHelper drops the entry a destructor no longer needs.
const abiReleaseHelper = `
// cppgoRelease forgets the storage behind a handle. Calling it for a handle
// this package did not allocate is harmless: the object lives elsewhere.
func cppgoRelease(handle cppgo.Handle) {
	cppgoObjectsMu.Lock()
	delete(cppgoObjects, handle)
	cppgoObjectsMu.Unlock()
}
`

// lookupName is the dynamic symbol the generated initialisation resolves: the
// shim entry point in shim mode, the mangled C++ name in abi mode.
func (f *Func) lookupName() string {
	if f.abi != nil {
		return f.Symbol
	}
	return f.cName()
}

// emitABIWrapper renders the idiomatic Go binding for one direct ABI binding.
func emitABIWrapper(b *strings.Builder, sig *signature, source string) {
	f := sig.fn
	b.WriteString("\n")
	for _, line := range f.Doc {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(f.Doc) > 0 {
		b.WriteString("//\n")
	}
	fmt.Fprintf(b, "// %s binds %s.\n", f.wrapperName(), f.signature())
	fmt.Fprintf(b, "//\n// Direct C++ ABI: %s, mangled %s (abi from %s).\n", f.abi.sym.QualifiedName, f.abi.sym.Mangled, source)
	for _, note := range f.abi.notes {
		fmt.Fprintf(b, "//\n// Assumption: %s.\n", note)
	}
	for _, forced := range f.abi.forced {
		fmt.Fprintf(b, "//\n// NOT VALIDATED: -force accepted this binding as written, because %s.\n", forced)
	}
	fmt.Fprintf(b, "func %s(%s)%s {\n", f.wrapperName(), abiWrapperParams(f), wrapperResults(f))

	if hasErrorResult(f) {
		fmt.Fprintf(b, "\tif err := cppgoEnsure(); err != nil {\n\t\treturn %serr\n\t}\n", abiZeroPrefix(f))
	} else {
		b.WriteString("\tif err := cppgoEnsure(); err != nil {\n\t\tpanic(err)\n\t}\n")
	}
	b.WriteString("\tcppgo.ProbeStack()\n")

	allocated := ""
	if f.Kind == bindCtor {
		fmt.Fprintf(b, "\tcppgoThis := cppgoAlloc(%d, %d)\n", f.Class.Size, f.Class.Align)
		allocated = "cppgoThis"
	}
	call := fmt.Sprintf("%s(%s)", f.trampName(), strings.Join(abiCallArgs(sig, allocated), ", "))
	switch f.abi.shape {
	case abiObject:
		fmt.Fprintf(b, "\t%s\n", call)
		fmt.Fprintf(b, "\treturn %s(%s)", f.Class.GoName, allocated)
		if hasErrorResult(f) {
			b.WriteString(", nil")
		}
		b.WriteString("\n")
	case abiStatus:
		fmt.Fprintf(b, "\tcppgoStatus := %s\n", call)
		emitKeepAlive(b, sig)
		b.WriteString("\tif cppgoStatus != 0 {\n")
		emitStatusErrors(b, sig, "cppgoStatus")
		b.WriteString("\t}\n\treturn nil\n")
	case abiVoid, abiErrorOnly:
		fmt.Fprintf(b, "\t%s\n", call)
		emitKeepAlive(b, sig)
		if f.Kind == bindDtor {
			// The object is destroyed; the storage this package allocated for
			// it is no longer needed, and dropping the entry is what lets the
			// collector reclaim it.
			fmt.Fprintf(b, "\tcppgoRelease(cppgo.Handle(%s))\n", f.Recv.Name)
		}
		if f.abi.shape == abiErrorOnly {
			b.WriteString("\treturn nil\n")
		}
	default: // abiValue, abiValueError
		ret := localName(f, "cppgoRet")
		fmt.Fprintf(b, "\t%s := %s\n", ret, call)
		emitKeepAlive(b, sig)
		value := abiFromMirror(f.Results[0].Info, sig.ret.slot.goType, ret)
		if f.abi.shape == abiValueError {
			fmt.Fprintf(b, "\treturn %s, nil\n", value)
			break
		}
		fmt.Fprintf(b, "\treturn %s\n", value)
	}
	b.WriteString("}\n")
}

// abiCallArgs renders the arguments of the trampoline call: the symbol first,
// then the reserved sret pointer (never filled in v1), then `this`, then the
// explicit arguments converted to their mirror types.
func abiCallArgs(sig *signature, allocated string) []string {
	f := sig.fn
	args := []string{f.symVar()}
	if sig.sret {
		// TODO(abi): the destination of a by-value class result. The validator
		// rejects those bindings, so this is the reserved slot only.
		args = append(args, "nil")
	}
	if f.Kind.hasThis() {
		if f.Kind == bindCtor {
			args = append(args, "uintptr("+allocated+")")
		} else {
			args = append(args, "uintptr("+f.Recv.Name+")")
		}
	}
	for _, p := range f.Params {
		switch p.Info.kind {
		case kindString:
			args = append(args, "cppgo.StrPtr("+p.Name+")", "uintptr(len("+p.Name+"))")
		case kindSlice:
			args = append(args, "cppgo.Ptr("+p.Name+")", "uintptr(len("+p.Name+"))")
		case kindBool:
			args = append(args, "cppgoBoolInt64("+p.Name+")")
		default:
			args = append(args, mirrorExpr(p))
		}
	}
	return args
}

// abiFromMirror converts a call result to the bound function's result type.
//
// bool is the only shape that needs more than a cast: the Itanium ABI returns
// it in AL and leaves the rest of RAX unspecified, so the value is masked
// before it is compared.
func abiFromMirror(info *typeInfo, src, expr string) string {
	if info.kind == kindBool {
		return expr + "&0xff != 0"
	}
	return fromMirror(info, src, expr)
}

// abiZeroPrefix renders the zero value(s) a failed library load returns, before
// the error of a binding that has one.
func abiZeroPrefix(f *Func) string {
	if value := valueResult(f); value != nil {
		return zeroValue(value.Info) + ", "
	}
	return ""
}

// abiWrapperParams renders the parameter list of a direct ABI wrapper: the
// receiver first, because it is the `this` argument, then the declaration's own
// parameters.
func abiWrapperParams(f *Func) string {
	parts := make([]string, 0, len(f.Params)+1)
	if f.Recv != nil {
		parts = append(parts, f.Recv.Name+" "+f.Recv.Info.goType)
	}
	for _, p := range f.Params {
		parts = append(parts, p.Name+" "+p.Info.goType)
	}
	return strings.Join(parts, ", ")
}

// emitWrapper renders the idiomatic Go binding for one shim-mode function.
func emitWrapper(b *strings.Builder, sig *signature) {
	f := sig.fn
	b.WriteString("\n")
	for _, line := range f.Doc {
		b.WriteString(line)
		b.WriteString("\n")
	}
	if len(f.Doc) > 0 {
		b.WriteString("//\n")
	}
	fmt.Fprintf(b, "// %s binds %s to the C function %q.\n", f.wrapperName(), f.signature(), f.Symbol)
	fmt.Fprintf(b, "func %s(%s)%s {\n", f.wrapperName(), wrapperParams(f), wrapperResults(f))

	if sig.ret.kind == resNone || sig.ret.kind == resValue {
		b.WriteString("\tif err := cppgoEnsure(); err != nil {\n\t\tpanic(err)\n\t}\n")
	} else {
		fmt.Fprintf(b, "\tif err := cppgoEnsure(); err != nil {\n\t\treturn %serr\n\t}\n", zeroPrefix(sig))
	}
	b.WriteString("\tcppgo.ProbeStack()\n")

	ret := localName(f, "cppgoRet")
	status := localName(f, "cppgoStatus")
	call := callExpr(sig, ret)

	switch sig.ret.kind {
	case resNone:
		fmt.Fprintf(b, "\t%s\n", call)
		emitKeepAlive(b, sig)
	case resValue:
		expr := fromMirror(sig.ret.info, sig.ret.slot.goType, ret)
		if len(keepAliveParams(sig)) == 0 {
			b.WriteString("\treturn ")
			b.WriteString(fromMirror(sig.ret.info, sig.ret.slot.goType, call))
			b.WriteString("\n")
			break
		}
		fmt.Fprintf(b, "\t%s := %s\n", ret, call)
		emitKeepAlive(b, sig)
		fmt.Fprintf(b, "\treturn %s\n", expr)
	case resStatus:
		fmt.Fprintf(b, "\t%s := %s\n", status, call)
		emitKeepAlive(b, sig)
		fmt.Fprintf(b, "\tif %s != 0 {\n", status)
		emitStatusErrors(b, sig, status)
		b.WriteString("\t}\n\treturn nil\n")
	case resStatusValue:
		fmt.Fprintf(b, "\tvar %s %s\n", ret, sig.ret.tmpType)
		fmt.Fprintf(b, "\t%s := %s\n", status, call)
		emitKeepAlive(b, sig)
		fmt.Fprintf(b, "\tif %s != 0 {\n", status)
		emitStatusErrors(b, sig, status)
		fmt.Fprintf(b, "\t}\n\treturn %s, nil\n", fromMirror(sig.ret.info, sig.ret.tmpType, ret))
	}
	b.WriteString("}\n")
}

// emitStatusErrors renders the failure branch of a status-returning call.
func emitStatusErrors(b *strings.Builder, sig *signature, status string) {
	fmt.Fprintf(b, "\t\tif err := cppgo.LastError(cppgoLib); err != nil {\n")
	fmt.Fprintf(b, "\t\t\treturn %sfmt.Errorf(%q, err)\n", zeroPrefix(sig), errorPrefix(sig.fn)+": %w")
	b.WriteString("\t\t}\n")
	fmt.Fprintf(b, "\t\treturn %sfmt.Errorf(%q, %s)\n", zeroPrefix(sig), errorPrefix(sig.fn)+": call failed with status %d", status)
}

// errorPrefix names the C++ entity in an error message: the symbol in shim
// mode, the qualified C++ name in abi mode.
func errorPrefix(f *Func) string {
	if f.abi != nil {
		return f.abi.sym.QualifiedName
	}
	return f.Symbol
}

// emitKeepAlive pins the arguments that point into Go memory for the whole
// duration of the call.
func emitKeepAlive(b *strings.Builder, sig *signature) {
	params := keepAliveParams(sig)
	if len(params) == 0 {
		return
	}
	names := make([]string, len(params))
	for i, p := range params {
		names[i] = p.Name
	}
	fmt.Fprintf(b, "\tcppgo.KeepAlive(%s)\n", strings.Join(names, ", "))
}

// keepAliveParams lists the parameters that reference Go memory.
func keepAliveParams(sig *signature) []*Param {
	var out []*Param
	for _, p := range sig.fn.Params {
		switch p.Info.kind {
		case kindString, kindSlice, kindPointer:
			out = append(out, p)
		}
	}
	return out
}

// zeroPrefix renders the zero value(s) of the value results plus a comma, so
// that an error can be appended to a return statement.
func zeroPrefix(sig *signature) string {
	if sig.ret.kind == resStatus || sig.ret.kind == resNone {
		return ""
	}
	return zeroValue(sig.ret.info) + ", "
}

// zeroValue is the Go zero value of a type.
func zeroValue(info *typeInfo) string {
	switch info.kind {
	case kindBool:
		return "false"
	case kindPointer:
		return "nil"
	case kindHandle:
		return "cppgo.Handle(0)"
	case kindObject:
		return info.goType + "(0)"
	}
	return "0"
}

// wrapperParams renders the Go parameter list of a wrapper.
func wrapperParams(f *Func) string {
	parts := make([]string, len(f.Params))
	for i, p := range f.Params {
		parts[i] = p.Name + " " + p.Info.goType
	}
	return strings.Join(parts, ", ")
}

// wrapperResults renders the Go result list of a wrapper.
func wrapperResults(f *Func) string {
	switch len(f.Results) {
	case 0:
		return ""
	case 1:
		return " " + f.Results[0].Info.goType
	}
	parts := make([]string, len(f.Results))
	for i, r := range f.Results {
		parts[i] = r.Info.goType
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// callExpr renders the trampoline invocation, including argument conversion.
func callExpr(sig *signature, ret string) string {
	args := []string{sig.fn.symVar()}
	for _, p := range sig.fn.Params {
		switch p.Info.kind {
		case kindString:
			args = append(args, "cppgo.StrPtr("+p.Name+")", "uintptr(len("+p.Name+"))")
		case kindSlice:
			args = append(args, "cppgo.Ptr("+p.Name+")", "uintptr(len("+p.Name+"))")
		case kindBool:
			args = append(args, "cppgoBoolInt64("+p.Name+")")
		default:
			args = append(args, mirrorExpr(p))
		}
	}
	if sig.ret.kind == resStatusValue {
		args = append(args, "unsafe.Pointer(&"+ret+")")
	}
	return fmt.Sprintf("%s(%s)", sig.fn.trampName(), strings.Join(args, ", "))
}

// mirrorExpr converts a parameter to the Go type of its mirror slot.
func mirrorExpr(p *Param) string {
	if p.Info.goType == p.Info.slot {
		return p.Name
	}
	return p.Info.slot + "(" + p.Name + ")"
}

// fromMirror converts an expression of Go type src back to the bound
// function's result type.
func fromMirror(info *typeInfo, src, expr string) string {
	switch info.kind {
	case kindBool:
		return expr + " != 0"
	case kindPointer:
		return "(" + info.goType + ")(" + expr + ")"
	case kindHandle:
		return "cppgo.Handle(" + expr + ")"
	}
	if info.goType == src {
		return expr
	}
	return info.goType + "(" + expr + ")"
}

// localName returns base, suffixed if needed so it cannot shadow a parameter.
func localName(f *Func, base string) string {
	name := base
	for {
		clash := false
		for _, p := range f.Params {
			if p.Name == name {
				clash = true
			}
		}
		if !clash {
			return name
		}
		name += "_"
	}
}

// trampolineParams renders the Go parameter list of a trampoline declaration.
func trampolineParams(sig *signature) string {
	parts := make([]string, 0, len(sig.slots))
	for _, sl := range sig.slots {
		parts = append(parts, sl.label+" "+sl.goType)
	}
	return strings.Join(parts, ", ")
}

// trampolineResult renders the result of a trampoline declaration.
func trampolineResult(sig *signature) string {
	if sig.ret.slot == nil {
		return ""
	}
	return " (ret " + sig.ret.slot.goType + ")"
}

// gofmtSource formats generated Go source.
func gofmtSource(src string) ([]byte, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("formatting generated Go source: %w", err)
	}
	return out, nil
}
