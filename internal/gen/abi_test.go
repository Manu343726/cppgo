package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMixedSignatureLayout pins the whole calling convention for a signature
// that mixes both register classes, expands a slice, and returns a value plus
// an error through an out-pointer.
func TestMixedSignatureLayout(t *testing.T) {
	sig := testSig(t, `func Mix(a int64, x float64, v []float32, b int32) (float64, error)`)

	// fn, a, x, v.ptr, v.len, b, out
	type want struct {
		label  string
		goType string
		class  class
		offset int
		reg    string
	}
	cases := []want{
		{"fn", "uintptr", classInt, 0, ""},
		{"a0", "int64", classInt, 8, "DI"},
		{"a1", "float64", classFloat, 16, "X0"},
		{"a2", "unsafe.Pointer", classInt, 24, "SI"},
		{"a2_len", "uintptr", classInt, 32, "DX"},
		{"a3", "int64", classInt, 40, "CX"},
		{"out", "unsafe.Pointer", classInt, 48, "R8"},
	}
	require.Len(t, sig.slots, len(cases))
	for i, w := range cases {
		got := sig.slots[i]
		assert.Equal(t, w.label, got.label, "slot %d", i)
		assert.Equal(t, w.goType, got.goType, "slot %d", i)
		assert.Equal(t, w.class, got.class, "slot %d", i)
		assert.Equal(t, w.offset, got.offset, "slot %d", i)
		assert.Equal(t, w.reg, got.reg, "slot %d", i)
	}

	// The status comes back in the last four bytes.
	assert.Equal(t, resStatusValue, sig.ret.kind)
	assert.Equal(t, "int32", sig.ret.slot.goType)
	assert.Equal(t, 56, sig.ret.slot.offset)
	assert.Equal(t, "ret", sig.ret.slot.label)
	assert.Equal(t, 60, sig.total)

	// C prototype: parameters in order, the out-pointer last.
	require.Len(t, sig.cParams, 6)
	assert.Equal(t, cParam{"int64_t", "a"}, sig.cParams[0])
	assert.Equal(t, cParam{"double", "x"}, sig.cParams[1])
	assert.Equal(t, cParam{"float*", "v"}, sig.cParams[2])
	assert.Equal(t, cParam{"size_t", "v_len"}, sig.cParams[3])
	assert.Equal(t, cParam{"int32_t", "b"}, sig.cParams[4])
	assert.Equal(t, cParam{"double*", "ret"}, sig.cParams[5])

	assert.True(t, sig.usesUnsafe())
}

// TestRegisterAssignment checks the two budgets independently.
func TestRegisterAssignment(t *testing.T) {
	t.Run("six integers fit", func(t *testing.T) {
		sig := testSig(t, `func Six(a, b, c, d, e, f int64) int64`)
		var regs []string
		for _, sl := range sig.slots[1:] {
			regs = append(regs, sl.reg)
		}
		assert.Equal(t, []string{"DI", "SI", "DX", "CX", "R8", "R9"}, regs)
	})

	t.Run("seven integers do not fit", func(t *testing.T) {
		f := testFunc(t, `func Seven(a, b, c, d, e, f, g int64) int64`)
		_, err := buildSignature(f)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs 7 integer argument registers")
		assert.Contains(t, err.Error(), "RDI, RSI, RDX, RCX, R8, R9")
	})

	t.Run("eight floats fit", func(t *testing.T) {
		sig := testSig(t, `func Eight(a, b, c, d, e, f, g, h float64) float64`)
		var regs []string
		for _, sl := range sig.slots[1:] {
			regs = append(regs, sl.reg)
		}
		assert.Equal(t, []string{"X0", "X1", "X2", "X3", "X4", "X5", "X6", "X7"}, regs)
	})

	t.Run("nine floats do not fit", func(t *testing.T) {
		f := testFunc(t, `func Nine(a, b, c, d, e, f, g, h, i float64) float64`)
		_, err := buildSignature(f)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs 9 float argument registers")
		assert.Contains(t, err.Error(), "XMM0-XMM7")
	})

	t.Run("strings and slices spend integer registers", func(t *testing.T) {
		f := testFunc(t, `func Many(a, b string, c []int32, d int32) int64`)
		_, err := buildSignature(f)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs 7 integer argument registers")
	})

	t.Run("the out pointer spends an integer register", func(t *testing.T) {
		f := testFunc(t, `func Many(a, b, c, d, e int64, s string) (int32, error)`)
		_, err := buildSignature(f)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "needs 7 integer argument registers")
	})
}

