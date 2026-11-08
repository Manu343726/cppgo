package cppgo_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// applySymbol resolves the fixture's function-pointer entry point, the callee a
// reentrant call needs.
func applySymbol(t *testing.T) *cppgo.Library {
	t.Helper()
	lib, err := cppgo.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lib.Close() })
	return lib
}

// TestCallReentrantInvokesACallback measures the runtime entry the generated
// reentrant carrier uses: a Go function reaches C++ as a function pointer, the
// C++ side calls it, and the result travels back through the argument frame.
func TestCallReentrantInvokesACallback(t *testing.T) {
	lib := applySymbol(t)
	addr, err := lib.Symbol("cppgo_fixture_apply")
	require.NoError(t, err)

	cb, err := cppgo.RegisterCallback(func(a *cppgo.Args) {
		a.Ret = a.I[0] + a.I[1]
	})
	require.NoError(t, err)
	require.NotZero(t, cb)
	t.Cleanup(func() { _ = cppgo.ReleaseCallback(cb) })

	var args cppgo.Args
	args.I[0] = cb
	args.I[1] = 40
	args.I[2] = 2
	cppgo.CallReentrant(addr, &args)

	assert.EqualValues(t, 42, args.Ret)
	assert.Nil(t, cppgo.TakeCallbackPanic())
}

// TestReleaseCallback checks the table's accounting through the exported
// wrappers: a second release of the same address is not a live registration,
// and a callback that was never registered is not either.
func TestReleaseCallback(t *testing.T) {
	cb, err := cppgo.RegisterCallback(func(*cppgo.Args) {})
	require.NoError(t, err)

	require.NoError(t, cppgo.ReleaseCallback(cb))
	assert.Error(t, cppgo.ReleaseCallback(cb), "a released address is not a live registration")
	assert.Error(t, cppgo.ReleaseCallback(cb+1), "an address that was never registered is not one either")

	_, err = cppgo.RegisterCallback(nil)
	assert.Error(t, err, "a nil function is not registrable")
}

// TestCallbackPanicIsHandedBack measures the panic path through the exported
// wrapper: the dispatcher captures the panic, zeroes the result, and
// TakeCallbackPanic hands it to the goroutine that owns the call.
func TestCallbackPanicIsHandedBack(t *testing.T) {
	lib := applySymbol(t)
	addr, err := lib.Symbol("cppgo_fixture_apply")
	require.NoError(t, err)

	cb, err := cppgo.RegisterCallback(func(*cppgo.Args) {
		panic("callback exploded")
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cppgo.ReleaseCallback(cb) })

	var args cppgo.Args
	args.I[0] = cb
	cppgo.CallReentrant(addr, &args)

	assert.Zero(t, args.Ret, "a panicking callback returns a zero result")
	captured := cppgo.TakeCallbackPanic()
	require.NotNil(t, captured)
	assert.Equal(t, "callback exploded", captured.Value)
	assert.NotEmpty(t, captured.Stack)
	assert.Contains(t, captured.Error(), "callback exploded")
	assert.Nil(t, cppgo.TakeCallbackPanic(), "the panic is handed back once")
}
