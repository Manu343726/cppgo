package benchcgo

import (
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
	sinkPoint   Point
	sinkSample  Sample
	sinkErr     error
	sinkCounter *Counter
)

var (
	addA, addB       = int64(1), int64(2)
	ratioA           = int32(12)
	ratioZeroDivisor = int32(0)
	statusText       = "bench"
	pointA, pointB   = benchdataPointPair()
	sampleA, sampleB = benchdataSamplePair()
	existingCounter  = mustCounter(7)
	describePrefix   = benchdata.Prefix
	describeValue    = benchdata.Value
	blendT           = 0.25
	foldFnArg        = benchdata.Add32
)

// benchdataPointPair converts the shared fixture to the cgo package's own
// Point, so the benchmark and the test read the same numbers.
func benchdataPointPair() (Point, Point) {
	a, b := benchdata.Points()
	return Point{X: a.X, Y: a.Y}, Point{X: b.X, Y: b.Y}
}

// benchdataSamplePair is the same for the 24-byte record.
func benchdataSamplePair() (Sample, Sample) {
	a, b := benchdata.Samples()
	return Sample{A: a.A, B: a.B, C: a.C}, Sample{A: b.A, B: b.B, C: b.C}
}

// mustCounter builds the Counter every object benchmark calls into. It runs at
// package initialisation, before any timer starts.
func mustCounter(start int64) *Counter {
	counter, err := NewCounter(start)
	if err != nil {
		panic(err)
	}
	return counter
}

// BenchmarkCall is the crossing floor through cgo: the direct entry point and
// the same arithmetic behind the status/out-parameter wrapper cgo needs to
// survive an exception.
func BenchmarkCall(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64 = Add(addA, addB)
		}
	})
	b.Run("cgo_thunk", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64, sinkErr = AddThunk(addA, addB)
		}
	})
}

// BenchmarkMidpoint is the 16-byte POD by value, copied into the C ABI and out
// of it.
func BenchmarkMidpoint(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkPoint, sinkErr = Midpoint(pointA, pointB)
		}
	})
}

// BenchmarkBlend is the 24-byte result written through the caller's storage.
func BenchmarkBlend(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkSample = Blend(sampleA, sampleB, blendT)
		}
	})
}

// BenchmarkDescribe is the std::string result.
func BenchmarkDescribe(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkString, sinkErr = Describe(describePrefix, describeValue)
		}
	})
}

// BenchmarkDoubled is the std::vector in and out at both fixture sizes.
func BenchmarkDoubled(b *testing.B) {
	b.Run("n=8", func(b *testing.B) {
		benchDoubled(b, benchdata.Floats(benchdata.SmallN))
	})
	b.Run("n=1024", func(b *testing.B) {
		benchDoubled(b, benchdata.Floats(benchdata.LargeN))
	})
}

func benchDoubled(b *testing.B, values []float64) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkFloats, sinkErr = Doubled(values)
		}
	})
}

// BenchmarkThrow is the failure path: std::domain_error caught by the shim and
// rebuilt into a Go error.
func BenchmarkThrow(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt32, sinkErr = Ratio(ratioA, ratioZeroDivisor)
		}
	})
}

// BenchmarkStatusOK is the status convention's success path.
func BenchmarkStatusOK(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkErr = Parse(statusText)
		}
	})
}

// BenchmarkStatusFailure is the convention's failure path.
func BenchmarkStatusFailure(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkErr = Parse("!" + statusText)
		}
	})
}

// BenchmarkReentrant is the C++ -> Go round trip: bench::fold builds a
// std::function from the exported Go function and calls it once.
func BenchmarkReentrant(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt32, sinkErr = Fold(foldFnArg, 3, 4)
		}
	})
}

// BenchmarkObjectNewClose is the lifecycle: the constructor on the C++ heap,
// then the destructor.
func BenchmarkObjectNewClose(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			counter, err := NewCounter(7)
			if err != nil {
				b.Fatal(err)
			}
			counter.Bump(1)
			sinkCounter = counter
			if err := counter.Close(); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkObjectGet is the method call on a live object: the direct call with
// a receiver.
func BenchmarkObjectGet(b *testing.B) {
	b.Run("cgo", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			sinkInt64 = existingCounter.Get()
		}
	})
}

