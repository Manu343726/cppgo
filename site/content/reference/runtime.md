---
title: "Runtime"
description: "The cppgo package: loading, calls, objects, wire helpers and errors."
weight: 30
---

Generated packages import one non-standard package,
`github.com/Manu343726/cppgo`. This page documents its surface for a
**hand-written binding** — Go code that calls C++ without a package
`cppgo gen` wrote. A program that only calls a generated package does not need
the page: the generated file imports the package and calls it.

A **shared object** is the compiled form of a C++ library: a `.so` file that the
dynamic **loader** — the part of the operating system that starts a program and
maps in the libraries it needs — maps into the process at run time. A **shim** is
the shared object `cppgo gen` writes and builds: the C++ that carries the calls a
direct one cannot. A generated package loads both the shim and the user's library
the shim was linked against, and it resolves every address it calls through a
`Library`.

```go
import "github.com/Manu343726/cppgo"
```

## Loading a shared object

A `Library` is a handle to one loaded shared object; every address a binding
calls is resolved through it.

```go
// Open loads the shared object at path and returns a handle to its symbols.
// A hand-written binding calls it once, when it starts, and keeps the *Library.
//
// The path goes to the loader unchanged: it may be absolute, a name the loader
// searches for such as "libm.so.6", or a relative path containing a slash. An
// empty path is refused, because the loader reads "" as the main program.
func Open(path string) (*Library, error)

// Path returns the path the library was opened with; use it in a message that
// has to name the object a call went to.
func (l *Library) Path() string

// Close unloads the library. Every later Symbol call fails with ErrClosed, and
// so does a second Close. Call it once, after the work that uses the library is
// done.
func (l *Library) Close() error

// Symbol resolves name to the address of the foreign function or variable, as
// a uintptr: an integer, not a Go pointer. A hand-written binding calls it once
// per symbol, before the first call through that symbol.
func (l *Library) Symbol(name string) (uintptr, error)

// MustSymbol is Symbol for generated initialisation code: it panics on a
// missing symbol. A hand-written binding uses Symbol instead, and handles the
// error.
func (l *Library) MustSymbol(name string) uintptr

// LastError reads the message the library stored for its most recent failure.
// Call it after a call returned a failure status from the shim.
func LastError(lib *Library) error
```

A **symbol** is a name in the object's symbol table that resolves to an address:
the entry point of a function, or the storage of a variable. For C++ code the
name is the **mangled name** — the name the C++ compiler records for a
declaration, encoding its namespace, class and parameter types. A **foreign**
function is one compiled outside Go; the address `Symbol` returns is where its
first instruction starts.

