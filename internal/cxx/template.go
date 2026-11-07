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

// specializationName is the identity of a specialisation: the template's
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

// TemplateArgument is one concrete argument of a template specialisation. The
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
		return a.Spelling
	}
	return a.Kind.String()
}

// exceptionSpec maps libclang's exception specification kind onto the model.
func (l *Libclang) exceptionSpec(cursor CXCursor) ExceptionSpec {
	switch l.f.cursorExceptionSpec(cursor) {
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

// templateArguments reads the concrete arguments of a specialization.
func (l *Libclang) templateArguments(cursor CXCursor) []TemplateArgument {
	n := int(l.f.getNumTemplateArguments(cursor))
	if n <= 0 {
		return nil
	}
	args := make([]TemplateArgument, 0, n)
	for i := 0; i < n; i++ {
		index := uint32(i)
		arg := TemplateArgument{}
		switch l.f.getTemplateArgumentKind(cursor, index) {
		case templateArgumentType:
			arg.Kind = TemplateArgumentType
			typ := l.f.getTemplateArgumentType(cursor, index)
			arg.Spelling = l.str(l.f.getTypeSpelling(typ))
			arg.Type = l.param(typ)
		case templateArgumentIntegral:
			arg.Kind = TemplateArgumentIntegral
			arg.Value = l.f.getTemplateArgumentValue(cursor, index)
			arg.UnsignedValue = l.f.getTemplateArgumentUnsignedValue(cursor, index)
			arg.Spelling = fmt.Sprintf("%d", arg.Value)
		case templateArgumentDeclaration:
			arg.Kind = TemplateArgumentDeclaration
		case templateArgumentTemplate:
			arg.Kind = TemplateArgumentTemplate
		case templateArgumentExpression:
			arg.Kind = TemplateArgumentExpression
		case templateArgumentPack, templateArgumentExpansion:
			arg.Kind = TemplateArgumentPack
		default:
			arg.Kind = TemplateArgumentUnknown
		}
		args = append(args, arg)
	}
	return args
}
