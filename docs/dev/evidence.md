# Evidence

Every behavioural claim in the user documentation is pinned by a test or a
fixture. This page is the map, so the manual can state what cppgo does without
carrying test names.

Run everything with `make test` (two passes: `CGO_ENABLED=0`, then `-race` with
cgo). The commands below are the narrow versions.

## Carrier and construct claims

| Claim | Test | Package |
|---|---|---|
| an inline body is reachable only through the shim; the user's library exports no symbol for it | `TestInlineHeaderOnlyIsReachableOnlyThroughTheShim` | `internal/shim` |
| virtual dispatch and a virtual destructor work | `TestVirtualDispatchAndVirtualDestructor` | `internal/shim` |
| `this` is adjusted for multiple inheritance | `TestMultipleInheritanceAdjustment` | `internal/shim` |
| `std::string` and `std::vector` cross in and out | `TestStdStringAndVector` | `internal/shim` |
| a non-POD passed by value is copied inside the shim | `TestNonPODByValue` | `internal/shim` |
| exceptions become Go errors | `TestExceptionsBecomeErrors` | `internal/shim` |
| a callback reaches Go through a shim thunk | `TestCallbackThroughTheShim` | `internal/shim` |
| the reentrant carrier reads its result from RAX/XMM0 | `TestReentrantResult` | `internal/gen` |
| every construct in the standard-library table round-trips through a generated package | `TestStdFixtureEndToEnd` | `internal/gen` |
| a POD and a MEMORY-class record cross by value | `TestByValueRecordShape`, `TestByValueRecordShimCompiles` | `internal/gen` |
| the two-phase template flow works, and a missing instantiation names the line to add | `TestTwoPhaseGenerics` | `internal/gen` |
| an enum, operator, smart pointer, argument block and the other refusals are generation errors | `TestGenerationErrors` | `internal/gen` |
| the enum refusal names the declaration | `TestEnumDiagnostic` | `internal/gen` |
| `-status` drops the integer, returns the callee's code and fails on non-zero | `TestStatusConventionShape`, `TestStatusEndToEnd` | `internal/gen` |
| a compilation database's relative include paths are rebased | `TestRebaseArgs` | `internal/gen` |
| the runtime never imports the generator | `TestRuntimeHasNoGeneratorDependency` | repository root |

## Hard limits

| Claim | Test | Package |
|---|---|---|
| an exception with no C++ frame terminates the process | `TestExceptionFromCIsFatal`, `TestExceptionAfterACallbackIsFatal`, `TestExceptionThroughTheGoCallbackIsFatal` | `internal/callback` |
| a callback on the direct trampoline is fatal (`morestack on g0`) | `TestCallbackOnTheDirectTrampolineIsFatal` | `internal/callback` |
| a callback panic is captured and re-raised on the Go side | `TestCallbackPanicIsCaptured`, `TestCallbackPanicReachesTheCaller` | `internal/callback` |
| building a `std::function` from a Go-side layout needs exported instantiations | `TestStdFunctionFromGo` | `internal/callback` |
| `runtime.cgocall` needs the cgo runtime initialised | the runtime's own test, plus importing `purego` provides fakecgo under `CGO_ENABLED=0` | root |

## Fixtures

| Path | What it holds |
|---|---|
| `internal/gen/testdata/errors/` | one header per refused construct; the table is `TestGenerationErrors` |
| `internal/gen/testdata/golden/` | the golden output for the `box`, `mathx`, `overloads`, `std` and `types` fixtures |
| `internal/gen/testdata/{mathx,status,std,types,tmpl,project}/` | the end-to-end fixtures |
| `internal/shim/testdata/` | the user library and the shim, built as two objects |
| `internal/callback` | the callback table, the child-process fatal tests and the exception tests |
| `examples/mathx`, `examples/stats` | the examples' own suites |

The example suites assert what each example teaches:

| Example | Tests |
|---|---|
| `mathx` | `TestConstructorAndAccessors`, `TestConstMethod`, `TestDirectMethod`, `TestMethodTakingSlice`, `TestThrowingMethod`, `TestPointByValue`, `TestSaxpy`, `TestObjectsAreIndependent`, `TestClone`, `TestCloseLifetime` |
| `stats` | `TestMean`, `TestStddev`, `TestCountAbove`, `TestHistogramWritesThroughTheSlice`, `TestHistogramErrorPath`, `TestSliceIsNotCopied`, `TestLargeInput` |

## Embedded extraction, as measured

The real `embed.FS` path, with a demo program built `CGO_ENABLED=0`
(ADR-0003):

```console
$ XDG_CACHE_HOME=/tmp/…/demo-cache ./demo
loaded /tmp/…/demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
cppgo_embed_main_add(2, 3) = 10
$ stat -c '%A %n' demo-cache/cppgo demo-cache/cppgo/*
drwx------ demo-cache/cppgo
drwx------ demo-cache/cppgo/ae667028…924bf1
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_dep.so
-rw------- demo-cache/cppgo/ae667028…924bf1/libcppgo_embed_main.so
```

Eight concurrent runs of that binary all printed the same path and `= 10`, and
the cache held one entry afterwards.

## Known defects, as measured

### `char *` parameters do not bind

`char *` and `const char *` are mapped to `*int8`, so the generated C++ then
fails to compile:

```
cppgo gen: cppgo: zz_cppgo_shim.cpp:34: ch::take: the generated shim does not
compile; invalid conversion from 'const int8_t*' {aka 'const signed char*'} to
'const char*' [-fpermissive]
```

Measured 2026-11-08 with the generator at that commit, using a header with
`void take(const char *text, std::size_t n) noexcept`. `DESIGN.md`'s type map
says `(char *p, size_t n)` becomes a Go `string`; it does not. The user
documentation reports the limitation and the workaround (`std::string` or
`std::string_view`) without the measurement.

### The examples' `make check` was stale

At `ce10171` the committed `examples/*/cppgo_shim/zz_cppgo_shim.cpp` predated the
emitter's `#include <cstdint>`, so `make check` failed in both examples. Fixed in
`ba7d8e5` by regenerating; `make examples` now runs `check` before anything
regenerates.

## Status vocabulary

The manual marks rows **designed** and **not shipped**. The authoritative list is
`rejectUnimplemented` in `internal/gen/reject.go`; [Decisions](decisions.md)
tracks the designed-but-unshipped items with their issues.
