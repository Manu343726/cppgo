package cppgo_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
)

func TestPtr(t *testing.T) {
	t.Run("empty slice is nil", func(t *testing.T) {
		assert.Nil(t, cppgo.Ptr([]float64(nil)))
		assert.Nil(t, cppgo.Ptr([]float64{}))
		assert.Nil(t, cppgo.Ptr([]byte{}))
	})

	t.Run("non-empty slice points at the first element", func(t *testing.T) {
		buf := []float64{1, 2, 3}
		require.Equal(t, unsafe.Pointer(&buf[0]), cppgo.Ptr(buf))

		bytes := []byte{7, 8}
		require.Equal(t, unsafe.Pointer(&bytes[0]), cppgo.Ptr(bytes))
	})

	t.Run("length zero with spare capacity is nil", func(t *testing.T) {
		// &s[0] would panic: there is no element 0.
		buf := make([]float64, 4)[:0:4]
		assert.Nil(t, cppgo.Ptr(buf))
	})

	t.Run("struct elements", func(t *testing.T) {
		type pair struct {
			A int64
			B int64
		}

		buf := []pair{{1, 2}}
		require.Equal(t, unsafe.Pointer(&buf[0]), cppgo.Ptr(buf))
	})
}

func TestStrPtr(t *testing.T) {
	t.Run("empty string is nil", func(t *testing.T) {
		assert.Nil(t, cppgo.StrPtr(""))
	})

	t.Run("non-empty string points at its bytes", func(t *testing.T) {
		const s = "cppgo"
		ptr := cppgo.StrPtr(s)
		require.NotNil(t, ptr)
		require.Equal(t, unsafe.Pointer(unsafe.StringData(s)), ptr)

		// The bytes are not copied and not NUL-terminated.
		got := unsafe.String((*byte)(ptr), len(s))
		assert.Equal(t, s, got)
	})

	t.Run("string with an interior NUL keeps all its bytes", func(t *testing.T) {
		const s = "a\x00b"
		ptr := cppgo.StrPtr(s)
		require.NotNil(t, ptr)

		got := unsafe.String((*byte)(ptr), len(s))
		assert.Equal(t, s, got)
		assert.Equal(t, byte(0), *(*byte)(unsafe.Add(ptr, 1)))
	})
}

func TestKeepAlive(t *testing.T) {
	buf := []float64{1, 2, 3}

	ptr := cppgo.Ptr(buf)
	require.NotNil(t, ptr)

	// KeepAlive's whole contract is "this compiles, is a no-op at run time and
	// keeps buf reachable"; the observable part is that it accepts any mix of
	// values and never panics.
	cppgo.KeepAlive(buf)
	cppgo.KeepAlive(ptr, "s", 42)
	cppgo.KeepAlive()

	assert.Equal(t, unsafe.Pointer(&buf[0]), ptr)
}
