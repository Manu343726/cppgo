package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// templateSymbolName is the thunk name of one binding of one instantiation. It
// is the generator's half of the naming: the generated Go computes the same
// string from the instantiation key with FNV-1a.
func templateSymbolName(tmpl, bindingID, key string) string {
	return "cppgo_tmpl_" + shortHash(tmpl+"|"+bindingID+"|"+key)
}

// emitTemplateShim writes the second phase's C++: one explicit instantiation
// per collected specialization (which is what makes the members exist), the
// sizeof/alignof thunks the generic constructor needs, and one extern "C"
// thunk per binding of every instantiation.
func (m *packageModel) emitTemplateShim(h, s *strings.Builder, spans *[]thunkSpan) error {
	if len(m.instantiations) == 0 {
		return nil
	}
	s.WriteString("\n// The specializations the Go program instantiates. cppgo build collected\n")
	s.WriteString("// them from the program; adding one here is what makes its members exist.\n")
	for _, inst := range m.instantiations {
		if inst.Template.IsFunction {
			// A function template has no explicit instantiation of this form,
			// and the thunk's call instantiates it implicitly.
			continue
		}
		fmt.Fprintf(s, "%s\n", inst.explicitInstantiation())
	}
	for _, inst := range m.instantiations {
		target := "::" + inst.cxxName()
		if !inst.Template.IsFunction {
			m.emitTemplateRaw(h, s, spans, templateSymbolName(inst.Template.CxxName, "size", inst.Key),
				"return sizeof("+target+");", inst.Template.CxxName, inst.Key)
			m.emitTemplateRaw(h, s, spans, templateSymbolName(inst.Template.CxxName, "align", inst.Key),
				"return alignof("+target+");", inst.Template.CxxName, inst.Key)
		}
		for _, binding := range inst.Template.bindings() {
			thunk, err := m.templateThunk(binding, inst)
			if err != nil {
				return err
			}
			h.WriteString("\n")
			fmt.Fprintf(h, "/* %s: %s */\n", thunk.symbol, displaySignature(binding.sym))
			fmt.Fprintf(h, "int32_t %s(%s);\n", thunk.symbol, thunk.params)
			s.WriteString("\n")
			start := shimLine(s)
			fmt.Fprintf(s, "extern \"C\" int32_t %s(%s) {\n", thunk.symbol, thunk.params)
			s.WriteString("\tcppgo_error_message.clear();\n")
			s.WriteString("\ttry {\n")
			fmt.Fprintf(s, "\t\t%s;\n", thunk.call)
			s.WriteString("\t\treturn 0;\n")
			s.WriteString("\t} catch (const std::exception &error) {\n")
			s.WriteString("\t\tcppgo_error_message = error.what();\n")
			s.WriteString("\t\treturn 1;\n")
			s.WriteString("\t} catch (...) {\n")
			s.WriteString("\t\tcppgo_error_message = \"unknown C++ exception\";\n")
			s.WriteString("\t\treturn 1;\n")
			s.WriteString("\t}\n")
			s.WriteString("}\n")
			*spans = append(*spans, thunkSpan{start: start, end: shimLine(s), symbol: binding.sym.QualifiedName})
		}
	}
	return nil
}

// emitTemplateRaw writes a thunk with no parameters and no exception handler,
// which is all a sizeof or alignof thunk needs.
func (m *packageModel) emitTemplateRaw(h, s *strings.Builder, spans *[]thunkSpan, symbol, body, tmpl, key string) {
	h.WriteString("\n")
	fmt.Fprintf(h, "/* %s: layout of %s<%s> */\n", symbol, tmpl, key)
	fmt.Fprintf(h, "size_t %s(void);\n", symbol)
	s.WriteString("\n")
	start := shimLine(s)
	fmt.Fprintf(s, "extern \"C\" size_t %s(void) {\n\t%s\n}\n", symbol, body)
	*spans = append(*spans, thunkSpan{start: start, end: shimLine(s), symbol: tmpl + "<" + key + ">"})
}

