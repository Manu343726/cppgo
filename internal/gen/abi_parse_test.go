package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestABIParseErrors pins every diagnostic the direct ABI mode's directives can
// produce, so the message a user sees is part of the contract.
func TestABIParseErrors(t *testing.T) {
	const header = "//cppgo:library libx\n//cppgo:header vec.hpp\n\n"

	cases := map[string]struct {
		files map[string]string
		want  string
		// noPos marks a package-level diagnostic, which has no file:line.
		noPos bool
	}{
		"empty header": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n//cppgo:header\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "//cppgo:header needs the path",
		},
		"header with spaces": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n//cppgo:header a b.hpp\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "takes a single path",
		},
		"duplicate header": {
			files: map[string]string{
				"a.go": "package x\n\n//cppgo:library libx\n//cppgo:header a.hpp\n\n//cppgo:function ns::f\nfunc F()\n",
				"b.go": "package x\n\n//cppgo:header b.hpp\n",
			},
			want: "duplicate //cppgo:header",
		},
		"shim mode with a header": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n//cppgo:mode shim\n//cppgo:header vec.hpp\n\n//cppgo:external f\nfunc F()\n"},
			want:  "opts out of",
		},
		"class in shim mode": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:external f\nfunc F()\n"},
			want:  "add //cppgo:header, or drop //cppgo:mode shim",
		},
		"empty class name": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class\ntype Vec cppgo.Handle\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "needs the qualified C++ class name",
		},
		"class on a var": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\nvar Vec int\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "must be on a type declaration",
		},
		"class on a grouped type declaration": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype (\n\tVec cppgo.Handle\n\tOther cppgo.Handle\n)\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "single type declaration",
		},
		"class on a generic type": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec[T any] cppgo.Handle\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "non-generic type declaration",
		},
		"class over a struct": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec struct{ A int }\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "must be declared over cppgo.Handle",
		},
		"duplicate class for one Go type": {
			files: map[string]string{
				"a.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:function ns::f\nfunc F()\n",
				"b.go": "package x\n\n//cppgo:class ns::Other\ntype Vec cppgo.Handle\n",
			},
			want: "duplicate //cppgo:class for Vec",
		},
		"two Go types for one class": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:class ns::Vec\ntype Other cppgo.Handle\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "is already bound to a Go type",
		},
		"bind directive outside a function doc": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\nvar V int\n"},
			want:  "must be in the doc comment of a function declaration",
		},
		"two bind directives": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\n//cppgo:method g\nfunc F()\n"},
			want:  "duplicate //cppgo:method on F",
		},
		"external in abi mode": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:external f\nfunc F()\n"},
			want:  "//cppgo:external selects the shim mode",
		},
		"abi directive in shim mode": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:external f\nfunc F()\n"},
			want:  "add //cppgo:header",
		},
		"generic binding": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\nfunc F[T any](v T)\n"},
			want:  "cannot be used on a generic function",
		},
		"ctor with a receiver": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:ctor\nfunc (v Vec) New() (Vec, error)\n"},
			want:  "cannot be used on a method",
		},
		"function with a receiver": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:function ns::f\nfunc (v Vec) F()\n"},
			want:  "cannot be used on a method",
		},
		"method without a receiver": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:method norm\nfunc F()\n"},
			want:  "must be declared on a method of the class it binds",
		},
		"dtor without a receiver": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:dtor\nfunc F()\n"},
			want:  "must be declared on a method of the class it binds",
		},
		"unnamed receiver": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:method norm\nfunc (Vec) Norm() float64\n"},
			want:  "receiver of a bound method must be named",
		},
		"receiver is not a class": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:method norm\nfunc (v int32) Norm() float64\n"},
			want:  "declare the class with //cppgo:class",
		},
		"method without a name": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:method\nfunc (v Vec) Norm() float64\n"},
			want:  "needs the C++ member name",
		},
		"method with a qualified name": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:method ns::Vec::norm\nfunc (v Vec) Norm() float64\n"},
			want:  "takes a single member name",
		},
		"function without a name": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function\nfunc F()\n"},
			want:  "needs the qualified C++ name",
		},
		"function with a signature": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f(int)\nfunc F()\n"},
			want:  "takes a single qualified name",
		},
		"ctor with no results": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:ctor\nfunc New()\n"},
			want:  "must return the object handle",
		},
		"ctor returning another type": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:ctor\nfunc New() (int32, error)\n"},
			want:  "result 0 must be a //cppgo:class handle type",
		},
		"ctor returning the wrong class": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:class ns::Other\ntype Other cppgo.Handle\n\n//cppgo:ctor\nfunc (v Other) New() (Other, error)\n"},
			want:  "cannot be used on a method",
		},
		"dtor with arguments": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:dtor\nfunc (v Vec) Destroy(k int32)\n"},
			want:  "takes no arguments",
		},
		"dtor with results": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:dtor\nfunc (v Vec) Destroy() error\n"},
			want:  "returns nothing",
		},
		"too many results": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\nfunc F() (int32, int32, error)\n"},
			want:  "too many results",
		},
		"second result is not an error": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\nfunc F() (int32, int32)\n"},
			want:  "second result must be error",
		},
		"no bindings": {
			files: map[string]string{"x.go": "package x\n\n" + header + "func F() {}\n"},
			want:  "no //cppgo:ctor, //cppgo:method, //cppgo:dtor or //cppgo:function declarations found",
			noPos: true,
		},
		"unsupported parameter type": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:function ns::f\nfunc F(v complex128)\n"},
			want:  "unsupported type complex128",
		},
		"pointer to a class": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\ntype Vec cppgo.Handle\n\n//cppgo:function ns::f\nfunc F(v *Vec)\n"},
			want:  "is already the object pointer",
		},
		"two class directives on one declaration": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:class ns::Vec\n//cppgo:class ns::Other\ntype Vec cppgo.Handle\n\n//cppgo:function ns::f\nfunc F()\n"},
			want:  "duplicate //cppgo:class on ns::Vec",
		},
		"abi function directive in shim mode": {
			files: map[string]string{"x.go": "package x\n\n//cppgo:library libx\n\n//cppgo:dtor\nfunc F()\n"},
			want:  "is a directive of the direct ABI mode",
		},
		"receiver type cannot cross the boundary": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:method norm\nfunc (v map[int]int) Norm() float64\n"},
			want:  "receiver of Norm: unsupported type map[int]int",
		},
		"ctor without a result, before the class is known": {
			files: map[string]string{"x.go": "package x\n\n" + header + "//cppgo:ctor\nfunc New()\n"},
			want:  "must return the object handle, e.g. func New(...) (Vector, error)",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parsePackage(writePkg(t, tc.files))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			if !tc.noPos {
				assert.Contains(t, err.Error(), ".go:", "the diagnostic must point at the directive")
			}
		})
	}
}

