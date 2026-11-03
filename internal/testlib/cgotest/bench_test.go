//go:build cgo && linux && amd64

package cgotest

import (
	"os"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
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

// benchSink keeps benchmark results observable.
var benchSink any

// goAdd is called indirectly so the compiler cannot inline the baseline away.
var goAdd = func(a, b int64) int64 { return a + b }

// BenchmarkGoCall is the floor: an indirect Go call.
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

// BenchmarkCgo is the cgo call path for the same operation.
func BenchmarkCgo(b *testing.B) {
	var sink int64

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sink += CgoAdd(int64(i), 1)
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkCgoSixArgs shows how the cgo call path behaves with a full argument
// list.
func BenchmarkCgoSixArgs(b *testing.B) {
	var sink int64

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sink += CgoAdd6(1, 2, 3, 4, 5, int64(i))
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkTrampoline is the cppgo call path for the same operation.
func BenchmarkTrampoline(b *testing.B) {
	_, add := fixture(b, "cppgo_fixture_add")

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

// BenchmarkTrampolineSixArgs is the cppgo call path with a full argument list.
func BenchmarkTrampolineSixArgs(b *testing.B) {
	_, add6 := fixture(b, "cppgo_fixture_add6")

	args := &asm.Args{I: [6]uintptr{1, 2, 3, 4, 5, 0}}

	var sink uintptr

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		args.I[5] = uintptr(i)
		raw, _ := asm.Call(add6, args)
		sink += raw
	}

	b.StopTimer()
	benchSink = sink
}

// BenchmarkTrampolineProbeStack adds the stack probe a generated wrapper puts
// before every call.
func BenchmarkTrampolineProbeStack(b *testing.B) {
	_, add := fixture(b, "cppgo_fixture_add")

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

// fixture loads the C++ fixture and resolves name.
func fixture(b *testing.B, name string) (*cppgo.Library, unsafe.Pointer) {
	b.Helper()

	lib, err := cppgo.Open(testlib.Build(b, testlib.Full))
	if err != nil {
		b.Fatalf("open fixture: %v", err)
	}
	b.Cleanup(func() { _ = lib.Close() })

	addr, err := lib.Symbol(name)
	if err != nil {
		b.Fatalf("resolve %s: %v", name, err)
	}

	return lib, rtl.Pointer(addr)
}
