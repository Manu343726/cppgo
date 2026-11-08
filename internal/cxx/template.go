package cxx

import (
	"fmt"
	"strings"
)

// ExceptionSpec is the C++ exception specification a declaration carries. It
// is what decides whether a binding can take the direct path: a direct call has
// no handler, so only a declaration that promises not to throw may use it.
type ExceptionSpec uint8

const (
	// ExceptionUnspecified is a declaration with no exception specification:
	// anything may be thrown.
	ExceptionUnspecified ExceptionSpec = iota
	// ExceptionNone is `throw()`: no exception.
	ExceptionNone
	// ExceptionDynamic is `throw(T1, T2)` or `throw(...)`: a dynamic
	// specification, which does not promise nothing escapes.
	ExceptionDynamic
	// ExceptionNoexcept is `noexcept`: no exception.
	ExceptionNoexcept
	// ExceptionComputedNoexcept is `noexcept(expr)`. libclang reports the kind
	// but not the evaluated result, so it is treated conservatively: it may
	// throw, and the binding goes through a thunk.
	ExceptionComputedNoexcept
	// ExceptionNoThrow is `__declspec(nothrow)`: no exception.
	ExceptionNoThrow
	// ExceptionUnknown is a specification this package does not classify
	// (unevaluated, uninstantiated, unparsed): it may throw.
	ExceptionUnknown
)

// String implements fmt.Stringer.
func (s ExceptionSpec) String() string {
	switch s {
	case ExceptionUnspecified:
		return "unspecified"
	case ExceptionNone:
		return "throw()"
	case ExceptionDynamic:
		return "dynamic"
	case ExceptionNoexcept:
		return "noexcept"
	case ExceptionComputedNoexcept:
		return "computed noexcept"
	case ExceptionNoThrow:
		return "__declspec(nothrow)"
	default:
		return "unknown"
	}
}

// CanThrow reports whether a call to the declaration may let an exception
// escape. Only the specifications known to promise otherwise answer false.
func (s ExceptionSpec) CanThrow() bool {
	switch s {
	case ExceptionNone, ExceptionNoexcept, ExceptionNoThrow:
		return false
	default:
		return true
	}
}

// TemplateArgumentKindFromLibclang maps one CXTemplateArgumentKind onto the
// model. It is exported for the tests and the report, which must name what an
// argument is even when the platform cannot produce one.
func TemplateArgumentKindFromLibclang(kind int32) TemplateArgumentKind {
	switch kind {
	case templateArgumentType:
		return TemplateArgumentType
	case templateArgumentIntegral:
		return TemplateArgumentIntegral
	case templateArgumentDeclaration:
		return TemplateArgumentDeclaration
	case templateArgumentTemplate:
		return TemplateArgumentTemplate
	case templateArgumentExpression:
		return TemplateArgumentExpression
	case templateArgumentPack, templateArgumentExpansion:
		return TemplateArgumentPack
	default:
		return TemplateArgumentUnknown
	}
}

// TemplateArgumentKind is what one template argument is: a type, an integral
// value, a template, an expression, or a pack.
type TemplateArgumentKind uint8

const (
	// TemplateArgumentUnknown is an argument libclang did not classify.
	TemplateArgumentUnknown TemplateArgumentKind = iota
	// TemplateArgumentType is a type argument: `Box<int>`.
	TemplateArgumentType
	// TemplateArgumentIntegral is an integral value: `array<int, 4>`.
	TemplateArgumentIntegral
	// TemplateArgumentDeclaration is a declaration argument.
	TemplateArgumentDeclaration
	// TemplateArgumentTemplate is a template used as an argument.
	TemplateArgumentTemplate
	// TemplateArgumentExpression is an expression argument.
	TemplateArgumentExpression
	// TemplateArgumentPack is a parameter pack or an expansion.
	TemplateArgumentPack
)

// String implements fmt.Stringer.
func (k TemplateArgumentKind) String() string {
	switch k {
	case TemplateArgumentType:
		return "type"
	case TemplateArgumentIntegral:
		return "integral"
	case TemplateArgumentDeclaration:
		return "declaration"
	case TemplateArgumentTemplate:
		return "template"
	case TemplateArgumentExpression:
		return "expression"
	case TemplateArgumentPack:
		return "pack"
	default:
		return "unknown"
	}
}

// specializationName is the identity of a specialization: the template's
// qualified name followed by its arguments. libclang spells both a pattern and
// its instantiation "Box", so the arguments are what tells them apart, and
// "mathx::Box<int>" is the name the generator keys on.
func specializationName(templateName string, args []TemplateArgument) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, arg.String())
	}
	return templateName + "<" + strings.Join(parts, ",") + ">"
}

// TemplateArgument is one concrete argument of a template specialization. The
// generator names the Go type from these: `Box<int>` is `BoxInt`, and an
// argument it cannot name is reported with the manifest entry that resolves it.
type TemplateArgument struct {
	// Kind is what the argument is.
	Kind TemplateArgumentKind
	// Spelling is the argument as written, e.g. "int", "mathx::Vector", "4".
	// It is empty for the kinds libclang does not expose a spelling for.
	Spelling string
	// Type is the classified type of a type argument.
	Type Param
	// Value is the signed reading of an integral argument.
	Value int64
	// UnsignedValue is the same bits read as unsigned.
	UnsignedValue uint64
}

