package gen

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode"
)

// goKeywords are the Go reserved words a generated identifier must not be.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// exportName turns a C++ name into an exported Go identifier: split on
// underscores, strip empty parts, capitalise each part. "count_above" ->
// "CountAbove", "x_" -> "X", "saxpy" -> "Saxpy". A name that would start with a
// digit gets an X, and an empty name (an unnamed entity) becomes "X".
func exportName(name string) string {
	parts := strings.Split(name, "_")
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		runes := []rune(part)
		runes[0] = unicode.ToUpper(runes[0])
		b.WriteString(string(runes))
	}
	out := b.String()
	if out == "" {
		return "X"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "X" + out
	}
	return out
}

// typeSuffix renders a Go type as the CamelCase suffix an overloaded name
// carries: float64 -> Float64, []float64 -> Float64Slice, *Vector -> Vector.
// It is the rule DESIGN.md states for constructor and function overloads.
func typeSuffix(goType string) string {
	switch {
	case strings.HasPrefix(goType, "[]"):
		return typeSuffix(goType[2:]) + "Slice"
	case strings.HasPrefix(goType, "*"):
		return typeSuffix(goType[1:])
	}
	var b strings.Builder
	capitalise := true
	for _, r := range goType {
		switch {
		case r == '_' || r == '.':
			capitalise = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if capitalise {
				b.WriteRune(unicode.ToUpper(r))
				capitalise = false
			} else {
				b.WriteRune(r)
			}
		default:
			capitalise = true
		}
	}
	return b.String()
}

// shortHash is the stable identifier a binding's thunk, trampoline and symbol
// variable share. It is FNV-1a of the qualified signature, so the same input
// produces the same bytes regardless of file-system order, and a different
// overload produces a different name.
func shortHash(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return fmt.Sprintf("%012x", h.Sum64()&0xffffffffffff)
}

// receiverName is the Go receiver for a type: the first rune of the type name,
// lowercased, which is the convention a reviewer expects: Vector -> v,
// BoxInt -> b.
func receiverName(goType string) string {
	if goType == "" {
		return "recv"
	}
	return string(unicode.ToLower([]rune(goType)[0]))
}

// paramName is the Go name of one parameter. The C++ declared name is used
// when it is a usable identifier; otherwise the position decides. Uniqueness
// within the signature, and against the receiver, is the caller's job.
func paramName(declared string, index int) string {
	name := strings.TrimSpace(declared)
	if name == "" || name == "_" {
		return fmt.Sprintf("arg%d", index)
	}
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || unicode.IsLetter(r):
			b.WriteRune(r)
		case unicode.IsDigit(r):
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if out == "" || goKeywords[out] {
		return fmt.Sprintf("arg%d", index)
	}
	return out
}

// uniqueName makes name distinct from every taken name by appending _2, _3, ...
func uniqueName(name string, taken map[string]bool) string {
	if !taken[name] {
		taken[name] = true
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", name, i)
		if !taken[candidate] {
			taken[candidate] = true
			return candidate
		}
	}
}

