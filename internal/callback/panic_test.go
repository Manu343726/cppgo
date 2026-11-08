package callback_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// errBoom is the error a panicking callback carries, so errors.As can find it
// through the *Panic wrapper.
var errBoom = errors.New("boom")

// TestCallbackPanicIsCaptured measures what happens when a callback panics: the
// panic never unwinds into C++ (the unwinder would find no Go frame to stop in
// and the process would terminate), the trampoline's dispatcher recovers it,
// the C caller receives a zero result, and the panic is handed back to the
// goroutine that owns the call.
func TestCallbackPanicIsCaptured(t *testing.T) {
	sym := fixture(t)

	cb, err := callback.Register(func(*callback.Args) {
		panic(errBoom)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	var args asm.Args
	args.I[0] = cb
	args.I[1] = 6
	args.I[2] = 7
	got, fgot := asm.CallReentrant(sym("cb_apply"), &args)

	assert.Zero(t, got, "the C caller sees the zero result, not a torn-down frame")
	assert.Zero(t, fgot)

	p := callback.TakePanic()
	require.NotNil(t, p, "the panic is captured, not lost")
	assert.ErrorIs(t, p, errBoom)
	assert.NotEmpty(t, p.Stack)
	assert.Nil(t, callback.TakePanic())
}

// TestCallbackPanicReachesTheCaller is the rule the contract states: the
// binding that passed a callback takes the panic when the foreign call returns
// and re-panics there, on a stack where a panic may propagate. This is that
// helper, and the test observes it from a recover.
func TestCallbackPanicReachesTheCaller(t *testing.T) {
	sym := fixture(t)

	cb, err := callback.Register(func(*callback.Args) { panic(errBoom) })
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	callWithCallback := func() {
		var args asm.Args
		args.I[0] = cb
		asm.CallReentrant(sym("cb_apply"), &args)
		// What a generated binding does after every foreign call that
		// passed a callback.
		if p := callback.TakePanic(); p != nil {
			panic(p)
		}
	}

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		callWithCallback()
	}()

	p, ok := recovered.(*callback.Panic)
	require.True(t, ok, "the panic is re-raised as *callback.Panic")
	assert.ErrorIs(t, p, errBoom)
}

// TestCallbackPanicDoesNotLeakBetweenCalls pins the clearing rule: a panic
// belongs to the call that raised it, and the next call must not see it.
func TestCallbackPanicDoesNotLeakBetweenCalls(t *testing.T) {
	sym := fixture(t)

	panicking, err := callback.Register(func(*callback.Args) { panic(errBoom) })
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(panicking) })

	fine, err := callback.Register(func(a *callback.Args) { a.Ret = 5 })
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(fine) })

	// A call with no callback at all must not pick up an earlier panic
	// either.
	var args asm.Args
	args.I[0] = panicking
	asm.CallReentrant(sym("cb_apply"), &args)
	require.NotNil(t, callback.TakePanic())

	args.I[0] = fine
	got, _ := asm.CallReentrant(sym("cb_apply"), &args)
	assert.EqualValues(t, 5, got)
	assert.Nil(t, callback.TakePanic())
}
