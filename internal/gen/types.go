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
	// Const is top-level const of the pointee, for the C parameter type.
	Const bool
	// Param is the model type this was mapped from.
	Param cxx.Param
}

// mapper maps C++ types onto Go, using the package's record model to tell a
// mirrorable aggregate from an opaque value.
type mapper struct {
	records map[string]*recordModel
}

// mapType maps one C++ type, rejecting only what this build cannot emit yet.
//
// A standard-library type is recognised by its declaration spelling, before the
// canonical kind is consulted: libclang's canonical form of a reference to a
// class template alias can be wrong (std::string & resolves to int & on some
// versions), so the spelling is the reliable half exactly as it is for streams.
func (m *mapper) mapType(p cxx.Param) (*goType, error) {
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
		return nil, pending(fmt.Sprintf("%q is an enum", p.Spelling), "pass the value as its underlying integer at the C++ boundary")
	case cxx.TypeArray:
		return nil, pending(fmt.Sprintf("%q is a C array", p.Spelling), "decay it to a pointer and a length at the C++ boundary")
	default:
		return nil, pending(fmt.Sprintf("%q is not a type this build classifies", p.Spelling), "expose it as a scalar, a pointer, a reference or a supported std:: type")
	}
}

// mapStd maps a standard-library type. Every type in DESIGN.md's table is
// implemented by the marshalling unit; until then the construct is named, not
// guessed.
func (m *mapper) mapStd(p cxx.Param, name string) (*goType, error) {
	return nil, pending(fmt.Sprintf("%q is a standard-library type", name), "pass the data as a supported value (a scalar, a pointer and length, or a POD) at the C++ boundary")
}

// stdTypeName recognises a standard-library type by its spelling and returns
// the normalised name. It only fires for record-shaped declarations: a
// std::size_t parameter is a scalar and is mapped as one.
func stdTypeName(p cxx.Param) (string, bool) {
	record := p.Kind == cxx.TypeRecord
	if p.Kind == cxx.TypePointer && p.Pointee != nil {
		record = p.Pointee.Kind == cxx.TypeRecord
	}
	if !record {
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
	return s, true
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
	var elem *goType
	if p.Pointee.Kind == cxx.TypeRecord {
		// A record pointer does not need the record to be mirrorable: the Go
		// type is *Record either way, and an opaque one crosses as the address
		// of the object it owns.
		record, ok := m.records[p.Pointee.RecordName]
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

// mapRecord maps a record by value. Only a mirrored aggregate can be a Go
// value; an opaque one has no layout Go may copy, and crosses by pointer.
func (m *mapper) mapRecord(p cxx.Param) (*goType, error) {
	record, ok := m.records[p.RecordName]
	if !ok {
		return nil, missingRecord(p)
	}
	if !record.Mirrored {
		return nil, pending(fmt.Sprintf("%q is passed by value but is not a POD", p.Spelling), "take it by const reference, or make it a POD that Go can mirror")
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
// template specialisation is the common cause: it exists as a type but the
// manifest has not named it.
func missingRecord(p cxx.Param) error {
	fix := "include its complete definition in the header"
	if strings.Contains(p.RecordName, "<") {
		fix = "name it with -instantiate '" + p.RecordName + "'"
	}
	return pending(fmt.Sprintf("%q is not part of the generated API", p.Spelling), fix)
}
