//go:build stackalloc

package stackalloc

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// growFrame is one recursion level's frame. It has to stay under the compiler's
// limit for a variable it keeps on the stack (ir.MaxStackVarSize, 128 KiB in
// this toolchain) and be far larger than anything else in these tests, so that
// entering one more level always asks the runtime for a bigger stack.
const growFrame = 100 << 10

// growStack descends depth levels, each with a growFrame-byte frame, which
// forces the runtime to move the goroutine's stack to a bigger one. It takes a
// depth rather than checking anything itself, so a callback can call it without
// capturing any address in the frame being demonstrated.
//
//go:noinline
func growStack(depth int) {
	var frame [growFrame]byte
	frame[0] = byte(depth)
	if depth > 0 {
		growStack(depth - 1)
	}
	runtime.KeepAlive(&frame)
}

// growUntilMoved descends until c's address differs from pre, and reports
// whether it did. A goroutine's starting stack size adapts to the stacks of the
// goroutines before it, so no fixed depth is correct on its own; this grows the
// stack as far as it has to and lets the caller assert on the result.
//
//go:noinline
func growUntilMoved(c *Cell, pre uintptr, depth int) bool {
	var frame [growFrame]byte
	frame[0] = byte(depth)

	if c.Addr() != pre {
		runtime.KeepAlive(&frame)
		return true
	}
	if depth == 0 {
		runtime.KeepAlive(&frame)
		return false
	}

	moved := growUntilMoved(c, pre, depth-1)
	runtime.KeepAlive(&frame)
	return moved
}

// TestFrameMovesAcrossStackGrowth is the fact everything else in this file is
// built on: a local's address is not stable across a stack growth, and the
// runtime adjusts only the pointers it can see in stack maps. The uintptr a C++
// caller holds is not one of them.
func TestFrameMovesAcrossStackGrowth(t *testing.T) {
	pre, post, moved := addressAcrossGrowth()
	require.True(t, moved,
		"the storage did not move, so the Cell escaped to the heap and the demonstration is void")
	assert.NotEqual(t, pre, post)
}

//go:noinline
func addressAcrossGrowth() (pre, post uintptr, moved bool) {
	var c Cell
	pre = c.Addr()
	moved = growUntilMoved(&c, pre, 32)
	post = c.Addr()
	return pre, post, moved
}

// TestPinTakesAnInterfaceSoTheStorageEscapes is the first half of why
// runtime.Pinner cannot make a stack object stay put: Pin's parameter is `any`,
// and a pointer converted to an interface escapes to the heap. The compiler
// refuses the premise before the runtime could: -gcflags=-m reports "moved to
// heap: c" for this function.
func TestPinTakesAnInterfaceSoTheStorageEscapes(t *testing.T) {
	pre, post, moved := addressAcrossGrowthWhilePinningThroughTheInterface()
	assert.False(t, moved,
		"the storage moved even though it was passed to Pin through an interface")
	assert.Equal(t, pre, post)
}

//go:noinline
func addressAcrossGrowthWhilePinningThroughTheInterface() (pre, post uintptr, moved bool) {
	var c Cell
	var p runtime.Pinner
	pre = c.Addr()
	p.Pin(&c.buf[0]) // the interface conversion moves c to the heap
	moved = growUntilMoved(&c, pre, 32)
	post = c.Addr()
	p.Unpin()
	return pre, post, moved
}

// TestPinnerIgnoresAStackAddress is the second half: even when the address
// reaches Pin as a uintptr, so that nothing escapes and the storage really is in
// the frame, the pin is a silent no-op. runtime.setPinned looks the address up
// with spanOfHeap, which returns nil for anything that is not a heap object;
// Pin then records nothing and panics on nothing. The address moves anyway.
func TestPinnerIgnoresAStackAddress(t *testing.T) {
	pre, post, moved := addressAcrossGrowthWhilePinnedAsUintptr()
	require.True(t, moved,
		"the storage did not move, so the Cell escaped to the heap and the demonstration is void")
	assert.NotEqual(t, pre, post)
}

//go:nocheckptr
//go:noinline
func addressAcrossGrowthWhilePinnedAsUintptr() (pre, post uintptr, moved bool) {
	var c Cell
	var p runtime.Pinner
	pre = c.Addr()
	// The uintptr round-trip is deliberate: it is the only way to hand Pin an
	// address without the interface conversion moving the object first, and it
	// is the same shape a generated binding uses. go vet -tags stackalloc
	// reports it as unsafeptr; that warning is the subject of the test.
	p.Pin(unsafe.Pointer(pre))
	moved = growUntilMoved(&c, pre, 32)
	post = c.Addr()
	p.Unpin()
	return pre, post, moved
}