func TestResultKinds(t *testing.T) {
	t.Run("void", func(t *testing.T) {
		sig := testSig(t, `func Nothing(a int32)`)
		assert.Equal(t, resNone, sig.ret.kind)
		assert.Nil(t, sig.ret.slot)
		assert.Equal(t, 16, sig.total)
		assert.False(t, sig.usesUnsafe())
	})

	t.Run("integer value", func(t *testing.T) {
		sig := testSig(t, `func One(a int32) int32`)
		assert.Equal(t, resValue, sig.ret.kind)
		assert.Equal(t, "int64", sig.ret.slot.goType)
		assert.Equal(t, classInt, sig.ret.slot.class)
		assert.Equal(t, 16, sig.ret.slot.offset)
		assert.Equal(t, 24, sig.total)
	})

	t.Run("narrow integer value", func(t *testing.T) {
		sig := testSig(t, `func One(a uint8) uint8`)
		assert.Equal(t, resValue, sig.ret.kind)
		assert.Equal(t, "uint64", sig.ret.slot.goType)
	})

	t.Run("float32 value", func(t *testing.T) {
		sig := testSig(t, `func One(a int8) float32`)
		assert.Equal(t, "float32", sig.ret.slot.goType)
		assert.Equal(t, classFloat, sig.ret.slot.class)
		// a@8 (int8 slot, 8 bytes) then ret@16, aligned to 4.
		assert.Equal(t, 16, sig.ret.slot.offset)
		assert.Equal(t, 20, sig.total)
	})

	t.Run("bool value", func(t *testing.T) {
		sig := testSig(t, `func One(a bool) bool`)
		assert.Equal(t, "int32", sig.ret.slot.goType)
		assert.Equal(t, 4, sig.ret.slot.size)
	})

	t.Run("pointer and handle values", func(t *testing.T) {
		sig := testSig(t, `func One(p *Point) *Point`)
		assert.Equal(t, "unsafe.Pointer", sig.ret.slot.goType)
		assert.True(t, sig.usesUnsafe())

		sig = testSig(t, `func Two() cppgo.Handle`)
		assert.Equal(t, "unsafe.Pointer", sig.ret.slot.goType)
	})

	t.Run("status only", func(t *testing.T) {
		sig := testSig(t, `func Do(a int32) error`)
		assert.Equal(t, resStatus, sig.ret.kind)
		assert.Equal(t, "int32", sig.ret.slot.goType)
		assert.Equal(t, 16, sig.ret.slot.offset)
		assert.Equal(t, 20, sig.total)
		assert.Len(t, sig.cParams, 1)
	})

	t.Run("status and value", func(t *testing.T) {
		sig := testSig(t, `func Do(a int32) (bool, error)`)
		assert.Equal(t, resStatusValue, sig.ret.kind)
		assert.Equal(t, "int32", sig.ret.tmpType) // C returns int32_t
		assert.Equal(t, "bool", sig.ret.goType)
		assert.Equal(t, 2, len(sig.cParams))
		assert.Equal(t, "int32_t*", sig.cParams[1].Type)
	})
}

func TestOutParamTempTypes(t *testing.T) {
	cases := map[string]string{
		`func F() (int8, error)`:         "int8",
		`func F() (int16, error)`:        "int16",
		`func F() (int32, error)`:        "int32",
		`func F() (int64, error)`:        "int64",
		`func F() (int, error)`:          "int64",
		`func F() (uint, error)`:         "uint64",
		`func F() (uintptr, error)`:      "uintptr",
		`func F() (uint8, error)`:        "uint8",
		`func F() (float32, error)`:      "float32",
		`func F() (float64, error)`:      "float64",
		`func F() (bool, error)`:         "int32",
		`func F() (*Point, error)`:       "unsafe.Pointer",
		`func F() (cppgo.Handle, error)`: "unsafe.Pointer",
	}
	for decl, want := range cases {
		t.Run(decl, func(t *testing.T) {
			sig := testSig(t, decl)
			assert.Equal(t, want, sig.ret.tmpType)
		})
	}
}

