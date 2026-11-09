# Evidence

Every behavioural claim in the manual is pinned by a test or a fixture. This page
is the map: it says which test asserts which claim, what the test asserts, and
how to run that test on its own. Read it when you change a claim — in
[`README.md`](../../README.md), a page under [`site/`](../../site) or
[`DESIGN.md`](../../DESIGN.md) — and need the test that guards it, or when a test
fails and you need to know which promise it is holding.

A claim that has no row here is a claim nobody measured. Add the test, or mark
the claim as designed rather than shipped; see [Decisions](decisions.md) for the
list of the latter.

## Running one test

A Go test belongs to a package, and a package is a directory. The command that
runs one test is:

```console
$ go test ./internal/shim -run '^TestStdStringAndVector$' -v
```

- `./internal/shim` is the package directory. The commands below name it in the
  `Package` column.
- `-run` takes a regular expression and runs only the top-level tests whose name
  matches it. `^` and `$` anchor the whole name, so `^TestStdStringAndVector$`
  does not also run `TestStdStringAndVectorSomethingElse`.
- `-v` prints each test and its subtests instead of one line per package.
- `-count=1` disables Go's test-result cache. Use it when you want the test to
  run rather than to be remembered; `make test` always passes it.

Two properties of this project's suite need their own commands:

- **cgo.** [`cgo`](../../site/content/explanation/benchmarks.md) is Go's
  built-in way to call C, and it is enabled by `CGO_ENABLED=1`. The runtime and
  the generated packages are built and tested with `CGO_ENABLED=0`, because they
  must not need cgo. `make test` runs both passes:

  ```console
  $ CGO_ENABLED=0 go test -count=1 ./...
  $ CGO_ENABLED=1 go test -race -count=1 ./...
  ```

- **the race detector.** `-race` makes the Go runtime report two goroutines that
  touch the same memory without synchronisation. It is built on cgo, so the
  toolchain rejects it with `CGO_ENABLED=0`: that is why there are two passes
  rather than one. A test that fails only under `-race` is a data race, not a
  slow test.

## Carrier and construct claims

The carriers — the mechanisms that carry a call across the boundary (a direct
trampoline, a thunk in the shim, or the reentrant call) — are defined with their
code in [Carriers](../../site/content/explanation/carriers.md).

| Claim | Test | Package | What the test asserts |
|---|---|---|---|
| an inline body is reachable only through the shim; the user's library exports no symbol for it | [`TestInlineHeaderOnlyIsReachableOnlyThroughTheShim`](../../internal/shim/shim_test.go) | `internal/shim` | the user's library exports no `_Z5twiced`, the shim exports `shim_twice`, and the call returns 42 |
| virtual dispatch and a virtual destructor work | [`TestVirtualDispatchAndVirtualDestructor`](../../internal/shim/shim_test.go) | `internal/shim` | `Shape::area` on a `Circle` of radius 2 is 12, the override supplies the name, and deleting through a `Shape *` runs `~Disk` |
| `this` is adjusted for multiple inheritance | [`TestMultipleInheritanceAdjustment`](../../internal/shim/shim_test.go) | `internal/shim` | the `Circle` subobject of a `Disk` is at a non-zero offset, `Disk::serial` is 30, and a virtual call through the adjusted pointer reaches the same object |
| `std::string` and `std::vector` cross in and out | [`TestStdStringAndVector`](../../internal/shim/shim_test.go) | `internal/shim` | a greeting written into a Go buffer is `"hello, world!"`, and doubling `[1,2,3]` writes `[2,4,6]` |
| a non-POD passed by value is copied inside the shim | [`TestNonPODByValue`](../../internal/shim/shim_test.go) | `internal/shim` | the copy constructor ran: the value arrives as 1007, not 7 |
| exceptions become Go errors | [`TestExceptionsBecomeErrors`](../../internal/shim/shim_test.go) | `internal/shim` | a divide by zero is reported as status -1 and the last error is `"division by zero"` |
| a callback reaches Go through a shim thunk | [`TestCallbackThroughTheShim`](../../internal/shim/shim_test.go) | `internal/shim` | the Go callback adds 40 and 2, the result is 42, and no panic was captured |
| a header-only template instantiation lives in the shim | [`TestTemplateInstantiation`](../../internal/shim/shim_test.go) | `internal/shim` | the user's library exports no `maxof` instantiation, and `shim_maxof_double` and `shim_maxof_int` return 2.5 and 9 |
| the shim is an object of its own | [`TestArtifactModel`](../../internal/shim/shim_test.go) | `internal/shim` | the user's library contains no `cppgo` or `shim_` symbol, and the shim exports its thunks and `cppgo_last_error` |
| the reentrant carrier reads its result from the return registers | [`TestReentrantResult`](../../internal/gen/reentrant_test.go) | `internal/gen` | the generated wrapper reads each result class from the right place: the integer result register (`RAX`) for integers, the first floating-point return register (`XMM0`) for `float64`, and the same word as a pointer for a pointer result |
| every construct in the standard-library table round-trips through a generated package | [`TestStdFixtureEndToEnd`](../../internal/gen/std_test.go) | `internal/gen` | a generated package for [`testdata/std`](../../internal/gen/testdata/std) is compiled with `g++` and a real program calls every binding |
| a POD and a MEMORY-class record cross by value | [`TestByValueRecordShape`](../../internal/gen/byvalue_test.go), [`TestByValueRecordShimCompiles`](../../internal/gen/byvalue_test.go) | `internal/gen` | the generated Go and shim have the expected shape for both record classes, and the shim compiles with `g++` |
| the two-phase template flow works, and a missing instantiation names the line to add | [`TestTwoPhaseGenerics`](../../internal/gen/template_e2e_test.go), [`TestMissing`](../../internal/gen/template_e2e_test.go) | `internal/gen` | the end-to-end fixture generates, scans a Go program, compiles the specialisations and calls them; a type with no C++ counterpart fails with the exact `template struct ::box::Box<std::int64_t>;` line |
| an enum, operator, smart pointer, argument block and the other refusals are generation errors | [`TestGenerationErrors`](../../internal/gen/errors_test.go) | `internal/gen` | each header under [`testdata/errors`](../../internal/gen/testdata/errors) is refused with the message that names the declaration and the fix |
| the enum refusal names the declaration | [`TestEnumDiagnostic`](../../internal/gen/errors_test.go) | `internal/gen` | the message contains `err::Color: enums are not emitted` |
| `-status` drops the integer, returns the callee's code and fails on non-zero | [`TestStatusConventionShape`](../../internal/gen/status_test.go), [`TestStatusEndToEnd`](../../internal/gen/status_test.go) | `internal/gen` | the Go signature is `func Parse(text string) error`, the wrapper tests the integer it got, and a real program observes both outcomes |
| a compilation database's relative include paths are rebased | [`TestRebaseArgs`](../../internal/cxx/flags_rebase_test.go) | `internal/cxx` | `-Iinclude` and `-I include` become `-I/build/tree/include`; the other flag forms in the table are rebased or left alone |
| the runtime never imports the generator | [`TestRuntimeHasNoGeneratorDependency`](../../deps_test.go) | repository root | `go list -deps .` contains none of `internal/cxx`, `cmd/cppgo` or `internal/gen` |

