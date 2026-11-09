//go:build stackalloc

package declspec

import (
	"errors"
	"fmt"
	"strings"
)

// Kind is a type in the slice of C++ and Go that a declaration can name. It is
// the generator's own projection, not a general C++ type system: a construct
// that is not in this list is not bindable, which is what rejectUnimplemented
// already enforces.
type Kind int

const (
	KindInvalid Kind = iota
	KindBool
	KindInt32
	KindInt64
	KindSize          // size_t
	KindUint8         // char, unsigned char
	KindFloat64       // double
	KindPointer       // T*
	KindPointerLength // T*, size_t: one parameter in C++, one slice in Go
	KindStdSpan       // std::span<T>
	KindStdStringView // std::string_view
	KindStdString     // std::string
	KindStdVector     // std::vector<T>
	KindRecord        // a mirrorable record, by value
)

// String names the kind as the C++ declaration would spell it, for messages.
func (k Kind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindInt32:
		return "int"
	case KindInt64:
		return "long"
	case KindSize:
		return "size_t"
	case KindUint8:
		return "unsigned char"
	case KindFloat64:
		return "double"
	case KindPointer:
		return "T*"
	case KindPointerLength:
		return "(T*, size_t)"
	case KindStdSpan:
		return "std::span"
	case KindStdStringView:
		return "std::string_view"
	case KindStdString:
		return "std::string"
	case KindStdVector:
		return "std::vector"
	case KindRecord:
		return "record"
	default:
		return "invalid"
	}
}

// CxxType is one C++ type as far as a binding needs to know it.
type CxxType struct {
	Kind Kind
	// Elem is the element kind of a pointer, span, vector, or the pointee of
	// an out-parameter. It is KindUint8 for `char *` and KindInt32 for `int *`.
	Elem Kind
	// Const is the pointee's constness: `const int *`.
	Const bool
	// Writable is set on a slice whose length is the third element of a
	// `(T *p, size_t n)` pair when the generator established the callee may
	// write through p; it is derived from Const.
	Writable bool
}

// CxxParam is one C++ parameter, with (T*, size_t) already grouped.
type CxxParam struct {
	Name string
	Type CxxType
}

// CxxDecl is a C++ declaration as the generator models it. Throws says the
// declaration can throw (it is not noexcept); Noexcept is its negation stated
// positively, because that is the direction the ABI cares about.
type CxxDecl struct {
	Qualified string
	Noexcept  bool
	Throws    bool
	Params    []CxxParam
	Result    CxxType
	// HasResult is false for void.
	HasResult bool
}

// GoKind is a Go parameter or result shape.
type GoKind int

const (
	GoInvalid GoKind = iota
	GoBool
	GoInt32
	GoInt64
	GoUint8
	GoUint64
	GoFloat64
	GoSlice
	GoString
	GoPointer
	GoError
)

// GoParam is one hand-written Go parameter or result.
type GoParam struct {
	Name string
	Kind GoKind
	// Elem is the element kind for a slice: GoUint8 for []byte, GoInt32 for
	// []int.
	Elem GoKind
}

// GoSig is the declaration a user writes in the override file.
type GoSig struct {
	Params  []GoParam
	Results []GoParam
}

// ErrRejected wraps every refusal so a caller can branch on it.
var ErrRejected = errors.New("declspec: declaration rejected")

// Resolve answers whether the hand-written Go signature may stand for the C++
// declaration, and why not when it may not.
//
// It enforces four things, and nothing else:
//
//   - the parameter list groups the same way on both sides (a (T*, size_t) pair
//     is one Go slice, and nothing else is);
//   - the element kinds match, because the mapping is by width and signedness;
//   - an error result is present if and only if the C++ side has a channel for
//     it, which is exactly "the declaration throws";
//   - a borrow is only accepted where the C++ type is a borrow: a parameter that
//     owns memory (std::string, std::vector) is a copy, whatever the Go
//     signature says.
func Resolve(decl CxxDecl, sig GoSig) error {
	if len(decl.Params) != len(sig.Params) {
		return rejectf("%s: the Go signature has %d parameters, the C++ declaration groups into %d",
			decl.Qualified, len(sig.Params), len(decl.Params))
	}

	for i, p := range decl.Params {
		if err := resolveParam(decl.Qualified, i, p, sig.Params[i]); err != nil {
			return err
		}
	}

	if err := resolveResults(decl, sig.Results); err != nil {
		return err
	}
	return nil
}

