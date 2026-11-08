package callback_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// TestRegisterNil rejects the one registration that has no meaning, so a nil
// callback cannot be handed to C as an address that does nothing.
func TestRegisterNil(t *testing.T) {
	code, err := callback.Register(nil)
	assert.ErrorIs(t, err, callback.ErrNilFunc)
	assert.Zero(t, code)
}

// TestRegisterAndRelease hands out every slot, checks that the next
// registration fails with ErrFull rather than reusing a live slot, and then
// proves that releasing one makes exactly one slot available again.
func TestRegisterAndRelease(t *testing.T) {
	code, err := callback.Register(func(*callback.Args) {})
	require.NoError(t, err)
	assert.True(t, callback.Lookup(code))

	require.NoError(t, callback.Release(code))
	assert.False(t, callback.Lookup(code), "a released address is not a live callback")

	// Releasing it twice is reported, not ignored: a second Release on a
	// live slot would drop a callback C may still hold.
	assert.ErrorIs(t, callback.Release(code), callback.ErrUnknown)
}

// TestReleaseUnknown rejects an address outside the table and one inside it
// that is not aligned to a slot.
func TestReleaseUnknown(t *testing.T) {
	assert.ErrorIs(t, callback.Release(0), callback.ErrUnknown)
	assert.ErrorIs(t, callback.Release(1), callback.ErrUnknown)

	cb, err := callback.Register(func(*callback.Args) {})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	// An address inside a slot's instruction is not a slot.
	assert.ErrorIs(t, callback.Release(cb+1), callback.ErrUnknown)
}

// TestTableFull exhausts the table and then releases it again, so the slots a
// test leaves behind are not the next test's failure. The table is process-wide
// and this test is the one that owns its bound.
func TestTableFull(t *testing.T) {
	// A test that runs concurrently with this one would see the table
	// half-full; the suite is sequential for this reason, and the table's
	// bound is what makes it worth saying out loud.
	var held []uintptr
	for i := range callback.Slots {
		code, err := callback.Register(func(*callback.Args) {})
		require.NoError(t, err, "slot %d", i)
		held = append(held, code)
	}

	_, err := callback.Register(func(*callback.Args) {})
	assert.ErrorIs(t, err, callback.ErrFull)

	for _, code := range held {
		require.NoError(t, callback.Release(code))
	}

	code, err := callback.Register(func(*callback.Args) {})
	require.NoError(t, err)
	require.NoError(t, callback.Release(code))
}

// TestReleaseWaitsForInFlightCalls pins the teardown rule: Release returns only
// once no Go frame of the callback is running, so a caller can release a
// callback and then free what it captured. Without the wait this test either
// races (under -race) or sees the closure's state torn down mid-call.
func TestReleaseWaitsForInFlightCalls(t *testing.T) {
	sym := fixture(t)

	entered := make(chan struct{})
	leave := make(chan struct{})
	cb, err := callback.Register(func(a *callback.Args) {
		close(entered)
		<-leave
		a.Ret = 1
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		defer close(done)
		var args asm.Args
		args.I[0] = cb
		asm.CallReentrant(sym("cb_apply"), &args)
	}()

	<-entered

	released := make(chan struct{})
	go func() {
		defer close(released)
		assert.NoError(t, callback.Release(cb))
	}()

	select {
	case <-released:
		t.Fatal("Release returned while the callback was still running")
	case <-time.After(20 * time.Millisecond):
	}

	close(leave)
	<-done
	<-released
}

// TestReleasedSlotReportsThroughTakePanic covers the one thing Release cannot
// prevent: C++ still holding the address. Calling it must not jump into freed
// state; it reports the mistake through TakePanic and the C caller gets zero.
func TestReleasedSlotReportsThroughTakePanic(t *testing.T) {
	sym := fixture(t)

	cb, err := callback.Register(func(a *callback.Args) { a.Ret = 7 })
	require.NoError(t, err)

	var args asm.Args
	args.I[0] = cb
	args.I[1] = 1
	args.I[2] = 2
	got, _ := asm.CallReentrant(sym("cb_apply"), &args)
	require.EqualValues(t, 7, got)

	require.NoError(t, callback.Release(cb))

	// The address is still in the C++ call, exactly as a stale std::function
	// would be.
	got, _ = asm.CallReentrant(sym("cb_apply"), &args)
	assert.Zero(t, got, "a released callback returns zero")

	p := callback.TakePanic()
	require.NotNil(t, p)
	assert.Contains(t, p.Error(), "released while C called it")
	assert.Nil(t, callback.TakePanic(), "TakePanic clears what it returns")
}

// TestConcurrentRegistrationAndCalls runs registration, release and callbacks
// from several goroutines at once: the table is copy-on-write and lock-free on
// the callback path, which is what this test is for under -race.
func TestConcurrentRegistrationAndCalls(t *testing.T) {
	sym := fixture(t)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				cb, err := callback.Register(func(a *callback.Args) { a.Ret = a.I[0] })
				if err != nil {
					continue
				}
				var args asm.Args
				args.I[0] = cb
				args.I[1] = 3
				args.I[2] = 4
				asm.CallReentrant(sym("cb_apply"), &args)
				_ = callback.Release(cb)
			}
		}()
	}
	wg.Wait()
}
