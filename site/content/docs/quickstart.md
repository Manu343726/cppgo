---
title: "Quickstart"
description: "A full worked example: bind a C++ dot product, generate the wrapper and trampoline, implement the shim, and call it from Go."
weight: 20
---

This page builds one real binding end to end. The C++ side is a dot product
over two `[]float64`; the Go side ends up calling `mathx.Dot(a, b)` and getting
`(float64, error)` back, with no cgo anywhere.

Everything below is one small module. Create it and follow along.

```console
$ mkdir -p demo/mathx && cd demo
$ go mod init example.com/demo
$ go get github.com/Manu343726/cppgo
```

## 1. Write the bound signatures

The input to the generator is a normal Go file with directives on it. No
bodies: the signature *is* the binding.

`mathx/bindings.go`:

```go
package mathx

//cppgo:library libmathx

//cppgo:external dot
func Dot(a, b []float64) (float64, error)
```

Two directives, two jobs:

- `//cppgo:library libmathx` names the shared object this package binds to.
  It is package-level and appears once.
- `//cppgo:external dot` marks the next function as a foreign function and
  names the C symbol to resolve: `dot`.

The Go signature is what callers see. `[]float64` becomes a pointer and a
length, the `error` becomes an `int32_t` status plus a message, and the
`float64` stays a `float64` in `XMM0` — see [type mapping](/docs/type-mapping/)
for the full table.

## 2. Generate

```console
$ cppgo gen -dir ./mathx
```

Four files appear in `mathx/`:

| file | contents |
|---|---|
| `zz_cppgo.gen.go` | the idiomatic wrapper for `Dot`, and the symbol binding that ties it to the loaded library |
| `zz_cppgo_amd64.s` | one `NOSPLIT` assembly trampoline for this signature |
| `cppgo_shim.h` | the `extern "C"` declarations your C++ implements |
| `zz_cppgo_stub.go` | the original function rewritten to delegate, so no stub body panics are left behind |

Those files are generated: do not edit them by hand, and do not treat them as
the source of truth — the annotated `bindings.go` is. Re-running `cppgo gen`
regenerates all four.

Look at `zz_cppgo_amd64.s` once. It is the whole trick, and it is twelve
instructions:

```asm
TEXT ·Dot(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	a+8(FP), DI          // pointer
	MOVQ	n+16(FP), SI         // length
	MOVQ	SP, R13
	ANDQ	$~15, SP             // SysV wants RSP 16-aligned before the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

No `entersyscall`, no `P` handoff, no `g0` switch. The
[how it works](/docs/how-it-works/) page explains why that is safe.

## 3. Implement the shim in C++

`cppgo_shim.h` declares exactly one `extern "C"` function per bound symbol,
wrapped so C++ exceptions can be caught before they reach Go. Include it and
define what it declares:

`mathx/mathx.cpp`:

```cpp
#include "cppgo_shim.h"

#include <cstddef>
#include <cstdint>
#include <cstring>
#include <stdexcept>

// The declaration below comes from cppgo_shim.h — copy the parameter list
// from the generated header rather than from this page. Order follows the
// ABI contract: pointer then length for a slice, the result last.
int32_t dot(const double *a, std::size_t an,
            const double *b, std::size_t bn,
            double *out) {
  if (an != bn) {
    throw std::runtime_error("dot: length mismatch");
  }

  double sum = 0.0;
  for (std::size_t i = 0; i < an; ++i) {
    sum += a[i] * b[i];
  }

  *out = sum;
  return 0;
}
```

Three things about that function:

- **It throws, and that is fine.** The generated shim catches `std::exception`
  and `...`, stores the message, and converts it to a non-zero return. Nothing
  unwinds into Go.
- **It returns `0` for success.** Any non-zero value means failure, and the
  message is read back with `cppgo_last_error()`.
- **It does not keep `a` or `b`.** The pointers borrow Go memory; retaining
  them past the return is a use-after-free waiting to happen.

Build it as a shared object:

```console
$ g++ -O2 -std=c++17 -fPIC -shared -I mathx -o libmathx.so mathx/mathx.cpp
```

The `-I mathx` is there because `cppgo_shim.h` lives beside the Go file.

## 4. Call it from Go

`main.go`:

```go
package main

import (
	"fmt"
	"log"

	"example.com/demo/mathx"
)

func main() {
	a := []float64{1, 2, 3}
	b := []float64{4, 5, 6}

	sum, err := mathx.Dot(a, b)
	if err != nil {
		log.Fatal(err) // "dot: length mismatch" arrives here as a Go error
	}

	fmt.Printf("dot = %.1f\n", sum) // dot = 32.0
}
```

```console
$ CGO_ENABLED=0 go build -o demo .
$ ./demo
dot = 32.0
```

Note the `CGO_ENABLED=0`. It is not a workaround or a special mode: it is the
normal way to build a program that uses cppgo, and it is what proves no C
compiler is involved in the Go build.

Because the library is loaded at runtime, `libmathx.so` has to be findable by
the dynamic loader: beside the binary and opened by absolute path, on
`LD_LIBRARY_PATH`, or installed in a directory the loader knows. The binding
stanza in `zz_cppgo.gen.go` is where the name from `//cppgo:library` turns into
a load — that is the line to change if you want to pin a deployment path.

## 5. Break it on purpose

The interesting part of a binding is what happens when it fails, so try it:

- Pass slices of different lengths and check that the Go error contains the
  message the C++ side stored. That message crossed the boundary in a
  thread-local C string and was copied into a Go string before it could be
  overwritten by the next call.
- Rename `dot` in the `.cpp` file and rebuild the library without regenerating.
  The symbol lookup fails with an error naming the symbol, not a segfault.
- Throw a `std::string` instead of a `std::runtime_error` and confirm you still
  get an error rather than a crash — the `catch (...)` branch handles it.

## Where to go next

- [Type mapping](/docs/type-mapping/) — what crosses, and in what shape.
- [ABI contract](/docs/abi-contract/) — the registers, if you ever need to
  read the generated assembly.
- [Stateful objects](/tutorials/stateful-objects/) — the next tutorial, where
  the C++ side keeps state behind a handle.
- [Safety](/docs/safety/) — the GC and preemption argument, in full.
