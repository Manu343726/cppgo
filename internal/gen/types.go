package gen

import (
	"fmt"

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
func (m *mapper) mapType(p cxx.Param) (*goType, error) {
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
		return nil, unsupported(p, "enum values")
	case cxx.TypeArray:
		return nil, unsupported(p, "array values")
	default:
		return nil, unsupported(p, "this type")
	}
}

// mapInteger maps every integer width the type map covers, and checks the LP64
// assumption the contract makes: on a platform where long is not 64-bit the
// generated code would silently reinterpret a value.
func (m *mapper) mapInteger(p cxx.Param) (*goType, error) {
	if p.ClangKind == "Bool" {
		return &goType{Kind: goBool, Go: "bool", C: "bool", Cxx: "bool", Param: p}, nil
	}
	if (p.ClangKind == "Long" || p.ClangKind == "ULong") && p.SizeBytes != 8 {
		return nil, fmt.Errorf("cppgo: %s: long is %d bytes on this target, not 8; the type map is LP64 and a silent reinterpretation is not emitted", p.Spelling, p.SizeBytes)
	}
	goName, cName, ok := integerType(p.SizeBytes, p.Unsigned)
	if !ok {
		return nil, fmt.Errorf("cppgo: %s: a %d-byte integer has no Go value type in the type map", p.Spelling, p.SizeBytes)
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
		return nil, fmt.Errorf("cppgo: %s: long double and __int128 have no Go value type; widen or narrow it at the C++ boundary to double or int64, or wrap it in a representable type", p.Spelling)
	}
}

// mapPointer maps T* and T&. A pointer to a scalar is *T; a pointer to a
// record is *Record, where a record value is the address of the Go value or of
// the object the opaque Go type owns.
func (m *mapper) mapPointer(p cxx.Param) (*goType, error) {
	if p.Spelling == "std::nullptr_t" || p.ClangKind == "NullPtr" {
		return nil, unsupported(p, "nullptr_t")
	}
	if p.Pointee == nil {
		return nil, fmt.Errorf("cppgo: %s: a pointer libclang did not describe cannot be mapped", p.Spelling)
	}
	var elem *goType
	if p.Pointee.Kind == cxx.TypeRecord {
		// A record pointer does not need the record to be mirrorable: the Go
		// type is *Record either way, and an opaque one crosses as the address
		// of the object it owns.
		record, ok := m.records[p.Pointee.RecordName]
		if !ok {
			return nil, fmt.Errorf("cppgo: %s: the record is not part of the generated API", p.Spelling)
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
		return nil, unsupported(p, "void pointers")
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
		return nil, fmt.Errorf("cppgo: %s: the record is not part of the generated API", p.Spelling)
	}
	if !record.Mirrored {
		return nil, unsupported(p, "passing an opaque record by value")
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

// unsupported names a construct this build does not emit yet. It is not a
// refusal of the construct: DESIGN.md's carrier model covers it, and phase 2b
// of the emitter workstream implements it.
func unsupported(p cxx.Param, what string) error {
	return fmt.Errorf("cppgo: %s: emitting %s is not implemented by this build yet", p.Spelling, what)
}
