package gen

import (
	"fmt"
	"sort"
	"strings"
)

// emitShim renders cppgo_shim.h: the extern "C" prototypes the C++ side has
// to implement. Every prototype is named cppgo_<symbol> so the Go side can
// resolve it with dlsym after dlopen.
func emitShim(pkg *packageInfo, sigs []*signature) []byte {
	guard := "CPPGO_" + guardName(pkg.Name) + "_SHIM_H"
	var b strings.Builder
	b.WriteString("/*" + GeneratedBanner[2:] + " */\n")
	fmt.Fprintf(&b, "/* C++ shim for the Go package %s (library %q). */\n\n", pkg.Name, pkg.Library)
	fmt.Fprintf(&b, "#ifndef %s\n#define %s\n\n", guard, guard)
	b.WriteString("#include <stddef.h>\n#include <stdint.h>\n\n")
	b.WriteString("#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	b.WriteString("/* cppgo_last_error returns the message recorded by the last failed cppgo_*\n")
	b.WriteString(" * call on this thread, or an empty string when there is none. The pointer\n")
	b.WriteString(" * stays valid until the next cppgo_* call on the same thread. */\n")
	b.WriteString("const char *cppgo_last_error(void);\n")

	if named := namedPointees(sigs); len(named) > 0 {
		b.WriteString("\n/* The bound package passes these types by pointer. The go types are\n")
		b.WriteString(" * forward-declared as opaque structs; include the header that defines\n")
		b.WriteString(" * them (or replace these lines) if the C++ side needs their layout. */\n")
		for _, name := range named {
			fmt.Fprintf(&b, "struct %s;\ntypedef struct %s %s;\n", name, name, name)
		}
	}

	for _, sig := range sigs {
		f := sig.fn
		b.WriteString("\n")
		fmt.Fprintf(&b, "/* %s implements %s.\n", f.cName(), f.signature())
		switch sig.ret.kind {
		case resStatus, resStatusValue:
			b.WriteString(" * Returns 0 on success. On failure it returns a non-zero status and\n")
			b.WriteString(" * leaves the message for cppgo_last_error()")
			if sig.ret.kind == resStatusValue {
				fmt.Fprintf(&b, "; the value is written through %s", sig.cParams[len(sig.cParams)-1].Name)
			}
			b.WriteString(".\n")
		}
		b.WriteString(" * The callee must not keep a pointer to Go memory after returning. */\n")
		fmt.Fprintf(&b, "%s %s(%s);\n", shimReturn(sig), f.cName(), shimParams(sig))
	}

	fmt.Fprintf(&b, "\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(b.String())
}

// shimReturn is the C return type of a prototype.
func shimReturn(sig *signature) string {
	switch sig.ret.kind {
	case resNone:
		return "void"
	case resValue:
		return sig.ret.info.cType
	}
	return "int32_t"
}

// shimParams renders the C parameter list.
func shimParams(sig *signature) string {
	if len(sig.cParams) == 0 {
		return "void"
	}
	parts := make([]string, len(sig.cParams))
	for i, p := range sig.cParams {
		parts[i] = p.Type + " " + p.Name
	}
	return strings.Join(parts, ", ")
}

// namedPointees lists the sorted, de-duplicated names of the types of the
// bound package that cross the boundary by pointer.
func namedPointees(sigs []*signature) []string {
	seen := map[string]bool{}
	for _, sig := range sigs {
		for _, p := range sig.fn.Params {
			if p.Info.named != "" {
				seen[p.Info.named] = true
			}
		}
		for _, r := range sig.fn.Results {
			if r.Info.named != "" {
				seen[r.Info.named] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// guardName turns a package name into an include-guard fragment.
func guardName(pkg string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(pkg) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 || (b.String()[0] >= '0' && b.String()[0] <= '9') {
		return "PKG_" + b.String()
	}
	return b.String()
}
