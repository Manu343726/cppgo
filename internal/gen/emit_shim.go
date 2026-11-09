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
	if m.usesFree {
		h.WriteString("/* cppgo_free releases a result copy the Go side has read. */\n")
		h.WriteString("void cppgo_free(void *block);\n")
	}

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
	s.WriteString("\n#include <array>\n#include <cstdint>\n#include <cstring>\n#include <exception>\n#include <memory>\n#include <new>\n#include <optional>\n#include <string>\n#include <string_view>\n#include <vector>\n")
	if m.usesSpan {
		s.WriteString("#include <span>\n")
	}
	s.WriteString("\n")
	s.WriteString("namespace {\n\n")
	s.WriteString("// The message the failing thunk on this thread stored. It is cleared at the\n")
	s.WriteString("// entry of every thunk, so a successful call can never report a stale error.\n")
	s.WriteString("thread_local std::string cppgo_error_message;\n\n")
	s.WriteString("} // namespace\n\n")
	s.WriteString("extern \"C\" const char *cppgo_last_error(void) {\n")
	s.WriteString("\treturn cppgo_error_message.c_str();\n")
	s.WriteString("}\n")
	if m.usesFree {
		s.WriteString("\n// cppgo_free releases a result copy the Go side has read.\n")
		s.WriteString("extern \"C\" void cppgo_free(void *block) {\n")
		s.WriteString("\t::operator delete[](block);\n")
		s.WriteString("}\n")
	}

	var spans []thunkSpan
	for _, binding := range m.bindings {
		if !binding.Carrier.shimCarried() || (only != nil && binding != only) {
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

	if only == nil {
		if err := m.emitTemplateShim(&h, &s, &spans); err != nil {
			return nil, nil, nil, err
		}
	}

	fmt.Fprintf(&h, "\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(h.String()), []byte(s.String()), spans, nil
}

// shimLine is the line number the next write lands on.
func shimLine(b *strings.Builder) int { return strings.Count(b.String(), "\n") + 1 }

// shimReturnType is the C return type of a thunk: the status when the call can
// fail, the value itself when it cannot, and nothing when the value travels
// through an out-pointer (a record the caller owns, whatever its size).
func shimReturnType(binding *binding) string {
	if binding.Status {
		return "int32_t"
	}
	if binding.Out != nil {
		return "void"
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
	// store is the whole statement that delivers the value: a record is
	// constructed into the caller's storage through the out-pointer, which is
	// where C++'s own sret form is materialized. It is only reached when the
	// binding has an out-pointer, which newBinding only adds with one.
	store := func() string {
		if binding.Result.Kind == goRecord {
			return fmt.Sprintf("*static_cast<%s *>(cppgo_result) = %s;", binding.Result.Cxx, call)
		}
		return fmt.Sprintf("*cppgo_result = %s;", m.shimValue(binding, call))
	}
	var b strings.Builder
	if !binding.Status {
		// A noexcept call whose arguments are scalars, pointers or PODs cannot
		// throw, and its Go signature has no error. Nothing is caught because
		// there is nothing to catch.
		switch {
		case binding.Out != nil:
			fmt.Fprintf(&b, "\t%s\n", store())
		case binding.Result != nil:
			fmt.Fprintf(&b, "\treturn %s;\n", m.shimValue(binding, call))
		default:
			fmt.Fprintf(&b, "\t%s;\n", call)
		}
		return b.String(), nil
	}
	b.WriteString("\tcppgo_error_message.clear();\n")
	b.WriteString("\ttry {\n")
	b.WriteString(m.stdLocals(binding))
	switch {
	case binding.StdResult:
		result, err := m.stdResult(binding, call)
		if err != nil {
			return "", err
		}
		b.WriteString(result)
		b.WriteString("\t\treturn 0;\n")
	case binding.statusResult:
		// The callee's own return value is the status: 0 is success, anything
		// else is the failure the Go side turns into an error.
		fmt.Fprintf(&b, "\t\treturn %s;\n", call)
	case binding.Out != nil:
		fmt.Fprintf(&b, "\t\t%s\n", store())
		b.WriteString("\t\treturn 0;\n")
	default:
		fmt.Fprintf(&b, "\t\t%s;\n", call)
		b.WriteString("\t\treturn 0;\n")
	}
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
		if arg.label == "this" || arg.label == "out" || arg.isOut || arg.stdRole == stdRoleExtra {
			continue
		}
		if arg.callable != nil && !arg.callable.Std {
			// A plain function pointer: the address the callback table handed
			// out, cast to the declaration's own type.
			args = append(args, "reinterpret_cast<"+arg.callable.Cxx+">("+arg.cName+")")
			continue
		}
		if arg.stdRole == stdRolePrimary {
			args = append(args, arg.stdLocal)
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
	if binding.castTo != nil {
		return m.castTarget(binding)
	}
	if binding.memberField != nil {
		return m.fieldTarget(binding, args)
	}
	if binding.staticField != nil {
		qualified := "::" + binding.Owner.CxxName + "::" + binding.staticField.Name
		if !binding.staticWrite {
			return qualified, nil
		}
		if len(args) != 1 {
			return "", pending("a static data member's setter takes exactly one value", "fix the declaration")
		}
		return qualified + " = " + args[0], nil
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
		if binding.sym.IsConversion {
			// A conversion operator is called by casting the object to its
			// target type: libclang's spelling of the member name is not
			// something a call site can write.
			return "static_cast<" + conversionTarget(binding) + ">(*(" + cast + "))", nil
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
	if p.Kind == cxx.TypeEnum {
		// The wire word is the enum's underlying integer; the callee takes the
		// enum type, and a scoped enum has no implicit conversion.
		return "static_cast<::" + p.RecordName + ">(" + name + ")", nil
	}
	if p.Kind == cxx.TypeRecord {
		// A record passed by value: the Go side handed over the address of the
		// value it holds, and the copy into the call is C++'s.
		record := m.mapper.records[p.RecordName]
		if record == nil {
			return "", fmt.Errorf("cppgo: %s: a record the model does not know", p.Spelling)
		}
		return "*static_cast<const ::" + record.CxxName + " *>(" + name + ")", nil
	}
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

// conversionTarget is the C++ type a conversion operator converts to, spelled
// so the shim can name it: the mapped type's qualified name when it has one, and
// the declaration's own spelling otherwise (std::string, double).
func conversionTarget(binding *binding) string {
	if binding.Result != nil && binding.Result.Cxx != "" {
		return binding.Result.Cxx
	}
	return binding.sym.Result.Spelling
}

// shimValue renders a call's value as the thunk's C result type.
func (m *packageModel) shimValue(binding *binding, call string) string {
	if binding.ViewResult {
		// A borrowed record is a reference or a pointer, and the thunk hands
		// out its address: a reference has no pointer conversion of its own.
		constResult := binding.sym.Result.Pointee != nil && binding.sym.Result.Pointee.IsConst
		if constResult || (binding.memberField != nil && binding.sym.IsConst) {
			// The address points into the object Go owns; the qualifier
			// belongs to the C++ view C++ handed over, not to the storage.
			return "static_cast<void *>(const_cast<" + binding.Result.Cxx + " *>(&(" + call + ")))"
		}
		if strings.HasSuffix(strings.TrimSpace(binding.sym.Result.Spelling), "&") {
			return "static_cast<void *>(&(" + call + "))"
		}
		return "static_cast<void *>(" + call + ")"
	}
	if binding.Result != nil && binding.Result.Kind == goPointer && !binding.ViewResult {
		// The C prototype names the pointer with the mapped scalar's type, and
		// the declaration's own spelling may differ in qualification or in the
		// signedness of char. A pointer cast is exact and says so.
		return "reinterpret_cast<" + binding.Result.C + ">(" + call + ")"
	}
	return m.shimResultExpr(binding.Result, call)
}

// shimResultExpr renders a call's value as the thunk's C result type. An enum
// is cast to its underlying integer: a scoped enum has no implicit conversion,
// and the Go side reads the width, not the C++ name.
func (m *packageModel) shimResultExpr(result *goType, expr string) string {
	if result != nil && result.Kind == goEnum {
		return "static_cast<" + result.C + ">(" + expr + ")"
	}
	return expr
}

// stdLocals renders the C++ values the shim builds for a binding's
// standard-library parameters. They live inside the try block, so an allocation
// failure is translated like any other exception.
func (m *packageModel) stdLocals(binding *binding) string {
	var b strings.Builder
	for _, arg := range binding.Wire {
		if arg.stdRole != stdRolePrimary {
			continue
		}
		elem := arg.stdElem
		switch arg.stdKind {
		case stdFunction:
			// The std::function is constructed here, from the plain function
			// pointer the callback table handed out: Go never builds one.
			fmt.Fprintf(&b, "\t\t%s %s = reinterpret_cast<%s>(%s);\n",
				arg.callable.StdCxx, arg.stdLocal, arg.callable.Cxx, arg.stdArgs[0])
		case stdString:
			fmt.Fprintf(&b, "\t\tstd::string %s(%s, %s);\n", arg.stdLocal, arg.stdArgs[0], arg.stdArgs[1])
		case stdStringView:
			fmt.Fprintf(&b, "\t\tstd::string_view %s(%s, %s);\n", arg.stdLocal, arg.stdArgs[0], arg.stdArgs[1])
		case stdVector:
			fmt.Fprintf(&b, "\t\tstd::vector<%s> %s;\n", elem.Cxx, arg.stdLocal)
			fmt.Fprintf(&b, "\t\tif (%s != 0) { %s.assign(%s, %s + %s); }\n",
				arg.stdArgs[1], arg.stdLocal, arg.stdArgs[0], arg.stdArgs[0], arg.stdArgs[1])
		case stdSpan:
			qualifier := ""
			if elem.Const {
				qualifier = "const "
			}
			fmt.Fprintf(&b, "\t\tstd::span<%s%s> %s(static_cast<%s%s *>(%s), %s);\n",
				qualifier, elem.Cxx, arg.stdLocal, qualifier, elem.Cxx, arg.stdArgs[0], arg.stdArgs[1])
		case stdArray:
			fmt.Fprintf(&b, "\t\tstd::array<%s, %d> %s{};\n", elem.Cxx, arg.stdType.ArrayLen, arg.stdLocal)
			fmt.Fprintf(&b, "\t\tfor (size_t cppgo_i = 0; cppgo_i < %d; ++cppgo_i) { %s[cppgo_i] = static_cast<const %s *>(%s)[cppgo_i]; }\n",
				arg.stdType.ArrayLen, arg.stdLocal, elem.Cxx, arg.stdArgs[0])
		case stdOptional:
			fmt.Fprintf(&b, "\t\tstd::optional<%s> %s;\n", elem.Cxx, arg.stdLocal)
			fmt.Fprintf(&b, "\t\tif (%s != nullptr) { %s = *static_cast<const %s *>(%s); }\n",
				arg.stdArgs[0], arg.stdLocal, elem.Cxx, arg.stdArgs[0])
		}
	}
	return b.String()
}

// stdResult renders the copy-out of a standard-library result into the
// out-parameters the Go caller provided. The C++ value is destroyed at the end
// of the thunk; what crosses is a plain array the Go side owns and frees.
func (m *packageModel) stdResult(binding *binding, call string) (string, error) {
	result := binding.Result
	elem := result.Elem
	var b strings.Builder
	switch result.Std {
	case stdString, stdStringView:
		fmt.Fprintf(&b, "\t\tauto cppgo_value = %s;\n", call)
		b.WriteString("\t\tconst size_t cppgo_size = cppgo_value.size();\n")
		b.WriteString("\t\tchar *cppgo_copy = static_cast<char *>(::operator new[](cppgo_size == 0 ? 1 : cppgo_size));\n")
		b.WriteString("\t\tif (cppgo_size != 0) { std::memcpy(cppgo_copy, cppgo_value.data(), cppgo_size); }\n")
		b.WriteString("\t\t*cppgo_out_data = cppgo_copy;\n")
		b.WriteString("\t\t*cppgo_out_size = cppgo_size;\n")
	case stdVector, stdSpan:
		fmt.Fprintf(&b, "\t\tauto cppgo_value = %s;\n", call)
		b.WriteString("\t\tconst size_t cppgo_size = cppgo_value.size();\n")
		fmt.Fprintf(&b, "\t\t%s *cppgo_copy = static_cast<%s *>(::operator new[]((cppgo_size == 0 ? 1 : cppgo_size) * sizeof(%s)));\n",
			elem.Cxx, elem.Cxx, elem.Cxx)
		b.WriteString("\t\tfor (size_t cppgo_i = 0; cppgo_i < cppgo_size; ++cppgo_i) { cppgo_copy[cppgo_i] = cppgo_value[cppgo_i]; }\n")
		b.WriteString("\t\t*cppgo_out_data = cppgo_copy;\n")
		b.WriteString("\t\t*cppgo_out_size = cppgo_size;\n")
	case stdArray:
		fmt.Fprintf(&b, "\t\tauto cppgo_value = %s;\n", call)
		fmt.Fprintf(&b, "\t\t%s *cppgo_copy = static_cast<%s *>(cppgo_out_data);\n", elem.Cxx, elem.Cxx)
		fmt.Fprintf(&b, "\t\tfor (size_t cppgo_i = 0; cppgo_i < %d; ++cppgo_i) { cppgo_copy[cppgo_i] = cppgo_value[cppgo_i]; }\n", result.ArrayLen)
	case stdOptional:
		fmt.Fprintf(&b, "\t\tauto cppgo_value = %s;\n", call)
		fmt.Fprintf(&b, "\t\tif (cppgo_value) { *static_cast<%s *>(cppgo_out_value) = *cppgo_value; *static_cast<int32_t *>(cppgo_out_has) = 1; }\n", elem.Cxx)
		b.WriteString("\t\telse { *static_cast<int32_t *>(cppgo_out_has) = 0; }\n")
	default:
		return "", pending(fmt.Sprintf("a %q result is not emitted by this build yet", result.Go), "return it through a pointer or a reference at the C++ boundary")
	}
	return b.String(), nil
}