// templateThunk is one generated thunk: its symbol, its C parameter list and
// the statement that performs the call.
type templateThunk struct {
	symbol string
	params string
	call   string
}

// templateThunk renders one binding of one instantiation as C++.
func (m *packageModel) templateThunk(binding *binding, inst *templateInstantiation) (templateThunk, error) {
	params := make([]string, 0, len(binding.Params)+2)
	args := make([]string, 0, len(binding.Params))
	if binding.This {
		params = append(params, "void *self")
	}
	if binding.Result != nil {
		params = append(params, "void *cppgo_result")
	}
	for _, param := range binding.Params {
		concrete, err := concreteType(param.Type, inst)
		if err != nil {
			return templateThunk{}, fmt.Errorf("cppgo: %s: parameter %s: %w", binding.sym.QualifiedName, param.Name, err)
		}
		name := cIdent(param.Name)
		params = append(params, concrete.C+" "+name)
		if concrete.Kind == goPointer && isReferenceParam(param.Type) {
			args = append(args, "*"+name)
		} else {
			args = append(args, name)
		}
	}
	target := "::" + inst.cxxName()
	call := ""
	switch {
	case binding.IsConstructor:
		call = "new (self) " + target + "(" + strings.Join(args, ", ") + ")"
	case binding.IsDestructor:
		call = "static_cast<" + target + " *>(self)->~" + destructorSimpleName(inst.Template.CxxName) + "()"
	case binding.Template != nil && binding.Template.IsFunction:
		// The explicit argument list is what makes a function template
		// instantiate when a parameter cannot be deduced from the call.
		call = "::" + binding.sym.QualifiedName + genericArguments(inst) + "(" + strings.Join(args, ", ") + ")"
	case binding.IsStatic:
		// A static member of a class template is reached through the
		// instantiated type; a free function template through its own name.
		call = target + "::" + simpleName(binding.sym.QualifiedName) + "(" + strings.Join(args, ", ") + ")"
	default:
		cast := "static_cast<" + target + " *>(self)"
		if binding.sym.IsConst {
			cast = "static_cast<const " + target + " *>(self)"
		}
		call = cast + "->" + simpleName(binding.sym.QualifiedName) + "(" + strings.Join(args, ", ") + ")"
	}
	if binding.Result != nil {
		result, err := concreteType(binding.Result, inst)
		if err != nil {
			return templateThunk{}, fmt.Errorf("cppgo: %s: result: %w", binding.sym.QualifiedName, err)
		}
		call = "*static_cast<" + result.Cxx + " *>(cppgo_result) = " + call
	}
	return templateThunk{
		symbol: templateSymbolName(inst.Template.CxxName, binding.ID, inst.Key),
		params: strings.Join(params, ", "),
		call:   call,
	}, nil
}

// genericArguments is a function template's explicit argument list.
func genericArguments(inst *templateInstantiation) string {
	parts := make([]string, len(inst.Args))
	for i, arg := range inst.Args {
		parts[i] = arg.Cxx
	}
	return "<" + strings.Join(parts, ", ") + ">"
}

// isReferenceParam reports whether the C++ parameter is a reference. The C
// prototype carries a pointer either way; only the call site differs.
func isReferenceParam(mapped *goType) bool {
	return mapped.Param.Kind == cxx.TypePointer && referenceParam(mapped.Param)
}

// referenceParam reports whether a C++ parameter is a reference. libclang
// spells a reference to a named type through its alias (`InputArray` for
// `const _InputArray &`), so the canonical kind is the reliable half and the
// written spelling is the other.
func referenceParam(p cxx.Param) bool {
	if p.ClangKind == "LValueReference" || p.ClangKind == "RValueReference" {
		return true
	}
	return p.Kind == cxx.TypePointer && strings.HasSuffix(strings.TrimSpace(p.Spelling), "&")
}
