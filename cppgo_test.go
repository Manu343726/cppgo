package cppgo_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/testlib"
)

func TestOpenPathClose(t *testing.T) {
	path := testlib.Build(t, testlib.Full)

	lib, err := cppgo.Open(path)
	require.NoError(t, err)
	require.NotNil(t, lib)

	assert.Equal(t, path, lib.Path())
	require.NoError(t, lib.Close())

	// Closing twice is an error, not a silent success.
	err = lib.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, cppgo.ErrClosed)
}

func TestOpenNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libcppgo-does-not-exist.so")

	lib, err := cppgo.Open(path)
	require.Error(t, err)
	assert.Nil(t, lib)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, path, openErr.Path)
	assert.Contains(t, err.Error(), path)
	assert.Contains(t, err.Error(), "cppgo: open")

	// The loader's own message survives, so a user can tell "no such file"
	// from "wrong architecture".
	assert.NotEmpty(t, openErr.Err.Error())
	assert.NotNil(t, errors.Unwrap(err))
}

func TestOpenEmptyPath(t *testing.T) {
	lib, err := cppgo.Open("")
	require.Error(t, err)
	assert.Nil(t, lib)
	assert.Contains(t, err.Error(), "empty library path")

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Empty(t, openErr.Path)
}

func TestOpenNotSharedObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-library.so")
	require.NoError(t, os.WriteFile(path, []byte("this is not an ELF file\n"), 0o644))

	lib, err := cppgo.Open(path)
	require.Error(t, err)
	assert.Nil(t, lib)

	var openErr *cppgo.OpenError
	require.ErrorAs(t, err, &openErr)
	assert.Equal(t, path, openErr.Path)
}

func TestSymbol(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	addr, err := lib.Symbol("cppgo_fixture_add")
	require.NoError(t, err)
	assert.NotZero(t, addr)

	// Two lookups of the same symbol must agree.
	again, err := lib.Symbol("cppgo_fixture_add")
	require.NoError(t, err)
	assert.Equal(t, addr, again)

	// A data symbol resolves too: symbols are not necessarily code.
	dataAddr, err := lib.Symbol("cppgo_fixture_exported_data")
	require.NoError(t, err)
	assert.NotZero(t, dataAddr)
	assert.NotEqual(t, addr, dataAddr)
}

func TestSymbolMissing(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	addr, err := lib.Symbol("cppgo_fixture_no_such_symbol")
	require.Error(t, err)
	assert.Zero(t, addr)

	var symErr *cppgo.SymbolError
	require.ErrorAs(t, err, &symErr)
	assert.Equal(t, "cppgo_fixture_no_such_symbol", symErr.Name)
	assert.Contains(t, err.Error(), "cppgo_fixture_no_such_symbol")
	assert.Contains(t, err.Error(), "cppgo: symbol")
	require.NotNil(t, symErr.Err)
}

func TestSymbolEmptyName(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	addr, err := lib.Symbol("")
	require.Error(t, err)
	assert.Zero(t, addr)
	assert.Contains(t, err.Error(), "empty symbol name")
}

func TestSymbolAfterClose(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	require.NoError(t, lib.Close())

	addr, err := lib.Symbol("cppgo_fixture_add")
	require.Error(t, err)
	assert.Zero(t, addr)
	assert.ErrorIs(t, err, cppgo.ErrClosed)

	var symErr *cppgo.SymbolError
	require.ErrorAs(t, err, &symErr)
	assert.Equal(t, "cppgo_fixture_add", symErr.Name)

	// Path stays readable after Close.
	assert.NotEmpty(t, lib.Path())
}

func TestMustSymbol(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	addr := lib.MustSymbol("cppgo_fixture_add")
	require.NotZero(t, addr)

	symAddr, err := lib.Symbol("cppgo_fixture_add")
	require.NoError(t, err)
	assert.Equal(t, symAddr, addr)
}