func resolveParam(qualified string, i int, cxx CxxParam, gov GoParam) error {
	where := fmt.Sprintf("%s: parameter %d (%s)", qualified, i+1, cxx.Type.Kind)

	switch cxx.Type.Kind {
	case KindBool:
		if gov.Kind != GoBool {
			return rejectf("%s is %s, not %s", where, cxx.Type.Kind, gov.Kind)
		}
	case KindInt32:
		if gov.Kind != GoInt32 {
			return rejectf("%s is int32, not %s", where, gov.Kind)
		}
	case KindInt64:
		if gov.Kind != GoInt64 {
			return rejectf("%s is int64, not %s", where, gov.Kind)
		}
	case KindSize:
		if gov.Kind != GoUint64 {
			return rejectf("%s is uint64, not %s", where, gov.Kind)
		}
	case KindFloat64:
		if gov.Kind != GoFloat64 {
			return rejectf("%s is float64, not %s", where, gov.Kind)
		}

	case KindPointerLength:
		// The only borrow the design allows from a slice: a pointer and a
		// length adjacent in the parameter list. `char *` + size_t may also be
		// a Go string, which is a borrow of read-only bytes.
		switch gov.Kind {
		case GoSlice:
			if want, got := goKindOf(cxx.Type.Elem), gov.Elem; want != got {
				return rejectf("%s: []%s cannot borrow a %s element", where, got, want)
			}
		case GoString:
			if cxx.Type.Elem != KindUint8 {
				return rejectf("%s: a Go string is bytes, the C++ element is %s", where, cxx.Type.Elem)
			}
		default:
			return rejectf("%s must be a slice or a string in Go, not %s", where, gov.Kind)
		}

	case KindStdSpan:
		if gov.Kind != GoSlice {
			return rejectf("%s must be a slice in Go, not %s", where, gov.Kind)
		}
		if want, got := goKindOf(cxx.Type.Elem), gov.Elem; want != got {
			return rejectf("%s: []%s cannot borrow a %s element", where, got, want)
		}

	case KindStdStringView:
		if gov.Kind != GoString {
			return rejectf("%s must be a Go string, not %s", where, gov.Kind)
		}

	case KindPointer:
		if gov.Kind != GoPointer {
			return rejectf("%s must be a pointer in Go, not %s", where, gov.Kind)
		}

	case KindStdString, KindStdVector:
		// The one case the Go signature cannot win: the C++ type owns memory,
		// so the parameter is a copy in the shim whatever the Go side declares.
		return rejectf("%s is %s, which owns memory: a Go slice or string cannot be adopted by it, "+
			"so it cannot be a zero-copy parameter (the copy is the only mapping)", where, cxx.Type.Kind)

	default:
		return rejectf("%s is %s, which no declaration can bind", where, cxx.Type.Kind)
	}

	return nil
}

func resolveResults(decl CxxDecl, results []GoParam) error {
	// The error result is the only result a declaration decides: it exists
	// exactly when the C++ declaration can fail.
	_, hasError := splitError(results)

	switch {
	case decl.Throws && !hasError:
		return rejectf("%s can throw, so its Go signature must carry an error result: "+
			"dropping it would make a C++ exception terminate the process", decl.Qualified)
	case decl.Noexcept && hasError:
		return rejectf("%s is noexcept: it has no channel to report a failure through, "+
			"so a Go error result cannot be produced", decl.Qualified)
	}

	values := valuesOnly(results)
	if !decl.HasResult {
		if len(values) != 0 {
			return rejectf("%s returns void, the Go signature returns %d values", decl.Qualified, len(values))
		}
		return nil
	}
	if len(values) != 1 {
		return rejectf("%s returns one value, the Go signature returns %d", decl.Qualified, len(values))
	}

	want := goKindOf(decl.Result.Kind)
	if values[0].Kind != want {
		return rejectf("%s returns %s, the Go signature returns %s", decl.Qualified, want, values[0].Kind)
	}
	return nil
}

func splitError(results []GoParam) ([]GoParam, bool) {
	if n := len(results); n > 0 && results[n-1].Kind == GoError {
		return results[:n-1], true
	}
	return results, false
}

func valuesOnly(results []GoParam) []GoParam {
	values, _ := splitError(results)
	return values
}

// goKindOf maps a C++ element kind onto the Go kind it is represented by.
func goKindOf(k Kind) GoKind {
	switch k {
	case KindBool:
		return GoBool
	case KindInt32:
		return GoInt32
	case KindInt64:
		return GoInt64
	case KindSize:
		return GoUint64
	case KindUint8:
		return GoUint8
	case KindFloat64:
		return GoFloat64
	case KindPointer:
		return GoPointer
	default:
		return GoInvalid
	}
}

func rejectf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrRejected, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// String implements fmt.Stringer for messages and test names.
func (k GoKind) String() string {
	switch k {
	case GoBool:
		return "bool"
	case GoInt32:
		return "int32"
	case GoInt64:
		return "int64"
	case GoUint8:
		return "uint8"
	case GoUint64:
		return "uint64"
	case GoFloat64:
		return "float64"
	case GoSlice:
		return "slice"
	case GoString:
		return "string"
	case GoPointer:
		return "pointer"
	case GoError:
		return "error"
	default:
		return "invalid"
	}
}
