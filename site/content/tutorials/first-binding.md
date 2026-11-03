---
title: "Your first binding"
description: "A slow, deliberate walkthrough of binding two C++ functions — one that cannot fail and one that can — and testing the result."
weight: 10
---

The [quickstart](/docs/quickstart/) gets you to a working call as fast as
possible. This tutorial is slower: two functions, one that cannot fail and one
that can, plus the tests and the debugging steps that make the difference
between "it compiled" and "I understand what it does".

We will bind a `clamp01` in C++ and call it from Go, and add a plain integer
add so you can see both return conventions side by side.

## The C++ we start from

```cpp
// clamp.hpp
#pragma once
#include <cstdint>
#include <stdexcept>

inline double clamp01_impl(double v) {
  if (v < 0.0) return 0.0;
  if (v > 1.0) return 1.0;
  return v;
}

inline int64_t add_i64_impl(int64_t a, int64_t b) { return a + b; }

inline double scale_impl(double v, double lo, double hi) {
  if (hi <= lo) {
    throw std::invalid_argument("scale: hi must be greater than lo");
  }
  return lo + (hi - lo) * v;
}
```

Two of these are total functions and one can fail. That is the interesting
split, because cppgo signals failure with a status rather than a value.

## Step 1: the binding file

```go
package geometry

//cppgo:library libgeometry

//cppgo:external add_i64
func AddI64(a, b int64) int64

//cppgo:external clamp01
func Clamp01(v float64) float64

//cppgo:external scale
func Scale(v, lo, hi float64) (float64, error)
```

Notice what is *not* in this file: no `import "C"`, no unsafe, no assembly, no
cgo preamble, no forward declarations of C types. The signature is the whole
interface. `float64` will still be a `float64` on the other side (`double`, in
`XMM0`) and `int64` will still be `int64_t` (`RAX`).

The `error` in `Scale`'s result list is what changes its wire shape: the
function stops returning a value in `RAX` directly and starts returning a
status, because "failed" has to be distinguishable from "returned zero".

## Step 2: generate and read what came out

```console
$ cppgo gen -dir ./geometry
```

Before writing any C++, open the three files. They are short, and reading them
once will save you hours later.

`zz_cppgo_amd64.s` has one trampoline per function, all of them the same shape
as the `Dot` listing in the quickstart. The only difference between a
float-returning and an integer-returning function is the instruction that
copies the result out after the stack pointer is restored:

```asm
	MOVQ	AX, ret+24(FP)      // integer result, from RAX
	MOVSD	X0, ret+24(FP)      // floating point result, from XMM0
```

That is the entire return convention — `RAX` for integers and pointers, `XMM0`
for floats.

`cppgo_shim.h` is the contract you implement. Every declaration in it is
`extern "C"`, so the symbols are unmangled and `dlsym("add_i64")` finds them.
`zz_cppgo.gen.go` is the wrapper: it is where the slice pointer and length are
built, where `ProbeStack` is called, and where a non-zero status becomes a Go
error.

## Step 3: the shim

`cppgo_shim.h` is the authority here. It names each entry point, spells out the
parameter order (a pointer always before its length), and gives the status form
for a function that can fail. Implement what it declares, in a file beside the
Go code:

```cpp
// geometry/geometry.cpp
#include "cppgo_shim.h"
#include "clamp.hpp"

// These definitions match the declarations in cppgo_shim.h. The parameter
// lists are spelled out here so the ordering is visible; copy them from your
// generated header rather than from this page.

int64_t add_i64(int64_t a, int64_t b) { return add_i64_impl(a, b); }

double clamp01(double v) { return clamp01_impl(v); }

double scale(double v, double lo, double hi) {
  return scale_impl(v, lo, hi); // throws on a degenerate range
}
```

Throwing is how you report failure. The generated shim catches `std::exception`
and `...` around the entry point, stores `what()` where `cppgo_last_error` can
find it, and returns a non-zero status — so the `throw` in `scale_impl` reaches
Go as an `error`, and nothing unwinds into a Go frame. If a declaration in your
generated header differs in shape from the snippet above, the header wins: it
encodes the rules from the [ABI contract](/docs/abi-contract/) for exactly this
signature.

Build it:

```console
$ g++ -O2 -std=c++17 -fPIC -shared -I geometry -o libgeometry.so geometry/geometry.cpp
```

## Step 4: call it

```go
package main

import (
	"fmt"
	"log"

	"example.com/demo/geometry"
)

func main() {
	fmt.Println(geometry.AddI64(2, 40))       // 42
	fmt.Println(geometry.Clamp01(1.7))        // 1
	fmt.Println(geometry.Clamp01(-0.2))       // 0

	v, err := geometry.Scale(0.5, 10, 20)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(v)                            // 15
}
```

## Step 5: test the failure path

The success path proves the ABI; the failure path proves the error convention.
A table test is the cheapest way to cover both:

```go
func TestScale(t *testing.T) {
	tests := []struct {
		name    string
		v       float64
		lo, hi  float64
		want    float64
		wantErr bool
	}{
		{"midpoint", 0.5, 10, 20, 15, false},
		{"degenerate range", 0.5, 10, 10, 0, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := geometry.Scale(tc.v, tc.lo, tc.hi)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "hi must be greater than lo")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}
```

The `require.Contains` is the assertion that matters: the message you threw in
C++ travelled through a thread-local C string, was copied into Go, and arrived
with the symbol name prefixed. If the message is empty, the shim is not storing
it; if it is garbled, something retained a pointer it should have copied.

## When something goes wrong

| symptom | likely cause |
|---|---|
| panic at startup naming a symbol | the `.so` does not export it — check `nm -D libgeometry.so \| grep add_i64` |
| `cppgo: open "...": ...` | the library is not where the loader looks; try an absolute path |
| a truncated or wrong string | the shim kept a pointer to Go memory instead of copying |
| an empty error message | the shim returned non-zero without storing a message |
| a crash only under load | the shim recursed too deep, or retained Go memory — see [safety](/docs/safety/) |
| results that look like another function's | stale generated assembly; re-run `cppgo gen` and rebuild |

## What you have learned

- One directive per function, and the signature decides the wire format.
- A value-only function returns in `RAX`/`XMM0`; a function with an `error`
  returns a status and reports through `cppgo_last_error`.
- The header is the contract; the generated Go and assembly are build
  artifacts.
- Testing the failure path is not optional — it is the half of the boundary
  that can silently regress.

Next: [stateful objects](/tutorials/stateful-objects/), where the C++ side
starts keeping state between calls.
