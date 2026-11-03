---
title: "Stateful objects with handles"
description: "Keep a C++ object alive across calls with an opaque handle: creation, destruction, double-close and use-after-free, and error handling that survives both."
weight: 20
---

Stateless functions are the easy half. Real C++ libraries have objects:
an index, a parser, a connection pool, a model. Their state lives in the C++
address space, and Go must be able to say "that one" without knowing what a
`void*` points at.

That is what a `Handle` is. On the wire it is a `void *` — an opaque C++
pointer that Go carries as a number. It is the only way an object survives
between two calls, and it comes with the oldest rule in manual memory
management: nobody is going to free it for you.

## The C++ object

A running mean is a good example because it has invariants worth protecting:

```cpp
// accumulator.hpp
#pragma once
#include <cstddef>
#include <stdexcept>

class Accumulator {
 public:
  void add(double v) {
    sum_ += v;
    ++n_;
  }

  double sum() const { return sum_; }

  double mean() const {
    if (n_ == 0) {
      throw std::runtime_error("mean of an empty accumulator");
    }
    return sum_ / static_cast<double>(n_);
  }

  void reset() {
    sum_ = 0.0;
    n_ = 0;
  }

 private:
  double sum_ = 0.0;
  std::size_t n_ = 0;
};
```

No smart pointers, no globals: the object's lifetime is exactly the pair of
calls that create and destroy it.

## The binding

```go
package acc

import "github.com/Manu343726/cppgo"

//cppgo:library libacc

//cppgo:external acc_new
func New() (cppgo.Handle, error)

//cppgo:external acc_add
func Add(h cppgo.Handle, v float64) error

//cppgo:external acc_sum
func Sum(h cppgo.Handle) (float64, error)

//cppgo:external acc_mean
func Mean(h cppgo.Handle) (float64, error)

//cppgo:external acc_reset
func Reset(h cppgo.Handle) error

//cppgo:external acc_free
func Free(h cppgo.Handle) error
```

Five functions, one handle in every signature after the first. On the wire each
of those is a single pointer register — `RDI` — with `v` in `XMM0` when there is
a value.

`acc_new` is the interesting one: it returns a `Handle` **and** an `error`, so
it reports a null handle the same way everything else reports failure, through
the status. Creation can fail, and pretending otherwise is how you end up with
a nil handle in a Go struct that looks initialised.

## The shim

```cpp
// acc.cpp
#include "cppgo_shim.h"
#include "accumulator.hpp"

// A helper so the entry points read as one line each. Doing the cast in one
// place is also how you make sure every entry point checks for null.
static Accumulator *as_acc(void *h) {
  if (h == nullptr) {
    throw std::runtime_error("acc: null handle");
  }
  return static_cast<Accumulator *>(h);
}
```

Each entry point then follows the same three steps: check the handle, do the
work, let an exception carry the failure. The exact parameter lists — including
the status form for functions that can fail — are in `cppgo_shim.h`; what goes
inside them is one line. `acc_free` is the only one that deletes:

```cpp
// signature as declared in cppgo_shim.h
void acc_free(void *h) {
  delete as_acc(h);   // throws on a null handle rather than deleting null
}
```

Whether a null handle is an error or a no-op is your contract to choose. cppgo
does not care — it only requires that the choice is the same in every entry
point, because a handle that is sometimes null and sometimes not is a bug that
shows up three layers away. Throwing is the safer default: it turns
use-after-free from a segfault into an error.

## Wrapping it in Go

`cppgo.Handle` is an opaque value on the Go side — on the wire it is a
`void *` — with no methods and no ownership. The idiomatic move is to wrap it
in a Go type so that ownership has exactly one representation:

