package shim_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// call invokes a shim thunk through the direct trampoline and returns the
// integer result. Every shim thunk is extern "C" and noexcept, which is exactly
// the direct carrier's shape: the call needs no runtime bookkeeping.
//
// The stack probe is the caller's, and it must happen before any pointer into
// the caller's frame is put in the argument block: the probe grows the stack,
// which moves that frame. Doing it here would be too late, which is the bug
// this comment exists to prevent.
func call(t *testing.T, f *fixture, name string, a *asm.Args) uintptr {
	t.Helper()
	got, _ := asm.Call(f.sym(name), a)
	return got
}

// ptr is the uintptr a pointer argument crosses as.
func ptr(p unsafe.Pointer) uintptr { return uintptr(p) }

// TestVirtualDispatchAndVirtualDestructor measures row 1: a virtual call
// through a base pointer, and destruction through a virtual destructor. Both
// happen in the shim, where the vtable is; Go holds only an opaque address.
func TestVirtualDispatchAndVirtualDestructor(t *testing.T) {
	f := build(t)
	asm.Probe()

	var circle unsafe.Pointer
	var a asm.Args
	a.F[0] = 2
	a.I[0] = ptr(unsafe.Pointer(&circle))
	call(t, f, "shim_circle_new", &a)
	require.NotNil(t, circle)

	t.Run("virtual call through the base pointer", func(t *testing.T) {
		asm.Probe()
		var area float64
		var a asm.Args
		a.I[0] = ptr(circle)
		a.I[1] = ptr(unsafe.Pointer(&area))
		require.Zero(t, call(t, f, "shim_shape_area", &a))
		assert.Equal(t, 12.0, area, "Circle::area over 3*r^2 with r=2")
	})

	t.Run("name comes from the override", func(t *testing.T) {
		asm.Probe()
		buf := make([]byte, 32)
		var a asm.Args
		a.I[0] = ptr(circle)
		a.I[1] = ptr(unsafe.Pointer(&buf[0]))
		a.I[2] = uintptr(len(buf))
		require.Zero(t, call(t, f, "shim_shape_name", &a))
		assert.Equal(t, "Circle", cstring(unsafe.Pointer(&buf[0])))
	})

	t.Run("non-virtual member through the derived type", func(t *testing.T) {
		asm.Probe()
		var a asm.Args
		a.I[0] = ptr(circle)
		_, radius := asm.Call(f.sym("shim_circle_radius"), &a)
		assert.Equal(t, 2.0, radius)
	})

	t.Run("virtual destructor reaches the derived one", func(t *testing.T) {
		asm.Probe()
		var disk unsafe.Pointer
		var a asm.Args
		a.I[0] = 1
		a.F[0] = 1
		a.I[1] = ptr(unsafe.Pointer(&disk))
		call(t, f, "shim_disk_new", &a)

		var d asm.Args
		d.I[0] = ptr(disk)
		assert.EqualValues(t, 0, call(t, f, "shim_disk_destroys", &d))

		// delete through a Shape*: the virtual destructor runs ~Disk.
		call(t, f, "shim_disk_delete", &d)

		assert.EqualValues(t, 1, call(t, f, "shim_disk_destroys", &d),
			"the derived destructor ran through the base pointer")
	})
}

// TestInlineHeaderOnlyIsReachableOnlyThroughTheShim measures row 2 and states
// its exact limit: user::twice is inline in the header and the library never
// calls it, so the library exports no such symbol and no Go trampoline can reach
// it. The shim includes the header, so the body is compiled into the shim.
func TestInlineHeaderOnlyIsReachableOnlyThroughTheShim(t *testing.T) {
	f := build(t)
	asm.Probe()

	assert.False(t, exported(t, f.user, "_Z5twiced"),
		"the user's library exports no symbol for an inline body it never calls")
	assert.True(t, exported(t, f.shim, "shim_twice"),
		"the shim exports the thunk that contains the body")

	var a asm.Args
	a.F[0] = 21
	_, got := asm.Call(f.sym("shim_twice"), &a)
	assert.Equal(t, 42.0, got)
}

