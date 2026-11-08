# Benchmarks

The manual has a [Performance page](../../site/content/explanation/benchmarks.md)
that carries the comparison and the characteristics it supports. This page is
where those numbers come from: the commands, the hardware, the clock state and
the raw values, including the ones not published in the manual yet.

The benchmark **suite** that will back the full comparison — object lifecycle,
error paths, data passing, plus the carriers — is being built in issue #39. Until
it lands, the manual publishes the two runs below as measurements, because they
are real and complete for the rows they cover; nothing is extrapolated.

## How to run them

```console
$ make bench
```

runs `CGO_ENABLED=0 go test -run '^$' -bench . -benchmem ./...` over every
package. The carrier benchmarks live in `internal/callback`, and
`internal/asm` has the call-path ones. `examples/stats` adds the application-shaped
comparison (the same arithmetic behind the boundary and in Go, at 8 and at a
million elements) and runs with `CGO_ENABLED=0 go test -run '^$' -bench . -benchmem .`
in that directory.

The numbers below are also stated in `DESIGN.md` (§ *The trampoline*), which is
the contract they were measured against.

## Carriers, side by side — 2026-11-08

`go test -bench . -benchtime 200000x` on the machine that built this tree:

| Path | ns/op |
|---|---|
| cppgo trampoline, direct | 15.9 |
| cppgo trampoline, reentrant | 151 |
| callback round trip | 481 |

The reentrant carrier allocates one 16-byte argument block per call, because the
block cannot live on a goroutine stack that a callback may grow. A thunk adds one
C++ call frame inside the library; the Go side pays the same trampoline either
way.

## Reference points — 2026-11-03

Same machine, pinned to 964 MHz with boost disabled:

| Path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| cppgo trampoline, direct | 13.9 | 13 |
| cppgo trampoline, stack switch | 14.9 | 14 |

**Caveat.** The clock pin inflates the absolute nanoseconds by roughly 4.5×.
The ratios and the cycle counts are the honest numbers from that run; the
absolute values are not comparable with the 2026-11-08 table above, which ran
unpinned.

## What the manual publishes

The Performance page publishes the two tables above with a neutral environment
statement, and the characteristics they support. It does not carry the clock
caveat as a methodology section, the dates, or the commands; those stay here.

| Characteristic on the manual page | Backed by |
|---|---|
| a direct call stays in the same order as a Go call | 13.9 ns against a 2.1 ns Go indirect call, same run (2026-11-03) |
| cgo is roughly an order of magnitude slower than a cppgo direct call | 105 ns against 13.9 ns, same run (2026-11-03) |
| a callback-capable call costs about ten times a direct one | 151 ns against 15.9 ns, same run (2026-11-08) |

No other cost number belongs on a user page. If one is added, the run that
produced it belongs on this page first.