func TestMustSymbolMissingPanics(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	value := panicValue(t, func() { lib.MustSymbol("cppgo_fixture_no_such_symbol") })
	require.NotNil(t, value, "MustSymbol must panic for a missing symbol")

	err, ok := value.(error)
	require.True(t, ok, "panic value is %T, want error", value)

	var symErr *cppgo.SymbolError
	require.ErrorAs(t, err, &symErr)
	assert.Equal(t, "cppgo_fixture_no_such_symbol", symErr.Name)
}

func TestMustSymbolAfterClosePanics(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	require.NoError(t, lib.Close())

	value := panicValue(t, func() { lib.MustSymbol("cppgo_fixture_add") })
	require.NotNil(t, value)

	err, ok := value.(error)
	require.True(t, ok, "panic value is %T, want error", value)
	assert.ErrorIs(t, err, cppgo.ErrClosed)
}

func TestLastError(t *testing.T) {
	// The message lives in thread-local storage on the fixture's side.
	lockThread(t)

	lib := openFixture(t, testlib.Full)

	setError := fn(t, lib, "cppgo_fixture_set_error")
	clearError := fn(t, lib, "cppgo_fixture_clear_error")

	t.Run("no error recorded", func(t *testing.T) {
		callInt(t, clearError, [6]uintptr{})
		assert.NoError(t, cppgo.LastError(lib))
	})

	t.Run("message recorded", func(t *testing.T) {
		msg := "boom: the widget exploded"
		buf := append([]byte(msg), 0)
		callInt(t, setError, [6]uintptr{uintptr(cppgo.Ptr(buf))})

		err := cppgo.LastError(lib)
		require.Error(t, err)
		assert.Equal(t, msg, err.Error())

		var nativeErr *cppgo.NativeError
		require.ErrorAs(t, err, &nativeErr)
		assert.Equal(t, msg, nativeErr.Message)

		cppgo.KeepAlive(buf)
	})

	t.Run("cleared again", func(t *testing.T) {
		callInt(t, clearError, [6]uintptr{})
		assert.NoError(t, cppgo.LastError(lib))
	})

	t.Run("long message is copied whole", func(t *testing.T) {
		long := strings.Repeat("x", 200)
		buf := append([]byte(long), 0)
		callInt(t, setError, [6]uintptr{uintptr(cppgo.Ptr(buf))})

		err := cppgo.LastError(lib)
		require.Error(t, err)
		assert.Equal(t, long, err.Error())

		cppgo.KeepAlive(buf)
	})

	t.Run("nil library", func(t *testing.T) {
		assert.NoError(t, cppgo.LastError(nil))
	})

	t.Run("closed library", func(t *testing.T) {
		closed, err := cppgo.Open(testlib.Build(t, testlib.Full))
		require.NoError(t, err)
		require.NoError(t, closed.Close())

		assert.NoError(t, cppgo.LastError(closed))
	})
}

func TestLastErrorAbsentSymbol(t *testing.T) {
	lib := openFixture(t, testlib.NoLastError)

	// The fixture is healthy, it just has no cppgo_last_error: a library
	// that cannot fail needs none.
	require.EqualValues(t, 5, callInt(t, fn(t, lib, "cppgo_fixture_add"), [6]uintptr{2, 3}))

	assert.NoError(t, cppgo.LastError(lib))
}

func TestErrorConvention(t *testing.T) {
	lockThread(t)

	lib := openFixture(t, testlib.Full)

	fail := fn(t, lib, "cppgo_fixture_fail")
	msg := "convention: failure reported in-band"
	buf := append([]byte(msg), 0)

	code := callInt(t, fail, [6]uintptr{uintptr(cppgo.Ptr(buf))})
	require.NotZero(t, code, "the fixture must report failure with a non-zero int32")

	err := cppgo.LastError(lib)
	require.Error(t, err)
	assert.Equal(t, msg, err.Error())

	cppgo.KeepAlive(buf)
}

