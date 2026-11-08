+++
title = "The cppgo package"
description = "Every exported identifier of the runtime package with its exact signature and behaviour: Open, OpenFS, Library, Object, LastError, CallReentrant, the callback table, the wire helpers and the error types."
weight = 10
+++

```go
import "github.com/Manu343726/cppgo"
```

**Audience:** you are writing a binding by hand, reading the generated `Open`,
or handling an error the generated package returned.

The package is four things: the loader (`Open`, `OpenFS`, `Library`), the
storage an opaque C++ value lives in (`Object`), the callback and reentrant-call
entry points, and the error types. It contains no generated code — the generator
emits code that *uses* this package.

Everything below is exported. `internal/asm`, `internal/rtl`, `internal/cxx` and
`internal/gen` are not importable, by design: the assembly trampolines, the
`dlopen` wrapper, libclang and the generator are implementation.

## `Open`

```go
func Open(path string) (*Library, error)
```

Loads the shared object at `path` and returns a handle to its symbols. The path
is passed to the dynamic loader **as given**: absolute, a name on the loader's
search path such as `libm.so.6`, or a relative path containing a slash.

`Open("")` does not call `dlopen`. It returns
`*cppgo.OpenError{Path: "", Err: ...}` with the message `empty library path`,
because `dlopen("")` hands back the main program, which is never what the caller
meant. A loader failure is wrapped in `*OpenError` with the original error
available through `Unwrap`.

## `OpenFS`

```go
func OpenFS(fsys fs.FS, name string) (*Library, error)
```

Loads a shared object that is **embedded in the program**. It materialises every
`.so` in `fsys` in a private per-user cache and then loads `name` from there.
`//go:embed` is the intended source; `fstest.MapFS` and `os.DirFS` are the same
API in tests and during development.

- **Cache layout:** `os.UserCacheDir()/cppgo/<sha256 of the whole set>/<name>`.
  The digest covers every file's name, size and content, so a change to any byte
  lands in a new directory and two programs with the same bytes share one entry.
- **Permissions:** the cache root and entry directories are `0700`, every object
  `0600`. A loose mode is tightened; a path that is not a private directory (a
  symlink, a regular file) is refused with `cppgo.ErrInsecureCache`.
- **Atomicity:** an object is written to a temporary file in the same directory
  and renamed into place. An existing file is reused only when its size and
  SHA-256 match the embedded bytes; a corrupted entry is replaced.
- **Siblings:** every embedded object lands in one flat directory, and `$ORIGIN`
  is how one object finds another. The objects must be linked with
  `-Wl,-rpath,$ORIGIN` for that to work; the cppgo shim already is.
- **The limit:** the program is still dynamically linked to libc. Embedding makes
  the **C++ side** one file to ship, not the binary static.

`CPPGO_LIBRARY_PATH` (exported as `cppgo.LibraryPathEnv`) overrides it with an
explicit path and extracts nothing.

## `Library`

```go
type Library struct{ /* opaque */ }

func (l *Library) Path() string
func (l *Library) Symbol(name string) (uintptr, error)
func (l *Library) MustSymbol(name string) uintptr
func (l *Library) Close() error
```

A loaded shared object. It is safe for concurrent use: `Symbol` may be called
from many goroutines at once, and `Close` is serialised against them. What
`Close` cannot do is wait for calls already running inside the library — see
[object lifetimes]({{< ref "manual/lifetimes.md" >}}).

- `Path` is the path the library was opened with, exactly as passed.
- `Symbol` resolves a name to a code or data pointer inside the loaded library.
  **It is not a Go pointer** and must not be stored in Go memory the collector
  has to trace. A closed library returns `*SymbolError{Name, Err: ErrClosed}`; a
  missing symbol returns `*SymbolError{Name, Err: <loader error>}`.
- `MustSymbol` is `Symbol` for generated initialisation code: it panics with the
  `*SymbolError` instead of returning it.
- `Close` unloads the library. Every later `Symbol` fails with `ErrClosed`, and
  so does a **second** `Close`. The library is marked closed *before* `dlclose`
  runs, so a failed unload cannot leave a handle that looks open.

## `LastError`

```go
func LastError(lib *Library) error
```

Reads `cppgo_last_error()` from `lib` and returns it as `*cppgo.NativeError`. It
returns nil when `lib` is nil, when the library does not export the symbol (a
library that cannot fail), and when the C string is empty (no error recorded).
The message is copied out immediately, because the C string is only valid until
the next call into the library.

This is the only function in the package that performs a foreign call of its
own. It calls `ProbeStack` first, for the same reason the generated wrappers
do.

## `Object`

```go
type Object struct{ /* opaque */ }

func NewObject(size, align uintptr, release func()) *Object
func (o *Object) Addr() uintptr
func (o *Object) Retain()
func (o *Object) Release()
```

Storage a C++ value lives in whose layout Go does not know. The generated
package creates one for every opaque bound record: a Go allocation, aligned and
pinned with `runtime.Pinner` so its address does not move, handed to C++ as
`this`.

- `NewObject` allocates `size` zeroed bytes aligned to `align` and returns an
  object whose reference count is one. `align` 0 means 1; a size of 0 still
  yields a valid non-nil address, because C++ permits empty objects.
- `release` runs exactly once, when the last reference goes. It is the concrete
  destructor; a nil release is allowed for storage that needs none.