func TestResultErrors(t *testing.T) {
	cases := map[string]string{
		`func F() (int32, int32, error)`: "too many results",
		`func F() (int32, int32)`:        "second result must be error",
		`func F() (error, error)`:        "only have one error result",
		`func F() (error, int32)`:        "second result must be error",
		`func F() string`:                "unsupported result type string",
		`func F() ([]byte, error)`:       "unsupported result type []byte",
		`func F(a error) int32`:          "error is only supported as the last result",
	}
	for decl, want := range cases {
		t.Run(decl, func(t *testing.T) {
			f := testFunc(t, decl)
			_, err := buildSignature(f)
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)
		})
	}
}

func TestCParamNaming(t *testing.T) {
	t.Run("keywords are escaped", func(t *testing.T) {
		sig := testSig(t, `func F(class int32, new float64, ret int32) (int32, error)`)
		assert.Equal(t, "class_", sig.cParams[0].Name)
		assert.Equal(t, "new_", sig.cParams[1].Name)
		// A Go parameter called ret keeps the name, the out pointer moves.
		assert.Equal(t, "ret", sig.cParams[2].Name)
		assert.Equal(t, "ret_2", sig.cParams[3].Name)
	})

	t.Run("blank names get a name", func(t *testing.T) {
		sig := testSig(t, `func F(_ int32, _ int32)`)
		assert.Equal(t, "a0", sig.cParams[0].Name)
		assert.Equal(t, "a1", sig.cParams[1].Name)
	})

	t.Run("length parameters get a suffix", func(t *testing.T) {
		sig := testSig(t, `func F(s string, v []int32)`)
		assert.Equal(t, []string{"s", "s_len", "v", "v_len"}, cNames(sig))
		assert.Equal(t, "size_t", sig.cParams[1].Type)
		assert.Equal(t, "int32_t*", sig.cParams[2].Type)
	})

	t.Run("a parameter named like a length half does not collide", func(t *testing.T) {
		sig := testSig(t, `func F(s string, s_len int32)`)
		assert.Equal(t, []string{"s", "s_len", "s_len_2"}, cNames(sig))
	})
}

func cNames(sig *signature) []string {
	out := make([]string, len(sig.cParams))
	for i, p := range sig.cParams {
		out[i] = p.Name
	}
	return out
}

func TestCIdent(t *testing.T) {
	cases := map[string]string{
		"a":       "a",
		"":        "arg",
		"_":       "arg",
		"A_b9":    "A_b9",
		"class":   "class_",
		"9live":   "_9live",
		"weird.x": "weird_x",
		"Ñandú":   "_and_",
		"ret":     "ret",
		"size_t":  "size_t_",
		"int32_t": "int32_t_",
		"nullptr": "nullptr_",
		"x0":      "x0",
	}
	for in, want := range cases {
		assert.Equal(t, want, cIdent(in), in)
	}
}

func TestAlignUp(t *testing.T) {
	assert.Equal(t, 8, alignUp(8, 8))
	assert.Equal(t, 16, alignUp(9, 8))
	assert.Equal(t, 12, alignUp(9, 4))
	assert.Equal(t, 0, alignUp(0, 8))
}

func TestGuardName(t *testing.T) {
	assert.Equal(t, "MATHX", guardName("mathx"))
	assert.Equal(t, "MY_PKG", guardName("my.pkg"))
	assert.Equal(t, "PKG_1X", guardName("1x"))
	assert.Equal(t, "PKG_", guardName(""))
}

func TestSortFuncs(t *testing.T) {
	a := &Func{Name: "b"}
	b := &Func{Name: "a"}
	funcs := []*Func{a, b}
	sortFuncs(funcs)
	assert.Equal(t, []*Func{b, a}, funcs)
}