// cppKeywords are the C++ keywords a generated C parameter name must not be.
var cppKeywords = func() map[string]bool {
	words := strings.Fields(`
		alignas alignof and and_eq asm auto bitand bitor bool break case catch
		char char16_t char32_t class compl const constexpr const_cast continue
		decltype default delete do double dynamic_cast else enum explicit export
		extern false float for friend goto if inline int long mutable namespace
		new noexcept not not_eq nullptr operator or or_eq private protected
		public register reinterpret_cast return short signed sizeof static
		static_assert static_cast struct switch template this thread_local throw
		true try typedef typeid typename union unsigned using virtual void
		volatile wchar_t while xor xor_eq
		size_t int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t`)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

// cIdent turns a Go parameter name into a safe C++ identifier.
func cIdent(name string) string {
	if name == "" || name == "_" {
		name = "arg"
	}
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if cppKeywords[out] {
		out += "_"
	}
	return out
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

// validGoPackage reports whether name can be a Go package clause.
func validGoPackage(name string) bool {
	if name == "" || goKeywords[name] {
		return false
	}
	for i, r := range name {
		switch {
		case r == '_' || unicode.IsLetter(r):
		case unicode.IsDigit(r):
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// goRecordName is the Go type name of a record: its own name, or for a
// template specialization the template name followed by its arguments, as
// DESIGN.md specifies: mathx::Box<int> -> BoxInt, Box<mathx::Vector> ->
// BoxVector, Pair<int, double> -> PairIntFloat64.
func goRecordName(qualified string) string {
	if open := strings.Index(qualified, "<"); open >= 0 {
		closing := strings.LastIndex(qualified, ">")
		if closing > open {
			var b strings.Builder
			b.WriteString(exportName(simpleName(qualified[:open])))
			for _, arg := range splitTemplateArgs(qualified[open+1 : closing]) {
				b.WriteString(typeSuffix(cppArgGoName(arg)))
			}
			return b.String()
		}
	}
	return exportName(simpleName(qualified))
}

// splitTemplateArgs splits a template argument list at its top-level commas.
func splitTemplateArgs(args string) []string {
	parts := make([]string, 0, 2)
	depth, from := 0, 0
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(args[from:i]))
				from = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(args[from:]); rest != "" {
		parts = append(parts, rest)
	}
	return parts
}

// cppArgGoName names a template argument the way the Go type map would.
func cppArgGoName(arg string) string {
	name := strings.TrimSpace(arg)
	for {
		switch {
		case strings.HasPrefix(name, "const "):
			name = strings.TrimSpace(name[len("const "):])
		case strings.HasPrefix(name, "volatile "):
			name = strings.TrimSpace(name[len("volatile "):])
		default:
			goto trimmed
		}
	}
trimmed:
	if mapped, ok := scalarArgNames[name]; ok {
		return mapped
	}
	if name != "" && name[0] >= '0' && name[0] <= '9' {
		return name
	}
	if strings.Contains(name, "<") {
		return goRecordName(name)
	}
	return exportName(simpleName(name))
}

// scalarArgNames maps the scalar spellings a template argument uses to the
// name DESIGN.md gives the specialization: Box<int> is BoxInt and
// Pair<int, double> is PairIntFloat64, so the integer spelling is capitalised
// while the floating-point one is named by its Go width.
var scalarArgNames = map[string]string{
	"bool":               "Bool",
	"char":               "Char",
	"signed char":        "Schar",
	"unsigned char":      "Uchar",
	"short":              "Short",
	"unsigned short":     "Ushort",
	"int":                "Int",
	"unsigned int":       "Uint",
	"unsigned":           "Uint",
	"long":               "Long",
	"unsigned long":      "Ulong",
	"long long":          "LongLong",
	"unsigned long long": "UlongLong",
	"float":              "Float32",
	"double":             "Float64",
	"std::size_t":        "Size",
	"size_t":             "Size",
	"std::int8_t":        "Int8",
	"int8_t":             "Int8",
	"std::uint8_t":       "Uint8",
	"uint8_t":            "Uint8",
	"std::int16_t":       "Int16",
	"int16_t":            "Int16",
	"std::uint16_t":      "Uint16",
	"uint16_t":           "Uint16",
	"std::int32_t":       "Int32",
	"int32_t":            "Int32",
	"std::uint32_t":      "Uint32",
	"uint32_t":           "Uint32",
	"std::int64_t":       "Int64",
	"int64_t":            "Int64",
	"std::uint64_t":      "Uint64",
	"uint64_t":           "Uint64",
	"wchar_t":            "Wchar",
	"char16_t":           "Char16",
	"char32_t":           "Char32",
}

// destructorSimpleName is the name a destructor is called by: the simple name
// of the type without its template arguments, because C++ spells
// Cell<int>::~Cell() and never ~Cell<int>().
func destructorSimpleName(qualified string) string {
	name := simpleName(qualified)
	if open := strings.Index(name, "<"); open >= 0 {
		return name[:open]
	}
	return name
}
