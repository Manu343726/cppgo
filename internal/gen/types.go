package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// goKind is how a C++ type crosses into Go.
type goKind uint8

const (
	goVoid goKind = iota
	goInteger
	goFloat
	goBool
	goPointer
	goSlice
	goRecord
	goString
	goArray
	goOptional
	goFunc
	// goEnum is a C++ enum: a Go named integer type with the underlying
	// integer width on the wire.
	goEnum
	// goTypeParam is a class or function template's type parameter used as a
	// value, e.g. T in `T get() const`. The generic API's declaration spells
	// it; every concrete instantiation substitutes one scalar for it.
	goTypeParam
)

// stdKind is the standard-library shape a type crosses as.
type stdKind uint8

const (
	stdNone stdKind = iota
	stdString
	stdStringView
	stdVector
	stdSpan
	stdArray
	stdOptional
	stdUniquePtr
	stdSharedPtr
	stdFunction
)

// goType is one C++ type on both sides of the boundary.
type goType struct {
	// Kind is the Go shape of the value.
	Kind goKind
	// Go is the Go spelling in the generated API: "float64", "[]float64",
	// "*Vector", "Point".
	Go string
	// C is the parameter type in the generated extern "C" prototype. Records
	// cross as void pointers, so the shim header stays standalone and the
	// cast happens in the translation unit where the layout is known.
	C string
	// Cxx is the C++ type a thunk casts a pointer back to: "::mathx::Vector",
	// "double".
	Cxx string
	// Elem is the pointee of a pointer or the element of a slice.
	Elem *goType
	// Record is the record model a record or record pointer refers to.
	Record *recordModel
	// Enum is the enum model an enum value refers to.
	Enum *enumModel
	// Std is the standard-library shape, stdNone for an ordinary type.
	Std stdKind
	// ArrayLen is std::array's element count.
	ArrayLen int64
	// Const is top-level const of the pointee, for the C parameter type.
	Const bool
	// Param is the model type this was mapped from.
	Param cxx.Param
	// ParamName is the C++ template parameter a goTypeParam stands for, e.g.
	// "T". It is empty for every other kind.
	ParamName string
	// Callback is the signature of a function-pointer or std::function
	// parameter, nil for everything else.
	Callback *callbackType
	// ByValue is true when the type is a non-POD record that the C++ side takes
	// or returns by value. The Go value is *T, an opaque value owning the
	// storage, and the copy into or out of the call is made by the shim: it is
	// the only place the type's copy constructor exists.
	ByValue bool
	// CString is true when the type is `char *` or `const char *`: the Go value
	// is a string, a parameter crosses as a NUL-terminated copy, and a result
	// is copied out of the library's own storage before the call returns.
	CString bool
}

// callbackType is the signature of a Go function handed to C++. The carrier
// reads its arguments from the register file and writes one result register,
// so only scalars are carried: six INTEGER and eight SSE arguments, one
// register result.
type callbackType struct {
	// Params are the callback's arguments, in declaration order.
	Params []*goType
	// Result is the callback's result, nil for void.
	Result *goType
	// Go is the Go function type: "func(int32, int32) int32".
	Go string
	// Cxx is the C++ function-pointer type the shim casts the address to.
	Cxx string
	// StdCxx is the std::function spelling when the parameter is one, empty
	// for a plain function pointer.
	StdCxx string
	// Std is true when the C++ parameter is a std::function: the shim builds
	// the object from the plain function pointer.
	Std bool
	// Reason is the generation-time explanation when the signature is not
	// carried, empty when it is.
	Reason string
}

// mapper maps C++ types onto Go, using the package's record model to tell a
// mirrorable aggregate from an opaque value.
type mapper struct {
	records map[string]*recordModel
	// enums is the package's enum model, by qualified C++ name: an enum is a
	// Go named integer type, so a signature that names one crosses as that
	// type over its underlying integer width.
	enums map[string]*enumModel
	// usesSpan is true when std::span is part of the API, whose header needs
	// C++20.
	usesSpan bool
	// templateParams maps a class or function template's parameter names —
	// and libclang's indexed "type-parameter-0-N" spelling — to the Go name
	// the generic API declares. It is nil for a non-template declaration.
	templateParams map[string]string
}