// TestABIModeIsTheDefault checks the mode switch: a header means direct ABI
// calls without asking, and //cppgo:mode shim is the only way out.
func TestABIModeIsTheDefault(t *testing.T) {
	dir := writeABIPkg(t, "vec.hpp", `//cppgo:class ns::Vec
type Vec cppgo.Handle

//cppgo:method norm
func (v Vec) Norm() float64
`)
	pkg := mustParse(t, dir)
	assert.Equal(t, ModeABI, pkg.Mode)
	require.Len(t, pkg.Funcs, 1)
	assert.Equal(t, bindMethod, pkg.Funcs[0].Kind)
	assert.Equal(t, "ns::Vec::norm", pkg.Funcs[0].cxxName())

	explicit := writePkg(t, map[string]string{
		"x.go": "package x\n\n//cppgo:library libx\n//cppgo:mode abi\n//cppgo:header vec.hpp\n\n//cppgo:function ns::f\nfunc F() int32\n",
	})
	pkg = mustParse(t, explicit)
	assert.Equal(t, ModeABI, pkg.Mode)
	assert.Equal(t, bindFunction, pkg.Funcs[0].Kind)
	assert.Equal(t, "ns::f", pkg.Funcs[0].cxxName())
}

// TestABIHeaderResolvesRelativeToThePackage checks where the relative header
// path is looked up: next to the Go files, not next to the working directory.
func TestABIHeaderResolvesRelativeToThePackage(t *testing.T) {
	dir := writeABIPkg(t, "include/vec.hpp", `//cppgo:function ns::f
func F() int32
`)
	pkg := mustParse(t, dir)
	assert.Equal(t, "include/vec.hpp", pkg.Header)
	assert.Equal(t, filepath.Join(dir, "include", "vec.hpp"), pkg.HeaderPath)
}

// TestABIClassScanStepsOverOtherDirectives checks that the class scan reaches a
// //cppgo:class that shares its doc comment with the package-level directives.
func TestABIClassScanStepsOverOtherDirectives(t *testing.T) {
	dir := writePkg(t, map[string]string{"x.go": `package x

//cppgo:library libx
//cppgo:header vec.hpp
//cppgo:class ns::Vec
type Vec cppgo.Handle

//cppgo:function ns::f
func F() int32
`})
	pkg := mustParse(t, dir)
	require.Len(t, pkg.Classes, 1)
	assert.Equal(t, "ns::Vec", pkg.Classes["Vec"].CxxName)
	assert.Equal(t, "libx", pkg.Library)
	assert.Equal(t, "vec.hpp", pkg.Header)
}

// TestShimDirectiveString covers the spelling used in diagnostics.
func TestShimDirectiveString(t *testing.T) {
	assert.Equal(t, "//cppgo:method", directive{name: methodDirective}.String())
}