// TestStdStringAndVector measures row 3: std::string and std::vector cross as
// Go strings and slices, marshalled by the shim into real C++ objects.
func TestStdStringAndVector(t *testing.T) {
	f := build(t)
	asm.Probe()

	t.Run("std::string in and out", func(t *testing.T) {
		asm.Probe()
		prefix, name := "hello", "world"
		buf := make([]byte, 64)

		var a asm.Args
		a.I[0] = ptr(unsafe.Pointer(unsafe.StringData(prefix)))
		a.I[1] = uintptr(len(prefix))
		a.I[2] = ptr(unsafe.Pointer(unsafe.StringData(name)))
		a.I[3] = uintptr(len(name))
		a.I[4] = ptr(unsafe.Pointer(&buf[0]))
		a.I[5] = uintptr(len(buf))

		written := call(t, f, "shim_greet", &a)
		require.EqualValues(t, len("hello, world!"), written)
		assert.Equal(t, "hello, world!", string(buf[:written]))
	})

	t.Run("std::vector in and out", func(t *testing.T) {
		asm.Probe()
		values := []float64{1, 2, 3}
		out := make([]float64, len(values))
		var a asm.Args
		a.I[0] = ptr(unsafe.Pointer(&values[0]))
		a.I[1] = uintptr(len(values))
		a.I[2] = ptr(unsafe.Pointer(&out[0]))
		a.I[3] = uintptr(len(out))
		assert.EqualValues(t, 3, call(t, f, "shim_doubled", &a))
		assert.Equal(t, []float64{2, 4, 6}, out)
	})

	t.Run("std::vector const ref", func(t *testing.T) {
		asm.Probe()
		values := []float64{1, 2, 3.5}
		var sum float64
		var a asm.Args
		a.I[0] = ptr(unsafe.Pointer(&values[0]))
		a.I[1] = uintptr(len(values))
		a.I[2] = ptr(unsafe.Pointer(&sum))
		require.Zero(t, call(t, f, "shim_sum", &a))
		assert.Equal(t, 6.5, sum)
	})
}

// TestNonPODByValue measures row 4: a value with a user-declared copy
// constructor is constructed and copied inside the shim, so Go never needs the
// value's layout. The copy constructor adds 1000, which is the observable proof
// that it ran.
func TestNonPODByValue(t *testing.T) {
	f := build(t)
	asm.Probe()

	var value int32
	var a asm.Args
	a.I[0] = 7
	a.I[1] = ptr(unsafe.Pointer(&value))
	require.Zero(t, call(t, f, "shim_tiny_value", &a))
	assert.EqualValues(t, 1007, value, "the copy constructor ran on the by-value copy")
}

// TestExceptionsBecomeErrors measures row 5 the only way it can be measured: a
// C++ frame stops the exception, and the message arrives in Go as a string.
// Without such a frame the process terminates — see internal/callback's
// exception tests, which measure that half.
func TestExceptionsBecomeErrors(t *testing.T) {
	f := build(t)
	asm.Probe()

	t.Run("success", func(t *testing.T) {
		asm.Probe()
		var out int32
		var a asm.Args
		a.I[0] = 42
		a.I[1] = 2
		a.I[2] = ptr(unsafe.Pointer(&out))
		require.Zero(t, call(t, f, "shim_checked_div", &a))
		assert.EqualValues(t, 21, out)
	})

	t.Run("throw is caught and reported", func(t *testing.T) {
		asm.Probe()
		var out int32
		var a asm.Args
		a.I[0] = 42
		a.I[1] = 0
		a.I[2] = ptr(unsafe.Pointer(&out))
		status := call(t, f, "shim_checked_div", &a)

		assert.EqualValues(t, -1, int32(status), "the thunk reports failure")
		assert.Equal(t, "division by zero", f.lastError(t))
	})
}

