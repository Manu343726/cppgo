package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReentrantResult pins the expression each result kind reads out of the
// argument frame. The INTEGER result is RAX and the floating one XMM0, and a
// bool occupies only the low byte of RAX.
func TestReentrantResult(t *testing.T) {
	cases := []struct {
		name   string
		result *goType
		want   string
	}{
		{"bool", &goType{Kind: goBool, Go: "bool"}, "cppgoFrame.Ret&0xff != 0"},
		{"double", &goType{Kind: goFloat, Go: "float64"}, "cppgoFrame.RetF"},
		{"int32", &goType{Kind: goInteger, Go: "int32"}, "int32(cppgoFrame.Ret)"},
		{"uint64", &goType{Kind: goInteger, Go: "uint64"}, "uint64(cppgoFrame.Ret)"},
		{"pointer", &goType{Kind: goPointer, Go: "*Point"}, "(*Point)(unsafe.Pointer(cppgoFrame.Ret))"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, reentrantResult(&binding{Result: tc.result}))
		})
	}
}