## Runtime claims

| Claim | Test | Package | What the test asserts |
|---|---|---|---|
| an embedded shared object is extracted to a private cache and loaded from there | [`TestOpenFSEmbedded`](../../loadfs_test.go), [`TestOpenFSExtractsEverySharedObject`](../../loadfs_test.go) | repository root | the fixture's sibling object resolves through `$ORIGIN`, every shared object in the `embed.FS` is written, and the loaded object answers |
| two processes and many goroutines share one cache entry | [`TestOpenFSConcurrentGoroutines`](../../loadfs_test.go), [`TestOpenFSConcurrentProcesses`](../../loadfs_test.go) | repository root | concurrent extraction leaves one entry and every caller loads it |
| a cache directory another user can write is refused | [`TestOpenFSRejectsInsecureCache`](../../loadfs_test.go) | repository root | the error is `cppgo.ErrInsecureCache`, not a generic extraction failure |
| a name resolved through a `Library` is cached, including the failure | [`TestSymbolCacheAnswersRepeatably`](../../cppgo_test.go), [`TestSymbolCacheFollowsTheLibrary`](../../cppgo_test.go) | repository root | a repeated lookup returns the same address, a missing name returns the same error, and closing one library does not disturb another's cache |

## Hard limits

These are the behaviours the manual reports as limits rather than features. The
fatal ones run the failing call in a child process, so the parent can assert the
exit status and the message.

| Claim | Test | Package | What the test asserts |
|---|---|---|---|
| an exception with no C++ frame terminates the process | [`TestExceptionFromCIsFatal`](../../internal/callback/exception_test.go) | `internal/callback` | the child exits non-zero with `terminate called after throwing an instance of 'std::runtime_error'` and names `cb_throws` |
| an exception thrown after a callback is also fatal | [`TestExceptionAfterACallbackIsFatal`](../../internal/callback/exception_test.go) | `internal/callback` | the child exits non-zero with the same `terminate called` message |
| an exception raised through the Go callback is fatal | [`TestExceptionThroughTheGoCallbackIsFatal`](../../internal/callback/exception_test.go) | `internal/callback` | the child exits non-zero with the same message, so unwinding cannot cross the Go frames |
| a callback on the direct trampoline is fatal | [`TestCallbackOnTheDirectTrampolineIsFatal`](../../internal/callback/exception_test.go) | `internal/callback` | the child dies with `morestack on g0` or another runtime fatal error |
| an exception the C++ code catches itself never reaches Go | [`TestExceptionThrownAndCaughtInCPPWorks`](../../internal/callback/exception_test.go) | `internal/callback` | the call returns normally, which is why a `noexcept` wrapper is a fix rather than a workaround |
| a callback panic is captured and re-raised on the Go side | [`TestCallbackPanicIsCaptured`](../../internal/callback/panic_test.go), [`TestCallbackPanicReachesTheCaller`](../../internal/callback/panic_test.go) | `internal/callback` | the C caller sees a zero result, the panic value is retrievable, and the caller re-panics it |
| building a `std::function` from a Go-side layout needs exported instantiations | [`TestStdFunctionFromGo`](../../internal/callback/stdfunction_test.go) | `internal/callback` | `sizeof(std::function<int(int,int)>)` is 32 there, and the library exports `_M_invoke` and `_M_manager` for the layout the test builds |

