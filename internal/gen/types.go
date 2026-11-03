package gen

import (
	"fmt"
	"go/ast"
	"go/types"
)

// kind classifies how a Go type crosses the ABI boundary.
type kind uint8

const (
	kindInt     kind = iota // signed/unsigned integer of any width
	kindFloat               // float32/float64
	kindBool                // bool, passed as int32_t
	kindString              // const char*, size_t
	kindSlice               // T*, size_t
	kindPointer             // *T
	kindHandle              // cppgo.Handle, i.e. void*
	kindError               // error, delivered as an int32_t status
)

// supportedTypes is the list quoted back to the user in rejection messages.
const supportedTypes = "int8/16/32/64, uint8/16/32/64, int, uint, uintptr, " +
	"float32, float64, bool, string, []T (T numeric), *T, cppgo.Handle, " +
	"and error as the last result"

// typeInfo describes one Go type on both sides of the boundary.
type typeInfo struct {
	kind kind
	// goType is the canonical Go spelling of the type, as it appears in the
	// generated source.
	goType string
	// slot is the Go type of the single 8-(or 4-)byte mirror slot the type
	// occupies on the trampoline's frame. It is empty for types that expand
	// to more than one slot (string, slices).
	slot string
	// size is the width of the mirror slot in bytes: 4 for float32/int32,
	// 8 for everything else.
	size int
	// cType is the C type used in the shim header. For types that expand to
	// a (pointer, length) pair it is the pointer half only.
	cType string
	// elemGo and elemC describe the element of slices and pointers.
	elemGo string
	elemC  string
	// named is the C name of a user-declared pointee type, empty when the
	// pointee is a builtin.
	named string
}

// align is the natural alignment of the mirror slot, which for every
// supported type equals its size.
func (t *typeInfo) align() int { return t.size }

// resolveType maps a Go type expression to its ABI description.
func resolveType(expr ast.Expr) (*typeInfo, error) {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return resolveType(e.X)
	case *ast.Ellipsis:
		return nil, fmt.Errorf("variadic parameters are not supported: pass a slice instead")
	case *ast.Ident:
		return resolveIdent(e.Name, expr)
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok && x.Name == "cppgo" && e.Sel.Name == "Handle" {
			return &typeInfo{
				kind:   kindHandle,
				goType: "cppgo.Handle",
				slot:   "unsafe.Pointer",
				size:   8,
				cType:  "void*",
			}, nil
		}
		return nil, unsupportedType(expr)
	case *ast.StarExpr:
		return resolvePointer(e, expr)
	case *ast.ArrayType:
		if e.Len != nil {
			return nil, fmt.Errorf("unsupported type %s: only slices ([]T) cross the boundary, arrays must be passed as pointers", types.ExprString(expr))
		}
		return resolveSlice(e.Elt, expr)
	case *ast.InterfaceType:
		if e.Methods == nil || len(e.Methods.List) == 0 {
			return nil, fmt.Errorf("unsupported type %s: use cppgo.Handle to pass an opaque C++ object", types.ExprString(expr))
		}
		return nil, unsupportedType(expr)
	case *ast.FuncType:
		return nil, fmt.Errorf("unsupported type %s: callbacks from C++ into Go are not supported in v1", types.ExprString(expr))
	default:
		return nil, unsupportedType(expr)
	}
}