func TestCallIntegerArguments(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	t.Run("two arguments", func(t *testing.T) {
		add := fn(t, lib, "cppgo_fixture_add")
		assert.EqualValues(t, 5, callInt(t, add, [6]uintptr{2, 3}))

		// Negative integers survive the trip: the register is just bits.
		negOne := int64(-1)
		assert.EqualValues(t, 2, callInt(t, add, [6]uintptr{3, uintptr(negOne)}))
	})

	t.Run("six arguments", func(t *testing.T) {
		add6 := fn(t, lib, "cppgo_fixture_add6")

		// a + 2b + 3c + 4d + 5e + 6f with distinct weights: a swapped or
		// dropped register changes the result.
		got := callInt(t, add6, [6]uintptr{1, 2, 3, 4, 5, 6})
		assert.EqualValues(t, 1+4+9+16+25+36, got)

		got = callInt(t, add6, [6]uintptr{1, 0, 0, 0, 0, 0})
		assert.EqualValues(t, 1, got)

		got = callInt(t, add6, [6]uintptr{0, 0, 0, 0, 0, 1})
		assert.EqualValues(t, 6, got)
	})

	t.Run("pointer argument", func(t *testing.T) {
		identity := fn(t, lib, "cppgo_fixture_identity_ptr")

		buf := []float64{1, 2, 3, 4}
		want := uintptr(cppgo.Ptr(buf))

		got := callInt(t, identity, [6]uintptr{want})
		assert.Equal(t, int64(want), got)

		cppgo.KeepAlive(buf)
	})
}

func TestCallFloatArguments(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	t.Run("eight arguments", func(t *testing.T) {
		weighted := fn(t, lib, "cppgo_fixture_weighted8")

		// a + 2b + ... + 8h with a=b=...=h=1 is 1+2+...+8.
		got := callFloat(t, weighted, [6]uintptr{}, [8]float64{1, 1, 1, 1, 1, 1, 1, 1})
		assert.EqualValues(t, 36, got)

		// Every lane contributes its own weight.
		for i := range 8 {
			var args [8]float64
			args[i] = 1
			got = callFloat(t, weighted, [6]uintptr{}, args)
			assert.EqualValues(t, i+1, got, "argument %d", i)
		}
	})

	t.Run("mixed integer and float", func(t *testing.T) {
		mix := fn(t, lib, "cppgo_fixture_mix")

		// a + 10b + 100x + 1000y
		got := callFloat(t, mix, [6]uintptr{1, 2}, [8]float64{3, 4})
		assert.EqualValues(t, 1+20+300+4000, got)
	})

	t.Run("slice arguments", func(t *testing.T) {
		dot := fn(t, lib, "cppgo_fixture_dot")

		a := []float64{1, 2, 3, 4}
		b := []float64{5, 6, 7, 8}

		args := [6]uintptr{uintptr(cppgo.Ptr(a)), uintptr(cppgo.Ptr(b)), uintptr(len(a))}
		assert.EqualValues(t, 5+12+21+32, callFloat(t, dot, args, [8]float64{}))

		cppgo.KeepAlive(a, b)

		// An empty slice is passed as nil, length 0.
		empty := [6]uintptr{uintptr(cppgo.Ptr([]float64(nil))), uintptr(cppgo.Ptr([]float64(nil))), 0}
		assert.EqualValues(t, 0, callFloat(t, dot, empty, [8]float64{}))
	})

	t.Run("C string argument", func(t *testing.T) {
		strlen := fn(t, lib, "cppgo_fixture_strlen")

		buf := []byte("hello\x00")
		got := callInt(t, strlen, [6]uintptr{uintptr(cppgo.Ptr(buf))})
		assert.EqualValues(t, 5, got)
		cppgo.KeepAlive(buf)

		// NULL is reported by the fixture, not dereferenced.
		got = callInt(t, strlen, [6]uintptr{0})
		assert.EqualValues(t, -1, got)
	})
}

func TestCallNilArgsPanics(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	add := fn(t, lib, "cppgo_fixture_add")

	value := panicValue(t, func() { asm.Call(add, nil) })
	require.NotNil(t, value)
	assert.Equal(t, "cppgo: asm.Call: nil Args", value)
}

