+++
title = "Your first binding"
description = "Bind one free C++ function and call it from Go: the header, one cppgo gen command, the generated Add, and why noexcept changes the Go signature."
weight = 10
+++

This tutorial binds one free function — the smallest end-to-end path. No
objects, no slices, no library layout to think about. The point is to see the
complete loop: a header, a generator run, a generated function, a call.

**Audience:** your first binding. You need libclang and a C++ compiler as
described in [installation]({{< ref "manual/installation.md" >}}).

## The C++ side

<span class="filename">include/mathx/add.hpp</span>

```cpp
#pragma once

namespace mathx {

// Add returns a + b.
int add(int a, int b) noexcept;

} // namespace mathx
```

<span class="filename">src/add.cpp</span>

```cpp
#include "mathx/add.hpp"

namespace mathx {

int add(int a, int b) noexcept { return a + b; }

} // namespace mathx
```

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Wl,-soname,libmathx.so \
      -Iinclude -o build/libmathx.so src/add.cpp
$ nm -D --defined-only build/libmathx.so | grep add
0000000000001140 T _ZN5mathx3addEii
```

That `_ZN5mathx3addEii` is the whole ABI in one string: namespace `mathx`,
function `add`, two `int` parameters. You will not type it anywhere — libclang
produces it — but seeing it once makes the rest of the tooling legible.

## Generate

There is no Go declaration to write. The header and the build flags are the
input:

```console
$ cppgo gen -header include/mathx/add.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::add(int, int) noexcept                direct _ZN5mathx3addEii             noexcept, out of line, not virtual, register class
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

Read the line as three answers:

- **`mathx::add(int, int) noexcept`** — the declaration libclang found, with
  the qualifier that decides the carrier.
- **`direct`** — the generated trampoline calls `_ZN5mathx3addEii` in your
  library. There is no C wrapper; the trampoline is the whole boundary.
- **`noexcept, out of line, not virtual, register class`** — why it is direct.
  Every one of those is required for the fast path.

## The Go package

```console
$ ls mathx
cppgo_shim  libcppgo_mathx.so  zz_cppgo_amd64.s  zz_cppgo.gen.go
```

The generated API is one function:

```go
func Open(path string) error
func Add(a int32, b int32) int32
```

Why `int32` and not `int`: the C++ function takes `int`, which is 4 bytes on
LP64. cppgo reads the width from the header and picks the Go type; you do not
translate types by hand.

`Add` has no `error` because the C++ declaration is `noexcept`: a call that
cannot throw cannot fail. Here is the generated body — the whole boundary:

```go
func Add(a int32, b int32) int32 {
	if err := cppgoOpen(""); err != nil {
		panic(err)
	}
	cppgo.ProbeStack()
	cppgoRet := cppgoCall_b315eb6ce761(cppgoSym_b315eb6ce761, int64(a), int64(b))
	return int32(cppgoRet)
}
```

1. **`cppgoOpen`** loads the shim and the user's library and resolves the symbol
   on the first call; later calls read the cached result.
2. **`ProbeStack`** makes sure the C++ frame cannot grow past the goroutine's
   stack guard.
3. **`cppgoCall_…`** is the generated trampoline: it moves the arguments into
   registers, switches to an aligned stack, calls the symbol and returns the
   result.
4. **The cast back** narrows the 64-bit mirror to the declared `int32`.

That is the whole boundary. Why the trampoline is assembly, which register
carries what, and what the collector does with a foreign frame are in [How it
works]({{< ref "project/architecture.md" >}}) and [register
assignment]({{< ref "manual/calling-convention.md" >}}); this page is the
end-to-end path.

## Call it

<span class="filename">main.go</span>

```go
package main

import (
	"fmt"
	"log"

	"example.com/adddemo/mathx"
)

func main() {
	if err := mathx.Open("./mathx/libcppgo_mathx.so"); err != nil {
		log.Fatal(err)
	}
	fmt.Println(mathx.Add(2, 3))
}
```

```console
$ cp build/libmathx.so ./mathx/   # the shim names it through DT_NEEDED
$ go run .
5
```

No cgo in the build:

```console
$ CGO_ENABLED=0 go build -o adddemo .
$ ./adddemo
5
```

The binary is dynamically linked because it loads `libmathx.so` at run time — but
it needed no C toolchain to build, and it contains no `import "C"`.

## Break it on purpose

Two experiments worth doing once, because they teach the rules.

**Remove `noexcept`.** Change the header to `int add(int a, int b);`, rebuild and
regenerate:

```console
$ cppgo gen -header include/mathx/add.hpp -package mathx \
      -library build/libmathx.so -out ./mathx
mathx::add(int, int)                         thunk  cppgo_thunk_924fc10357e9     not noexcept: an exception would unwind into Go
```

and the Go signature becomes:

```go
func Add(a int32, b int32) (int32, error)
```

Nothing else changed. The thunk now catches a `std::exception`, so the call can
report a failure and the signature says so. That is the v4 rule in one
experiment: **the C++ declaration decides the Go shape, not a declaration you
write.**

**Add an enum.** A header that declares an enum anywhere is refused today:

```cpp
enum class Unit { Metre, Foot };
```

```
cppgo gen: cppgo: include/mathx/add.hpp:5: mathx::Unit: enums are not emitted
by this build yet; pass the value as its underlying integer at the C++ boundary
```

Nothing is emitted; there is no package that could fail later. The full list of
current refusals is in [validation]({{< ref "manual/validation.md" >}}).

## Where to go next

The function has no state, so there is no lifetime to manage. The next tutorial
adds a class, a constructor and a `Close`, and with them the first real
question: where does the C++ object live? See [stateful
objects]({{< ref "tutorials/stateful-objects.md" >}}).
