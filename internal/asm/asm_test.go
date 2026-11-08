package asm_test

import (
	"os"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// TestMain removes the C++ fixture testlib compiles on demand.
func TestMain(m *testing.M) {
	code := m.Run()
	testlib.Cleanup()
	os.Exit(code)
}

// fixture opens the C++ fixture and returns a resolver for its symbols.
func fixture(t *testing.T) func(string) unsafe.Pointer {
	t.Helper()

	lib, err := rtl.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	t.Cleanup(func() { _ = lib.Close() })

	return func(name string) unsafe.Pointer {
		t.Helper()

		addr, err := lib.Symbol(name)
		require.NoError(t, err, "symbol %s", name)

		return rtl.Pointer(addr)
	}
}

// TestArgsLayout pins the argument block's memory layout: the assembly reads
// I at offsets 0..40, F at 48..104, and writes the two result slots at
// 112..120. Changing Args without changing the trampolines would otherwise
// produce calls with garbage arguments.
func TestArgsLayout(t *testing.T) {
	var args asm.Args

	// The two register banks, back to back, no padding.
	assert.Equal(t, uintptr(48), unsafe.Sizeof(args.I))
	assert.Equal(t, uintptr(64), unsafe.Sizeof(args.F))
	assert.Equal(t, uintptr(128), unsafe.Sizeof(args))
	assert.Equal(t, uintptr(8), unsafe.Alignof(args))

	// The assembly addresses F as a fixed offset from I, and the results
	// follow F.
	assert.Equal(t, uintptr(0), unsafe.Offsetof(args.I))
	assert.Equal(t, uintptr(48), unsafe.Offsetof(args.F))
	assert.Equal(t, unsafe.Offsetof(args.I)+unsafe.Sizeof(args.I), unsafe.Offsetof(args.F))
	assert.Equal(t, uintptr(112), unsafe.Offsetof(args.Ret))
	assert.Equal(t, uintptr(120), unsafe.Offsetof(args.RetF))

	var zero asm.Args
	assert.Equal(t, zero, asm.Args{})
}

func TestCallIntegerRegisters(t *testing.T) {
	symbol := fixture(t)
	add6 := symbol("cppgo_fixture_add6")

	// a + 2b + 3c + 4d + 5e + 6f: every weight is distinct, so a call that
	// puts an argument in the wrong register cannot produce the right answer.
	got, _ := asm.Call(add6, &asm.Args{I: [6]uintptr{1, 2, 3, 4, 5, 6}})
	assert.EqualValues(t, 91, int64(got))

	// Each register on its own.
	for i := range 6 {
		var args asm.Args
		args.I[i] = 1

		got, _ = asm.Call(add6, &args)
		assert.EqualValues(t, (i+1)*1, int64(got), "argument register %d", i)
	}
}

func TestCallFloatRegisters(t *testing.T) {
	symbol := fixture(t)
	weighted := symbol("cppgo_fixture_weighted8")

	// a + 2b + ... + 8h.
	var all asm.Args
	for i := range 8 {
		all.F[i] = 1
	}

	_, got := asm.Call(weighted, &all)
	assert.EqualValues(t, 36, got)

	// Each lane on its own.
	for i := range 8 {
		var args asm.Args
		args.F[i] = 1

		_, got = asm.Call(weighted, &args)
		assert.EqualValues(t, i+1, got, "float argument register %d", i)
	}
}

func TestCallMixedBanks(t *testing.T) {
	symbol := fixture(t)
	mix := symbol("cppgo_fixture_mix")

	// a + 10b + 100x + 1000y with the integer arguments in I[0], I[1] and the
	// floats in F[0], F[1]: the two banks are independent.
	args := &asm.Args{I: [6]uintptr{1, 2}, F: [8]float64{3, 4}}
	_, got := asm.Call(mix, args)
	assert.EqualValues(t, 1+20+300+4000, got)

	// The same call with the float bank shifted must change only the float
	// part.
	args = &asm.Args{I: [6]uintptr{1, 2}, F: [8]float64{0, 4}}
	_, got = asm.Call(mix, args)
	assert.EqualValues(t, 1+20+4000, got)
}

func TestCallPointerArguments(t *testing.T) {
	symbol := fixture(t)
	identity := symbol("cppgo_fixture_identity_ptr")

	buf := []float64{1, 2, 3}
	want := uintptr(unsafe.Pointer(&buf[0]))

	got, _ := asm.Call(identity, &asm.Args{I: [6]uintptr{want}})
	require.Equal(t, want, got)

	// A NULL pointer round-trips as zero.
	got, _ = asm.Call(identity, &asm.Args{})
	assert.Zero(t, got)
}

func TestCallReturnsBothBanks(t *testing.T) {
	symbol := fixture(t)

	// A function with a double result leaves a stale RAX; the float result
	// must still arrive in X0.
	_, f := asm.Call(symbol("cppgo_fixture_weighted8"), &asm.Args{F: [8]float64{0, 0, 0, 0, 0, 0, 0, 1}})
	assert.EqualValues(t, 8, f)

	// And an integer result must arrive in RAX regardless of what XMM0 holds.
	got, _ := asm.Call(symbol("cppgo_fixture_add"), &asm.Args{I: [6]uintptr{40, 2}})
	assert.EqualValues(t, 42, int64(got))
}

func TestCallNilArgsPanics(t *testing.T) {
	symbol := fixture(t)
	add := symbol("cppgo_fixture_add")

	assert.PanicsWithValue(t, "cppgo: asm.Call: nil Args", func() {
		_, _ = asm.Call(add, nil)
	})
}

// TestCallDoesNotDisturbTheCaller checks that the trampoline leaves the Go
// frame intact: locals live across the call keep their values, which they
// would not if the stack switch were wrong.
func TestCallDoesNotDisturbTheCaller(t *testing.T) {
	symbol := fixture(t)
	add := symbol("cppgo_fixture_add")

	local := int64(7)
	buf := make([]byte, 1024)
	for i := range buf {
		buf[i] = byte(i)
	}

	var sum int64
	for i := range 1000 {
		local += int64(i)

		got, _ := asm.Call(add, &asm.Args{I: [6]uintptr{uintptr(i), 1}})
		sum += int64(got)
	}

	assert.EqualValues(t, 7+499500, local)
	assert.EqualValues(t, 500500, sum) // sum over i in [0,1000) of (i+1)
	for i := range buf {
		if buf[i] != byte(i) {
			require.Failf(t, "buffer corrupted", "buf[%d] = %d, want %d", i, buf[i], byte(i))
		}
	}
}

// TestCallManyTimes is a light soak: the trampoline is used a lot in a row, so
// a mistake in the stack pointer restore (a leak of a few bytes per call)
// would overflow the goroutine stack instead of passing.
func TestCallManyTimes(t *testing.T) {
	symbol := fixture(t)
	add := symbol("cppgo_fixture_add")

	var last uintptr

	for i := range 1_000_000 {
		last, _ = asm.Call(add, &asm.Args{I: [6]uintptr{uintptr(i), 0}})
	}

	assert.EqualValues(t, 999_999, last)
}

// TestProbeGrowsStack is the assembly package's own check of the probe: a
// goroutine that has never called into the fixture gets a 28 KiB foreign frame
// below its stack pointer, which only fits because Probe entered a function
// with a larger frame first.
func TestProbeGrowsStack(t *testing.T) {
	symbol := fixture(t)
	deep := symbol("cppgo_fixture_deep_stack")

	results := make(chan uintptr, 1)

	go func() {
		asm.Probe()
		raw, _ := asm.Call(deep, &asm.Args{I: [6]uintptr{28 << 10}})
		results <- raw
	}()

	require.NotZero(t, <-results)
}

// TestProbeRepeated checks that probing is idempotent: after the first call the
// stack is already large enough and further probes must not disturb it.
func TestProbeRepeated(t *testing.T) {
	symbol := fixture(t)
	add := symbol("cppgo_fixture_add")

	for range 100_000 {
		asm.Probe()
	}

	got, _ := asm.Call(add, &asm.Args{I: [6]uintptr{20, 22}})
	assert.EqualValues(t, 42, int64(got))
}
