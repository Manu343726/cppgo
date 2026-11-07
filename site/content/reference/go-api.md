+++
title = "The cppgo package"
description = "Every exported identifier of the runtime package with its exact signature and behaviour: Open, Library, Symbol, Close, LastError, Handle, ProbeStack, StackReserve, Ptr, StrPtr, KeepAlive and the error types."
weight = 10
+++

```go
import "github.com/Manu343726/cppgo"
```

The package is three things: a library handle, the wire helpers that move Go
memory across the boundary safely, and the error types. It contains no
generated code — the generator emits code that *uses* this package.

Everything below is exported. `internal/asm`, `internal/rtl`, `internal/cxx` and
`internal/gen` are not importable, by design: the assembly trampoline, the
`dlopen` wrapper, libclang and the generator are implementation.

## `Open`

```go
func Open(path string) (*Library, error)
```

Loads the shared object at `path` and returns a handle to its symbols. The path
is passed to the dynamic loader **as given**: absolute, a name on the loader's
search path such as `libm.so.6`, or a relative path containing a slash.

`Open("")` does not call `dlopen`. It returns:

```go
*cppgo.OpenError{Path: "", Err: errors.New("empty library path")}
```

because `dlopen("")` hands back the main program, which is never what the caller
meant. A loader failure is wrapped in `*OpenError` with the original error
available through `Unwrap`.

## `Library`

```go
type Library struct{ /* opaque */ }
```

A loaded shared object. It is safe for concurrent use: `Symbol` may be called
from many goroutines at once, and `Close` is serialised against them. What
`Close` cannot do is wait for calls that are already running inside the library
— see [lifetimes]({{< ref "manual/lifetimes.md" >}}).

### `(*Library) Path`

```go
func (l *Library) Path() string
```

The path the library was opened with, exactly as it was passed to `Open`.

### `(*Library) Symbol`

```go
func (l *Library) Symbol(name string) (uintptr, error)
```

Resolves `name` to the address of the foreign function or variable. The returned
address is a code or data pointer inside the loaded library. **It is not a Go
pointer** and must not be stored in Go memory the garbage collector has to trace.

Errors:

- a closed library returns `*SymbolError{Name: name, Err: ErrClosed}`, and
  `errors.Is(err, cppgo.ErrClosed)` is true;
- a missing symbol returns `*SymbolError{Name: name, Err: <loader error>}`.

### `(*Library) MustSymbol`

```go
func (l *Library) MustSymbol(name string) uintptr
```

`Symbol` for generated initialisation code: it panics with the `*SymbolError`
instead of returning it. A missing symbol in a library the program was built
against is a fatal configuration mistake, not a value to handle.

### `(*Library) Close`

```go
func (l *Library) Close() error
```

Unloads the library. Every later `Symbol` call fails with `ErrClosed`, and so
does a **second** `Close`:

```go
lib.Close()        // nil, the library unloads
err := lib.Close() // ErrClosed, not the loader error
```

The library is marked closed *before* `dlclose` runs: if unloading itself fails,
the handle is gone either way, and reporting the library as still open would
invite a use-after-free.

## `LastError`

```go
func LastError(lib *Library) error
```

Reads `cppgo_last_error()` from `lib` and returns it as an error. It returns
`nil` in three cases:

- `lib` is nil;
- the library does not export `cppgo_last_error` (a library that cannot fail);
- the C string is empty (no error has been recorded).

Otherwise it returns `*NativeError`. The message is copied out immediately,
because the C string is only valid until the next call into the library.

This is the only function in the package that performs a foreign call of its
own. It calls `ProbeStack` first, for the same reason the generated wrappers do.

## `Handle`

```go
type Handle uintptr

func (h Handle) IsNil() bool
```

An opaque reference to a foreign object: a C++ instance, a context, an iterator —
anything whose layout the Go side must not know.

- It is a `uintptr`, not a pointer, so the collector and `vet` reason about it as
  a number.
- **It carries no ownership.** Nothing frees the object when the handle becomes
  unreachable. For every constructor there must be a matching destroy, and Go is
  responsible for calling it.
- **There is no finalizer**, deliberately: finalizers run at unpredictable times
  on arbitrary goroutines, possibly while the library is closing.
- **The zero value is null.** A failing constructor is expected to leave it
  behind; generated code never synthesises a handle.

`IsNil` reports `h == 0`.

## `ProbeStack` and `StackReserve`

```go
const StackReserve = 32 << 10   // 32768 bytes

func ProbeStack()
```

