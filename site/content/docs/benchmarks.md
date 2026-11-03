---
title: "Benchmarks"
description: "The measured call costs, the clock caveat that makes the nanoseconds honest, and how to reproduce the numbers with make bench."
weight: 70
---

The project's claim is not that a foreign call is free. It is that a foreign
call is cheap enough to stop dictating your architecture. These are the numbers
that claim rests on, and the conditions they were measured under.

## The numbers

Measured 2026-11-03 with Go 1.27 on a **Ryzen 7 6800HS pinned to 964 MHz**
(the `power-saver` profile).

| path | ns/op | ~cycles | |
|---|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 | baseline |
| cgo (Go → C, same function) | 105 | 101 | |
| **cppgo trampoline, direct** | **13.9** | **13** | **7.5x cheaper than cgo** |
| cppgo trampoline, stack switch | 14.9 | 14 | |

> **The clock caveat.** At 964 MHz the absolute nanoseconds are inflated roughly
> 4.5x by the clock. Treat them as an upper bound on what the same code does at
> full speed, and trust the **cycles and ratios**. Any comparison to a number
> measured at a different frequency — in a blog post, on another machine — is
> meaningless until both are converted to cycles.

The stack-switch row is the same call with a real goroutine stack switch in
front of it: one extra nanosecond at this clock, about one extra cycle. It is
listed separately because it is the part of the mechanism that a reader is
most likely to assume is expensive, and it is not.

## What the numbers mean

- **13.9 ns is not 2.1 ns.** Moving the boundary into a loop still costs
  something. If a call is inside an inner loop over a million elements, the
  boundary will dominate, and that is what bulk transfer is for.
- **13.9 ns is not 105 ns.** cgo's 101 cycles are mostly the `cgocall`
  machinery: handing off the `P`, switching to `g0`, entering and leaving
  syscall state. cppgo skips all of it, and that is the whole difference.
- **A per-element binding is viable.** At ~13 cycles you no longer have to batch
  10 000 rows into one call to amortise the crossing. Batching is still often
  the right call — it amortises your own marshalling too — but it is now a
  performance tuning decision, not a structural requirement.

## Reproducing them

```console
$ make bench
```

That runs, with cgo off so the numbers describe the real configuration:

```console
$ CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./...
```

To get numbers you can compare against the table, reproduce the conditions:

1. **Pin the clock.** The measurements were taken with the CPU held at 964 MHz
   (`power-saver`). If the frequency wanders, both `ns/op` and the comparison
   wander with it. Record the frequency you measured at.
2. **Disable cgo** (`CGO_ENABLED=0`), which is also the configuration the
   library is meant to run in.
3. **Run the whole benchmark set**, not a single benchmark, so the baseline and
   the trampoline are measured in the same session.
4. **Quote cycles, not just nanoseconds**, when comparing across machines:
   `cycles ≈ ns/op × clock in GHz`.

## The GC-delay measurement

The call cost is only half the story; the other half is what the call does to
everything else. Since the `P` is not released during a foreign call, a call in
flight delays a GC cycle by up to its duration.

Measured: a **2.0 ms** foreign call delayed the worst GC cycle by **1.1 ms**,
versus **2.4 ms** for a spinning Go goroutine. The foreign call is *less*
disruptive than a goroutine that will not yield, which is the honest way to read
that number — it is a real cost, but not a pathological one.

That measurement is the reason the design says: short, compute-bound calls are
the right use; long or blocking work belongs in another process. It is not a
smaller number to be optimised away later — it is the trade.

## Methodology notes

- All rows measure the same underlying operation: one call to the same
  `dlopen`'d C function, so the comparison is the *crossing*, not the work.
- `-benchmem` is on, so an allocation regression in the wrapper shows up as
  `B/op` rather than being hidden in the timing.
- The cgo row goes through the runtime's `cgocall` path to the same symbol; it
  is not a comparison against a static C link.
- Benchmarks are run with the race detector **off** (the race detector's cost
  would swamp the difference) and with `-race` in the separate correctness pass,
  where the question is different.
