package shim_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
)

// Ordered is the constraint the Go side of a generic binding uses. The Go
// program is written against it; nothing in the program names a C++ type.
type Ordered interface {
	~int32 | ~int64 | ~float32 | ~float64
}

// instantiation is what a generated package derives from the type parameter:
// the name of the thunk that carries it, and the C++ spelling the shim has to
// instantiate. In a generated package both come from the same table the shim is
// emitted from, so a Go instantiation cannot silently lack a C++ counterpart.
type instantiation struct {
	thunk string
	cpp   string
}

var instantiations = map[string]instantiation{
	"int32":   {"shim_maxof_int", "user::maxof<int32_t>"},
	"int64":   {"shim_maxof_int64", "user::maxof<int64_t>"},
	"float64": {"shim_maxof_double", "user::maxof<double>"},
}

// Max is the generic Go API over user::maxof: one Go function, one thunk per
// instantiation.
func Max[T Ordered](f *fixture, a, b T) (T, error) {
	name := fmt.Sprintf("%T", a)
	spec, ok := instantiations[name]
	if !ok {
		var zero T
		return zero, fmt.Errorf("cppgo: no instantiation for %s", name)
	}

	addr, err := f.symbol(spec.thunk)
	if err != nil {
		var zero T
		return zero, fmt.Errorf(
			"cppgo: %s is instantiated in Go but not in the shim: add `template %s;` to the shim's instantiation list: %w",
			name, spec.cpp, err)
	}

	// The result bank follows the type: an INTEGER result comes back in
	// RAX, an SSE result in XMM0. The generated code knows which one the
	// instantiation's C++ signature names; here the type switch is that
	// knowledge.
	switch any(a).(type) {
	case float64:
		var args asm.Args
		args.F[0] = float64(a)
		args.F[1] = float64(b)
		_, got := asm.Call(addr, &args)
		return T(got), nil
	default:
		var args asm.Args
		args.I[0] = uintptr(a)
		args.I[1] = uintptr(b)
		got, _ := asm.Call(addr, &args)
		return T(got), nil
	}
}

// TestTwoPhaseGenerics measures the union point between a Go generic API and a
// C++ template library: the Go program instantiates the generic; the shim must
// contain the matching C++ instantiation; a missing one is a load-time error
// that names the line to add, not a wrong answer.
func TestTwoPhaseGenerics(t *testing.T) {
	f := build(t)
	asm.Probe()

	t.Run("instantiated on both sides", func(t *testing.T) {
		asm.Probe()
		got, err := Max(f, float64(2.5), float64(1.5))
		require.NoError(t, err)
		assert.Equal(t, 2.5, got)

		gotInt, err := Max(f, int32(3), int32(9))
		require.NoError(t, err)
		assert.EqualValues(t, 9, gotInt)
	})

	t.Run("Go instantiation with no C++ counterpart", func(t *testing.T) {
		asm.Probe()
		// int64 is in the Go table (the generic API accepts it) and has no
		// thunk in the shim, which is exactly the failure mode the contract
		// has to define.
		_, err := Max(f, int64(1), int64(2))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "user::maxof<int64_t>")
		assert.Contains(t, err.Error(), "template user::maxof<int64_t>;")
	})

	t.Run("type outside the table", func(t *testing.T) {
		asm.Probe()
		// float32 satisfies the constraint but has no entry in the Go
		// table, so it never reaches a thunk.
		_, err := Max(f, float32(1), float32(2))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no instantiation for float32")
	})
}
