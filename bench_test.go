package cppgo_test

import (
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// benchSink keeps benchmark results observable so the compiler cannot delete
// the loops under test.
var benchSink any

// goAdd is the Go function the "baseline" benchmark calls. It is a package
// level variable so the call is indirect, exactly like a Go closure or an
// interface method would be, and so the compiler cannot inline it into the
// loop.
var goAdd = func(a, b int64) int64 { return a + b }

// BenchmarkGoCall is the floor: an indirect Go call with two arguments and an
// integer result.
func BenchmarkGoCall(b *testing.B) {
	var sink int64

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sink += goAdd(int64(i), 1)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkTrampoline measures the foreign call itself: load the argument
// block, enter the C ABI, copy the result out. This is the "cppgo trampoline,
// direct" number.
func BenchmarkTrampoline(b *testing.B) {
	add := benchAdd(b)

	args := &asm.Args{I: [6]uintptr{2, 3}}

	var sink uintptr

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		raw, _ := asm.Call(add, args)
		sink += raw
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkTrampolineProbeStack adds the stack probe a generated wrapper puts
// in front of every call. This is the "cppgo trampoline, stack switch" number:
// the difference between it and BenchmarkTrampoline is what the headroom
// guarantee costs.
func BenchmarkTrampolineProbeStack(b *testing.B) {
	add := benchAdd(b)

	args := &asm.Args{I: [6]uintptr{2, 3}}

	var sink uintptr

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		cppgo.ProbeStack()
		raw, _ := asm.Call(add, args)
		sink += raw
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkProbeStack measures the headroom guarantee on its own. After the
// first call on a goroutine this is a frame adjustment, so it has to be the
// same order as the call it protects.
func BenchmarkProbeStack(b *testing.B) {
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		cppgo.ProbeStack()
	}
}

// BenchmarkSymbolResolution measures what generated code pays at init time per
// bound function, not per call.
func BenchmarkSymbolResolution(b *testing.B) {
	lib := benchLibrary(b)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		addr, err := lib.Symbol("cppgo_fixture_add")
		if err != nil {
			b.Fatal(err)
		}
		benchSink = addr
	}
}

// benchLibrary loads the fixture for a benchmark and closes it afterwards.
func benchLibrary(b *testing.B) *cppgo.Library {
	b.Helper()

	lib, err := cppgo.Open(testlib.Build(b, testlib.Full))
	if err != nil {
		b.Fatalf("open fixture: %v", err)
	}
	b.Cleanup(func() { _ = lib.Close() })

	return lib
}

// benchAdd resolves the fixture's two-argument add function.
func benchAdd(b *testing.B) unsafe.Pointer {
	b.Helper()

	addr, err := benchLibrary(b).Symbol("cppgo_fixture_add")
	if err != nil {
		b.Fatalf("resolve cppgo_fixture_add: %v", err)
	}

	return rtl.Pointer(addr)
}
