---
title: "Carriers and the construct table"
description: "How each C++ construct reaches C++, and which ones are supported."
weight: 25
---

A **carrier** is the mechanism that carries one call from Go across the boundary
into C++. There are exactly three: a few instructions of Go assembly that jump
straight to the declaration; a C++ function that cppgo writes into its **shim** —
the C++ file the generator creates and compiles beside your library — and that
calls the declaration as C++; and a third form that enters C++ the way cgo does,
so that C++ may call back into Go. Every **binding** — one C++ declaration turned
into a callable Go function — is assigned exactly one of them before anything is
written. That assignment is the generator's central decision, and this page is
about it.

So the page covers which carrier a declaration gets, what each of the three is,
and the C++ constructs the generator supports today. It is the companion to
[Architecture]({{< ref "explanation/architecture.md" >}}), where one call is
followed step by step; the API a caller sees is in
[Generated API]({{< ref "reference/generated-api.md" >}}), and the cost of each
carrier is in [Performance]({{< ref "explanation/benchmarks.md" >}}).

The carrier is invisible in the generated API. It shows up in the report
`cppgo gen` prints, and in the comment above each generated function.

## Direct

The generated assembly calls the C++ symbol in your library. All of these must
hold:

- the declaration has an out-of-line body — one defined in a `.cpp` file rather
  than inside the class body — and its symbol is exported by the library (a
  header-only declaration whose symbol happens to be exported is direct too);
- it is **not virtual**: `virtual` means the implementation a call reaches is
  chosen at run time from the object's type, and the direct call goes to a fixed
  address, so dispatch has to stay a concern of the C++ side;
- it is **`noexcept`** — the C++ keyword by which a function promises not to
  throw. The direct path has no handler above it, so an exception that escapes
  terminates the process;
- the signature is register-representable: scalars, enums, pointers and
  references, and a POD of at most 16 bytes with no MEMORY-class part. No `std::`
  type.

Three of those words need unpacking. A **POD** — *plain old data* — is a struct
with no constructor, no virtual function and no private members, whose bytes are
its whole representation. The **ABI** — *application binary interface* — is how a
compiled call is laid out: which registers and which memory carry its arguments
and its result. **MEMORY-class** is a classification the ABI makes for an
argument it passes in memory rather than in a register.

## Thunk

A **thunk** is a generated `extern "C"` function — C++ with C linkage, so it has
the C calling convention and a name that is not mangled — compiled by cppgo into
the shim, and it calls the declaration as C++. Everything the direct path cannot
express goes here: inline bodies (defined inside the class body, so the library
exports no symbol for them), virtual members, `std::` types, by-value records,
constructors and destructors (the functions that create and destroy an object),
and every call that may throw. One such translation unit — one `.cpp` file,
compiled as a unit — is
[`examples/mathx/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp).

The thunk is where exceptions are caught: each one clears the error message
stored for the current thread (a `thread_local`, the C++ storage class that gives
each thread its own copy), runs the call inside `try`/`catch`, and returns a
status — 0 for success, non-zero after a caught exception. That is why a
non-`noexcept` declaration is never direct, whatever its signature.

## Reentrant

The **reentrant** carrier is a thunk reached through `runtime.cgocall` — the Go
runtime's entry point for "this thread is now inside foreign code" — instead of
through the assembly trampoline. That entry is the only state in which the Go
runtime accepts a callback: a call on the direct path never enters it, so a
callback reached that way kills the process. A binding whose signature passes a
Go function to C++ therefore gets this carrier.

Template calls go through this carrier as well: the generated generic code calls
`cppgo.CallReentrant`
([runtime reference]({{< ref "reference/runtime.md" >}}#calls-and-callbacks)),
so a template binding costs what the reentrant carrier costs. That is one
implementation detail the generated package exposes in its source, not in its
API.

## The report

`cppgo gen` prints one line per binding, naming the carrier it chose:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
```

The thunk names in that report are functions in the shim linked under
*Thunk* above, one per non-direct binding.

The header decides, not a configuration file, and
[`examples/stats`](https://github.com/Manu343726/cppgo/blob/main/examples/stats) is
the example where the difference is the whole point. These two declarations come
from
[`examples/stats/include/stats/stats.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/stats/include/stats/stats.hpp):

```cpp
// mean of n values, or 0 for n == 0.
double mean(const double *xs, size_t n) noexcept;

// histogram counts the values into nbins buckets spanning [lo, hi] and writes
// the counts straight into the caller's array: the output crosses as a
// (pointer, length) pair too. Values outside [lo, hi] (and NaNs) are dropped.
//
// It reports failure by throwing std::invalid_argument. The shim catches it, so
// the generated Go signature is just an error — the C++ function has no status
// return to explain and none appears in Go.
void histogram(const double *xs, size_t n, double *bins, size_t nbins, double lo,
               double hi);
```

`mean` is out of line and `noexcept`, so it takes the direct carrier; `histogram`
throws, so it takes a thunk. The other three `noexcept` functions in the header,
`stddev`, `count_above` and `data_pointer`, are direct for the same reason.

## The construct table

"Supported" means a generated package does it today. "Refused" means generation
stops with a message naming the declaration and the fix. "Designed" means the
mechanism is stated in the design and is not in this build. A row that adds a
clause of its own says exactly how far the construct has been carried:
**measured** means the test fixture for the carrier calls it against a real
compiler, **generated and compiled, not executed** means the code is emitted and
built but never called, and **emitted, not exercised by an example** means no
example runs it.

Two words in the table are the convention's names for its argument registers:
**INTEGER**, the six general-purpose registers a call passes integers and
pointers in, and **SSE**, the eight floating-point registers beside them.

| Construct | Carrier | Status |
|---|---|---|
| inline / header-only body | thunk | supported |
| virtual member function | thunk (real vtable dispatch: the table of function pointers a virtual call reads the address from) | supported |
| virtual destructor | thunk | supported |
| multiple / virtual inheritance | thunk (`static_cast`/`dynamic_cast` for `this`) | measured, **refused by the generator**: base classes are not emitted yet |
| `std::string` | thunk (constructed and copied in the C++ side) | supported |
| `std::string_view`, `std::span` | thunk (borrowed, valid for the call) | supported |
| `std::vector<T>` | thunk | supported |
| `std::array<T,N>` | thunk, by value (a fixed-size array) | supported |
| `std::optional<T>` | thunk; Go value is `*T` (a C++ value that may be absent) | supported |
| `std::unique_ptr`, `std::shared_ptr` | — | **refused**: they are owning pointers, which free their object |
| POD ≤ 16 bytes by value | thunk | supported |
| POD > 16 bytes / MEMORY class by value | thunk, through an sret pointer (a hidden first argument through which the callee writes a large result) | generated and compiled, not executed |
| non-POD by value | thunk | measured, **refused by the generator** |
| more than 6 INTEGER / 8 SSE arguments | — | **refused**: the argument block is not implemented |
| exception-throwing call | thunk (`try`/`catch`) | supported |
| callback parameter, `std::function` | reentrant | emitted, not exercised by an example |
| class / function template | thunk, called reentrantly | supported |
| static member | thunk, no `this` (the object a method was called on) | supported |
| enum | — | **refused** |
| operators, conversion operators (an operator that converts the object to another type) | — | **refused** |
| function-like macro that is the API (a macro is a text substitution performed before compilation) | — | not bindable: a macro's expansion is not a declaration |

## Related

- The mechanism in prose: [Architecture]({{< ref "explanation/architecture.md" >}}).
- What the generated signatures look like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Everything that is refused, with the message: [Limits]({{< ref "explanation/limits.md" >}}).
- What each carrier costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
