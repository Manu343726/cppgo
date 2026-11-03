package cppgo_test

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/testlib"
)

func TestStackReserve(t *testing.T) {
	// The contract is 32 KiB of headroom; a smaller value would not cover a
	// generated shim's frame.
	assert.Equal(t, 32<<10, cppgo.StackReserve)
}

// TestProbeStackDeepForeignFrame is the test the whole mechanism exists for: a
// foreign function burns up to 28 KiB of stack below the trampoline's stack
// pointer, which only works because ProbeStack grew the goroutine's stack
// first. The fixture refuses to go deeper than 28 KiB, i.e. it stays inside the
// 32 KiB reserve.
func TestProbeStackDeepForeignFrame(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	deep := fn(t, lib, "cppgo_fixture_deep_stack")

	for _, size := range []int64{0, 4096, 16 << 10, 24 << 10, 28 << 10} {
		cppgo.ProbeStack()
		raw, _ := asm.Call(deep, &asm.Args{I: [6]uintptr{uintptr(size)}})
		assert.NotZero(t, raw, "deep_stack(%d) must return the checksum", size)
	}
}

// TestProbeStackDeepFrameInFreshGoroutine does the same on goroutines that
// start with the minimum stack, which is what a short-lived goroutine that
// calls a foreign function looks like.
func TestProbeStackDeepFrameInFreshGoroutine(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	deep := fn(t, lib, "cppgo_fixture_deep_stack")

	const goroutines = 16

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			for range 20 {
				cppgo.ProbeStack()
				raw, _ := asm.Call(deep, &asm.Args{I: [6]uintptr{28 << 10}})
				if raw == 0 {
					errs <- fmt.Errorf("deep_stack returned %d", raw)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
}

// TestProbeStackReuse checks that the probe is reusable: after the first call
// on a goroutine the stack is already large enough, and further calls must
// neither grow it again nor disturb the frame below which the foreign call
// runs.
func TestProbeStackReuse(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	add := fn(t, lib, "cppgo_fixture_add")
	deep := fn(t, lib, "cppgo_fixture_deep_stack")

	done := make(chan error, 1)

	go func() {
		for range 100_000 {
			cppgo.ProbeStack()
		}

		// The stack is comfortably grown by now; the trampoline must still
		// work, both for a tiny call and for one that uses the reserve.
		raw, _ := asm.Call(add, &asm.Args{I: [6]uintptr{20, 22}})
		if raw != 42 {
			done <- fmt.Errorf("add after 100000 probes = %d, want 42", raw)
			return
		}

		raw, _ = asm.Call(deep, &asm.Args{I: [6]uintptr{28 << 10}})
		if raw == 0 {
			done <- fmt.Errorf("deep_stack after 100000 probes = %d", raw)
			return
		}

		done <- nil
	}()

	require.NoError(t, <-done)
}

func TestProbeStackDoesNotAllocate(t *testing.T) {
	// AllocsPerRun reports the average number of heap allocations per run.
	allocs := testing.AllocsPerRun(1000, cppgo.ProbeStack)

	assert.Zero(t, allocs, "ProbeStack must not allocate")
}

// TestProbeStackDeepFrameUnderGC runs the deep foreign frame while the
// garbage collector is stopping and copying goroutine stacks continuously.
// A stack that shrank back between the probe and the call, or a stack move the
// foreign frame did not survive, shows up here as a crash rather than a failed
// assertion: the point of the test is that it does not happen.
func TestProbeStackDeepFrameUnderGC(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	deep := fn(t, lib, "cppgo_fixture_deep_stack")

	oldGC := debug.SetGCPercent(1)
	defer debug.SetGCPercent(oldGC)

	stop := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()

	const goroutines = 16

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()

			for range 200 {
				cppgo.ProbeStack()
				raw, _ := asm.Call(deep, &asm.Args{I: [6]uintptr{28 << 10}})
				if raw == 0 {
					errs <- fmt.Errorf("deep_stack returned %d under GC pressure", raw)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(stop)
	<-done
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}
}
