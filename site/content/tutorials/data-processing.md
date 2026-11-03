---
title: "Data processing: slices and batching"
description: "How slices cross the boundary, when per-element calls beat a batch, and how to measure the crossover instead of guessing it."
weight: 30
---

The original reason anyone amortises a foreign call is that the crossing was
expensive: with cgo at ~101 cycles, a per-element binding over a million rows
spends more time entering C than doing work. At ~13 cycles the arithmetic
changes, and the honest answer becomes "it depends — measure it".

This tutorial covers what a slice looks like on the wire, the three strategies
for moving bulk data, and how to decide between them with `make bench` rather
than instinct.

## What actually crosses

A `[]float64` does not travel as a slice header. It travels as two registers:

```
RDI = &a[0]    RSI = len(a)
```

That is it. The data stays where it is — no copy, no marshalling, no
serialisation. The shim receives a `const double *` and a `size_t` pointing
straight into Go's backing array.

Three consequences follow, and they explain every rule in this page:

1. **Passing a slice is nearly free.** You are passing two words. A 10 MB slice
   and a 10-byte slice cost the same to hand over.
2. **The memory is borrowed.** The callee must not keep the pointer. It is
   valid for the call and no longer; the wrapper's `KeepAlive` is what stops
   the collector from reclaiming the array mid-call, and nothing stops you from
   writing a shim that stores the pointer and reads freed memory later.
3. **In-place mutation is visible to Go.** If the C++ side writes through the
   pointer, the Go slice changes, because it is the same memory. That is
   sometimes exactly what you want (`sort`, `fft`) and sometimes a contract you
   need to document.

`Ptr` gives you the pointer and returns `nil` for an empty slice, so a shim
that checks for null handles the empty case without a special case:

```go
func Ptr[T any](s []T) unsafe.Pointer  // nil when len(s) == 0
```

## Three strategies

### 1. Bulk: one call, whole slice

```go
//cppgo:external smooth
func Smooth(in []float64, out []float64, window int) error
```

```cpp
void smooth(const double *in, size_t in_len,
            double *out, size_t out_len,
            int64_t window) { /* one loop, one call */ }
```

One crossing amortised over the entire dataset. This is the right default for
anything that is O(n) in the data — smoothing, scaling, a dot product, a
histogram. The boundary cost disappears next to the loop.

### 2. Per element: one call per item

```go
//cppgo:external hash_one
func HashOne(v int64) int64
```

Fine for small, irregular workloads: a handful of values, or a loop whose trip
count is data-dependent and usually tiny. The cost is `n × 13.9 ns` of pure
boundary overhead — negligible at 100 elements, dominant at 100 million.

### 3. Batched in chunks: reusable buffers, fixed-size calls

```go
const chunk = 4096

for start := 0; start < len(rows); start += chunk {
	end := min(start+chunk, len(rows))
	if err := engine.Feed(rows[start:end]); err != nil {
		return err
	}
}
```

The compromise that matters when the C++ side wants to stream — when results
must be flushed incrementally, when a single call over the whole dataset would
exceed a buffer, or when you want to bound latency. Slicing a slice is free
(no copy), so a chunk loop costs one pointer and one length per iteration.

## Doing the arithmetic

At the measured ~13.9 ns per call (964 MHz, `power-saver` — the clock caveat
applies, so convert to cycles before comparing with anything else):

| elements | per-element overhead | verdict |
|---|---|---|
| 100 | ~1.4 µs | invisible |
| 100 000 | ~1.4 ms | visible in a profile |
| 10 000 000 | ~139 ms | dominates the work |

The same table for cgo at ~105 ns per call is 7.5x worse at every row, which is
the origin of the old "always batch" advice. It is not wrong advice; it was
calibrated for a different constant.

**But the boundary is not the only cost.** A per-element binding also pays for
whatever the wrapper does per call — building the argument block, checking the
status, converting an error — and for the loss of locality when the C++ side
touches memory one element per call instead of streaming through it. Bulk
transfer usually wins by more than the crossing alone, especially once the
working set stops fitting in cache.

## When per-element is the right answer

- **The batch would be tiny anyway.** A `SetPixel`-shaped API called 20 times
  per frame does not need a bulk variant.
- **The C++ side is inherently stateful per item** and the state is the point —
  feeding one row at a time to a parser that emits events.
- **Batching would change semantics.** If order of interleaved Go work matters,
  a bulk call serialises it.
- **You need to observe intermediate results** and cannot afford to buffer a
  whole dataset in C++ memory just to hand it back at the end.

## When bulk is the right answer

- **The loop body is arithmetic on numbers you already have in a slice.** There
  is no reason to cross the boundary per element.
- **Allocation is involved on the C++ side.** One allocation per call is the
  same whether the call has one element or a million; per-element turns it into
  a million allocations.
- **You are already holding the data contiguously**, which in Go is the normal
  case: `[]float64` is a flat array, not a list of boxes.

## Write the shim to be loop-friendly

The same advice as any tight loop, but it matters more when the loop is on the
other side of a boundary you cannot see from Go:

```cpp
void smooth(const double *in, size_t in_len,
            double *out, size_t out_len,
            int64_t window) {
  if (in_len != out_len) {
    throw std::invalid_argument("smooth: length mismatch");
  }
  if (window <= 0 || static_cast<size_t>(window) > in_len) {
    throw std::invalid_argument("smooth: window out of range");
  }

  for (size_t i = 0; i < in_len; ++i) {
    // Bounds are checked once, above, not per element.
  }
}
```

Validate the lengths **once** and then trust them inside the loop. A shim that
does not validate is a buffer overflow waiting for a caller who passes a
mismatched pair; a shim that validates per element throws away the reason to
batch.

## Measuring your own crossover

Do not take the table above as a property of your workload. Write the two
variants and run them:

```console
$ make bench
```

which is `CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./...`. Then:

1. **Benchmark both shapes** — a per-element loop and a bulk call over the same
   data — and compare `ns/op` normalised per element.
2. **Watch `B/op` and `allocs/op`.** A batching win is often an allocation win
   in disguise, and the allocation shows up in `-benchmem` even when the timing
   is noisy.
3. **Pin the clock** when you compare against the published numbers, or convert
   to cycles. A benchmark run on a boosting CPU will look 4x faster than one run
   on a `power-saver` profile, for reasons that have nothing to do with your
   code.
4. **Scale the input** from 1e3 to 1e6 elements. The crossover is where the
   per-element curve stops being flat, and it is different for every binding.

## Hand-written bindings

If you write a binding by hand instead of generating one, the two rules that
generated wrappers already follow are yours to repeat:

```go
cppgo.ProbeStack()             // before: reserve 32 KiB of stack headroom
ret := yourTrampoline(fn, ...)  // the NOSPLIT asm, generated or written by hand
cppgo.KeepAlive(buf)           // after: keep the borrowed memory alive
```

`KeepAlive` **after**, never before. Called too early it does nothing useful;
called too late it is already a use-after-free. The generated wrapper gets this
right, which is a good reason to let it do the work.

Next: [the API reference](/docs/api/) for the exact signatures, or
[benchmarks](/docs/benchmarks/) for how the numbers were produced.