A `uintptr` is an unsigned integer wide enough to hold a pointer. The address
`Symbol` returns is stored as an integer on purpose: the garbage collector does
not trace it and does not move what it points at. That is correct for foreign
memory and it is why the address must not outlive its `Library`. A `Library` is
safe for concurrent use, and resolution is cached: a name is resolved once,
whether the loader answered with an address or with an error. `Close` waits for
no call already running inside the library, so closing a library that another
goroutine is still calling is a **use-after-free** — the address of a function in
an object the loader has already unmapped is called. Close once, after the work
is over. Every later `Symbol` call fails with `ErrClosed`, the sentinel
[Errors](#errors) documents.

`LastError` returns `nil` when the library exports no message accessor (a
library that cannot fail) and when the stored message is empty. It copies the
message out immediately: the string belongs to the library, and it is valid only
until the next call into it.

A generated package usually has a bare library name and an override rather than
a path. `LibrarySearch` is that search order, and a hand-written binding that
deploys the same way uses it:

```go
// LibrarySearch is how a cppgo binding resolves the user's shared object: an
// explicit override first, CPPGO_LIBRARY_PATH next, an embedded set after that,
// and the loader's own search last.
type LibrarySearch struct {
	// Name is the bare library name: the soname the shim records, or the file
	// name of an object that declares none.
	Name string
	// Override is an exact path tried before anything else. Empty means no
	// override.
	Override string
	// FS is the embedded file system holding the library, or nil when the
	// package does not embed it.
	FS fs.FS
}

// Open resolves the library and loads it, the first match winning.
func (s LibrarySearch) Open() (*Library, error)
```

The soname is the name recorded inside a shared object that the loader looks it
up by. A name that nothing resolved is a `*SearchError` (see [Errors](#errors)),
which lists every path the overrides covered and names the two variables that
would change the answer:
[`CppgoLibraryPath`]({{< ref "reference/generated-api.md" >}}#package-entry), the
generated package's override, and `CPPGO_LIBRARY_PATH`.

## Embedded objects

A Go binary can carry its C++ side inside it. The `.so` files are embedded as a
file system — `fs.FS`, Go's read-only file-system interface — and written out to
a cache on first use. `OpenFS` is the call:

```go
// OpenFS loads a shared object that is embedded in the program, materializing
// every shared object of fsys in a private cache first. Use it when the program
// must arrive as a single file.
func OpenFS(fsys fs.FS, name string) (*Library, error)

// CacheDir returns the directory below which OpenFS extracts embedded shared
// objects: os.UserCacheDir()/cppgo, one subdirectory per embedded set.
func CacheDir() (string, error)

// CleanCache removes every library OpenFS has extracted for the current user.
func CleanCache() error

// LibraryPathEnv is the environment variable that overrides the search with an
// explicit list of locations, separated the way LD_LIBRARY_PATH is. An entry is
// a directory searched for the requested name, or the object itself.
const LibraryPathEnv = "CPPGO_LIBRARY_PATH"
```

`name` is the base name of the file to load, and it must be one of the `.so`
entries. Every `.so` entry is extracted, not only `name`, because a shim and the
library it links against have to be unpacked together. The files are reused while
they are unchanged, and the cache path is refused with `ErrInsecureCache` (see
[Errors](#errors)) if it is not a private directory. `CacheDir` is for a message
or a test that has to name the cache; `CleanCache` is for a program that is the
cache's only user, normally at startup or shutdown. A generated package does not
call `OpenFS`: the call belongs to a hand-written binding, or to a program that
controls how its library is opened. The steps are in
[Embed the C++ side in the Go binary]({{< ref "how-to/embed.md" >}}).

## Stack headroom

A **goroutine** is a Go function running on its own stack. Go stacks are
contiguous and grow by being copied to a larger allocation, and that copy moves
every frame on the stack. A C++ frame cannot be moved that way — it carries no
Go stack map, the record that tells the runtime where the pointers in a frame
are — so the room a call needs has to exist before control leaves Go.
**Headroom** is that free room below the current frame.

```go
// StackReserve is the amount of goroutine stack headroom, in bytes, that
// ProbeStack guarantees below the point it is called from: enough for the
// deepest C++ frame a bound call can create.
const StackReserve = 32 << 10   // 32 KiB

// ProbeStack guarantees at least StackReserve bytes of goroutine stack headroom
// below the caller, so a foreign call cannot run out of stack.
func ProbeStack()
```

Generated wrappers call `ProbeStack` before every foreign call. If you write a
binding by hand, call it first, before taking a pointer into the caller's frame:
the growth the probe may trigger moves that frame, so a pointer taken earlier
would be stale. The headroom belongs to the goroutine and to the depth it was
asked at; called from another goroutine, or from below the foreign call, it
protects nothing.

## Objects

The Go runtime is free to move a value on the heap unless it is told that the
address is in use. **Pinning** is that instruction, and `runtime.Pinner` is the
Go type that records it: a pinned object stays at its address until it is
unpinned. That is what lets C++ hold the address as `this`, the hidden pointer to
the object a member function was called on.

```go
// NewObject allocates size bytes of zeroed storage aligned to align, pins it,
// and returns an object whose reference count starts at one. A hand-written
// binding creates one per C++ object Go owns.
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

An `Object` is storage the Go side owns and the C++ side writes into. The
reference count is Go's: several Go values can share one object, and the
destructor passed to `NewObject` as `release` runs exactly once, when the last
reference is released. Hold the `*Object`, never a copy; the zero value is
unusable. A `Pinner` works only on heap memory — an address on a goroutine stack
cannot be pinned — which is why a bound C++ object lives in storage Go allocated
and never in a local variable.

## Wire helpers

A C++ declaration that takes a pointer is passed the address of Go memory. That
address is an `unsafe.Pointer`, Go's untyped pointer type, and these are the
helpers that produce it and keep the memory alive across the call:

```go
// Ptr returns a pointer to the first element of s, or nil when s is empty.
func Ptr[T any](s []T) unsafe.Pointer

// StrPtr returns a pointer to the bytes of s, for a callee that takes a string
// as a (pointer, length) pair. Go strings are not NUL-terminated.
func StrPtr(s string) unsafe.Pointer

// KeepAlive keeps its arguments reachable until the call returns, so the memory
// a foreign call was handed stays alive while the callee is still reading it.
func KeepAlive(v ...any)
```

A pointer from `Ptr` or `StrPtr` **borrows** the Go memory: it stays valid for
the duration of the call only, and the callee must not keep it. `KeepAlive` is
called after the call, never before. Without it, the compiler may treat the last
use of the slice or string as the line that formed the pointer, and the garbage
collector may free or move the memory while C++ is still reading it. The order is
always:

```go
cppgo.ProbeStack()
p := cppgo.StrPtr(name)
callSetName(p, uintptr(len(name)))
cppgo.KeepAlive(name)
```

## Calls and callbacks

A **callback** is a Go function that C++ calls. The C calling convention on
x86-64 Linux passes arguments in CPU registers: the first integer and pointer
arguments in six general-purpose registers, the first floating-point arguments in
eight SSE registers. `Args` is one such frame — one slot per argument register
and the two result registers:

```go
// Args is one C argument frame: six INTEGER and eight SSE argument registers in
// I and F, and the two result registers in Ret and RetF.
type Args = asm.Args

// CallReentrant invokes the foreign function at fn with the arguments in a,
// entering through runtime.cgocall so the callee may call back into Go.
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

`runtime.cgocall` is the Go runtime's own function for entering a foreign call;
it marks the thread as being in C, which is the only state from which a callback
may re-enter Go. Using it needs no cgo in the build. The mechanism is in
[The Go runtime and a native call]({{< ref "explanation/go-runtime.md" >}}).

Use `CallReentrant` when the call passes a Go function to C++, as a function
pointer or a `std::function`; a call that passes none does not need it. The
sequence is: register the Go function with `RegisterCallback`, pass the returned
address to the C++ side, call through `CallReentrant`, release the registration
afterwards, and check `TakeCallbackPanic` after the call returns, re-raising its
`Value`. The `*CallbackPanic` it returns also carries the goroutine stack at the
panic, in `Stack`. A panic inside the callback does not unwind into C++: the
result is zeroed, the panic is recorded, and it is delivered to Go when the
foreign call returns.

## Errors

| Type or value | Cause | Fix |
|---|---|---|
| `ErrClosed` | a `Symbol` call after `Close`, a second `Close`, or a generated method that returns an error, called on a nil or closed value; a generated method with no error return panics in that state instead | close once, after the work; never use the value or the library afterwards |
| `*OpenError` | a library that could not be loaded. `Path` and `Err` | check that the path names the object, that it is readable, and that its dependencies resolve |
| `*SymbolError` | a symbol that could not be resolved. `Name` and `Err` | the name is absent from the object's dynamic symbol table: check it, or regenerate the binding |
| `*ExtractError` | an embedded object that could not be materialized. `Name`, `Dir`, `Err` | check the embedded set holds the name, the disk space and the cache permissions |
| `ErrInsecureCache` | a cache path that fails the private-directory check | remove the path, or let cppgo create it; never point the cache at a shared directory |
| `*SearchError` | a bare library name that no override and no loader search resolved. `Name`, `Paths`, `Err` | set `CppgoLibraryPath` or `CPPGO_LIBRARY_PATH`, or deploy the object where the loader looks |
| `*DependencyError` | a `DT_NEEDED` entry of an embedded object that neither the set nor the loader provides: the list inside a shared object naming the other objects it needs. `Object`, `Name` | package the dependency in the embedded set, or install it where the loader finds it |
| `*NativeError` | a failure the library reported, with the message verbatim in `Message` | read the message: it is the text the C++ side stored, normally an exception's |
| `*CallError` | a bound call that failed. `Symbol` names the C++ declaration, `Err` wraps the native failure | read the message; `Err` is normally a `*NativeError` |

`*OpenError`, `*SymbolError`, `*ExtractError`, `*SearchError` and `*CallError`
implement `Unwrap`, so `errors.As` finds the loader or native failure inside
them. `*NativeError` and `*DependencyError` do not: neither wraps another error,
the first carrying the library's message verbatim and the second the object and
the missing `DT_NEEDED` entry. `ErrClosed` and `ErrInsecureCache` are plain
values, and `errors.Is` matches them through any error that wraps them.

```go
type CallError struct {
	Symbol string
	Err    error
}
```

`CallError` formats as `cppgo: <symbol>: <message>`.

## The low-level handle

```go
// Handle refers to memory whose layout the Go side cannot see: a C++ instance,
// a context, an iterator.
type Handle uintptr

// IsNil reports whether the address is zero.
func (h Handle) IsNil() bool
```

`Handle` is for memory whose layout the Go side cannot see, when it owns
nothing: it carries no ownership and no finalizer — the function the garbage
collector would otherwise run at collection — so nothing frees what it refers
to. **A generated binding never puts one in a signature.** Use it in a
hand-written binding that only passes a pointer it received back to C++; use an
`Object` when Go owns the storage.

## Related

- What a generated package imports and calls: [Generated API]({{< ref "reference/generated-api.md" >}}).
- How errors reach Go: [Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}).
