package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestReentrantRefusals covers what a call that may run a callback refuses. A
// view of Go memory the callee keeps, a container the callee replaces, an
// untyped pointer and a standard-library result are the four shapes no copy can
// make safe; each names the fix.
//
// The shapes live in one table here rather than in a header because the
// standard-library ones need C++20 to parse, and the refusals are the generator's
// own rule, not libclang's answer.
func TestReentrantRefusals(t *testing.T) {
	param := func(name string, mapped *goType) *goParam {
		return &goParam{Name: name, Type: mapped}
	}
	cases := []struct {
		name    string
		binding *binding
		want    string
	}{
		{
			name: "span parameter",
			binding: &binding{Carrier: reentrant, Params: []*goParam{param("values", &goType{
				Kind: goSlice, Std: stdSpan, Param: cxx.Param{Spelling: "std::span<double>"},
			})}},
			want: "a view of Go memory the callee keeps for the whole call",
		},
		{
			name: "string_view parameter",
			binding: &binding{Carrier: reentrant, Params: []*goParam{param("name", &goType{
				Kind: goString, Std: stdStringView, Param: cxx.Param{Spelling: "std::string_view"},
			})}},
			want: "a view of Go memory the callee keeps for the whole call",
		},
		{
			name: "vector the callee replaces",
			binding: &binding{Carrier: reentrant, Params: []*goParam{{
				Name: "values", StdOut: true,
				Type: &goType{Kind: goSlice, Std: stdVector, Param: cxx.Param{Spelling: "std::vector<double> &"}},
			}}},
			want: "the callee replaces",
		},
		{
			name: "void pointer parameter",
			binding: &binding{Carrier: reentrant, Params: []*goParam{param("context", &goType{
				Kind: goPointer, Go: "unsafe.Pointer", Param: cxx.Param{Spelling: "void *"},
			})}},
			want: "a void pointer names memory nothing can copy",
		},
		{
			name: "standard-library result",
			binding: &binding{Carrier: reentrant, StdResult: true, Result: &goType{
				Kind: goSlice, Std: stdVector, Param: cxx.Param{Spelling: "std::vector<double>"},
			}},
			want: "copies a standard-library result out through the goroutine's frame",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := (&builder{}).checkReentrant(tc.binding)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	// The shapes a pinned copy makes safe pass the same check.
	safe := &binding{Carrier: reentrant, Params: []*goParam{
		param("data", &goType{Kind: goSlice, Param: cxx.Param{Spelling: "const uint8_t *"}}),
		param("name", &goType{Kind: goString, CString: true, Param: cxx.Param{Spelling: "const char *"}}),
		param("point", &goType{Kind: goPointer, Go: "*Point", Param: cxx.Param{Spelling: "Point *"}}),
		param("owned", &goType{Kind: goPointer, Go: "*Enrollment", Record: &recordModel{CxxName: "Enrollment"}, Param: cxx.Param{Spelling: "Enrollment *"}}),
	}, StdResult: true, Result: &goType{Kind: goString, Std: stdString, Param: cxx.Param{Spelling: "std::string"}}}
	assert.NoError(t, (&builder{}).checkReentrant(safe))
}