// TestConcurrentCalls hammers the trampoline from many goroutines while
// another goroutine drags the garbage collector through cycle after cycle:
// the foreign call runs on the Go stack and the arguments handed to it are Go
// memory, so both have to survive a moving, collecting heap.
func TestConcurrentCalls(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	add := fn(t, lib, "cppgo_fixture_add")
	add6 := fn(t, lib, "cppgo_fixture_add6")
	dot := fn(t, lib, "cppgo_fixture_dot")

	const (
		goroutines = 32
		iterations = 500
	)

	a := []float64{1, 2, 3, 4}
	b := []float64{2, 3, 4, 5}
	const wantDot = 2.0 + 6.0 + 12.0 + 20.0

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

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errs := make(chan error, goroutines)

	for g := range goroutines {
		go func() {
			defer wg.Done()

			for i := range iterations {
				cppgo.ProbeStack()

				raw, _ := asm.Call(add, &asm.Args{I: [6]uintptr{uintptr(g), uintptr(i)}})
				if got, want := int64(raw), int64(g+i); got != want {
					errs <- fmt.Errorf("add(%d, %d) = %d, want %d", g, i, got, want)
					return
				}

				raw, _ = asm.Call(add6, &asm.Args{I: [6]uintptr{1, 2, 3, 4, 5, 6}})
				if got, want := int64(raw), int64(91); got != want {
					errs <- fmt.Errorf("add6 = %d, want %d", got, want)
					return
				}

				args := &asm.Args{
					I: [6]uintptr{uintptr(cppgo.Ptr(a)), uintptr(cppgo.Ptr(b)), uintptr(len(a))},
				}
				_, sum := asm.Call(dot, args)
				cppgo.KeepAlive(a, b)
				if sum != wantDot {
					errs <- fmt.Errorf("dot = %v, want %v", sum, wantDot)
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

// TestConcurrentCallsSingleP runs the same hammering with GOMAXPROCS=1, where
// a preempted goroutine resumes on the only M available.
func TestConcurrentCallsSingleP(t *testing.T) {
	lib := openFixture(t, testlib.Full)
	add := fn(t, lib, "cppgo_fixture_add")

	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)

	var wg sync.WaitGroup
	wg.Add(32)

	errs := make(chan error, 32)

	for g := range 32 {
		go func() {
			defer wg.Done()
			for i := range 200 {
				cppgo.ProbeStack()
				raw, _ := asm.Call(add, &asm.Args{I: [6]uintptr{uintptr(g), uintptr(i)}})
				if got, want := int64(raw), int64(g+i); got != want {
					errs <- fmt.Errorf("add(%d, %d) = %d, want %d", g, i, got, want)
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

// TestConcurrentSymbolResolution covers the Library's read path under the race
// detector from several goroutines at once.
func TestConcurrentSymbolResolution(t *testing.T) {
	lib := openFixture(t, testlib.Full)

	var wg sync.WaitGroup
	wg.Add(8)

	errs := make(chan error, 8)

	for range 8 {
		go func() {
			defer wg.Done()
			for range 50 {
				addr, err := lib.Symbol("cppgo_fixture_add")
				if err != nil {
					errs <- err
					return
				}
				if addr == 0 {
					errs <- errors.New("Symbol returned a zero address")
					return
				}
				if got, want := lib.Path(), lib.Path(); got != want {
					errs <- fmt.Errorf("Path = %q, want %q", got, want)
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

// TestSymbolRacingClose closes the library while symbols are being resolved:
// after Close, every resolution must fail with ErrClosed rather than touch a
// freed handle.
func TestSymbolRacingClose(t *testing.T) {
	lib, err := cppgo.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(4)

	errs := make(chan error, 4)

	for range 4 {
		go func() {
			defer wg.Done()
			for range 100 {
				addr, err := lib.Symbol("cppgo_fixture_add")
				if err != nil {
					if !errors.Is(err, cppgo.ErrClosed) {
						errs <- fmt.Errorf("Symbol after Close: %w", err)
					}
					return
				}
				if addr == 0 {
					errs <- errors.New("Symbol returned a zero address")
					return
				}
			}
		}()
	}

	require.NoError(t, lib.Close())
	wg.Wait()
	close(errs)

	for err := range errs {
		require.NoError(t, err)
	}

	_, err = lib.Symbol("cppgo_fixture_add")
	require.ErrorIs(t, err, cppgo.ErrClosed)
}
