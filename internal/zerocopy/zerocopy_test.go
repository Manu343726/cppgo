//go:build stackalloc

package zerocopy

import (
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
)

// benchSink keeps results observable so the compiler cannot delete the loops.
var benchSink any

// The payload is large enough that the copy has something to copy and small
// enough to stay in cache: 1 KiB of bytes and 256 ints.
var (
	benchBytes = make([]uint8, 1024)
	benchInts  = make([]int32, 256)
	benchText  = "a string the fixture only measures the length of"
)

func init() {
	for i := range benchBytes {
		benchBytes[i] = uint8(i)
	}
	for i := range benchInts {
		benchInts[i] = int32(i)
	}
}

// BenchmarkBytesBorrowed is []byte -> (const uint8_t*, size_t): the Go backing
// array is the callee's argument. No allocation anywhere.
func BenchmarkBytesBorrowed(b *testing.B) {
	fn := fixture(b)("zc_bytes_borrowed")
	data := cppgo.Ptr(benchBytes)
	n := len(benchBytes)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkBytesCopied is the same operation behind a
// const std::vector<uint8_t>& parameter: the shim constructs the vector, which
// is one C++ allocation and one copy. -benchmem cannot see the C++ allocation,
// so ns/op is the number that carries the difference.
func BenchmarkBytesCopied(b *testing.B) {
	fn := fixture(b)("zc_bytes_copied")
	data := cppgo.Ptr(benchBytes)
	n := len(benchBytes)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkIntsBorrowed is []int -> (const int*, size_t).
func BenchmarkIntsBorrowed(b *testing.B) {
	fn := fixture(b)("zc_ints_borrowed")
	data := cppgo.Ptr(benchInts)
	n := len(benchInts)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkIntsCopied is []int behind a const std::vector<int>& parameter.
func BenchmarkIntsCopied(b *testing.B) {
	fn := fixture(b)("zc_ints_copied")
	data := cppgo.Ptr(benchInts)
	n := len(benchInts)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkIntsSpan is []int -> std::span<const int>. The span is two
// registers, so the call is the borrowed one with a different callee; the
// difference against BenchmarkIntsBorrowed is what constructing the span inside
// the callee costs.
func BenchmarkIntsSpan(b *testing.B) {
	fn := fixture(b)("zc_ints_span")
	data := cppgo.Ptr(benchInts)
	n := len(benchInts)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkStringViewLength is string -> std::string_view, built in the callee
// from (data, length): the borrow.
func BenchmarkStringViewLength(b *testing.B) {
	fn := fixture(b)("zc_string_view_length")
	data := cppgo.StrPtr(benchText)
	n := len(benchText)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkStringCopyLength is the same length behind a std::string parameter:
// one C++ allocation and one copy.
func BenchmarkStringCopyLength(b *testing.B) {
	fn := fixture(b)("zc_string_length")
	data := cppgo.StrPtr(benchText)
	n := len(benchText)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// BenchmarkIntsSpanFromVector keeps the span and puts the copy in front of it,
// so the two halves of the cost are separable.
func BenchmarkIntsSpanFromVector(b *testing.B) {
	fn := fixture(b)("zc_ints_span_from_vector")
	data := cppgo.Ptr(benchInts)
	n := len(benchInts)

	var sink uintptr
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		sink += call(fn, data, n)
	}
	b.StopTimer()
	benchSink = sink
}

// spanCall invokes a two-argument function through the argument block, which is
// the ABI a std::span parameter is: pointer in RDI, length in RSI.
func spanCall(fn unsafe.Pointer, data unsafe.Pointer, n int) uintptr {
	var args asm.Args
	args.I[0] = uintptr(data)
	args.I[1] = uintptr(n)
	got, _ := asm.Call(fn, &args)
	return got
}

// TestSpanIsPointerAndLength proves the ABI claim: std::span<const int> arrives
// in the same two registers as (const int*, size_t), so the span form costs the
// caller nothing extra and needs no thunk.
func TestSpanIsPointerAndLength(t *testing.T) {
	sym := fixture(t)

	values := []int32{1, 2, 3, 4}
	data := cppgo.Ptr(values)

	borrowed := spanCall(sym("zc_ints_borrowed"), data, len(values))
	span := spanCall(sym("zc_ints_span"), data, len(values))

	if borrowed != 10 || span != 10 {
		t.Fatalf("want 10 from both forms, got borrowed=%d span=%d", borrowed, span)
	}
}

// TestSpanWritesThroughToTheGoSlice is the writable half: std::span<int> is a
// []int the callee may modify, and the modification is visible in Go, which is
// what "borrowed" means. Nothing is copied back.
func TestSpanWritesThroughToTheGoSlice(t *testing.T) {
	sym := fixture(t)

	values := newInts()
	var args asm.Args
	args.I[0] = uintptr(cppgo.Ptr(values))
	args.I[1] = uintptr(len(values))
	args.I[2] = 3
	asm.Call(sym("zc_ints_span_scale"), &args)

	want := []int32{3, 6, 9, 12}
	got := snapshot(values)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("value %d: want %d, got %d", i, want[i], got[i])
		}
	}
}

// newInts allocates the slice behind a call the compiler cannot see through.
// A composite literal whose only escape is a uintptr conversion is memory the
// compiler still believes is constant: measured, a slice literal declared in
// the test read back its original contents after the callee had written to it,
// while the same test passed when the package was compiled on its own. The
// allocation has to be opaque for the observation to be the callee's.
//
//go:noinline
func newInts() []int32 { return []int32{1, 2, 3, 4} }

// snapshot reads the slice in another function, so the read cannot be hoisted
// above the foreign call.
//
//go:noinline
func snapshot(s []int32) []int32 {
	out := make([]int32, len(s))
	copy(out, s)
	return out
}

// TestArrayLayoutIsTheGoArrayLayout answers the std::array<T,N> <-> [N]T
// question with the compiler: libstdc++'s std::array<int,4> is exactly a
// [4]int32 in size and alignment.
func TestArrayLayoutIsTheGoArrayLayout(t *testing.T) {
	sym := fixture(t)

	var four [4]int32
	size := call(sym("zc_sizeof_array4"), nil, 0)
	align := call(sym("zc_alignof_array4"), nil, 0)

	if size != unsafe.Sizeof(four) || align != unsafe.Alignof(four) {
		t.Fatalf("std::array<int,4> is %d/%d, [4]int32 is %d/%d",
			size, align, unsafe.Sizeof(four), unsafe.Alignof(four))
	}
}

// TestCopiedAndBorrowedAgree is the control: the copy path is semantically the
// borrow, so only the cost differs.
func TestCopiedAndBorrowedAgree(t *testing.T) {
	sym := fixture(t)

	bytes := []uint8{1, 2, 3, 250}
	ints := []int32{-1, 2, -3, 4}

	cases := []struct {
		name string
		a, b string
		data unsafe.Pointer
		n    int
	}{
		{name: "bytes", a: "zc_bytes_borrowed", b: "zc_bytes_copied", data: cppgo.Ptr(bytes), n: len(bytes)},
		{name: "ints", a: "zc_ints_borrowed", b: "zc_ints_copied", data: cppgo.Ptr(ints), n: len(ints)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			borrowed := call(sym(tc.a), tc.data, tc.n)
			copied := call(sym(tc.b), tc.data, tc.n)
			if borrowed != copied {
				t.Fatalf("%s: borrowed=%d copied=%d", tc.name, borrowed, copied)
			}
		})
	}
}
