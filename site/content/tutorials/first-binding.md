+++
title = "Your first binding"
description = "Bind one free C++ function and call it from Go: the header, the declaration, the generator run, the report line, and a program that prints the result."
weight = 10
+++

This tutorial binds one free function — the smallest end-to-end path. No objects,
no slices, no library layout to think about. The point is to see the complete
loop: a declaration, a generator run, a symbol, a call.

## The C++ side

<span class="filename">include/mathx/add.hpp</span>

```cpp
#pragma once

namespace mathx {

// Adds two 32-bit integers.
int add(int a, int b);

} // namespace mathx
```

<span class="filename">src/add.cpp</span>

```cpp
#include "mathx/add.hpp"

namespace mathx {

int add(int a, int b) { return a + b; }

} // namespace mathx
```

```console
$ g++ -std=c++17 -O2 -fPIC -shared -Iinclude -o build/libmathx.so src/add.cpp
$ nm -D --defined-only build/libmathx.so | grep add
0000000000001140 T _ZN5mathx3addEii
```

That `_ZN5mathx3addEii` is the whole ABI in one string: namespace `mathx`,
function `add`, two `int` parameters. You will not type it anywhere — libclang
produces it — but seeing it once makes the rest of the tooling legible.

## The Go side

```console
$ go mod init example.com/adddemo
$ go get github.com/Manu343726/cppgo
```

<span class="filename">bindings/mathx/mathx.go</span>

```go
package mathx

import "github.com/Manu343726/cppgo"

//cppgo:library libmathx
//cppgo:header ../../../include/mathx/add.hpp

// Add returns a + b.
//
//cppgo:function mathx::add
func Add(a, b int32) int32
```

Why `int32` and not `int`: the C++ function takes `int`, which is 4 bytes on
LP64. Go `int` is 8 bytes. The generator compares widths and refuses a mismatch
instead of truncating, so the declaration has to match the C++ type. A C++
`long` or `size_t` would be `int64`; a `double` would be `float64`.

## Generate

```console
$ cppgo gen -dir ./bindings/mathx
mathx::add       mangled _ZN5mathx3addEii       abi from fallback flags (assumed)
```

Read the line as three answers:

- **`mathx::add`** — the declaration libclang found, qualified.
- **`mangled _ZN5mathx3addEii`** — the exact symbol the trampoline will resolve.
  If this were empty, the entity would have no ABI name (a template that was
  never instantiated).
- **`abi from fallback flags (assumed)`** — the header was parsed with
  `-std=c++17 -I<header dir>` because there is no `compile_commands.json` or
  `.clangd`. For a header with no includes of its own, that is enough. On a real
  project, pass `-compile-commands ./build`.

There is also a warning on stderr:

```
cppgo: warning: the compilation flags for /home/you/adddemo/include/mathx/add.hpp
are an assumption (assumed: -std=c++17 -I/home/you/adddemo/include/mathx); a
validation this misses comes from a parse without the header's real flags
```

It is a warning, not an error, and it is worth silencing in a real project.

The directory now contains:

```
bindings/mathx/mathx.go          rewritten: func Add(...) { return cppgoCallAdd(a, b) }
bindings/mathx/zz_cppgo.gen.go
bindings/mathx/zz_cppgo_amd64.s
```

## Call it

<span class="filename">main.go</span>

```go
package main

import (
	"fmt"

	"example.com/adddemo/bindings/mathx"
)

func main() {
	mathx.CppgoLibraryPath = "./build/libmathx.so"
	fmt.Println(mathx.Add(2, 3))
}
```

```console
$ go run .
5
```

No `cgo` in the build:

```console
$ CGO_ENABLED=0 go build -o adddemo .
$ ./adddemo
5
```

The binary is dynamically linked because it loads `libmathx.so` at run time — but
it needed no C toolchain to build, and it contains no `import "C"`.

## What just happened

The generated wrapper did five things, in this order:

```go
func cppgoCallAdd(a int32, b int32) int32 {
	if err := cppgoEnsure(); err != nil { panic(err) }  // open + dlsym, once
	cppgo.ProbeStack()                                  // 32 KiB headroom
	cppgoRet := cppgo__ZN5mathx3addEii(cppgoSymAdd, int64(a), int64(b))
	return int32(cppgoRet)                              // narrow back to int32
}
```

- `cppgoEnsure` opened the library and resolved `_ZN5mathx3addEii` on the first
  call. A missing `.so` or a missing symbol would panic here, because the binding
  has no `error` result to return.
- `ProbeStack` made sure the C++ frame cannot grow past the goroutine's stack
  guard.
- `cppgo__ZN5mathx3addEii` is the generated trampoline: it moves the arguments
  into registers, switches to an aligned stack, calls the symbol and returns the
  result.
- The wrapper narrowed the `int64` mirror back to the declared `int32`.

That is the whole boundary. Why the trampoline is assembly, which register
carries what, and what the collector does with a foreign frame are in [How it
works]({{< ref "project/architecture.md" >}}) and [register
assignment]({{< ref "manual/calling-convention.md" >}}); this page is the
end-to-end path.

## Break it on purpose

Two experiments worth doing once, because they teach the failure modes:

**Wrong width.** Change the declaration to `func Add(a, b int) int` and
regenerate:

```
mathx.go:8: Add: parameter 1 (a int): the Go type is int (8-byte) but the C++
type is "int" (4-byte): the two must have the same width, because the value is
passed in one register
```

The call would have worked for small values and broken for large ones. The
generator refuses it instead.

**Wrong symbol.** Change the directive to `//cppgo:function mathx::sum`:

```
mathx::sum is not declared in .../add.hpp as a bindable function (a declaration
in a system header, or one with internal linkage, is invisible to the generator)
```

Nothing is emitted; there is no trampoline that could fail later.

## Where to go next

The function has no state, so there is no lifetime to manage. The next tutorial
adds a constructor and a destructor, and with them the first real question:
where does the C++ object live? See [stateful
objects]({{< ref "tutorials/stateful-objects.md" >}}).
