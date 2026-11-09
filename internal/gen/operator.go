package gen

import (
	"fmt"
	"strings"
)

// operatorNames is the fixed Go name of every C++ operator this build binds.
//
// The name is a word, never punctuation: a Go method is called by name, so
// operator+ is Add and operator[] is At. The table is deliberately complete for
// the language's operators, because a name outside it is a generation error
// asking for -rename, and a real header declares most of these.
//
// operator new and operator delete are deliberately absent: the Go side
// allocates, so a binding for either would expose an operation Go cannot call.
var operatorNames = map[string]string{
	"=":   "Assign",
	"+":   "Add",
	"-":   "Sub",
	"*":   "Mul",
	"/":   "Div",
	"%":   "Mod",
	"+=":  "AddAssign",
	"-=":  "SubAssign",
	"*=":  "MulAssign",
	"/=":  "DivAssign",
	"%=":  "ModAssign",
	"==":  "Equal",
	"!=":  "NotEqual",
	"<":   "Less",
	"<=":  "LessEqual",
	">":   "Greater",
	">=":  "GreaterEqual",
	"<=>": "Compare",
	"[]":  "At",
	"()":  "Call",
	"->":  "Arrow",
	"++":  "Inc",
	"--":  "Dec",
	"!":   "Not",
	"&&":  "And",
	"||":  "Or",
	"&":   "BitAnd",
	"|":   "BitOr",
	"^":   "BitXor",
	"~":   "BitNot",
	"<<":  "ShiftLeft",
	">>":  "ShiftRight",
	"<<=": "ShiftLeftAssign",
	">>=": "ShiftRightAssign",
	"&=":  "BitAndAssign",
	"|=":  "BitOrAssign",
	"^=":  "BitXorAssign",
	",":   "Comma",
}

// operatorGoName is the Go name of a C++ operator declaration.
//
// An ordinary operator takes the table's word. A conversion operator is named
// after the Go type it converts to: operator int is Int32, operator double is
// Float64, operator std::string is String, and operator T for a record is
// As<T>, because a record conversion is a view of the same object, not a value.
func (b *builder) operatorGoName(binding *binding) (string, error) {
	sym := binding.sym
	if sym.IsConversion {
		return b.conversionName(binding)
	}
	if name, ok := operatorNames[strings.TrimSpace(sym.Operator)]; ok {
		return name, nil
	}
	return "", pending(
		fmt.Sprintf("%q is an operator outside the naming table", sym.Operator),
		fmt.Sprintf("rename it with -rename '%s=<GoName>'", displaySignature(sym)))
}

// conversionName names a conversion operator after its target Go type.
func (b *builder) conversionName(binding *binding) (string, error) {
	mapped, err := b.mapper.mapType(binding.sym.Result)
	if err != nil {
		return "", fmt.Errorf("the conversion target: %w", err)
	}
	if mapped.Kind == goRecord {
		return "As" + mapped.Go, nil
	}
	name := typeSuffix(mapped.Go)
	if name == "" {
		return "", pending(
			fmt.Sprintf("the conversion target %q has no Go name", binding.sym.Result.Spelling),
			fmt.Sprintf("rename it with -rename '%s=<GoName>'", displaySignature(binding.sym)))
	}
	return name, nil
}