// mapType maps one C++ type, rejecting only what this build cannot emit yet.
//
// A standard-library type is recognized by its declaration spelling, before the
// canonical kind is consulted: libclang's canonical form of a reference to a
// class template alias can be wrong (std::string & resolves to int & on some
// versions), so the spelling is the reliable half exactly as it is for streams.
func (m *mapper) mapType(p cxx.Param) (*goType, error) {
	// A function pointer is decided before anything else: its spelling starts
	// with its return type, so "std::int32_t (*)(int)" would otherwise be read
	// as a standard-library type named std::int32_t (*)(int).
	if callable, ok := functionPointer(p); ok {
		return m.mapFunction(p, callable, false)
	}
	if name, ok := m.templateParam(p.Spelling); ok {
		return &goType{Kind: goTypeParam, Go: name, ParamName: name, Param: p}, nil
	}
	if name, ok := stdTypeName(p); ok {
		return m.mapStd(p, name)
	}
	switch p.Kind {
	case cxx.TypeVoid:
		return &goType{Kind: goVoid, Go: "", Param: p}, nil
	case cxx.TypeInteger:
		return m.mapInteger(p)
	case cxx.TypeFloat:
		return m.mapFloat(p)
	case cxx.TypePointer:
		return m.mapPointer(p)
	case cxx.TypeRecord:
		return m.mapRecord(p)
	case cxx.TypeEnum:
		return m.mapEnum(p)
	case cxx.TypeArray:
		return nil, pending(fmt.Sprintf("%q is a C array", p.Spelling), "decay it to a pointer and a length at the C++ boundary")
	default:
		return nil, pending(fmt.Sprintf("%q is not a type this build classifies", p.Spelling), "expose it as a scalar, a pointer, a reference or a supported std:: type")
	}
}

// mapStd maps DESIGN.md's standard-library table. The shape of the Go value is
// decided here; the generated thunk constructs, copies and destroys the C++
// value, so no standard-library type is ever visible in Go or in the C ABI.
func (m *mapper) mapStd(p cxx.Param, name string) (*goType, error) {
	if strings.HasSuffix(strings.TrimSpace(p.Spelling), "*") {
		return nil, pending(fmt.Sprintf("%q is a pointer to a standard-library type", p.Spelling),
			"take it by value or by reference at the C++ boundary")
	}
	switch {
	case name == "std::string":
		return &goType{Kind: goString, Go: "string", Std: stdString, Param: p}, nil
	case name == "std::string_view":
		return &goType{Kind: goString, Go: "string", Std: stdStringView, Param: p}, nil
	case strings.HasPrefix(name, "std::vector<"):
		elem, err := m.mapTemplateElement(name, "std::vector")
		if err != nil {
			return nil, err
		}
		return &goType{Kind: goSlice, Go: "[]" + elem.Go, Elem: elem, Std: stdVector, Param: p}, nil
	case strings.HasPrefix(name, "std::span<"):
		m.usesSpan = true
		elem, err := m.mapTemplateElement(name, "std::span")
		if err != nil {
			return nil, err
		}
		return &goType{Kind: goSlice, Go: "[]" + elem.Go, Elem: elem, Std: stdSpan, Param: p}, nil
	case strings.HasPrefix(name, "std::array<"):
		args, err := templateArguments(name, "std::array")
		if err != nil {
			return nil, err
		}
		if len(args) != 2 {
			return nil, pending(fmt.Sprintf("%q is not a two-argument std::array", name), "write std::array<T, N> at the C++ boundary")
		}
		elem, err := m.mapElement(args[0])
		if err != nil {
			return nil, err
		}
		count, err := arrayCount(args[1])
		if err != nil {
			return nil, fmt.Errorf("%q: %w", name, err)
		}
		return &goType{Kind: goArray, Go: fmt.Sprintf("[%d]%s", count, elem.Go), Elem: elem, ArrayLen: count, Std: stdArray, Param: p}, nil
	case strings.HasPrefix(name, "std::optional<"):
		elem, err := m.mapTemplateElement(name, "std::optional")
		if err != nil {
			return nil, err
		}
		return &goType{Kind: goOptional, Go: "*" + elem.Go, Elem: elem, Std: stdOptional, Param: p}, nil
	case strings.HasPrefix(name, "std::unique_ptr<"):
		elem, err := m.mapSmartPointer(name, "std::unique_ptr")
		if err != nil {
			return nil, err
		}
		return &goType{Kind: goPointer, Go: "*" + elem.Go, Elem: elem, Record: elem.Record, Std: stdUniquePtr, Param: p}, nil
	case strings.HasPrefix(name, "std::shared_ptr<"):
		elem, err := m.mapSmartPointer(name, "std::shared_ptr")
		if err != nil {
			return nil, err
		}
		return &goType{Kind: goPointer, Go: "*" + elem.Go, Elem: elem, Record: elem.Record, Std: stdSharedPtr, Param: p}, nil
	case strings.HasPrefix(name, "std::function<"):
		signature, err := functionSignature(name, "std::function")
		if err != nil {
			return nil, err
		}
		return m.mapFunction(p, signature, true)
	default:
		return nil, pending(fmt.Sprintf("%q is a standard-library type outside the emitted table", name),
			"cross it as a scalar, T*/length or POD, or bind a C++ wrapper with a supported signature")
	}
}

