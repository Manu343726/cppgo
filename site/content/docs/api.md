---
title: "API reference"
description: "The public Go API — Library, ProbeStack, wire helpers and error types — and the generator's directives and output."
weight: 80
---

The public surface is deliberately small: a handle for a loaded library, three
helpers for the memory that crosses the boundary, one stack prober, and the
error types. Everything else in a binding is generated.

Full godoc: [pkg.go.dev/github.com/Manu343726/cppgo](https://pkg.go.dev/github.com/Manu343726/cppgo).

## Loading a library

```go
type Library struct{ /* opaque */ }

func Open(path string) (*Library, error)
func (l *Library) Close() error
func (l *Library) Path() string
func (l *Library) Symbol(name string) (uintptr, error)
func (l *Library) MustSymbol(name string) uintptr
```

**`Open(path string) (*Library, error)`** loads the shared object at `path` and
returns a handle to its symbols. The path is passed to the dynamic loader as
given, so it may be absolute, a name on the loader's search path such as
`libm.so.6`, or a relative path containing a slash. An empty path is rejected
rather than being handed to `dlopen`, which would return the main program.

**`Close() error`** unloads the library. Every later `Symbol` call fails with
`ErrClosed`, and so does a second `Close`.

**`Path() string`** returns the path the library was opened with.

**`Symbol(name string) (uintptr, error)`** resolves a name to the address of a
foreign function or variable. The result is a code or data pointer inside the
loaded library — not a Go pointer, and not something to store in Go memory the
collector has to trace.

**`MustSymbol(name string) uintptr`** is `Symbol` for generated initialisation
code: it panics instead of returning an error, because a missing symbol in a
library the program was built against is a fatal configuration mistake.

A `Library` is safe for concurrent use: symbols can be resolved from many
goroutines at once, and `Close` is serialised against them.

## The stack guarantee

```go
// ProbeStack guarantees at least cppgo.StackReserve bytes of goroutine stack
// headroom below the caller, so a foreign call cannot grow past stack.lo.
func ProbeStack()

const StackReserve = 32 << 10
```

`ProbeStack` forces the goroutine's stack to grow to at least `StackReserve`
bytes. It is cheap (~ns) after the first call on a goroutine, because the stack
has already grown and the work is a frame adjustment plus one store per page.
Generated wrappers call it **before** the trampoline, every time; a hand-written
binding must do the same. See [safety](/docs/safety/) for how it works and why
`//go:noinline` is load-bearing.

## Wire helpers

Used by generated code, and exported so users can hand-write bindings:

```go
func Ptr[T any](s []T) unsafe.Pointer
func StrPtr(s string) unsafe.Pointer
func KeepAlive(...any)
```

**`Ptr`** returns a pointer to the first element of `s`, the form a `[]T` takes
on its way across the boundary, or `nil` when `s` is empty. The pointer borrows
the slice's backing array: it stays valid only for the duration of the foreign
call, the callee must not retain it, and the caller must keep `s` alive across
the call with `KeepAlive`.

**`StrPtr`** returns a pointer to the bytes of `s`, for a function that takes a
string as a `(pointer, length)` pair. Strings are **not** NUL-terminated; a
function that wants a C string needs an explicit terminator in a `[]byte`.

**`KeepAlive`** keeps its arguments reachable until the call returns, so memory
a foreign call was handed is not collected or moved while the callee is still
reading it. Call it **after** the trampoline, never before — the point is to
extend liveness past the call, and calling it earlier lets the compiler end the
value's life before the call happens.

## Errors

```go
func LastError(lib *Library) error

var ErrClosed = /* ... */

type OpenError struct {
	Path string
	Err  error
}

type SymbolError struct {
	Name string
	Err  error
}

type NativeError struct {
	Message string
}
```

**`LastError(lib)`** reads `cppgo_last_error()` from `lib` and returns it as an
error. It returns `nil` when the C string is empty or the symbol is absent — a
library that cannot fail does not need to export one. The message is copied out
immediately, because the C string is thread-local and only valid until the next
call into the library.

**`ErrClosed`** is returned by every `Library` method that needs the loaded
library once `Close` has run, and by a second `Close`.

**`OpenError`** reports a library that could not be loaded; `Path` is what was
passed to `Open`. **`SymbolError`** reports a symbol that could not be resolved;
`Name` is what was requested. Both implement `Unwrap`, so `errors.Is` and
`errors.As` work through them. **`NativeError`** is a failure reported by the
foreign library through the `cppgo_last_error` convention; its `Message` is the
C string verbatim, and generated wrappers add the symbol name on top as
`sym: message`.

## Generator

```console
$ cppgo gen -dir <pkg> [-out <dir>]
```

Input is a normal Go file with directives:

```go
//cppgo:library libmathx

//cppgo:external dot
func Dot(a, b []float64) (float64, error)
```

`//cppgo:library <name>` is package-level and appears once; it names the shared
object the package binds to. `//cppgo:external <symbol>` marks the next
function as foreign and names the C symbol to resolve.

Output, written into the package directory (or `-out`):

| file | contents |
|---|---|
| `zz_cppgo.gen.go` | idiomatic wrappers and symbol binding |
| `zz_cppgo_amd64.s` | one `NOSPLIT` trampoline per function |
| `cppgo_shim.h` | the `extern "C"` declarations your C++ implements |
| `zz_cppgo_stub.go` | the original functions rewritten to delegate, with no panicking stub bodies left behind |

The generated files are build artifacts. The annotated Go file is the source of
truth: edit that, re-run `cppgo gen`, and do not patch the output.