// Tests. The benchmarks measure; these prove the wrapper computes what the Go
// reference computes, so no arm can price a call that returns garbage.

func TestCall(t *testing.T) {
	if got, want := Add(2, 3), benchdata.Add(2, 3); got != want {
		t.Fatalf("Add(2, 3) = %d, want %d", got, want)
	}
	got, err := AddThunk(2, 3)
	if err != nil {
		t.Fatalf("AddThunk: %v", err)
	}
	if want := benchdata.AddThunk(2, 3); got != want {
		t.Fatalf("AddThunk(2, 3) = %d, want %d", got, want)
	}
}

func TestMidpoint(t *testing.T) {
	a, b := benchdata.Points()
	got, err := Midpoint(Point{X: a.X, Y: a.Y}, Point{X: b.X, Y: b.Y})
	if err != nil {
		t.Fatalf("Midpoint: %v", err)
	}
	want := benchdata.Midpoint(a, b)
	if got != (Point{X: want.X, Y: want.Y}) {
		t.Fatalf("Midpoint = %+v, want %+v", got, want)
	}
}

func TestBlend(t *testing.T) {
	a, b := benchdata.Samples()
	got := Blend(
		Sample{A: a.A, B: a.B, C: a.C},
		Sample{A: b.A, B: b.B, C: b.C},
		0.25)
	want := benchdata.Blend(a, b, 0.25)
	if got != (Sample{A: want.A, B: want.B, C: want.C}) {
		t.Fatalf("Blend = %+v, want %+v", got, want)
	}
}

func TestDescribe(t *testing.T) {
	got, err := Describe(benchdata.Prefix, benchdata.Value)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if want := benchdata.Describe(benchdata.Prefix, benchdata.Value); got != want {
		t.Fatalf("Describe = %q, want %q", got, want)
	}
}

func TestDoubled(t *testing.T) {
	for _, size := range []int{0, 1, benchdata.SmallN, benchdata.LargeN} {
		values := benchdata.Floats(size)
		got, err := Doubled(values)
		if err != nil {
			t.Fatalf("Doubled(%d): %v", size, err)
		}
		want := benchdata.Doubled(values)
		if len(got) != len(want) {
			t.Fatalf("Doubled(%d) returned %d elements, want %d", size, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Doubled(%d)[%d] = %v, want %v", size, i, got[i], want[i])
			}
		}
	}
}

func TestThrow(t *testing.T) {
	if _, err := Ratio(12, 0); err == nil {
		t.Fatal("Ratio(12, 0) returned a nil error; the C++ side throws std::domain_error")
	}
	got, err := Ratio(12, 5)
	if err != nil {
		t.Fatalf("Ratio(12, 5): %v", err)
	}
	if want, _ := benchdata.Ratio(12, 5); got != want {
		t.Fatalf("Ratio(12, 5) = %d, want %d", got, want)
	}
}

func TestStatus(t *testing.T) {
	cases := []string{"bench", "", "!bench"}
	for _, text := range cases {
		got := Parse(text)
		want := benchdata.Parse(text)
		if (got == nil) != (want == nil) {
			t.Fatalf("Parse(%q) error = %v, want %v", text, got, want)
		}
	}
}

func TestReentrant(t *testing.T) {
	got, err := Fold(benchdata.Add32, 3, 4)
	if err != nil {
		t.Fatalf("Fold: %v", err)
	}
	if want := benchdata.Fold(benchdata.Add32, 3, 4); got != want {
		t.Fatalf("Fold((3,4)) = %d, want %d", got, want)
	}
}

func TestObject(t *testing.T) {
	counter, err := NewCounter(40)
	if err != nil {
		t.Fatalf("NewCounter: %v", err)
	}
	counter.Bump(2)
	if got, want := counter.Get(), int64(42); got != want {
		t.Fatalf("Get after Bump = %d, want %d", got, want)
	}
	if err := counter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := counter.Close(); err == nil {
		t.Fatal("closing an already closed Counter returned a nil error")
	}
}