// String renders the argument for a diagnostic or a generated name.
func (a TemplateArgument) String() string {
	if a.Spelling != "" {
		return normalizeTypeSpelling(a.Spelling)
	}
	return a.Kind.String()
}

// normalizeTypeSpelling removes the spaces libclang puts around punctuation in
// a type spelling, so "Q<int *>" and "Q<int*>" are one identity. The spaces of
// a multiword type ("unsigned long") are kept.
func normalizeTypeSpelling(spelling string) string {
	for _, pair := range [][2]string{
		{" *", "*"}, {"* ", "*"},
		{" &", "&"}, {"& ", "&"},
		{" ,", ","}, {", ", ","},
		{"< ", "<"}, {" <", "<"},
		{" >", ">"}, {"> ", ">"},
	} {
		spelling = strings.ReplaceAll(spelling, pair[0], pair[1])
	}
	return strings.TrimSpace(spelling)
}

// exceptionSpec maps libclang's exception specification kind onto the model.
func (l *Libclang) exceptionSpec(cursor CXCursor) ExceptionSpec {
	return exceptionSpecFromKind(l.f.cursorExceptionSpec(cursor))
}

// exceptionSpecFromKind is the mapping on its own, so every kind libclang can
// report is tested even when the platform cannot declare it.
func exceptionSpecFromKind(kind int32) ExceptionSpec {
	switch kind {
	case exceptionSpecNone:
		return ExceptionUnspecified
	case exceptionSpecDynamicNone:
		return ExceptionNone
	case exceptionSpecDynamic, exceptionSpecMSAny:
		return ExceptionDynamic
	case exceptionSpecBasicNoexcept:
		return ExceptionNoexcept
	case exceptionSpecComputedNoexcept:
		return ExceptionComputedNoexcept
	case exceptionSpecNoThrow:
		return ExceptionNoThrow
	default:
		return ExceptionUnknown
	}
}

// cursorIsValid reports whether a cursor names anything. libclang returns a
// null cursor (InvalidFile) from the query functions that can find nothing.
func (l *Libclang) cursorIsValid(cursor CXCursor) bool {
	switch l.f.getCursorKind(cursor) {
	case cursorInvalidFile, cursorNoDeclFound:
		return false
	default:
		return true
	}
}

// specialization returns the template a declaration specializes or from which
// it was instantiated, and whether the cursor is a specialization at all. A
// null cursor (an invalid file or no declaration) means it is not.
func (l *Libclang) specialization(cursor CXCursor) (CXCursor, bool) {
	template := l.f.getSpecializedCursorTemplate(cursor)
	if !l.cursorIsValid(template) {
		return CXCursor{}, false
	}
	return template, true
}

// innerTemplateArguments splits the text between the outermost angle brackets
// of a type spelling at its top-level commas. It is the only source of a name
// for a template or pack argument, which libclang does not expose.
func innerTemplateArguments(spelling string) []string {
	start := strings.Index(spelling, "<")
	end := strings.LastIndex(spelling, ">")
	if start < 0 || end <= start {
		return nil
	}
	inner := spelling[start+1 : end]
	parts := make([]string, 0, 2)
	depth, from := 0, 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(inner[from:i]))
				from = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(inner[from:]))
	return parts
}

// fillArgumentSpellings gives a name to the arguments libclang does not spell
// (a template or a pack), by taking it from the type spelling.
func (l *Libclang) fillArgumentSpellings(args []TemplateArgument, typ CXType) []TemplateArgument {
	parts := innerTemplateArguments(l.str(l.f.getTypeSpelling(typ)))
	if len(parts) == 0 {
		return args
	}
	if len(args) == 1 && args[0].Kind == TemplateArgumentPack {
		args[0].Spelling = strings.Join(parts, ",")
		return args
	}
	for i := range args {
		if args[i].Spelling == "" && i < len(parts) {
			args[i].Spelling = parts[i]
		}
	}
	return args
}

// templateArguments reads the concrete arguments of a specialization.
func (l *Libclang) templateArguments(cursor CXCursor) []TemplateArgument {
	n := int(l.f.getNumTemplateArguments(cursor))
	if n <= 0 {
		return nil
	}
	args := make([]TemplateArgument, 0, n)
	for i := 0; i < n; i++ {
		index := uint32(i)
		arg := TemplateArgument{Kind: TemplateArgumentKindFromLibclang(l.f.getTemplateArgumentKind(cursor, index))}
		switch arg.Kind {
		case TemplateArgumentType:
			typ := l.f.getTemplateArgumentType(cursor, index)
			arg.Spelling = l.str(l.f.getTypeSpelling(typ))
			arg.Type = l.param(typ)
		case TemplateArgumentIntegral:
			arg.Value = l.f.getTemplateArgumentValue(cursor, index)
			arg.UnsignedValue = l.f.getTemplateArgumentUnsignedValue(cursor, index)
			arg.Spelling = fmt.Sprintf("%d", arg.Value)
		}
		args = append(args, arg)
	}
	return args
}
