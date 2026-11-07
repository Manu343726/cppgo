+++
title = "Benchmarks"
description = "The measured cost of a cppgo call against cgo and a plain Go call, with the clock caveat: a Ryzen 7 6800HS pinned to 964 MHz, where cycles and ratios are the honest numbers."
weight = 20
+++

## The table

Measured on **2026-11-03**, Go 1.27, on a **Ryzen 7 6800HS with the CPU pinned to
964 MHz** (the `power-saver` profile):

| path | ns/op | ~cycles | |
|---|---|---|---|
| plain Go indirect call | 2.1 | 2 | baseline |
| **cppgo trampoline, direct** | **13.9** | **13** | **7.5x cheaper than cgo** |
| cppgo trampoline, stack switch | 14.9 | 14 | adds `ProbeStack` |
| cgo (same function, same `dlopen`'d symbol) | 105 | 101 | |

<div class="callout callout-warn">
<span class="callout-title">Read the cycles, not the nanoseconds</span>
<p>
Every nanosecond figure on this page was measured with the CPU deliberately
pinned to 964&nbsp;MHz. The absolute times are inflated by roughly 4.5x by the
clock; <strong>cycles and ratios are the honest numbers</strong>. On a CPU
running at its normal clocks the same code is several times faster in
nanoseconds, with the same cycle counts.
</p>
</div>

What the rows mean:

- **2.1 ns / 2 cycles** is the floor: a Go call through a package-level function
  variable, so the compiler cannot inline it. This is the cost a normal Go call
  would have if the boundary were free.
- **13.9 ns / 13 cycles** is the generated trampoline: load the arguments, align
  `SP`, `CALL` the symbol, restore `SP`, store the result. That is the whole
  boundary.
- **14.9 ns / 14 cycles** adds `ProbeStack`. The difference — about one cycle —
  is the headroom guarantee, and it is why the generated wrapper calls it on
  every call rather than once.
- **105 ns / 101 cycles** is cgo calling the *same* function from the *same*
  `dlopen`'d library. It pays for `entersyscall`, the `P` handoff, the `g0`
  stack switch and the bookkeeping that marks the goroutine as being in a
  syscall. cppgo's point is that this is not inherent to calling C; it is the
  cost of the runtime's generic path.

The ratio is **7.5x** at these clocks. It is a ratio between two measurements
taken the same way on the same machine, so it survives the clock caveat.

## The cost that is not in the table

The trampoline is cheap because it does not release the `P`. The bill arrives at
the garbage collector:

> A foreign call in flight delays a GC cycle by up to the call's duration.
> Measured: a **2.0 ms** call delayed the worst GC cycle by **1.1 ms**, versus
> **2.4 ms** for a spinning Go goroutine.

(Again at 964 MHz; the comparison is the point, not the absolute times.) A
goroutine spinning in Go yields to the scheduler; a goroutine inside a cppgo call
does not, because the runtime cannot see the foreign frame. For short calls this
is invisible. For a call that blocks for milliseconds, it is the reason to move
that work out of process.

## Reproduce it

```console
$ make bench
```

That runs `CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./...` — every
benchmark in the repository, with allocation counts. The benchmarks are:

| benchmark | measures |
|---|---|
| `BenchmarkGoCall` | the baseline indirect Go call |
| `BenchmarkTrampoline` | the foreign call itself: argument block, C ABI, result copy |
| `BenchmarkTrampolineProbeStack` | the same plus `ProbeStack`, which is the "stack switch" row |
| `BenchmarkProbeStack` | the headroom guarantee alone |
| `BenchmarkSymbolResolution` | `dlsym` cost per symbol, paid once at init, not per call |

`make bench` does **not** measure cgo: the 105 ns row came from a separate cgo
program calling the same fixture. To reproduce the comparison, build that
program with `CGO_ENABLED=1`, call the same `dlopen`'d function, and run it under
the same `GOGC`/`GOMAXPROCS` and the same CPU pinning.

For honest numbers on your own machine:

- **Pin the CPU.** Otherwise the governor changes clocks between runs and the
  nanoseconds move by more than the effect you are measuring. `tlpctl get` /
  `cpupower frequency-set` or your distribution's equivalent.
- **Keep `CGO_ENABLED=0`** for the cppgo arm, as `make bench` does.
- **Run each benchmark more than once** and compare cycles, not just ns.
- **Do not compare across machines.** The cycle counts transfer; the nanoseconds
  do not.

## What these numbers do not say

- They do not measure your C++ function. A binding that spends 10 µs inside an
  algorithm is not made faster by a 13-cycle trampoline.
- They do not measure a call that allocates, blocks, or takes a lock. The model
  is "short, compute-bound call".
- They do not include the first call, which pays `dlopen`/`dlsym` for every
  symbol. `BenchmarkSymbolResolution` measures the per-symbol part of that.
- They do not include the GC delay above; that is a separate measurement with a
  different shape.

For a workload-shaped comparison — a real function over a `[]float64`, C++ against
pure Go — the `stats` example has a benchmark harness. This site does not quote
its results; run `make bench` in
[`examples/stats/`]({{< ref "examples/stats.md" >}}).

Next: [limitations and roadmap]({{< ref "project/limitations.md" >}}).
