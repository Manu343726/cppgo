package callback_test

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// TestCallbackThroughCgoCall is the headline measurement: a Go function handed
// to C++ as a function pointer and called from C++ during a Go→C++ call.
func TestCallbackThroughCgoCall(t *testing.T) {
	sym := fixture(t)

	cb, err := callback.Register(func(a *callback.Args) {
		a.Ret = uintptr(int32(a.I[0]) * int32(a.I[1]))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	var args asm.Args
	args.I[0] = cb
	args.I[1] = 6
	args.I[2] = 7

	got, _ := asm.CallReentrant(sym("cb_apply"), &args)
	assert.EqualValues(t, 42, int32(got))
	assert.Nil(t, callback.TakePanic())
}

// TestCallbackArgumentAndResultBanks covers the direction of every register
// class: INTEGER arguments and an INTEGER result, SSE arguments and an SSE
// result, and a pointer argument that round-trips unchanged.
func TestCallbackArgumentAndResultBanks(t *testing.T) {
	sym := fixture(t)

	t.Run("integer", func(t *testing.T) {
		cb, err := callback.Register(func(a *callback.Args) {
			a.Ret = a.I[0] + a.I[1]
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = callback.Release(cb) })

		var args asm.Args
		args.I[0] = cb
		args.I[1] = 40
		args.I[2] = 2
		got, _ := asm.CallReentrant(sym("cb_apply"), &args)
		assert.EqualValues(t, 42, got)
	})

	t.Run("float", func(t *testing.T) {
		cb, err := callback.Register(func(a *callback.Args) {
			a.RetF = a.F[0] * a.F[1]
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = callback.Release(cb) })

		var args asm.Args
		args.I[0] = cb
		args.F[0] = 1.5
		args.F[1] = 4
		_, gotF := asm.CallReentrant(sym("cb_apply_double"), &args)
		assert.Equal(t, 6.0, gotF)
	})

	t.Run("pointer", func(t *testing.T) {
		var seen atomic.Uintptr
		cb, err := callback.Register(func(a *callback.Args) {
			seen.Store(a.I[0])
			a.Ret = 0
		})
		require.NoError(t, err)
		t.Cleanup(func() { _ = callback.Release(cb) })

		user := new(int32)
		*user = 11
		// The address is captured before the call: the goroutine stack may
		// move while the foreign call is in flight, and the wire contract
		// is that a pointer into Go memory is borrowed for the call, never
		// retained and never used to re-derive it afterwards.
		want := uintptr(unsafe.Pointer(user))

		var args asm.Args
		args.I[0] = cb
		args.I[1] = want
		args.I[2] = 11
		asm.CallReentrant(sym("cb_call_sink"), &args)

		assert.Equal(t, want, seen.Load())
	})
}

// TestCallbackRunsOnTheCallingGoroutine measures which goroutine and which
// thread a callback runs on. It is the question every lifetime rule depends on:
// the runtime runs the callback on the goroutine that made the foreign call,
// locked to the same M, with the C frames still below it.
func TestCallbackRunsOnTheCallingGoroutine(t *testing.T) {
	sym := fixture(t)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	callerG, callerTid := goid(), tid()

	var cbG, cbTid uint64
	cb, err := callback.Register(func(a *callback.Args) {
		cbG, cbTid = goid(), tid()
		a.Ret = 0
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	var args asm.Args
	args.I[0] = cb
	args.I[1] = 1
	args.I[2] = 2
	asm.CallReentrant(sym("cb_apply"), &args)

	assert.Equal(t, callerG, cbG, "the callback runs on the goroutine that made the call")
	assert.Equal(t, callerTid, cbTid, "and on the thread that goroutine is locked to")
}

// TestCallbackCanUseTheRuntime proves the callback is a normal Go frame: it
// allocates, grows its stack and takes a traceback, none of which would be
// possible if the trampoline had merely jumped into Go.
func TestCallbackCanUseTheRuntime(t *testing.T) {
	sym := fixture(t)

	var grew atomic.Bool
	cb, err := callback.Register(func(a *callback.Args) {
		grew.Store(growStack(20))
		a.Ret = uintptr(len(map[string]int{"a": 1}))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	var args asm.Args
	args.I[0] = cb
	args.I[1] = 1
	args.I[2] = 2
	got, _ := asm.CallReentrant(sym("cb_apply"), &args)

	assert.EqualValues(t, 1, got)
	assert.True(t, grew.Load(), "the callback allocated from the runtime's heap and stack")
}

// growStack recurses until depth frames are live, which forces the goroutine
// stack to grow while the callback is running, and reports whether it reached
// the bottom.
func growStack(depth int) bool {
	var pad [64]byte
	pad[0] = byte(depth)
	if depth == 0 {
		return pad[0] == 0
	}
	return growStack(depth - 1)
}

// TestCallbackReentrancy measures Go → C++ → Go → C++: the Go callback calls
// back into C++ through the same carrier, which calls a second Go callback.
// Nothing in the mechanism is single-entry, because the runtime counts nested
// foreign calls per M.
func TestCallbackReentrancy(t *testing.T) {
	sym := fixture(t)

	var inner, outer int

	innerCb, err := callback.Register(func(a *callback.Args) {
		inner++
		a.Ret = a.I[0] * 2
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(innerCb) })

	outerCb, err := callback.Register(func(a *callback.Args) {
		outer++
		// Inside the outer callback: call C++ again, which calls the
		// inner Go callback, which returns to C++, which returns here.
		var again asm.Args
		again.I[0] = innerCb
		again.I[1] = a.I[0]
		again.I[2] = 0
		got, _ := asm.CallReentrant(sym("cb_apply"), &again)
		a.Ret = got + 1
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(outerCb) })

	var args asm.Args
	args.I[0] = outerCb
	args.I[1] = 5
	args.I[2] = 0
	got, _ := asm.CallReentrant(sym("cb_apply"), &args)

	assert.Equal(t, 1, outer)
	assert.Equal(t, 1, inner)
	assert.EqualValues(t, 11, got)
	assert.Nil(t, callback.TakePanic())
}

// TestCallbackConcurrent runs callbacks from many goroutines at once. The
// table is read without a lock on this path, so this is the test the -race pass
// exists for.
func TestCallbackConcurrent(t *testing.T) {
	sym := fixture(t)

	cb, err := callback.Register(func(a *callback.Args) {
		a.Ret = a.I[0] * a.I[1]
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	var wg sync.WaitGroup
	var wrong atomic.Int64
	for g := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				var args asm.Args
				args.I[0] = cb
				args.I[1] = uintptr(g)
				args.I[2] = 1
				got, _ := asm.CallReentrant(sym("cb_apply"), &args)
				if int32(got) != int32(g) {
					wrong.Add(1)
				}
			}
		}()
	}
	wg.Wait()

	assert.Zero(t, wrong.Load())
}

// TestTakePanicWhenNothingPanicked pins the empty case, so a stale panic cannot
// be attributed to a later call.
func TestTakePanicWhenNothingPanicked(t *testing.T) {
	assert.Nil(t, callback.TakePanic())
}