```go
package accumulator

import (
	"fmt"
	"sync"

	"github.com/Manu343726/cppgo"
	"example.com/demo/acc"
)

type Accumulator struct {
	mu     sync.Mutex
	h      cppgo.Handle
	closed bool
}

func New() (*Accumulator, error) {
	h, err := acc.New()
	if err != nil {
		return nil, fmt.Errorf("accumulator: %w", err)
	}
	return &Accumulator{h: h}, nil
}

func (a *Accumulator) Add(v float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return fmt.Errorf("accumulator: add after close")
	}
	return acc.Add(a.h, v)
}

func (a *Accumulator) Mean() (float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return 0, fmt.Errorf("accumulator: mean after close")
	}
	return acc.Mean(a.h)
}

func (a *Accumulator) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return fmt.Errorf("accumulator: close of closed accumulator")
	}
	a.closed = true
	return acc.Free(a.h)
}
```

Three things that wrapper buys you, none of which the handle gives you by
itself:

**Idempotent-looking shutdown.** `Close` sets `closed` before freeing, so a
second `Close` is an error in Go rather than a double `delete` in C++. The C++
side never sees the second call, which is the only way to make double-close
survivable.

**A serialised object.** The C++ `Accumulator` has no lock, and it does not need
one: the Go wrapper takes a mutex, and the object is only ever touched by
whichever goroutine holds it. If several goroutines must call concurrently,
either add the mutex in Go (as here) or synchronise in C++ — but decide, and put
it in the type's documentation.

**A place to hang the lifetime.** `defer a.Close()` after a successful `New` is
the whole memory management story:

```go
a, err := accumulator.New()
if err != nil {
	return err
}
defer a.Close()

for _, v := range samples {
	if err := a.Add(v); err != nil {
		return err
	}
}

mean, err := a.Mean()
```

## Lifetimes: the four ways to get it wrong

| mistake | what happens | how to notice |
|---|---|---|
| **forgetting `Close`** | the C++ object leaks; Go's GC will not help, it does not know about it | memory grows with the number of objects created |
| **`Close` then use** | in v1 terms: a use-after-free; the null check in `as_acc` does not help because the pointer is a dangling non-null value | crash, or corrupted results, usually not at the call site |
| **double `Close`** | second `delete` on freed memory | the `closed` flag in the wrapper is what prevents it |
| **copying the wrapper** | two Go values share one handle, and the first `Close` invalidates the second | keep the wrapper a pointer type, and never copy it by value |

The third row is the one people are surprised by: the C++ object is not garbage
collected, so *nothing* in the runtime notices a leak. A Go finalizer is not a
substitute — finalizers are not guaranteed to run, and they run at an
arbitrary later time, which is exactly wrong for an object that holds a file
descriptor or a lock.

## Errors across a handle's lifetime

The error convention does not change because there is state involved, and that
is worth spelling out because the three sources of failure are easy to conflate:

- **The handle is bad.** `acc: null handle` — the shim's check, or a dangling
  pointer that nobody can detect.
- **The object rejects the operation.** `mean of an empty accumulator` — the
  C++ invariant, thrown and converted.
- **The Go wrapper rejects the call.** `accumulator: add after close` — never
  reaches C++ at all.

Each layer produces a distinct message, which is what makes a production
incident diagnosable. Collapsing them into one generic error loses the only
information you had.

Because the message buffer is **thread-local and valid only until the next
call**, always copy it immediately — which is what the generated wrapper and
`cppgo.LastError` do for you. Never hold a `*C.char` equivalent across calls,
and never hold a `NativeError` you built by hand from a pointer.

## Patterns that scale

- **One handle per domain object**, created in a constructor and destroyed in
  exactly one place. If you have two `Free` call sites, you have a bug waiting.
- **Return the wrapper, not the handle**, from your public API. A `cppgo.Handle`
  escaping into application code means anyone can free it.
- **Prefer `defer Close()` over manual teardown**, and keep the window between
  `New` and `defer` to two lines.
- **Reuse when the object is expensive**, create per call when it is not. A
  handle round trip costs two calls; a `dlopen`'d object that allocates a 2 MB
  buffer is not something to construct per row.
- **Do not pass handles between goroutines without the wrapper's lock.** The
  handle is safe to copy, the object behind it is not.

Next: [data processing](/tutorials/data-processing/), where the question is
how much data crosses per call rather than how long an object lives.
