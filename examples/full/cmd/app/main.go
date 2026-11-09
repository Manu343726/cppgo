// Command app is the Go program of the complete example: it loads the
// cppgo-generated binding for the metrics library and runs a small report over
// every construct the example binds.
//
// It is an ordinary program, not a test: `make run` builds and starts it, and
// its exit status is non-zero when a call that must succeed does not.
package main

import (
	"fmt"
	"os"

	metrics "github.com/Manu343726/cppgo/examples/full/gen"
)

// The two objects a generated binding loads. The defaults are the places the
// Makefile puts them, relative to this module; the environment overrides them
// for an installation somewhere else.
func shimPath() string {
	if path := os.Getenv("CPPGO_FULL_SHIM"); path != "" {
		return path
	}
	return "gen/libcppgo_metrics.so"
}

func libraryPath() string {
	if path := os.Getenv("CPPGO_LIBRARY_PATH"); path != "" {
		return path
	}
	return "build/libmetrics.so"
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "app:", err)
		os.Exit(1)
	}
}

func run() error {
	// The library path is read when the package is opened, so it is set first.
	// CPPGO_LIBRARY_PATH in the environment wins over this value, and that is
	// the knob a deployment uses.
	metrics.CppgoLibraryPath = libraryPath()
	if err := metrics.Open(shimPath()); err != nil {
		return fmt.Errorf("opening the generated binding: %w", err)
	}

	fmt.Printf("metrics %d\n", metrics.HistogramVersion())

	h, err := metrics.NewHistogram("latency", 4)
	if err != nil {
		return err
	}
	defer h.Close()

	for _, sample := range []float64{0.1, 0.6, 0.9} {
		if err := h.Observe(sample); err != nil {
			return err
		}
	}
	name, err := h.Name()
	if err != nil {
		return err
	}
	bounds, err := h.Bounds()
	if err != nil {
		return err
	}
	counts, err := h.Counts()
	if err != nil {
		return err
	}
	mean, err := h.Mean()
	if err != nil {
		return err
	}
	fmt.Printf("histogram %q: %d buckets, %v samples\n", name, h.Buckets(), h.Total())
	fmt.Printf("  bounds: %v\n", bounds)
	fmt.Printf("  counts: %v\n", counts)
	fmt.Printf("  mean:   %v\n", mean)

	// A clone is an independent object with the same state, because the copy
	// constructor is C++'s.
	clone, err := h.Clone()
	if err != nil {
		return err
	}
	if err := clone.Observe(0.3); err != nil {
		clone.Close()
		return err
	}
	cloneTotal := clone.Total()
	if err := clone.Close(); err != nil {
		return err
	}
	fmt.Printf("  clone:  %v samples after one more, original %v\n", cloneTotal, h.Total())

	// Point crosses by value in both directions.
	origin, err := metrics.NewPoint()
	if err != nil {
		return err
	}
	corner := metrics.Point{X: 2, Y: 4}
	mid := metrics.Centroid(*origin, corner)
	fmt.Printf("centroid((%v, %v), (%v, %v)) = (%v, %v)\n",
		origin.X, origin.Y, corner.X, corner.Y, mid.X, mid.Y)

	described, err := metrics.Describe("corner", &corner)
	if err != nil {
		return err
	}
	fmt.Printf("describe = %q\n", described)

	scaled, err := metrics.Scaled([]float64{1, 2, 3}, 2)
	if err != nil {
		return err
	}
	fmt.Printf("scaled([1 2 3], 2) = %v\n", scaled)

	// A throw in C++ arrives as a Go error.
	ratio, err := metrics.Ratio(9, 4)
	if err != nil {
		return err
	}
	fmt.Printf("ratio(9, 4) = %v\n", ratio)
	if _, err := metrics.Ratio(1, 0); err != nil {
		fmt.Printf("ratio(1, 0) failed as expected: %v\n", err)
	} else {
		return fmt.Errorf("ratio(1, 0) must fail")
	}

	// The status convention returns only an error; a non-zero status is the
	// failure.
	if err := metrics.ValidateName("latency"); err != nil {
		return err
	}
	if err := metrics.ValidateName(""); err != nil {
		fmt.Printf("validate_name(\"\") failed as expected: %v\n", err)
	} else {
		return fmt.Errorf("validate_name(\"\") must fail")
	}

	// The Go function passed here is called by C++.
	applied, err := metrics.Apply(func(v float64) float64 { return v * 3 }, 4)
	if err != nil {
		return err
	}
	fmt.Printf("apply(v*3, 4) = %v\n", applied)

	// The generic API: the two specializations the shim was built for.
	i, err := metrics.NewAccumulator[int32](10)
	if err != nil {
		return err
	}
	defer i.Close()
	if err := i.Add(5); err != nil {
		return err
	}
	sum, err := i.Total()
	if err != nil {
		return err
	}
	fmt.Printf("Accumulator[int32](10) + 5 = %v\n", sum)

	d, err := metrics.NewAccumulator[float64](1.5)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Add(0.25); err != nil {
		return err
	}
	sumF, err := d.Total()
	if err != nil {
		return err
	}
	fmt.Printf("Accumulator[float64](1.5) + 0.25 = %v\n", sumF)

	twice, err := metrics.Twice[int64](21)
	if err != nil {
		return err
	}
	fmt.Printf("twice[int64](21) = %v\n", twice)

	return nil
}
