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
	for _, sig := range sigs {
		if sig.usesUnsafe() {
			usesUnsafe = true
		}
		for _, p := range sig.fn.Params {
			if p.Info.kind == kindBool {
				usesBool = true
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
		fmt.Fprintf(&b, "\t// %s is the resolved address of the %q symbol.\n", f.symVar(), f.cName())
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
		fmt.Fprintf(&b, "\t\t\t{name: %q, addr: &%s},\n", f.cName(), f.symVar())
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
		emitWrapper(&b, sig)
	}

	b.WriteString("\n// Assembly trampolines. Each one saves the Go stack pointer, switches to an\n")
	b.WriteString("// aligned scratch stack for the C call, restores it, and only then stores the\n")
	b.WriteString("// result (the result slot is addressed relative to SP).\n")
	for _, sig := range sigs {
		f := sig.fn
		b.WriteString("\n")
		fmt.Fprintf(&b, "// %s is the NOSPLIT trampoline for the C function %q.\n", f.trampName(), f.Symbol)
		fmt.Fprintf(&b, "// It must not be called directly: use %s.\n", f.Name)
		b.WriteString("//\n//go:noescape\n")
		fmt.Fprintf(&b, "func %s(%s)%s\n", f.trampName(), trampolineParams(sig), trampolineResult(sig))
	}

	return gofmtSource(b.String())
}

// emitWrapper renders the idiomatic Go binding for one function.
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
	fmt.Fprintf(b, "\t\t\treturn %sfmt.Errorf(%q, err)\n", zeroPrefix(sig), sig.fn.Symbol+": %w")
	b.WriteString("\t\t}\n")
	fmt.Fprintf(b, "\t\treturn %sfmt.Errorf(%q, %s)\n", zeroPrefix(sig), sig.fn.Symbol+": call failed with status %d", status)
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
