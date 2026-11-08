//go:build stackalloc

package declspec

import (
	"strings"
	"testing"
)

// The cases from the issue, one per row, plus the boundaries around them. Each
// row is a declaration a user could write by hand and the answer the generator
// must give: accepted, or refused with the reason in the message.
func TestResolve(t *testing.T) {
	// Shorthands for the C++ side.
	boolParam := CxxParam{Name: "flag", Type: CxxType{Kind: KindBool, Const: true}}
	charLen := CxxParam{Name: "data", Type: CxxType{Kind: KindPointerLength, Elem: KindUint8, Const: true}}
	spanInt := CxxParam{Name: "values", Type: CxxType{Kind: KindStdSpan, Elem: KindInt32}}
	viewText := CxxParam{Name: "text", Type: CxxType{Kind: KindStdStringView}}
	vectorInt := CxxParam{Name: "values", Type: CxxType{Kind: KindStdVector, Elem: KindInt32}}
	vectorText := CxxParam{Name: "values", Type: CxxType{Kind: KindStdVector, Elem: KindStdString}}
	ucharLen := CxxParam{Name: "bytes", Type: CxxType{Kind: KindPointerLength, Elem: KindUint8, Const: true}}

	noexcept := func(params ...CxxParam) CxxDecl {
		return CxxDecl{Qualified: "ns::f", Noexcept: true, Params: params}
	}
	throwing := func(params ...CxxParam) CxxDecl {
		return CxxDecl{Qualified: "ns::f", Throws: true, Params: params}
	}

	cases := []struct {
		name string
		decl CxxDecl
		sig  GoSig
		want string // "" = accepted, otherwise a substring of the refusal
	}{
		{
			name: "bool parameter declared as a string",
			decl: noexcept(boolParam),
			sig:  GoSig{Params: []GoParam{{Name: "s", Kind: GoString}}},
			want: "is bool, not string",
		},
		{
			name: "noexcept declaration with an error result",
			decl: noexcept(boolParam),
			sig: GoSig{
				Params:  []GoParam{{Name: "b", Kind: GoBool}},
				Results: []GoParam{{Kind: GoError}},
			},
			want: "is noexcept",
		},
		{
			name: "char* + size_t declared as []byte: the borrow",
			decl: noexcept(charLen),
			sig:  GoSig{Params: []GoParam{{Name: "data", Kind: GoSlice, Elem: GoUint8}}},
		},
		{
			name: "char* + size_t declared as a string: also a borrow",
			decl: noexcept(charLen),
			sig:  GoSig{Params: []GoParam{{Name: "data", Kind: GoString}}},
		},
		{
			name: "std::span<int> declared as []int: the borrow",
			decl: func() CxxDecl {
				d := noexcept(spanInt)
				d.HasResult, d.Result = true, CxxType{Kind: KindInt32}
				return d
			}(),
			sig: GoSig{
				Params:  []GoParam{{Name: "values", Kind: GoSlice, Elem: GoInt32}},
				Results: []GoParam{{Kind: GoInt32}},
			},
		},
		{
			name: "throwing declaration with no error result",
			decl: throwing(charLen),
			sig:  GoSig{Params: []GoParam{{Name: "data", Kind: GoSlice, Elem: GoUint8}}},
			want: "can throw",
		},
		{
			name: "throwing declaration with an error result",
			decl: throwing(charLen),
			sig: GoSig{
				Params:  []GoParam{{Name: "data", Kind: GoSlice, Elem: GoUint8}},
				Results: []GoParam{{Kind: GoError}},
			},
		},
		{
			name: "std::vector<int> declared as []int: cannot be a borrow",
			decl: noexcept(vectorInt),
			sig:  GoSig{Params: []GoParam{{Name: "values", Kind: GoSlice, Elem: GoInt32}}},
			want: "owns memory",
		},
		{
			name: "std::vector<std::string> declared as []string: cannot be a borrow",
			decl: noexcept(vectorText),
			sig:  GoSig{Params: []GoParam{{Name: "values", Kind: GoSlice, Elem: GoString}}},
			want: "owns memory",
		},
		{
			name: "std::string_view declared as []byte",
			decl: noexcept(viewText),
			sig:  GoSig{Params: []GoParam{{Name: "text", Kind: GoSlice, Elem: GoUint8}}},
			want: "must be a Go string",
		},
		{
			name: "span element kind mismatch",
			decl: noexcept(ucharLen),
			sig:  GoSig{Params: []GoParam{{Name: "bytes", Kind: GoSlice, Elem: GoInt32}}},
			want: "cannot borrow",
		},
		{
			name: "(T*, size_t) declared as two Go parameters",
			decl: noexcept(charLen),
			sig: GoSig{Params: []GoParam{
				{Name: "data", Kind: GoPointer},
				{Name: "n", Kind: GoUint64},
			}},
			want: "parameters",
		},
		{
			name: "noexcept int result with no result in Go",
			decl: func() CxxDecl { d := noexcept(); d.HasResult, d.Result = true, CxxType{Kind: KindInt32}; return d }(),
			sig:  GoSig{},
			want: "returns one value",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Resolve(tc.decl, tc.sig)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("want accepted, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want a refusal containing %q, got accepted", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want a refusal containing %q, got %q", tc.want, err.Error())
			}
			if !isRejection(err) {
				t.Fatalf("want ErrRejected, got %v", err)
			}
		})
	}
}

func isRejection(err error) bool {
	return err != nil && strings.Contains(err.Error(), ErrRejected.Error())
}
