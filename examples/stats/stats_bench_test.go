package stats

import (
	"strconv"
	"testing"
)

// The benchmark results are assigned to these so the compiler cannot prove them
// dead and drop the call being measured.
var (
	sinkFloat float64
	sinkInt   int
	sinkPtr   *float64
)

// benchData builds the input every benchmark shares. A power of two keeps the
// byte count round, so -benchmem and SetBytes report honest throughput.
func benchData(n int) []float64 {
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i%1000) / 1000.0
	}
	return xs
}

// BenchmarkMean is the comparison the example exists for: the same arithmetic
// over the same memory, once behind the boundary and once in Go.
//
// Two sizes, because they answer different questions. At 8 elements the cost is
// the crossing (measured separately by BenchmarkCall); at a million it is memory
// bandwidth, and the crossing is noise — which is why the slice is passed as
// (pointer, length) and not copied.
func BenchmarkMean(b *testing.B) {
	for _, n := range []int{8, 1 << 20} {
		xs := benchData(n)
		label := "n=" + strconv.Itoa(n)

		// SetBytes is per sub-benchmark, not inherited from the parent: it is
		// what turns ns/op into the MB/s column.
		b.Run(label+"/cppgo", func(b *testing.B) {
			b.SetBytes(int64(8 * n))
			for i := 0; i < b.N; i++ {
				sinkFloat = Mean(xs)
			}
		})
		b.Run(label+"/go", func(b *testing.B) {
			b.SetBytes(int64(8 * n))
			for i := 0; i < b.N; i++ {
				sinkFloat = goMean(xs)
			}
		})
	}
}

// BenchmarkStddev is the two-pass shape: the mean, then the deviations. It
// crosses the boundary twice per call, which is still nothing against the loop.
func BenchmarkStddev(b *testing.B) {
	const n = 1 << 20
	xs := benchData(n)

	b.Run("cppgo", func(b *testing.B) {
		b.SetBytes(int64(8 * n))
		for i := 0; i < b.N; i++ {
			sinkFloat = Stddev(xs)
		}
	})
	b.Run("go", func(b *testing.B) {
		b.SetBytes(int64(8 * n))
		for i := 0; i < b.N; i++ {
			sinkFloat = goStddev(xs)
		}
	})
}

// BenchmarkHistogram is the two-way slice case: the data goes in as (pointer,
// length) and the results come back the same way, through the caller's bins.
func BenchmarkHistogram(b *testing.B) {
	const n = 1 << 20
	xs := benchData(n)
	bins := make([]float64, 16)

	b.Run("cppgo", func(b *testing.B) {
		b.SetBytes(int64(8 * n))
		for i := 0; i < b.N; i++ {
			if err := Histogram(xs, bins, 0, 1); err != nil {
				b.Fatal(err)
			}
			sinkFloat = bins[0]
		}
	})
	b.Run("go", func(b *testing.B) {
		b.SetBytes(int64(8 * n))
		for i := 0; i < b.N; i++ {
			goHistogram(xs, bins, 0, 1)
			sinkFloat = bins[0]
		}
	})
}

// BenchmarkCall measures the crossing itself: a three-element slice handed over
// and a pointer handed back, with no arithmetic worth speaking of. The "go" arm
// is the same work without the boundary — &xs[0] — so the difference is the
// cost of entering C++.
//
// Compare this with the ~13 ns the project's own benchmark reports for a bare
// trampoline call at 964 MHz (see the repository README): the gap is the
// wrapper around it, not the ABI.
func BenchmarkCall(b *testing.B) {
	xs := []float64{1, 2, 3}
	b.ReportAllocs()

	b.Run("cppgo", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkPtr = DataPointer(xs)
		}
	})
	b.Run("go", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkPtr = &xs[0]
		}
	})
}

// BenchmarkCountAbove is the integer-result shape: a size_t back in RAX after a
// loop over the whole slice.
func BenchmarkCountAbove(b *testing.B) {
	xs := benchData(1 << 20)

	b.Run("cppgo", func(b *testing.B) {
		b.SetBytes(int64(8 * len(xs)))
		for i := 0; i < b.N; i++ {
			sinkInt = CountAbove(xs, 0.5)
		}
	})
	b.Run("go", func(b *testing.B) {
		b.SetBytes(int64(8 * len(xs)))
		for i := 0; i < b.N; i++ {
			sinkInt = goCountAbove(xs, 0.5)
		}
	})
}
