// Package benchgo is the plain-Go arm: the reference implementations in
// internal/benchdata, benchmarked with no foreign call anywhere.
//
// Two arms exist for the crossing floor. go_direct is an ordinary call to a
// //go:noinline function — a real CALL instruction, not inlined work. go_indirect
// goes through a package-level func value, which is what a closure, an
// interface method or a callback table costs in Go before any boundary is
// involved.
package benchgo

import (
	"strconv"
	"testing"

	"github.com/Manu343726/cppgo/benchmarks/internal/benchdata"
)

// sink* keep every result observable, so the compiler cannot delete the call
// being measured.
var (
	sinkInt64   int64
	sinkInt32   int32
	sinkFloats  []float64
	sinkString  string
	sinkPoint   benchdata.Point
	sinkSample  benchdata.Sample
	sinkErr     error
	sinkCounter *benchdata.Counter
)

// The fixtures are package-level and read-only: a benchmark that built them
// inside its loop would measure the builder.
var (
	addA, addB       = int64(1), int64(2)
	ratioA           = int32(12)
	ratioZeroDivisor = int32(0)
	statusText       = "bench"
	pointA, pointB   = benchdata.Points()
	sampleA, sampleB = benchdata.Samples()
	existingCounter  = benchdata.NewCounter(7)
	callback         = benchdata.Add32Fn
	describePrefix   = benchdata.Prefix
	describeValue    = benchdata.Value
	blendT           = 0.25
)

// BenchmarkCall is the crossing floor: two integers in, one out. The two arms
// are the same Go function, called directly and through a func value.
func BenchmarkCall(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64 = benchdata.Add(addA, addB)
		}
	})
	b.Run("go_indirect", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64 = benchdata.AddFn(addA, addB)
		}
	})
}

// BenchmarkMidpoint is the 16-byte POD by value: the Go struct is copied the
// same way the C ABI copies it.
func BenchmarkMidpoint(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkPoint = benchdata.Midpoint(pointA, pointB)
		}
	})
}

// BenchmarkBlend is the 24-byte POD: a result larger than any register pair.
func BenchmarkBlend(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkSample = benchdata.Blend(sampleA, sampleB, blendT)
		}
	})
}

// BenchmarkDescribe is the string in and out: one concatenation, so the cost
// is the string C++ builds versus the one Go builds.
func BenchmarkDescribe(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkString = benchdata.Describe(describePrefix, describeValue)
		}
	})
}

// BenchmarkDoubled is the vector in and out at the two sizes the fixtures
// define.
func BenchmarkDoubled(b *testing.B) {
	for _, size := range []int{benchdata.SmallN, benchdata.LargeN} {
		values := benchdata.Floats(size)
		b.Run("n="+strconv.Itoa(size), func(b *testing.B) {
			b.Run("go_direct", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					sinkFloats = benchdata.Doubled(values)
				}
			})
		})
	}
}

// BenchmarkThrow is the failure path: the call cannot succeed, and the arm
// measures the whole Go-visible operation, the error value included.
func BenchmarkThrow(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt32, sinkErr = benchdata.Ratio(ratioA, ratioZeroDivisor)
		}
	})
}

// BenchmarkStatusOK is the success path of the status convention.
func BenchmarkStatusOK(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkErr = benchdata.Parse(statusText)
		}
	})
}

// BenchmarkStatusFailure is the convention's failure path.
func BenchmarkStatusFailure(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkErr = benchdata.Parse("!" + statusText)
		}
	})
}

// BenchmarkReentrant is the callback round trip in Go: a func value called
// once. There is no boundary here, which is what makes it the floor for the
// two FFI arms.
func BenchmarkReentrant(b *testing.B) {
	b.Run("go_indirect", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt32 = benchdata.Fold(callback, 3, 4)
		}
	})
}

// BenchmarkObjectNewClose is the object lifecycle: one allocation, then the
// destructor.
func BenchmarkObjectNewClose(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			counter := benchdata.NewCounter(7)
			counter.Bump(1)
			sinkCounter = counter
			counter.Close()
		}
	})
}

// BenchmarkObjectGet is the per-call cost of a method on a live object: the
// direct call with a receiver.
func BenchmarkObjectGet(b *testing.B) {
	b.Run("go_direct", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64 = existingCounter.Get()
		}
	})
}

// TestDescribeFormat pins the one fixture whose Go and C++ spellings could
// drift: std::to_string(double) prints six decimals, and so must this.
func TestDescribeFormat(t *testing.T) {
	const want = "bench: 1.500000" // std::to_string(1.5) in libstdc++
	if got := benchdata.Describe(benchdata.Prefix, benchdata.Value); got != want {
		t.Fatalf("Describe(%q, %v) = %q, want %q", benchdata.Prefix, benchdata.Value, got, want)
	}
}