- `Addr` is the pinned, aligned address, or 0 for a nil object.
- `Retain` adds a reference (what a view does); `Release` drops one. Releasing a
  nil object is a no-op; releasing an object whose count is already zero does
  nothing more.

The zero `Object` is not usable, and `Object` values are never copied: hold a
`*Object`.

## `CallReentrant` and the callback table

```go
type Args = asm.Args

func CallReentrant(fn uintptr, a *Args)
func RegisterCallback(fn func(*Args)) (uintptr, error)
func ReleaseCallback(code uintptr) error
func TakeCallbackPanic() *CallbackPanic
```

These are the **reentrant carrier**: a binding whose signature passes a Go
function to C++ cannot use the direct trampoline, because the runtime only
accepts a callback on a thread already inside a foreign call.

- `Args` is one C argument frame: the six INTEGER argument registers in `I`, the
  eight SSE registers in `F`, and the results in `Ret` (RAX) and `RetF` (XMM0).
  A generated binding fills them, calls, and reads the results.
- `RegisterCallback` makes a Go function callable from C++ and returns its code
  address: a slot in a process-wide table of 64. Registering the same function
  twice gives two independent slots. `ReleaseCallback` removes one and waits for
  the calls already inside it to return.
- A **panic** inside a callback never unwinds into C++: the dispatcher recovers
  it, zeroes the result and records it. The binding calls `TakeCallbackPanic`
  after the foreign call returns and re-panics with `Value`.

The ABI is the register file only: at most six integer and eight floating-point
arguments and one register result (see [the callback ABI]({{< ref
"project/architecture.md" >}})).

## `ProbeStack` and `StackReserve`

```go
const StackReserve = 32 << 10   // 32768 bytes

func ProbeStack()
```

`ProbeStack` guarantees at least `StackReserve` bytes of goroutine stack headroom
below the point it is called from, so a foreign call cannot grow past
`stack.lo`. The mechanism is an assembly function with a 32 KiB frame — not a Go
function with a 32 KiB local array, because the compiler zeroes a large local
(~1.2 µs per call, measured) and the probe must cost the same order as the call
it protects.

It protects nothing unless it is called on the same goroutine, at a stack depth
at or above the foreign call.

## Wire helpers

```go
func Ptr[T any](s []T) unsafe.Pointer
func StrPtr(s string) unsafe.Pointer
func KeepAlive(v ...any)
```

- `Ptr` is a pointer to the first element of `s`, or nil when `s` is empty. It
  borrows the slice: valid only for the foreign call, and the callee must not
  retain it.
- `StrPtr` is a pointer to the bytes of `s`, or nil when `s` is empty. Strings
  are **not NUL-terminated**; a function that wants a C string needs an explicit
  terminator in a `[]byte` you own.
- `KeepAlive` keeps its arguments reachable until the call returns. Call it
  **after** the trampoline, never before. It wraps `runtime.KeepAlive`.

## `Handle`

```go
type Handle uintptr

func (h Handle) IsNil() bool
```

A low-level opaque reference to a foreign object. It is a `uintptr` rather than a
pointer on purpose: the memory is foreign and the collector must not trace it.
It carries no ownership and has no finalizer.

`cppgo.Handle` appears in **no generated signature**. A generated class holds a
`*cppgo.Object` instead. The type remains exported for a hand-written binding
that has only an integer handle to carry.

## Errors

### `ErrClosed`

```go
var ErrClosed error
```

Returned by every `Library` method that needs the loaded library after `Close`,
by a second `Close`, and by a generated method on a nil or closed object. The
same value is the loader's sentinel, so `errors.Is` covers both layers.

### `CallError`

```go
type CallError struct {
	Symbol string // the C++ declaration, e.g. "mathx::Vector::norm"
	Err    error  // normally *NativeError
}

func (e *CallError) Error() string  // "cppgo: <symbol>: <message>"
func (e *CallError) Unwrap() error  // e.Err
```

What a generated binding returns when a thunk reports a non-zero status.
`errors.As` recovers it and reaches the native message through `Unwrap`.

### `NativeError`

```go
type NativeError struct { Message string }

func (e *NativeError) Error() string // e.Message
```

The text the foreign library stored through `cppgo_last_error`. It has **no
`Unwrap`**, so `errors.Is` only matches the exact value; use `errors.As`.

### `OpenError`, `SymbolError`, `ExtractError`

```go
type OpenError struct {
	Path string
	Err  error
}

type SymbolError struct {
	Name string
	Err  error
}

type ExtractError struct {
	Name string
	Dir  string
	Err  error
}
```

Each has `Error()` and `Unwrap()`. `OpenError` wraps a loader failure;
`SymbolError` wraps a failed `dlsym` or `ErrClosed`; `ExtractError` names the
object and the cache directory that could not take it.

### `ErrInsecureCache`

```go
var ErrInsecureCache error
```

`OpenFS` refuses a cache path that is not a private directory: a symlink, a
regular file in its place, or a directory whose mode cannot be tightened.

## What is not exported

- The assembly layer (`internal/asm`) and the `dlopen` wrapper (`internal/rtl`).
- The libclang bindings and the C++ model (`internal/cxx`).
- The generator (`internal/gen`) and its configuration.
- `cppgo_last_error` itself: it is a C symbol the shim exports, not a Go
  identifier. `LastError` is the Go door to it.

For the canonical, versioned documentation of these identifiers, see
[pkg.go.dev](https://pkg.go.dev/github.com/Manu343726/cppgo).

Next: [the generated files]({{< ref "reference/generated-code.md" >}}).