// TestCallbackGrowthLeavesTheThisPointerStale is the case the carrier decides:
// with the reentrant carrier a Go frame runs while C++ is inside the call, that
// frame may grow the goroutine stack (runtime/cgocall.go: "run the Go code
// (which may grow the stack)"), and the addresses a caller passed into the call
// are then one stack copy behind.
//
// The two out-pointers make it a differential measurement. With no growth the
// value C++ writes reaches the caller's frame; with growth the heap object is
// still written and the frame slot is not, which is a lost write with no
// use-after-free in it.
func TestCallbackGrowthLeavesTheThisPointerStale(t *testing.T) {
	sym := fixture(t)
	construct := sym("cppgo_cell_construct")
	capture := sym("cppgo_cell_after_callback_capture")
	x := sym("cppgo_cell_x")

	t.Run("no growth", func(t *testing.T) {
		pre, post, heap, frame, live, moved := thisPointerAttempt(construct, capture, x, 0)
		require.False(t, moved, "0 is the control: the stack must not move")
		assert.Equal(t, pre, post)
		assert.Equal(t, pre, heap, "C++ holds the address it was given")
		assert.Equal(t, pre, frame, "the write into the caller's frame arrived")
		assert.Equal(t, 1.0, live)
	})

	t.Run("growth", func(t *testing.T) {
		pre, post, heap, frame, live, moved := thisPointerAcrossCallback(construct, capture, x)
		require.True(t, moved,
			"the stack did not move, so the Cell escaped to the heap and the demonstration is void")
		assert.Equal(t, pre, heap, "the heap out-pointer still names the address C++ was given")
		assert.NotEqual(t, post, heap, "the object is no longer at that address")
		assert.Zero(t, frame, "the write into the caller's frame was lost to the stack copy")
		assert.Equal(t, 1.0, live, "the live object is unchanged: C++ wrote to the copy that is gone")
	})
}

// thisPointerAcrossCallback retries with deeper recursion until the stack
// really moved, then returns the addresses plus a read through the live value.
func thisPointerAcrossCallback(construct, capture, query unsafe.Pointer) (pre, post, heap, frame uintptr, live float64, moved bool) {
	for depth := 8; depth <= 512; depth *= 2 {
		pre, post, heap, frame, live, moved = thisPointerAttempt(construct, capture, query, depth)
		if moved {
			return pre, post, heap, frame, live, moved
		}
	}
	return pre, post, heap, frame, live, moved
}

// newHeapUintptr returns a *uintptr that the compiler has to allocate on the
// heap: a `new(uintptr)` whose address never leaves the function as a typed
// pointer stays in the frame, and then the sink moves with it.
//
//go:noinline
func newHeapUintptr() *uintptr { return new(uintptr) }

//go:noinline
func thisPointerAttempt(construct, capture, query unsafe.Pointer, depth int) (pre, post, heapSink, frameSink uintptr, live float64, moved bool) {
	var c Cell
	c.Construct(construct, 1, 2)
	pre = c.Addr()

	// The callback captures nothing, so the Cell does not escape through it.
	cb, err := callback.Register(func(*callback.Args) {
		if depth > 0 {
			growStack(depth)
		}
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = callback.Release(cb) }()

	// One sink that cannot move, one that is in this frame. newHeapUintptr
	// keeps the allocation out of this frame: a `new(uintptr)` whose address is
	// only ever passed as a uintptr stays on the stack, which is the effect the
	// frame sink demonstrates on purpose.
	pinned := newHeapUintptr()
	var inFrame uintptr

	var args asm.Args
	args.I[0] = c.Addr()
	args.I[1] = cb
	args.I[2] = uintptr(unsafe.Pointer(pinned))
	args.I[3] = uintptr(unsafe.Pointer(&inFrame))
	asm.CallReentrant(capture, &args)
	runtime.KeepAlive(&inFrame)

	heapSink = *pinned
	frameSink = inFrame
	post = c.Addr()
	moved = pre != post
	// Every later call recomputes the address, so the live object is still
	// usable; what is stale is the pointer C++ was given during the call.
	live = c.X(query)
	return pre, post, heapSink, frameSink, live, moved
}

// TestCallbackGrowthLosesAWriteThroughThis is the consequence, measured in a
// child process because writing through the stale address is exactly the
// use-after-free: the write lands in a freed stack span or faults, and neither
// outcome is something the parent can assert on in-process.
func TestCallbackGrowthLosesAWriteThroughThis(t *testing.T) {
	code, out := runScenario(t, scenarioStaleWrite)
	t.Logf("child exit=%d output=%q", code, out)

	if code == 0 {
		assert.Contains(t, out, "LIVE=1",
			"the write through the stale this reached the live object")
	}
}