## Fixtures

| Path | What it holds |
|---|---|
| [`internal/gen/testdata/errors/`](../../internal/gen/testdata/errors) | one header per refused construct; the table is `TestGenerationErrors` |
| [`internal/gen/testdata/golden/`](../../internal/gen/testdata/golden) | the golden output for the `box`, `mathx`, `overloads`, `std` and `types` fixtures |
| [`internal/gen/testdata/mathx/`](../../internal/gen/testdata/mathx), [`status/`](../../internal/gen/testdata/status), [`std/`](../../internal/gen/testdata/std), [`types/`](../../internal/gen/testdata/types), [`tmpl/`](../../internal/gen/testdata/tmpl), [`project/`](../../internal/gen/testdata/project), [`box/`](../../internal/gen/testdata/box), [`duplicate/`](../../internal/gen/testdata/duplicate), [`badshim/`](../../internal/gen/testdata/badshim) | the end-to-end fixtures |
| [`internal/shim/testdata/`](../../internal/shim/testdata) | the user library and the shim, built as two objects by the tests |
| [`internal/callback`](../../internal/callback) | the callback table, the child-process fatal tests and the exception tests |
| [`examples/mathx`](../../examples/mathx), [`examples/stats`](../../examples/stats), [`examples/full`](../../examples/full) | the examples' own suites |

The example suites assert what each example teaches:

| Example | Tests |
|---|---|
| [`mathx`](../../examples/mathx/mathx_test.go) | `TestConstructorAndAccessors`, `TestConstMethod`, `TestDirectMethod`, `TestMethodTakingSlice`, `TestThrowingMethod`, `TestPointByValue`, `TestSaxpy`, `TestObjectsAreIndependent`, `TestClone`, `TestCloseLifetime` |
| [`stats`](../../examples/stats/stats_test.go) | `TestMean`, `TestStddev`, `TestCountAbove`, `TestHistogramWritesThroughTheSlice`, `TestHistogramErrorPath`, `TestSliceIsNotCopied`, `TestLargeInput` |
| [`full`](../../examples/full/cmd/app/app_test.go) | `TestHistogramValues`, `TestCloneIsIndependent`, `TestPointByValue`, `TestVectorCopiesInAndOut`, `TestThrowingCall`, `TestStatusCall`, `TestCallback`, `TestGenericInstantiations`, `TestRun` |

## A document and the code disagree, as measured

### `char *` binds, but not as the type map says

`DESIGN.md`'s type map says a `(char *p, size_t n)` parameter becomes a Go
`string`. It does not. Measured on 2026-11-09 with a header

```cpp
#pragma once
#include <cstddef>
namespace ch {
void take(const char *text, std::size_t n) noexcept;
}
```

and the command

```console
$ bin/cppgo gen -header ch.hpp -package ch -library ./libch.so -out .
cppgo: warning: the compilation flags for ch.hpp are an assumption (assumed: -std=c++17 -I/tmp/opencode/charbind); a parse without the project's real flags can misread the header
ch::take(const char *, std::size_t) noexcept direct _ZN2ch4takeEPKcm             noexcept, out of line, not virtual, register class
cppgo: built libcppgo_ch.so with /usr/bin/g++
```

The binding compiles, and the generated Go signature is

```go
func Take(text []int8)
```

So the older claim that these parameters "do not bind" is no longer true: they
bind as borrowed bytes — a Go `[]int8` whose memory the C++ side reads during the
call — and the shim compiles. What remains is the mismatch with
[`DESIGN.md`](../../DESIGN.md): the contract says `string`, the generator emits
`[]int8`. The user page that reports the old limitation is
[Limits](../../site/content/explanation/limits.md) § *`char *` parameters*; it
predates this measurement and is being corrected separately.

## Fixed, kept for the reader who finds the commit

The examples' `make check` was stale at `ce10171`: the committed
`examples/*/cppgo_shim/zz_cppgo_shim.cpp` predated the emitter's
`#include <cstdint>`, so `make check` failed in both examples. Fixed in `ba7d8e5`
by regenerating; `make examples` now runs `check` before anything regenerates.

## Status vocabulary

The manual marks rows **designed** and **not shipped**. The authoritative list is
`rejectUnimplemented` in [`internal/gen/reject.go`](../../internal/gen/reject.go);
[Decisions](decisions.md) tracks the designed-but-unshipped items with their
issues.