// templateArguments returns the top-level arguments of a class template
// spelling such as std::vector<double>.
func templateArguments(name, prefix string) ([]string, error) {
	if !strings.HasPrefix(name, prefix+"<") || !strings.HasSuffix(name, ">") {
		return nil, pending(fmt.Sprintf("%q is not a %s", name, prefix), "write "+prefix+"<...> at the C++ boundary")
	}
	args := splitTemplateArgs(name[len(prefix)+1 : len(name)-1])
	if len(args) == 0 {
		return nil, pending(fmt.Sprintf("%q has no template arguments", name), "write a concrete specialization at the C++ boundary")
	}
	return args, nil
}

// mapTemplateElement maps the first argument of a one-argument template.
func (m *mapper) mapTemplateElement(name, prefix string) (*goType, error) {
	args, err := templateArguments(name, prefix)
	if err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, pending(fmt.Sprintf("%q has %d template arguments", name, len(args)), "write a one-argument specialization at the C++ boundary")
	}
	return m.mapElement(args[0])
}

// mapSmartPointer maps std::unique_ptr<T> and std::shared_ptr<T>. The pointee
// is always a record the generated API knows: the Go value is *T and owns (or
// shares) the object.
func (m *mapper) mapSmartPointer(name, prefix string) (*goType, error) {
	args, err := templateArguments(name, prefix)
	if err != nil {
		return nil, err
	}
	if len(args) != 1 {
		return nil, pending(fmt.Sprintf("%q has %d template arguments", name, len(args)), "write a one-argument specialization at the C++ boundary")
	}
	record, ok := m.lookupRecord(strings.TrimSpace(args[0]))
	if !ok {
		return nil, pending(fmt.Sprintf("%q points at a record that is not part of the generated API", name),
			"include the record's definition and name a template instantiation if it is one")
	}
	return &goType{Kind: goRecord, Go: record.GoName, Cxx: "::" + record.CxxName, Record: record, Std: stdNone}, nil
}

// mapElement maps one template argument: a scalar or a mirrored record.
func (m *mapper) mapElement(spelling string) (*goType, error) {
	name := strings.TrimSpace(spelling)
	isConst := strings.HasPrefix(name, "const ")
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
	if param, ok := m.templateParam(name); ok {
		return &goType{Kind: goTypeParam, Go: param, ParamName: param, Const: isConst}, nil
	}
	if scalar, ok := elementScalars[name]; ok {
		return &goType{Kind: scalar.kind, Go: scalar.goName, C: scalar.cName, Cxx: scalar.cName, Const: isConst}, nil
	}
	if record, ok := m.lookupRecord(name); ok {
		if !record.Mirrored {
			return nil, pending(fmt.Sprintf("%q is not a POD", name), "use scalar elements, or bind a C++ wrapper that copies the objects")
		}
		return &goType{Kind: goRecord, Go: record.GoName, C: "void *", Cxx: "::" + record.CxxName, Record: record, Const: isConst}, nil
	}
	return nil, pending(fmt.Sprintf("%q is not a scalar or a known record", name), "use a scalar element type at the C++ boundary")
}