// resolveIdent maps a builtin type name.
func resolveIdent(name string, expr ast.Expr) (*typeInfo, error) {
	type scalar struct {
		kind   kind
		slot   string
		size   int
		cType  string
		signed bool
	}
	table := map[string]scalar{
		"int8":    {kindInt, "int64", 8, "int8_t", true},
		"int16":   {kindInt, "int64", 8, "int16_t", true},
		"int32":   {kindInt, "int64", 8, "int32_t", true},
		"rune":    {kindInt, "int64", 8, "int32_t", true},
		"int64":   {kindInt, "int64", 8, "int64_t", true},
		"int":     {kindInt, "int64", 8, "int64_t", true},
		"uint8":   {kindInt, "uint64", 8, "uint8_t", false},
		"byte":    {kindInt, "uint64", 8, "uint8_t", false},
		"uint16":  {kindInt, "uint64", 8, "uint16_t", false},
		"uint32":  {kindInt, "uint64", 8, "uint32_t", false},
		"uint64":  {kindInt, "uint64", 8, "uint64_t", false},
		"uint":    {kindInt, "uint64", 8, "uint64_t", false},
		"uintptr": {kindInt, "uintptr", 8, "uint64_t", false},
		"float32": {kindFloat, "float32", 4, "float", true},
		"float64": {kindFloat, "float64", 8, "double", true},
		"bool":    {kindBool, "int64", 8, "int32_t", false},
	}
	if name == "error" {
		return &typeInfo{kind: kindError, goType: "error", cType: "int32_t"}, nil
	}
	if name == "any" {
		return nil, fmt.Errorf("unsupported type any: use cppgo.Handle to pass an opaque C++ object")
	}
	if name == "string" {
		return &typeInfo{
			kind:   kindString,
			goType: "string",
			cType:  "const char*",
			elemGo: "byte",
			elemC:  "char",
		}, nil
	}
	s, ok := table[name]
	if !ok {
		return nil, unsupportedType(expr)
	}
	return &typeInfo{
		kind:   s.kind,
		goType: name,
		slot:   s.slot,
		size:   s.size,
		cType:  s.cType,
	}, nil
}

// resolvePointer maps *T. The pointee crosses as-is, so T must be a type the
// C++ side can name: a builtin or a type of the bound package, which the shim
// header forward-declares.
func resolvePointer(e *ast.StarExpr, expr ast.Expr) (*typeInfo, error) {
	var elemGo, elemC, named string
	switch inner := e.X.(type) {
	case *ast.ParenExpr:
		return resolvePointer(&ast.StarExpr{X: inner.X}, expr)
	case *ast.Ident:
		elemGo = inner.Name
		if info, err := resolveIdent(inner.Name, inner); err == nil {
			switch info.kind {
			case kindError:
				return nil, fmt.Errorf("unsupported type %s: a pointer to error has no C representation", types.ExprString(expr))
			case kindString, kindSlice:
				return nil, fmt.Errorf("unsupported type %s: a pointer to %s has no C representation", types.ExprString(expr), inner.Name)
			}
			elemC = info.cType
		} else {
			// A type of the bound package: the shim header declares it.
			elemC = inner.Name
			named = inner.Name
		}
	case *ast.SelectorExpr:
		if x, ok := inner.X.(*ast.Ident); ok && x.Name == "cppgo" && inner.Sel.Name == "Handle" {
			return nil, fmt.Errorf("unsupported type %s: pass cppgo.Handle directly, it already is a pointer", types.ExprString(expr))
		}
		return nil, fmt.Errorf("unsupported type %s: only pointers to builtin types and to types of the bound package are supported", types.ExprString(expr))
	default:
		return nil, fmt.Errorf("unsupported type %s: only pointers to builtin or named types can cross the boundary (pointers to %s are not supported)", types.ExprString(expr), types.ExprString(e.X))
	}
	return &typeInfo{
		kind:   kindPointer,
		goType: "*" + elemGo,
		slot:   "unsafe.Pointer",
		size:   8,
		cType:  elemC + "*",
		elemGo: elemGo,
		elemC:  elemC,
		named:  named,
	}, nil
}

// resolveSlice maps []T. T must be a numeric builtin.
func resolveSlice(elt ast.Expr, expr ast.Expr) (*typeInfo, error) {
	id, ok := elt.(*ast.Ident)
	if !ok {
		return nil, fmt.Errorf("unsupported type %s: only slices of numeric builtin types are supported", types.ExprString(expr))
	}
	info, err := resolveIdent(id.Name, id)
	if err != nil {
		return nil, fmt.Errorf("unsupported type %s: only slices of numeric builtin types are supported", types.ExprString(expr))
	}
	if info.kind != kindInt && info.kind != kindFloat {
		return nil, fmt.Errorf("unsupported type %s: only slices of numeric builtin types are supported", types.ExprString(expr))
	}
	return &typeInfo{
		kind:   kindSlice,
		goType: "[]" + id.Name,
		cType:  info.cType + "*",
		elemGo: id.Name,
		elemC:  info.cType,
	}, nil
}

func unsupportedType(expr ast.Expr) error {
	return fmt.Errorf("unsupported type %s: cppgo supports %s", types.ExprString(expr), supportedTypes)
}
