---
title: "Performance"
description: "What a call costs, compared with cgo and with calling Go directly."
weight: 28
---

cgo's per-call overhead is one of the reasons cppgo exists, so the comparison is
part of the product, not a footnote. This page states what a cppgo call costs
next to cgo and next to a plain Go call, and it is where the full benchmark
comparison lives.

## What is compared

- **plain Go**: an indirect call to a Go function, the baseline;
- **cgo**: a call to a trivial C function through the cgo path;
- **cppgo, direct**: a call to a C++ function that can be called without a C++
  frame;
- **cppgo, through the C++ side**: a call that needs one, for example anything
  that may throw;
- **cppgo, callback-capable**: a call that lets the C++ side call back into Go;
- **object lifecycle**: constructing and closing a C++ object;
- **errors**: the cost of a failed call that has to become a Go error;
- **data**: what passing a Go slice or string adds to the call.

## Characteristics

- A **direct** cppgo call stays in the same order of magnitude as an indirect Go
  call.
- **cgo is roughly an order of magnitude slower** than a cppgo direct call.
- A call that goes **through the C++ side** adds one C++ call frame inside the
  library; the Go side pays the same call either way.
- A **callback-capable** call costs about ten times a direct one and allocates one
  small argument block per call, because a callback may grow the goroutine stack.
- **Passing a slice or a string costs nothing beyond the pointer and the length**;
  passing a value type copies it.
- **Constructing and closing an object are calls like any other**, so a
  constructor plus a destructor costs about two calls.

## Measured

Both tables were measured on an 8-core x86-64 Linux machine running Go 1.27. The
first pinned the CPU clock, which inflates its absolute nanoseconds by about
4.5×, so the ratio is the number to read there; the second ran unpinned.

Plain Go, cgo, and a cppgo direct call, same run:

| Path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| cppgo call, direct | 13.9 | 13 |

The cppgo carriers, side by side, same run:

| Path | ns/op |
|---|---|
| cppgo call, direct | 15.9 |
| cppgo call, callback-capable | 151 |
| callback round trip | 481 |

A call through the C++ side is not in either table as a separate row: it is a
direct call plus one C++ frame in the library.

## What is still to be published

The suite that measures object construction and destruction, the error paths and
the data-passing cases is being assembled; those rows are not published here
until there are real runs behind them.
