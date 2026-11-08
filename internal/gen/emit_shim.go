package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// emitShim renders zz_cppgo_shim.h and zz_cppgo_shim.cpp: the extern "C"
// declarations of every thunk, and the C++ that implements them.
//
// The header is standalone: it uses only C types, so it can be included by
// other C++ code. The source includes the bound header and performs the calls
// as C++, which is what makes virtual dispatch, inline bodies, and this
// adjustment work at all.
func (m *packageModel) emitShim() ([]byte, []byte, []thunkSpan, error) {
	return m.renderShim(nil)
}

// thunkSpan is one thunk's line range in the generated translation unit. It is
// how a compiler diagnostic is turned back into the binding that produced it.
type thunkSpan struct {
	start  int
	end    int
	symbol string
}

// renderShim renders both shim files and the line range of every thunk. When
// only is non-nil, the source contains just that binding's thunk, which is how
// the self-check isolates the culprit of a compiler diagnostic.
func (m *packageModel) renderShim(only *binding) ([]byte, []byte, []thunkSpan, error) {
	guard := "CPPGO_" + guardName(m.name) + "_SHIM_H"
	var h strings.Builder
	h.WriteString("/*" + GeneratedBanner[2:] + " */\n")
	h.WriteString("/* The extern \"C\" declarations zz_cppgo_shim.cpp implements.\n")
	h.WriteString(" * Compile both files into the library this package loads. */\n\n")
	fmt.Fprintf(&h, "#ifndef %s\n#define %s\n\n", guard, guard)
	h.WriteString("#include <stddef.h>\n#include <stdint.h>\n\n")
	h.WriteString("#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	h.WriteString("/* cppgo_last_error returns the message recorded by the last failed thunk\n")
	h.WriteString(" * on this thread, or an empty string when there is none. The pointer\n")
	h.WriteString(" * stays valid until the next thunk call on the same thread. */\n")
	h.WriteString("const char *cppgo_last_error(void);\n")

	var s strings.Builder
	s.WriteString(GeneratedBanner + "\n")
	s.WriteString("//\n")
	s.WriteString("// The extern \"C\" thunks of one cppgo package. Compile this file into the\n")
	s.WriteString("// same library the generated Go package loads.\n\n")
	s.WriteString("#include \"zz_cppgo_shim.h\"\n\n")
	seen := map[string]bool{}
	for _, header := range m.headers {
		if seen[header] {
			continue
		}
		seen[header] = true
		fmt.Fprintf(&s, "#include %q\n", header)
	}
	s.WriteString("\n#include <exception>\n#include <new>\n#include <string>\n\n")
	s.WriteString("namespace {\n\n")
	s.WriteString("// The message the failing thunk on this thread stored. It is cleared at the\n")
	s.WriteString("// entry of every thunk, so a successful call can never report a stale error.\n")
	s.WriteString("thread_local std::string cppgo_error_message;\n\n")
	s.WriteString("} // namespace\n\n")
	s.WriteString("extern \"C\" const char *cppgo_last_error(void) {\n")
	s.WriteString("\treturn cppgo_error_message.c_str();\n")
	s.WriteString("}\n")

	var spans []thunkSpan
	for _, binding := range m.bindings {
		if binding.Carrier != thunk || (only != nil && binding != only) {
			continue
		}
		h.WriteString("\n")
		fmt.Fprintf(&h, "/* %s: %s */\n", binding.Thunk, displaySignature(binding.sym))
		fmt.Fprintf(&h, "%s %s(%s);\n", shimReturnType(binding), binding.Thunk, shimParams(binding))
		body, err := m.shimBody(binding)
		if err != nil {
			return nil, nil, nil, err
		}
		s.WriteString("\n")
		start := shimLine(&s)
		fmt.Fprintf(&s, "extern \"C\" %s %s(%s) {\n", shimReturnType(binding), binding.Thunk, shimParams(binding))
		s.WriteString(body)
		s.WriteString("}\n")
		spans = append(spans, thunkSpan{start: start, end: shimLine(&s), symbol: binding.sym.QualifiedName})
	}

	fmt.Fprintf(&h, "\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(h.String()), []byte(s.String()), spans, nil
}

// shimLine is the line number the next write lands on.
func shimLine(b *strings.Builder) int { return strings.Count(b.String(), "\n") + 1 }

// shimReturnType is the C return type of a thunk: the status when the call can
// fail, the value itself when it cannot.
func shimReturnType(binding *binding) string {
	if binding.Status {
		return "int32_t"
	}
	if binding.Result != nil {
		return cTypeOf(binding.Result)
	}
	return "void"
}

// cTypeOf is the C spelling of a result value.
func cTypeOf(t *goType) string {
	switch t.Kind {
	case goBool:
		return "bool"
	case goPointer:
		return t.C
	default:
		return t.C
	}
}

// shimParams renders the C parameter list of a thunk.
func shimParams(binding *binding) string {
	parts := make([]string, 0, len(binding.Wire))
	for _, arg := range binding.Wire {
		parts = append(parts, arg.cType+" "+arg.cName)
	}
	if len(parts) == 0 {
		return "void"
	}
	return strings.Join(parts, ", ")
}

// shimBody renders the statements inside one thunk.
func (m *packageModel) shimBody(binding *binding) (string, error) {
	call, err := m.targetCall(binding)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if !binding.Status {
		// A noexcept call whose arguments are scalars, pointers or PODs cannot
		// throw, and its Go signature has no error. Nothing is caught because
		// there is nothing to catch.
		if binding.Result != nil {
			fmt.Fprintf(&b, "\treturn %s;\n", call)
		} else {
			fmt.Fprintf(&b, "\t%s;\n", call)
		}
		return b.String(), nil
	}
	b.WriteString("\tcppgo_error_message.clear();\n")
	b.WriteString("\ttry {\n")
	if binding.Result != nil {
		fmt.Fprintf(&b, "\t\t*cppgo_result = %s;\n", call)
	} else {
		fmt.Fprintf(&b, "\t\t%s;\n", call)
	}
	b.WriteString("\t\treturn 0;\n")
	b.WriteString("\t} catch (const std::exception &error) {\n")
	b.WriteString("\t\tcppgo_error_message = error.what();\n")
	b.WriteString("\t\treturn 1;\n")
	b.WriteString("\t} catch (...) {\n")
	b.WriteString("\t\tcppgo_error_message = \"unknown C++ exception\";\n")
	b.WriteString("\t\treturn 1;\n")
	b.WriteString("\t}\n")
	return b.String(), nil
}

// targetCall renders the C++ expression that calls the bound declaration.
func (m *packageModel) targetCall(binding *binding) (string, error) {
	args := make([]string, 0, len(binding.Wire))
	for _, arg := range binding.Wire {
		if arg.label == "this" || arg.label == "out" {
			continue
		}
		expr, err := m.shimArg(arg.param, arg.cName)
		if err != nil {
			return "", err
		}
		args = append(args, expr)
	}
	list := strings.Join(args, ", ")
	owner := ""
	if binding.Owner != nil {
		owner = binding.Owner.CxxName
	}
	switch binding.sym.Kind {
	case cxx.Constructor:
		return "new (self) ::" + owner + "(" + list + ")", nil
	case cxx.Destructor:
		return "static_cast<::" + owner + " *>(self)->~" + destructorSimpleName(owner) + "()", nil
	case cxx.Method:
		cast := "static_cast<::" + owner + " *>(self)"
		if binding.sym.IsConst {
			cast = "static_cast<const ::" + owner + " *>(self)"
		}
		return cast + "->" + simpleName(binding.sym.QualifiedName) + "(" + list + ")", nil
	default:
		return "::" + binding.sym.QualifiedName + "(" + list + ")", nil
	}
}

// shimArg renders one C++ argument inside a thunk. The thunk's C parameter is a
// pointer to a record; the target wants the record type, a pointer, or a
// reference, and the conversion is written where the layout is known.
func (m *packageModel) shimArg(p cxx.Param, name string) (string, error) {
	if !p.Ptr || p.Pointee == nil {
		return name, nil
	}
	reference := strings.HasSuffix(strings.TrimSpace(p.Spelling), "&")
	if p.Pointee.Kind == cxx.TypeRecord {
		record := m.mapper.records[p.Pointee.RecordName]
		if record == nil {
			return "", fmt.Errorf("cppgo: %s: a record pointer the model does not know", p.Spelling)
		}
		cast := "static_cast<::" + record.CxxName + " *>"
		if p.Pointee.IsConst {
			cast = "static_cast<const ::" + record.CxxName + " *>"
		}
		expr := cast + "(" + name + ")"
		if reference {
			expr = "*" + expr
		}
		return expr, nil
	}
	if reference {
		return "*" + name, nil
	}
	return name, nil
}