// recordFor finds the record a declared type names: the exact qualified name
// first, then a unique simple-name match, because a declaration inside a
// namespace or a class spells its own types unqualified ("Matrix<double, 3, 3>"
// for Eigen::Matrix<double, 3, 3>).
func (m *mapper) recordFor(name string) (*recordModel, bool) {
	if record, ok := m.records[name]; ok {
		return record, true
	}
	return m.lookupRecord(simpleNameRespectingTemplates(name))
}

// simpleNameRespectingTemplates is the last component of a qualified name,
// split outside any template argument list.
func simpleNameRespectingTemplates(name string) string {
	if i := lastQualifiedSeparator(name); i >= 0 {
		return name[i+2:]
	}
	return name
}

// lookupRecord finds a record by its spelling. A template argument is often
// spelled unqualified inside its own namespace ("V" for "ns::V"), so an exact
// miss falls back to a unique simple-name match; an ambiguous name is a miss
// and the caller reports it.
func (m *mapper) lookupRecord(name string) (*recordModel, bool) {
	if record, ok := m.records[name]; ok {
		return record, true
	}
	// The same type is spelled with and without spaces after a template
	// argument's comma, depending on where libclang read it.
	wanted := strings.ReplaceAll(name, " ", "")
	var found *recordModel
	for qualified, record := range m.records {
		if strings.ReplaceAll(simpleName(qualified), " ", "") != wanted {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = record
	}
	return found, found != nil
}

// elementScalars maps the scalar spellings a container argument uses.
var elementScalars = map[string]struct {
	kind   goKind
	goName string
	cName  string
}{
	"bool":               {goBool, "bool", "bool"},
	"char":               {goInteger, "int8", "int8_t"},
	"signed char":        {goInteger, "int8", "int8_t"},
	"unsigned char":      {goInteger, "uint8", "uint8_t"},
	"short":              {goInteger, "int16", "int16_t"},
	"unsigned short":     {goInteger, "uint16", "uint16_t"},
	"int":                {goInteger, "int32", "int32_t"},
	"unsigned int":       {goInteger, "uint32", "uint32_t"},
	"unsigned":           {goInteger, "uint32", "uint32_t"},
	"long":               {goInteger, "int64", "int64_t"},
	"unsigned long":      {goInteger, "uint64", "uint64_t"},
	"long long":          {goInteger, "int64", "int64_t"},
	"unsigned long long": {goInteger, "uint64", "uint64_t"},
	"float":              {goFloat, "float32", "float"},
	"double":             {goFloat, "float64", "double"},
	"std::size_t":        {goInteger, "uint64", "uint64_t"},
	"size_t":             {goInteger, "uint64", "uint64_t"},
	"std::int8_t":        {goInteger, "int8", "int8_t"},
	"int8_t":             {goInteger, "int8", "int8_t"},
	"std::uint8_t":       {goInteger, "uint8", "uint8_t"},
	"uint8_t":            {goInteger, "uint8", "uint8_t"},
	"std::int16_t":       {goInteger, "int16", "int16_t"},
	"int16_t":            {goInteger, "int16", "int16_t"},
	"std::uint16_t":      {goInteger, "uint16", "uint16_t"},
	"uint16_t":           {goInteger, "uint16", "uint16_t"},
	"std::int32_t":       {goInteger, "int32", "int32_t"},
	"int32_t":            {goInteger, "int32", "int32_t"},
	"std::uint32_t":      {goInteger, "uint32", "uint32_t"},
	"uint32_t":           {goInteger, "uint32", "uint32_t"},
	"std::int64_t":       {goInteger, "int64", "int64_t"},
	"int64_t":            {goInteger, "int64", "int64_t"},
	"std::uint64_t":      {goInteger, "uint64", "uint64_t"},
	"uint64_t":           {goInteger, "uint64", "uint64_t"},
}

// arrayCount parses std::array's non-type size argument.
func arrayCount(spelling string) (int64, error) {
	value := strings.TrimRight(strings.TrimSpace(spelling), "uUlL")
	var count int64
	if _, err := fmt.Sscan(value, &count); err != nil || count < 0 {
		return 0, pending(fmt.Sprintf("%q is not an array size", spelling), "write a literal count in std::array<T, N>")
	}
	return count, nil
}

// stdTypeName recognizes a standard-library type by its declaration spelling
// and returns the normalised name.
//
// The canonical type cannot decide this: libclang's canonical form of a
// reference to a class-template alias can be wrong (std::string & resolves to
// int & on some versions), so a std:: spelling on a record or pointer/reference
// declaration is a library type even when the pointee kind says "integer". The
// scalar aliases (std::size_t, std::int32_t, ...) are excluded explicitly, and
// a pointer to a library type (as opposed to a reference or a value) is left to
// mapStd's error.
func stdTypeName(p cxx.Param) (string, bool) {
	if p.Kind != cxx.TypeRecord && p.Kind != cxx.TypePointer {
		return "", false
	}
	s := strings.TrimSpace(p.Spelling)
	for {
		switch {
		case strings.HasPrefix(s, "const "):
			s = strings.TrimSpace(s[len("const "):])
		case strings.HasPrefix(s, "volatile "):
			s = strings.TrimSpace(s[len("volatile "):])
		default:
			goto trimmed
		}
	}
trimmed:
	s = strings.TrimSuffix(s, "&&")
	s = strings.TrimSuffix(s, "&")
	s = strings.TrimSuffix(s, "*")
	s = strings.TrimSpace(s)
	if s == "std::nullptr_t" || !strings.HasPrefix(s, "std::") {
		return "", false
	}
	if _, scalar := elementScalars[s]; scalar {
		return "", false
	}
	return s, true
}

// mapEnum maps an enum to its named Go integer type. The wire width and the
// shim's C type are the underlying integer type, which is what the enum is at
// the ABI level, and the C++ side casts back to the enum where its name exists.
func (m *mapper) mapEnum(p cxx.Param) (*goType, error) {
	enum, ok := m.enums[p.RecordName]
	if !ok {
		return nil, pending(fmt.Sprintf("%q is an enum the generated API does not hold", p.Spelling),
			"include the enum's definition in the header")
	}
	return &goType{
		Kind:  goEnum,
		Go:    enum.GoName,
		C:     enum.C,
		Cxx:   "::" + enum.CxxName,
		Enum:  enum,
		Param: p,
		Const: p.IsConst,
	}, nil
}

// mapInteger maps every integer width the type map covers, and checks the LP64
// assumption the contract makes: on a platform where long is not 64-bit the
// generated code would silently reinterpret a value.
func (m *mapper) mapInteger(p cxx.Param) (*goType, error) {
	if p.ClangKind == "Bool" {
		return &goType{Kind: goBool, Go: "bool", C: "bool", Cxx: "bool", Param: p}, nil
	}
	if (p.ClangKind == "Long" || p.ClangKind == "ULong") && p.SizeBytes != 8 {
		return nil, pending(fmt.Sprintf("%q is %d bytes on this target, not 8", p.Spelling, p.SizeBytes), "the type map is LP64; expose a 64-bit value at the C++ boundary")
	}
	goName, cName, ok := integerType(p.SizeBytes, p.Unsigned)
	if !ok {
		return nil, pending(fmt.Sprintf("%q has a %d-byte width with no Go value type", p.Spelling, p.SizeBytes), "widen or narrow it to a fixed-width integer at the C++ boundary")
	}
	return &goType{Kind: goInteger, Go: goName, C: cName, Cxx: cName, Param: p}, nil
}

// integerType names the Go and C types of an integer of the given width.
func integerType(size int64, unsigned bool) (goName, cName string, ok bool) {
	switch size {
	case 1:
		if unsigned {
			return "uint8", "uint8_t", true
		}
		return "int8", "int8_t", true
	case 2:
		if unsigned {
			return "uint16", "uint16_t", true
		}
		return "int16", "int16_t", true
	case 4:
		if unsigned {
			return "uint32", "uint32_t", true
		}
		return "int32", "int32_t", true
	case 8:
		if unsigned {
			return "uint64", "uint64_t", true
		}
		return "int64", "int64_t", true
	default:
		return "", "", false
	}
}

// mapFloat maps float and double, and refuses long double with the wrapper that
// makes it representable instead of a silent narrowing.
func (m *mapper) mapFloat(p cxx.Param) (*goType, error) {
	if p.ClangKind == "LongDouble" {
		return nil, fmt.Errorf("cppgo: %s: long double and __int128 have no Go value type; widen or narrow it at the C++ boundary to double or int64, or wrap it in a representable type", p.Spelling)
	}
	switch p.SizeBytes {
	case 4:
		return &goType{Kind: goFloat, Go: "float32", C: "float", Cxx: "float", Param: p}, nil
	case 8:
		return &goType{Kind: goFloat, Go: "float64", C: "double", Cxx: "double", Param: p}, nil
	default:
		return nil, pending(fmt.Sprintf("%q has no Go value type", p.Spelling), "widen or narrow it at the C++ boundary to double or int64, or wrap it in a representable type")
	}
}

// mapPointer maps T* and T&. A pointer to a scalar is *T; a pointer to a
// record is *Record, where a record value is the address of the Go value or of
// the object the opaque Go type owns.
func (m *mapper) mapPointer(p cxx.Param) (*goType, error) {
	if p.Spelling == "std::nullptr_t" || p.ClangKind == "NullPtr" {
		return nil, pending(fmt.Sprintf("%q is nullptr_t", p.Spelling), "pass a typed null pointer at the C++ boundary")
	}
	if p.Pointee == nil {
		return nil, pending(fmt.Sprintf("%q is a pointer libclang did not describe", p.Spelling), "include the pointed-to definition in the header")
	}
	if p.Pointee.ClangKind == "Invalid" {
		// A typedef of `void *` (`af_array`) canonicalizes to a pointer whose
		// pointee libclang cannot name: there is no Go value for it, and the
		// declaration is reported, not guessed at.
		return nil, pending(fmt.Sprintf("%q is a void pointer", p.Spelling), "expose a typed pointer at the C++ boundary")
	}
	if isCString(p) {
		// A C string is the one char pointer with a Go value: string. The C
		// type keeps its constness, which is what the thunk's prototype and
		// the result copy need.
		qualifier := ""
		if p.Pointee.IsConst {
			qualifier = "const "
		}
		return &goType{
			Kind:    goString,
			Go:      "string",
			C:       qualifier + "char *",
			Cxx:     qualifier + "char *",
			CString: true,
			Param:   p,
		}, nil
	}
	var elem *goType
	if p.Pointee.Kind == cxx.TypeRecord {
		// A record pointer does not need the record to be mirrorable: the Go
		// type is *Record either way, and an opaque one crosses as the address
		// of the object it owns.
		record, ok := m.recordFor(p.Pointee.RecordName)
		if !ok {
			return nil, missingRecord(*p.Pointee)
		}
		elem = &goType{Kind: goRecord, Go: record.GoName, Cxx: "::" + record.CxxName, Record: record, Param: *p.Pointee}
	} else {
		mapped, err := m.mapType(*p.Pointee)
		if err != nil {
			return nil, err
		}
		elem = mapped
	}
	if elem.Kind == goVoid {
		return nil, pending(fmt.Sprintf("%q is a void pointer", p.Spelling), "expose a typed pointer at the C++ boundary")
	}
	if elem.Kind == goRecord {
		// A record pointer crosses as void *: the shim header stays standalone
		// and the thunk casts in the translation unit where the layout is
		// known. An opaque record's Go API is *Vector, and the value that
		// reaches C++ is the object's address, not the wrapper struct's.
		qualified := "void"
		if p.Pointee.IsConst {
			qualified = "const void"
		}
		return &goType{
			Kind:   goPointer,
			Go:     "*" + elem.Go,
			C:      qualified + " *",
			Cxx:    elem.Cxx,
			Elem:   elem,
			Record: elem.Record,
			Const:  p.Pointee.IsConst,
			Param:  p,
		}, nil
	}
	cType := elem.C
	if p.Pointee.IsConst {
		cType = "const " + cType
	}
	return &goType{
		Kind:  goPointer,
		Go:    "*" + elem.Go,
		C:     cType + " *",
		Cxx:   elem.Cxx,
		Elem:  elem,
		Const: p.Pointee.IsConst,
		Param: p,
	}, nil
}

// isCString reports whether the parameter is `char *` or `const char *`: a
// pointer to a plain char, never to signed char or unsigned char, which are
// different C++ types with different Go mappings.
func isCString(p cxx.Param) bool {
	// The caller has already checked that this is a pointer with a pointee.
	return p.Pointee.Kind == cxx.TypeInteger &&
		(p.Pointee.ClangKind == "Char_S" || p.Pointee.ClangKind == "Char_U")
}

// mapRecord maps a record by value.
//
// A mirrored aggregate is a Go struct whose layout *is* the C++ layout, so the
// Go value is the record itself and the thunk copies it into the call — the
// place the C++ copy constructor lives, and the place a value above 16 bytes is
// materialized without any Go-side stack knowledge.
//
// A record Go cannot mirror (a non-POD one: a user-declared constructor, a
// destructor, private members) has no layout Go may copy, so it crosses as *T,
// the opaque Go value that owns storage aligned for the C++ type. The shim
// copies it into the call, or constructs the result into Go's storage, which is
// why such a binding is always shim-carried. The Go side owns both directions:
// Close destroys the object when the record declares a destructor, and releases
// its storage alone when it declares none.
func (m *mapper) mapRecord(p cxx.Param) (*goType, error) {
	record, ok := m.recordFor(p.RecordName)
	if !ok {
		return nil, missingRecord(p)
	}
	if !record.Mirrored {
		if record.Size <= 0 || record.Align <= 0 {
			return nil, pending(fmt.Sprintf("%q is passed by value but its layout is unknown", p.Spelling),
				"make the type complete in the header, or take it by reference at the C++ boundary")
		}
		return &goType{
			Kind:    goPointer,
			Go:      "*" + record.GoName,
			C:       "void *",
			Cxx:     "::" + record.CxxName,
			Record:  record,
			Param:   p,
			ByValue: true,
		}, nil
	}
	return &goType{
		Kind:   goRecord,
		Go:     record.GoName,
		C:      "void *",
		Cxx:    "::" + record.CxxName,
		Record: record,
		Param:  p,
	}, nil
}

// missingRecord is the error for a record the generated API does not hold. A
// template specialization is the common cause: it exists as a type but the
// manifest has not named it.
func missingRecord(p cxx.Param) error {
	fix := "include its complete definition in the header"
	if strings.Contains(p.RecordName, "<") {
		fix = "name it with -instantiate '" + p.RecordName + "'"
	}
	return pending(fmt.Sprintf("%q is not part of the generated API", p.Spelling), fix)
}

// signature is a C++ return type and parameter list, split out of a spelling.
type signature struct {
	result string
	params []string
}

// functionSignature splits "R(A, B)" out of a std::function spelling.
func functionSignature(name, prefix string) (signature, error) {
	args, err := templateArguments(name, prefix)
	if err != nil {
		return signature{}, err
	}
	if len(args) != 1 {
		return signature{}, pending(fmt.Sprintf("%q has %d template arguments", name, len(args)),
			"write a one-argument "+prefix+"<R(A...)> at the C++ boundary")
	}
	return splitSignature(args[0])
}

// splitSignature splits a "R(A, B)" spelling at its top-level parentheses.
func splitSignature(text string) (signature, error) {
	text = strings.TrimSpace(text)
	open := strings.IndexByte(text, '(')
	if open < 0 || !strings.HasSuffix(text, ")") {
		return signature{}, pending(fmt.Sprintf("%q is not a function type", text),
			"write the signature as R(A...) at the C++ boundary")
	}
	result := strings.TrimSpace(text[:open])
	inner := strings.TrimSpace(text[open+1 : len(text)-1])
	var params []string
	if inner != "" && inner != "void" {
		params = splitTemplateArgs(inner)
	}
	return signature{result: result, params: params}, nil
}

// functionPointer recognizes a pointer to a function and returns its signature
// from the declaration spelling: libclang reports the pointee as a function
// prototype with no argument list of its own in the model.
func functionPointer(p cxx.Param) (signature, bool) {
	if p.Kind != cxx.TypePointer || p.Pointee == nil || p.Pointee.ClangKind != "FunctionProto" {
		return signature{}, false
	}
	callable, err := splitSignature(p.Pointee.Spelling)
	if err != nil {
		return signature{}, false
	}
	return callable, true
}

// mapFunction maps a function-pointer or std::function parameter: the Go value
// is a func, the C++ value is built by the shim from the callback table's
// address, and only scalars are carried.
func (m *mapper) mapFunction(p cxx.Param, callable signature, std bool) (*goType, error) {
	mapped := &callbackType{Std: std}
	for _, spelling := range callable.params {
		element, err := m.callbackElement(spelling)
		if err != nil {
			return nil, err
		}
		mapped.Params = append(mapped.Params, element)
	}
	if result := strings.TrimSpace(callable.result); result != "" && result != "void" {
		element, err := m.callbackElement(result)
		if err != nil {
			return nil, err
		}
		mapped.Result = element
	}
	// The ABI carries the register file only: six INTEGER and eight SSE
	// arguments, and one register result.
	ints, floats := 0, 0
	for _, param := range mapped.Params {
		if param.Kind == goFloat {
			floats++
		} else {
			ints++
		}
	}
	switch {
	case ints > maxIntRegs:
		return nil, pending(fmt.Sprintf("%q takes more than %d INTEGER arguments", p.Spelling, maxIntRegs),
			"take a pointer to a struct instead")
	case floats > maxFloatRegs:
		return nil, pending(fmt.Sprintf("%q takes more than %d SSE arguments", p.Spelling, maxFloatRegs),
			"take a pointer to a struct instead")
	}
	mapped.Go = goFunctionType(mapped)
	mapped.Cxx = cxxFunctionType(mapped)
	if std {
		mapped.StdCxx = stdFunctionType(mapped)
	}
	return &goType{Kind: goFunc, Go: mapped.Go, Callback: mapped, Std: stdKindFor(std), Param: p}, nil
}

// stdKindFor is the goType's standard-library tag of a callable, so that a
// std::function parameter routes through mapStd's machinery as well.
func stdKindFor(std bool) stdKind {
	if std {
		return stdFunction
	}
	return stdNone
}

// callbackElement maps one callback argument or its result.
func (m *mapper) callbackElement(spelling string) (*goType, error) {
	element, err := m.mapElement(spelling)
	if err != nil {
		return nil, err
	}
	if element.Kind == goFloat && element.Go == "float32" {
		return nil, pending(fmt.Sprintf("the callback argument %q is a float", spelling),
			"take a double instead: a float argument is not carried by this build yet")
	}
	if element.Kind == goRecord || element.Kind == goString || element.Kind == goSlice || element.Kind == goArray {
		return nil, pending(fmt.Sprintf("the callback argument %q is not a scalar", spelling),
			"take an integer, a double or a pointer instead")
	}
	return element, nil
}

// goFunctionType renders the Go func type of a callback.
func goFunctionType(callback *callbackType) string {
	params := make([]string, len(callback.Params))
	for i, param := range callback.Params {
		params[i] = param.Go
	}
	out := "func(" + strings.Join(params, ", ") + ")"
	if callback.Result != nil {
		out += " " + callback.Result.Go
	}
	return out
}

// cxxFunctionType renders the C++ function-pointer type the shim casts the
// callback address to, in the C spellings the Go types already carry.
func cxxFunctionType(callback *callbackType) string {
	params := make([]string, len(callback.Params))
	for i, param := range callback.Params {
		params[i] = param.C
	}
	result := "void"
	if callback.Result != nil {
		result = callback.Result.C
	}
	return result + " (*)(" + strings.Join(params, ", ") + ")"
}

// stdFunctionType renders the std::function the shim builds from a plain
// function pointer when the C++ parameter is one.
func stdFunctionType(callback *callbackType) string {
	params := make([]string, len(callback.Params))
	for i, param := range callback.Params {
		params[i] = param.C
	}
	result := "void"
	if callback.Result != nil {
		result = callback.Result.C
	}
	return "std::function<" + result + "(" + strings.Join(params, ", ") + ")>"
}

// templateParam reports whether a type spelling names one of the template's
// parameters, and returns the Go name the generic declaration uses. A
// parameter reached through libclang's canonical "type-parameter-0-N" spelling
// is matched by the map too; leading const and volatile qualifiers are not
// part of the identity.
func (m *mapper) templateParam(spelling string) (string, bool) {
	if m.templateParams == nil {
		return "", false
	}
	name := strings.TrimSpace(spelling)
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
	param, ok := m.templateParams[strings.TrimSpace(name)]
	return param, ok
}
