+++
title = "Error handling"
description = "The int32 status convention, cppgo_last_error, what the generated wrappers turn a failure into, and why a trailing error in direct ABI mode is always nil unless C++ returns a status."
weight = 60
+++

C++ has no Go-style multiple returns, so a failure has to travel in the return
value. cppgo uses one convention on the C side and translates it once in the
generated Go wrapper.

## The status convention

A C++ function that can fail returns `int32_t`:

- **`0` means success.**
- **anything else means failure**, and the shim stores a message that
  `cppgo_last_error()` returns.

```c
const char *cppgo_last_error(void);   /* thread-local, valid until the next call */
```

The accessor is thread-local and the pointer stays valid only until the next
`cppgo_*` call on the same thread, so the generated wrapper copies the message
out immediately. A library that cannot fail does not need to export it at all:
`cppgo.LastError` returns `nil` when the symbol is absent.

<span class="filename">cppgo_shim.h (excerpt)</span>

```c
/* cppgo_dot implements func Dot(a []float64, b []float64) (float64, error).
 * Returns 0 on success. On failure it returns a non-zero status and
 * leaves the message for cppgo_last_error(); the value is written through ret. */
int32_t cppgo_dot(double* a, size_t a_len, double* b, size_t b_len, double* ret);
```

The value is written through a trailing out-pointer because the ABI allows one
return register and the status already owns it. The wrapper declares a local,
passes its address, and only reads it when the status is zero.

## What the wrapper does with a non-zero status

```go
cppgoStatus := cppgo_dot(cppgoSymDot, cppgo.Ptr(a), uintptr(len(a)),
	cppgo.Ptr(b), uintptr(len(b)), unsafe.Pointer(&cppgoRet))
cppgo.KeepAlive(a, b)
if cppgoStatus != 0 {
	if err := cppgo.LastError(cppgoLib); err != nil {
		return 0, fmt.Errorf("dot: %w", err)
	}
	return 0, fmt.Errorf("dot: call failed with status %d", cppgoStatus)
}
```

Three details worth knowing:

- The message is prefixed with the C entity: the **symbol** in shim mode
  (`dot: ...`), the **qualified C++ name** in ABI mode
  (`mathx::Vector::dot: ...`). The native text is a `*cppgo.NativeError`, so
  `errors.As` can recover it if you need the message without the prefix.
- If the library recorded no message, the fallback is
  `"<prefix>: call failed with status %d"` — a non-zero status is never
  silently swallowed.
- A library that fails but exports no `cppgo_last_error` still produces that
  fallback, not a panic.

## Direct ABI mode: there is no status channel

The direct call returns whatever the C++ function returns. It has no place to
put a status beside a `double`, and no shim to store a message. So:

- If the C++ result is **not an integer**, a binding declared as
  `(T, error)` compiles to `return value, nil`. The generator says so in the
  report and in the generated file:

  > the trailing error is always nil: a direct C++ call has no status channel
  > (return an int32 status and declare only error to get one)

- If the C++ result **is an integer or enum** and the binding declares only
  `error`, the integer is read as the status: `0` is success and returns `nil`,
  non-zero returns the native message when the library exports
  `cppgo_last_error`, or `call failed with status N` otherwise.
- If the C++ function returns `void` and the binding declares `error`, the
  error is always `nil` (shape `abiErrorOnly`).

The integer status is read at its own width: a 4-byte result is read from `EAX`,
an 8-byte one from `RAX`, so a non-zero high word is not lost.

```go
//cppgo:function stats::load
func Load(path string) error
```

The C++ side is `int32_t stats::load(const char *path, std::size_t n)`: a
non-zero return becomes a Go error, and a zero return becomes `nil`.

This is the only way to get a real error out of the direct ABI mode without a
shim: return an `int32_t` status, and use the same `cppgo_last_error`
convention in your C++ source.

## Exceptions

- **Shim mode** gives you an `extern "C"` entry point in your own translation
  unit, where a C++ exception can be caught and converted into a non-zero status
  plus a message. The generated `cppgo_shim.h` documents that contract (return 0
  on success, leave the text for `cppgo_last_error()` on failure); the catching
  itself is in the code you write. An exception must never unwind into Go.
- **Direct ABI mode** has no shim in between. A C++ exception that escapes the
  function is **undefined behaviour** — it will attempt to unwind through a
  frame the Go runtime does not describe. Catch inside the C++ function and
  return a status.

## Library and symbol errors

These are Go errors from the runtime package, not statuses from C++:

| error | when | `errors.Is` / `errors.As` |
|---|---|---|
| `*cppgo.OpenError` | `dlopen` failed, or the path was empty | `errors.As(err, &openErr)`; `Unwrap` exposes the loader error |
| `*cppgo.SymbolError` | `dlsym` failed, or the library is closed | `errors.As(err, &symErr)`; `errors.Is(err, cppgo.ErrClosed)` |
| `cppgo.ErrClosed` | any `Symbol` after `Close`, and a second `Close` | `errors.Is` |
| `*cppgo.NativeError` | the message from `cppgo_last_error` | `errors.As`; it has no `Unwrap`, so `errors.Is` only matches the exact pointer |

A generated package resolves every symbol **once**, on the first call, and
panics if a binding has no `error` result:

```go
if err := cppgoEnsure(); err != nil {
	panic(err)   // for func F() float64
}
```

A binding with an error result returns the failure instead:

```go
if err := cppgoEnsure(); err != nil {
	return 0, err
}
```

So a missing `.so` surfaces as a panic on a no-error binding — which is the
intended severity: the program was built against a library that is not there.

## What is not an error

- A **zero handle** from a constructor is not a failure. In the direct ABI mode
  the handle is the address of storage Go allocated, so it is never zero. If you
  need "construction failed", return a status from a static factory function
  instead.
- A **negative** status is not special: any non-zero value is a failure.
- **Parameter validation** is not done at call time. A nil slice arrives as
  `(nil, 0)`, a null `*T` as a null pointer, and the C++ side has to reject
  them. The generated code does not synthesize checks.

Next: [object lifetimes]({{< ref "manual/lifetimes.md" >}}).