// TestTemplateInstantiation measures row 6: the instantiation lives in the shim
// and is selected by the generated code; the user's library contains none of
// it, and no C++ was added to the user's build.
func TestTemplateInstantiation(t *testing.T) {
	f := build(t)
	asm.Probe()

	assert.Equal(t, 0, len(symbolsMatching(t, f.user, "maxof")),
		"the user's library has no instantiation of the header-only template")

	var a asm.Args
	a.F[0] = 2.5
	a.F[1] = 1.5
	_, got := asm.Call(f.sym("shim_maxof_double"), &a)
	assert.Equal(t, 2.5, got)

	var b asm.Args
	b.I[0] = 3
	b.I[1] = 9
	gotInt, _ := asm.Call(f.sym("shim_maxof_int"), &b)
	assert.EqualValues(t, 9, int32(gotInt))
}

// TestMultipleInheritanceAdjustment measures row 7: the Circle subobject of a
// Disk is at a non-zero offset, and the shim's casts are the only place that
// offset exists. Go passes one address and reads two others back.
func TestMultipleInheritanceAdjustment(t *testing.T) {
	f := build(t)
	asm.Probe()

	var disk unsafe.Pointer
	var a asm.Args
	a.I[0] = 3
	a.F[0] = 1
	a.I[1] = ptr(unsafe.Pointer(&disk))
	call(t, f, "shim_disk_new", &a)

	var circleAt uintptr
	var serial int32
	var b asm.Args
	b.I[0] = ptr(disk)
	b.I[1] = ptr(unsafe.Pointer(&circleAt))
	b.I[2] = ptr(unsafe.Pointer(&serial))
	require.Zero(t, call(t, f, "shim_disk_serial", &b))

	assert.NotEqual(t, ptr(disk), circleAt,
		"Disk starts with Serializable, so its Circle subobject is not at offset 0")
	assert.EqualValues(t, 30, serial, "Disk::serial is id*10 for id=3")

	// A virtual call through the adjusted pointer is the same object.
	var area float64
	var c asm.Args
	c.I[0] = circleAt
	c.I[1] = ptr(unsafe.Pointer(&area))
	require.Zero(t, call(t, f, "shim_shape_area", &c))
	assert.Equal(t, 3.0, area)
}

// TestCallbackThroughTheShim measures the callback row with the shim as the
// boundary: the Go function pointer is handed to C++, which wraps it in a
// std::function and passes it to the user's function. Go never sees the
// std::function layout, and the shim needs no exported template instantiation.
func TestCallbackThroughTheShim(t *testing.T) {
	f := build(t)

	cb, err := callback.Register(func(a *callback.Args) {
		a.Ret = uintptr(int32(a.I[0]) + int32(a.I[1]))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	// The out-value is a heap allocation: the callback runs Go code that may
	// grow the stack, so the pointer the C++ side holds must not be in a
	// frame that can move.
	out := new(int32)
	var a asm.Args
	a.I[0] = cb
	a.I[1] = 40
	a.I[2] = 2
	a.I[3] = ptr(unsafe.Pointer(out))

	// The call may invoke Go, so it goes through the reentrant carrier.
	asm.CallReentrant(f.sym("shim_apply"), &a)

	assert.EqualValues(t, 42, *out)
	assert.Nil(t, callback.TakePanic())
}

// TestArtifactModel measures what the contract promises about the two builds:
// the shim is an object of its own, the user's library gains no cppgo symbol,
// and the shim's dynamic symbols are its own thunks.
func TestArtifactModel(t *testing.T) {
	f := build(t)

	assert.Empty(t, symbolsMatching(t, f.user, "cppgo"),
		"the user's library contains no cppgo symbol and no generated code")
	assert.Empty(t, symbolsMatching(t, f.user, "shim_"),
		"and none of the shim's thunks")

	for _, name := range []string{"shim_shape_area", "shim_greet", "shim_checked_div", "cppgo_last_error"} {
		assert.True(t, exported(t, f.shim, name), "the shim exports %s", name)
	}
}