`ProbeStack` guarantees at least `StackReserve` bytes of goroutine stack headroom
below the point it is called from, so a foreign call cannot grow past
`stack.lo`. A foreign call runs on the goroutine's own stack, and the Go runtime
cannot grow it while C++ is running.

The mechanism is an assembly function with a 32 KiB frame — not a Go function
with a 32 KiB local array, because the compiler zeroes a large local array on
entry (~1.2 µs per call, measured) and the probe must cost the same order as the
call it protects. The first call on a goroutine grows its stack through the
runtime's normal path; every later call is a frame adjustment.

`ProbeStack` protects nothing unless it is called on the same goroutine, at a
stack depth at or above the foreign call.

## Wire helpers

These exist so generated code and hand-written bindings can pass Go memory across
the boundary without keeping a pointer in a place the collector will not see.

### `Ptr`

```go
func Ptr[T any](s []T) unsafe.Pointer
```

A pointer to the first element of `s`, or `nil` when `s` is empty. This is how a
`[]T` starts its way across: pointer first, length beside it.

The pointer borrows the slice's backing array. It stays valid only for the
duration of the foreign call, the callee must not retain it, and the caller must
keep `s` alive across the call with `KeepAlive`.

### `StrPtr`

```go
func StrPtr(s string) unsafe.Pointer
```

A pointer to the bytes of `s`, or `nil` when `s` is empty. The pointer borrows
the string and has the same lifetime rules as `Ptr`.

Strings are **not NUL-terminated**: a function that wants a C string needs an
explicit terminator in a `[]byte` you own. The length is passed separately.

### `KeepAlive`

```go
func KeepAlive(v ...any)
```

Keeps its arguments reachable until the call returns, so memory a foreign call
was handed is not collected or moved while the callee is still reading it.

Call it **after** the trampoline, never before:

```go
cppgo.ProbeStack()
n, _ := asm.Call(...)   // internal, for illustration
cppgo.KeepAlive(buf)
```

Generated wrappers emit exactly that ordering. `KeepAlive` is a wrapper around
`runtime.KeepAlive`.

## Errors

### `ErrClosed`

```go
var ErrClosed error
```

Returned by every `Library` method that needs the loaded library once `Close`
has run, and by a second `Close`. It is the same value the underlying loader
shim uses, so `errors.Is` covers both layers:

```go
if errors.Is(err, cppgo.ErrClosed) { /* the library was closed */ }
```

### `OpenError`

```go
type OpenError struct {
	Path string // the path that was passed to Open
	Err  error  // the underlying loader error
}

func (e *OpenError) Error() string  // `cppgo: open "<path>": <err>`
func (e *OpenError) Unwrap() error  // e.Err
```

`errors.As(err, &openErr)` recovers it; `errors.Is` reaches the loader error
through `Unwrap`.

### `SymbolError`

```go
type SymbolError struct {
	Name string // the symbol that was requested
	Err  error  // the loader error, or ErrClosed
}

func (e *SymbolError) Error() string  // `cppgo: symbol "<name>": <err>`
func (e *SymbolError) Unwrap() error  // e.Err
```

`errors.Is(err, cppgo.ErrClosed)` works for a symbol lookup after close because
`Unwrap` returns exactly that sentinel in that case.

### `NativeError`

```go
type NativeError struct {
	Message string // the text the foreign library stored, without the NUL
}

func (e *NativeError) Error() string // e.Message
```

A failure reported by the foreign library through the `cppgo_last_error`
convention. The message is the C string verbatim; generated wrappers add the
symbol or qualified C++ name on top with `fmt.Errorf("%s: %w", ...)`.

`NativeError` has **no `Unwrap`**, so `errors.Is` only matches the exact value;
use `errors.As` to recover the message:

```go
var native *cppgo.NativeError
if errors.As(err, &native) {
	fmt.Println("C++ said:", native.Message)
}
```

## What is not exported

- The assembly layer (`internal/asm`) and the `dlopen` wrapper (`internal/rtl`).
- The libclang bindings and the symbol model (`internal/cxx`).
- The generator (`internal/gen`) and its configuration.
- `cppgo_last_error` itself: it is a C symbol the library may export, not a Go
  identifier. `LastError` is the Go door to it.

A hand-written binding uses `Library`, the wire helpers and `asm`-equivalent
trampoline code that you would have to write yourself — which is why generated
code exists. For the canonical, versioned documentation of these identifiers,
see [pkg.go.dev](https://pkg.go.dev/github.com/Manu343726/cppgo).

Next: [the generated files]({{< ref "reference/generated-code.md" >}}).
