---
title: "Runtime"
description: "The cppgo package: loading, calls, objects, wire helpers and errors."
weight: 30
---

Generated packages import one non-standard package:
`github.com/Manu343726/cppgo`. This page is its surface, copied from the package
source. It has no cgo and no libclang dependency; the only third-party
dependency is `github.com/ebitengine/purego v0.11.3` (`go.mod`), used for
`dlopen`/`dlsym` only.

```go
import "github.com/Manu343726/cppgo"
```

## Loading a shared object

```go
// Open loads the shared object at path and returns a handle to its symbols.
//
// The path is passed to the dynamic loader as given. It may be absolute, a
// name on the loader's search path such as "libm.so.6", or a relative path
// containing a slash.
func Open(path string) (*Library, error)

// Path returns the path the library was opened with.
func (l *Library) Path() string

// Close unloads the library. Every later Symbol call fails with ErrClosed, and
// so does a second Close.
func (l *Library) Close() error

// Symbol resolves name to the address of the foreign function or variable.
func (l *Library) Symbol(name string) (uintptr, error)

// MustSymbol is Symbol for generated initialisation code. It panics on a
// missing symbol.
func (l *Library) MustSymbol(name string) uintptr

// LastError reads cppgo_last_error from lib and returns it as an error.
func LastError(lib *Library) error
```

A `Library` is safe for concurrent use. `Close` waits for no call already running
inside the library, so closing a library another goroutine is still calling is a
use-after-free; close once, after the work is done.

`LastError` is the accessor a generated binding uses after a thunk reports a
non-zero status. It returns `nil` when the library has no such symbol (a library
that cannot fail) and when the stored C string is empty, and it copies the
message out immediately: the C string is valid only until the next call into the
library.

## Embedded objects

```go
// OpenFS loads a shared object that is embedded in the program, materializing
// every shared object of fsys in a private cache first.
func OpenFS(fsys fs.FS, name string) (*Library, error)

// CacheDir returns the directory below which OpenFS extracts embedded shared
// objects: os.UserCacheDir()/cppgo, one subdirectory per embedded set.
func CacheDir() (string, error)

// CleanCache removes every library OpenFS has extracted for the current user.
func CleanCache() error

// LibraryPathEnv is the environment variable that overrides OpenFS with an
// explicit library path.
const LibraryPathEnv = "CPPGO_LIBRARY_PATH"
```

The cache layout is `os.UserCacheDir()/cppgo/<sha256 of the whole set>/<name>`;
directories are `0700`, objects `0600`, and a file is reused only when its size
and SHA-256 match the embedded bytes. The full contract, including `$ORIGIN`, is
in [Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Stack headroom

```go
// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from.
const StackReserve = 32 << 10   // 32 KiB

// ProbeStack guarantees at least StackReserve bytes of goroutine stack
// headroom below the caller.
func ProbeStack()
```

A foreign call runs on the goroutine's stack and the trampoline switches the
stack pointer, so the runtime cannot grow the stack for the C frames. The
generated trampolines call `ProbeStack` before every call, from the same
goroutine and before any pointer into the caller's frame is taken. If you write a
binding by hand, the order is your responsibility.

## Objects

```go
// NewObject allocates size bytes of zeroed storage aligned to align, pins it,
// and returns an object whose reference count starts at one.
func NewObject(size, align uintptr, release func()) *Object

// Addr returns the pinned, aligned address of the storage, or 0 for a nil
// object.
func (o *Object) Addr() uintptr

// Retain adds one reference. It is what a view does when it shares ownership
// with the object it was derived from.
func (o *Object) Retain()

// Release drops one reference. When the last one goes, the object's release
// function runs and the storage is unpinned.
func (o *Object) Release()
```

`Object` pins its storage with `runtime.Pinner`, so the address a generated
constructor passes to C++ does not move. There is no package-level object
registry; the one piece of process-global state in the runtime is the callback
table.

## Wire helpers

```go
// Ptr returns a pointer to the first element of s, or nil when s is empty.
func Ptr[T any](s []T) unsafe.Pointer

// StrPtr returns a pointer to the bytes of s.
func StrPtr(s string) unsafe.Pointer

// KeepAlive keeps its arguments reachable until the call returns.
func KeepAlive(v ...any)
```

A pointer from `Ptr` borrows the slice's backing array and is valid only for the
duration of the call; the callee must not retain it. `KeepAlive` is called after
the call, so the memory stays alive and in place while the callee reads it.

## Calls and callbacks

```go
// Args is one C argument frame: six INTEGER and eight SSE argument registers in
// I and F, and the two result registers in Ret and RetF.
type Args = asm.Args

// CallReentrant invokes the foreign function at fn with the arguments in a,
// through runtime.cgocall. The callee may then call back into Go.
func CallReentrant(fn uintptr, a *Args)

// RegisterCallback makes fn callable from C++ and returns its code address.
func RegisterCallback(fn func(*Args)) (uintptr, error)

// ReleaseCallback removes a callback registered by RegisterCallback, waiting for
// the calls already inside it to return.
func ReleaseCallback(code uintptr) error

// TakeCallbackPanic returns the panic the most recent callback captured and
// clears it.
func TakeCallbackPanic() *CallbackPanic

// CallbackPanic is a panic a callback raised.
type CallbackPanic = callback.Panic
```

`CallReentrant` is the carrier of every binding whose signature passes a Go
function to C++. The runtime only accepts a callback on a thread that is already
inside a foreign call, and this entry establishes that state. It is the slow
path by design: 151 ns per call against 15.9 ns for the direct carrier, with one
16-byte allocation, because the argument block cannot live on a goroutine stack a
callback may grow (measured 2026-11-08; see the
[carrier table]({{< ref "reference/carriers.md" >}})).

## Errors

| Type or value | What it reports |
|---|---|
| `ErrClosed` | a `Library` method after `Close`, a second `Close`, and a method on a nil or closed generated value |
| `*OpenError` | a library that could not be loaded. `Path` and `Err` |
| `*SymbolError` | a symbol that could not be resolved. `Name` and `Err` |
| `*ExtractError` | an embedded object that could not be materialised. `Name`, `Dir`, `Err` |
| `ErrInsecureCache` | a cache path that fails the private-directory check |
| `*NativeError` | a failure the library reported through `cppgo_last_error`, with the C string verbatim in `Message` |
| `*CallError` | a bound call that failed. `Symbol` names the C++ declaration, `Err` wraps the native failure |

All of them implement `Unwrap` except the sentinels, so `errors.As` finds the
inner failure and `errors.Is` works on `ErrClosed`.

```go
type CallError struct {
	Symbol string
	Err    error
}
```

`CallError` formats as `cppgo: <symbol>: <message>`.

## The low-level handle

```go
type Handle uintptr
func (h Handle) IsNil() bool
```

`Handle` is for memory whose layout the Go side cannot see. It carries no
ownership and has no finalizer, and **a generated binding never puts one in a
signature**. Use it only in a hand-written binding.

## Related

- What a generated package imports and calls: [Generated API]({{< ref "reference/generated-api.md" >}}).
- How errors reach Go: [Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}).
