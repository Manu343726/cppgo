# cppgo — design contract

This file is the contract for every workstream. If something is not written
here, do not invent it: ask the orchestrator.

## What this is

`cppgo` calls C++ from Go with **no cgo at build time** and **no `import "C"`**,
by generating an assembly trampoline per bound function that enters the C ABI
directly from Go, instead of going through the runtime's cgo path.

## Validated facts (measured 2026-11-03, Go 1.27, Ryzen 7 6800HS @ 964 MHz
"power-saver" profile — absolute ns are inflated ~4.5x by the clock, **cycles
and ratios are the honest numbers**)

| path | ns/op | ~cycles |
|---|---|---|
| Go indirect call (baseline) | 2.1 | 2 |
| cgo (Go → C, same function) | 105 | 101 |
| **cppgo trampoline, direct** | **13.9** | **13** |
| cppgo trampoline, stack switch | 14.9 | 14 |

So the trampoline is **~7.5x cheaper than cgo** and within ~2x of a plain Go
call. These numbers must be reproducible with `make bench` and quoted in the
README/site *with the clock caveat*.

Correctness evidence already gathered on the prototype:

- `runtime.isAsyncSafePoint` returns false for **any** assembly function
  (`f.flag&abi.FuncFlagAsm != 0`) and for any `sp < gp.stack.lo`, so a NOSPLIT
  asm trampoline is never asynchronously preempted, and a foreign stack pointer
  is never mistaken for a Go stack.
- `//go:linkname runtime.asmcgocall` is **rejected** by the Go linker
  (`invalid reference to runtime.asmcgocall`). Do not try it. The switch is
  ours.
- Passing: GC torture (`GOGC=1` + `runtime.GC()` in a loop under 32 goroutines),
  SIGPROF/SIGURG torture, `-race`, `GOMAXPROCS=1`.
- A foreign call in flight delays a GC cycle by up to the call duration (2.0 ms
  call ⇒ 1.1 ms worst GC cycle, versus 2.4 ms for a spinning Go goroutine).
  This is the documented trade-off: the P is not released.

## Architecture

```
user code
  └── generated Go wrapper  (idiomatic types: []float64, string, error)
        └── generated asm trampoline   (NOSPLIT, per signature)
              └── generated C++ shim   (extern "C", ABI-stable, catches exceptions)
                    └── the user's C++ code
```

Symbol resolution uses `github.com/ebitengine/purego` (dlopen/dlsym without
cgo). We never use purego's call path — only `Dlopen`/`Dlsym`. This is the only
third-party dependency.

## Public API (exact signatures — do not change without asking)

```go
package cppgo

type Library struct{ /* opaque */ }

func Open(path string) (*Library, error)
func (l *Library) Close() error
func (l *Library) Path() string
func (l *Library) Symbol(name string) (uintptr, error)
func (l *Library) MustSymbol(name string) uintptr   // panics

// ProbeStack guarantees at least cppgo.StackReserve bytes of goroutine stack
// headroom below the caller, so a foreign call cannot grow past stack.lo.
// Cheap (~ns) after the first call on a goroutine.
func ProbeStack()

const StackReserve = 32 << 10

// LastError reads cppgo_last_error() from lib and returns it as an error.
// Returns nil when the C string is empty or absent.
func LastError(lib *Library) error
```

Wire helpers (used by generated code, exported so users can hand-write bindings):

```go
func Ptr[T any](s []T) unsafe.Pointer   // nil for empty
func StrPtr(s string) unsafe.Pointer
func KeepAlive(...any)
```

## Error convention

A C++ function that can fail returns `int32_t`. `0` means success. Non-zero
means the shim stored a message, read with `cppgo_last_error()`:

```c
const char *cppgo_last_error(void);   // thread-local, valid until next call
```

The generated Go wrapper turns a non-zero return into `fmt.Errorf("%s: %s", sym, msg)`.
The generated C++ shim catches `std::exception` and `...` and converts.

## ABI convention (both sides are generated, so we own it)

- Integer/pointer arguments: `RDI, RSI, RDX, RCX, R8, R9` in declaration order.
- Float arguments: `XMM0…XMM7` in declaration order.
- Return: `RAX` for integer/pointer, `XMM0` for float.
- Slices and strings are passed as **(pointer, length)** pairs, pointer first.
- Structs cross only by pointer, and must be declared `//cppgo:repr(C)`.
- Callee may not keep a pointer to Go memory after the call returns.

## The trampoline (validated asm, amd64)

```asm
TEXT ·Dot(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	a+8(FP), DI
	MOVQ	n+16(FP), SI
	MOVQ	SP, R13
	ANDQ	$~15, SP        // SysV: RSP 16-aligned *before* the CALL
	CALL	R12
	MOVQ	R13, SP
	MOVSD	X0, ret+24(FP)
	RET
```

Rules that are not negotiable:
- Args are read **before** SP is switched; the result is stored **after** it is
  restored (`FP` is relative to SP).
- The trampoline is `NOSPLIT` with `$0-frame`; it must not allocate, must not
  touch Go memory, and must not call back into Go.
- Float results come back in `X0`; integer in `AX`.
- `ProbeStack()` is called by the wrapper **before** the trampoline.

## Generator DSL

Input: a normal Go file with directives.

```go
package mathx

//cppgo:library libmathx

//cppgo:external add_i64
func AddI64(a, b int64) int64

//cppgo:external dot
func Dot(a []float64, b []float64) (float64, error)
```

`cppgo gen -dir <pkg> -out <dir>` emits:

- `zz_cppgo.gen.go` — idiomatic wrappers + symbol binding init.
- `zz_cppgo_amd64.s` — one NOSPLIT trampoline per function.
- `cppgo_shim.h` — the `extern "C"` declarations the C++ side must implement.
- `zz_cppgo_stub.go` — the original functions rewritten to delegate (no body
  panics left behind).

Type mapping (Go ⇄ C):

| Go | C | notes |
|---|---|---|
| `int8/16/32/64`, `uint8/16/32/64` | same width | |
| `int`, `uint`, `uintptr` | `int64_t`, `uint64_t` | |
| `float32`, `float64` | `float`, `double` | XMM |
| `bool` | `int32_t` | |
| `string` | `const char*, size_t` | |
| `[]T` (T numeric) | `T*, size_t` | caller owns, callee must not retain |
| `*T` (repr(C) struct) | `T*` | |
| `Handle` | `void*` | opaque C++ object |
| `error` (2nd result) | `int32_t` + `cppgo_last_error` | always last |

## Non-goals for v1

- Callbacks from C++ into Go (documented limitation).
- arm64 (the package must still *build*; it panics with a clear message).
- Windows.
- C++ exceptions crossing the boundary (they are caught in the shim).

## Test and coverage contract

- `testify` (`require`/`assert`) everywhere. No bare `if != { t.Fatal }`.
- Concurrency tests: `-race`, `GOMAXPROCS=1`, 32+ goroutines hammering calls.
- Coverage gate in CI: **≥ 99%** on `./...`, measured with `-covermode=atomic`.
  The **only** exclusions are machine-written artifacts: `zz_cppgo*.go`
  (generator output) and `internal/gen/testdata/` (golden fixtures).
  **`cmd/` is measured like everything else** — see below.
- Every error path has a test (bad library path, missing symbol, empty string).

### `cmd/` is covered, not excused

The CLI must be structured so it can be tested, not excluded:

```go
// cmd/cppgo/main.go — one statement, nothing else.
func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
```

`run(args []string, stdout, stderr io.Writer) int` lives in `cmd/cppgo/cli.go`
and carries **all** the logic: flag parsing, subcommands (`gen`, `check`,
`version`, `help`), exit codes (`0` ok, `1` generation error, `2` usage error)
and every message printed. `cmd/cppgo/cli_test.go` exercises every subcommand,
every malformed flag, every exit code, and both output streams.

The single uncovered statement is `func main()` itself, which is unavoidable
(you cannot call `os.Exit` in-process). A CLI package with a real `cli.go` keeps
that at well under 1% of statements, so the 99% gate is met with no exclusion.

**Two corrections to the original brief (measured, not negotiable):**

1. `CGO_ENABLED=0 go test -race` is impossible — the toolchain refuses it
   (`-race requires cgo`). Testing therefore runs **two passes**:
   - `CGO_ENABLED=0 go test -count=1 ./...` — proves the library needs no cgo.
   - `CGO_ENABLED=1 go test -race -count=1 ./...` — the race detector.
   Both must pass; neither replaces the other.
2. Coverage is collected on the cgo-enabled pass (that is where the race
   detector runs), from `-covermode=atomic`.

---

# v2 — direct C++ ABI mode (supersedes the C-shim default)

Decision by the operator: **cppgo calls the C++ symbol directly.** The
hand-written `extern "C"` layer stops being the default. `this` becomes an
implicit first argument, and the tool has to understand the Itanium C++ ABI.

The C-shim mode stays available as an opt-in escape hatch (`//cppgo:mode shim`)
for everything the direct ABI cannot express.

## Validated on this machine (2026-11-03, g++ 16 / clang 22)

Prototype at `/tmp/opencode/cxxabi` — a real `mathx::Vector`, dlopen'd, called
from Go asm with a Go-allocated object blob:

| what | result |
|---|---|
| `Vector(double,double)` ctor, out-of-line | works — `_ZN5mathx6VectorC1Edd`, `this`=RDI, x=XMM0, y=XMM1 |
| `double norm() const` | works — `_ZNK5mathx6Vector4normEv`, `this`=RDI, ret=XMM0 |
| `double dot(const double*, size_t) const` | works — `_ZNK5mathx6Vector3dotEPKdm` |
| `double x() const` **defined inline in the header** | **NO SYMBOL** — dlsym fails |
| ctor defined inline in the header | **NO SYMBOL** |

Two facts the tool must behave accordingly about:
1. **Inline/header-defined members emit no symbol.** Not an edge case: idiomatic
   C++ defines short members, and often the constructor, inside the class.
   The generator must detect this and fail with an actionable message
   ("move the definition out of the header or provide an out-of-line factory"),
   never emit a trampoline that will fail at runtime.
2. **`const` methods carry `K` in the mangling**, and ctor/dtor use `C1`/`C2`
   (complete/base object). We never construct mangled names by hand — we ask
   libclang.

## libclang, without cgo

`internal/cxx` binds `libclang.so` through purego. This is possible because
purego v0.11.3 classifies structs by value, including **memory-class returns via
a hidden pointer** (`export_test.go: StructReturnInMemory`,
`func_test.go: TestABI_StructReturnHiddenPointer`), which is exactly what
`CXType` (24 B), `CXCursor` (32 B) and `CXSourceLocation` (24 B) need.
`clang_visitChildren` uses `purego.NewCallback`.

Present here: `/usr/lib/libclang.so` (22.1.8), `clangd` 22, `clang++` 22.
When libclang is absent the tool degrades to the `shim` mode and says so.

### Compilation flags: take them from clangd, do not invent them

Resolution order for the translation unit's flags:
1. `-compile-commands <path>` or the nearest `compile_commands.json` found by
   walking up from the header (and honouring `.clangd`'s
   `CompileFlags.CompilationDatabase`).
2. `.clangd` in the header's directory or an ancestor, merged as clangd does:
   `CompileFlags.Add`, `CompileFlags.Remove`, `CompileFlags.Compiler`.
3. Fallback: `-std=c++17 -I<header dir>`, reported as an assumption on stderr.

`CDB`/`.clangd` parsing uses the YAML decoder already in the module graph.

## What the ABI mode supports (v1 of the mode)

| C++ construct | status |
|---|---|
| free function | yes |
| `static` member function | yes (no `this`) |
| non-virtual member function, defined out-of-line | yes |
| `const` member function | yes (`K`) |
| constructor / destructor | yes, out-of-line only (`C1`/`C2`, `D1`/`D2`) |
| pointer / reference parameters | yes (both are pointers at the ABI level) |
| scalar parameters, `bool`, enums | yes |
| struct by value as a parameter | rejected (MEMORY class on the stack) |
| struct returned by value | **yes**, via the sret hidden pointer |
| virtual function | rejected in this mode (needs vtable traversal) → use `shim` |
| inline / header-only function | rejected with the actionable message |
| template | rejected unless explicitly instantiated (then it is a normal symbol) |
| `std::string`, `std::vector`, any non-POD by value | rejected → use `shim` |
| exceptions | must not cross; a throw is undefined behaviour in this mode |

Register order for a member function: **sret pointer (if any), then `this`, then
the explicit arguments**, each classified INTEGER → `RDI,RSI,RDX,RCX,R8,R9` and
SSE → `XMM0..XMM7` independently.

## Validation is the point

When headers and flags are available (the normal case: a C++ SDK ships headers),
`cppgo gen` **fails the build** unless every binding is compatible. The checks,
each with a specific message:

- symbol not declared in the TU,
- declared but inline / no body out-of-line → "will have no symbol",
- virtual → "needs the shim mode",
- parameter count mismatch (Go params ≠ C++ params, `this` and sret excluded),
- type mismatch per parameter (int width, float vs integer class, pointer kind),
- register exhaustion (>6 INTEGER or >8 SSE slots after adding `this`/sret),
- unsupported C++ type (std::, by-value struct, template).

When the library is loaded from a runtime `.so` with no headers, validation is
impossible: that path requires an explicit opt-in flag and the binding is
resolved by mangled name at load time. The generated code must carry a comment
saying it was not validated.

## Effect on the existing workstreams

- `internal/cxx` is new and owns: libclang bindings, flag discovery, the
  `Symbol` model, and validation.
- the emitter grows: `this`/sret slots, direct mangled symbol, no `shim.h` in
  `abi` mode.
- `internal/testlib` and `examples/` must use **out-of-line** definitions, or the
  tests would be asserting on a limitation instead of a feature.

## Where the ABI comes from — three tiers, most-specific first

The operator's rule: **the clangd configuration is the primary source and should
be sufficient.** Only when it is not, fall back. Only when that fails too, the
ABI is written down by hand on the Go side.

Every tier produces the **same** `cxx.Symbol` model, so the emitter and the
validator do not know which tier answered:

```
cxx.Symbol {
    QualifiedName, Mangled
    Kind            Free | StaticMethod | Method | Constructor | Destructor
    IsConst, IsVirtual, IsStatic
    HasOutOfLineBody          // false => there is no symbol to dlsym
    Params []Param            // Spelling, Kind, SizeBytes, IsPtr, IsFloat
    Result Param
    HasSret                   // memory-class return => hidden first pointer
    Source   Source           // Clangd | Binary | Annotation
}
```

### Tier 1 — clangd compilation config (primary)

`compile_commands.json` (nearest, or `CompileFlags.CompilationDatabase` from
`.clangd`) and/or `.clangd` (`CompileFlags.Add/Remove/Compiler`), fed to libclang.
This tier answers **everything**: real types, out-of-line vs inline, virtual,
constructors, `sret`. It is the only tier that can *validate*; the other two
only *describe*.

When tier 1 answers, the generator may reject a binding at build time.

### Tier 2 — infer it from the loaded binary

No headers, no flags, but there is a `.so`: read `.dynsym` with `debug/elf`,
find the symbol, and **demangle it**. The Itanium mangling encodes the whole
signature — `_ZNK5mathx6Vector3dotEPKdm` already says
`mathx::Vector::dot(double const*, unsigned long) const` — so the mangled name
*is* the ABI description for everything we support.

Use `github.com/ianlancetaylor/demangle` (pure Go) rather than `__cxa_demangle`,
so this tier needs no system library.

What tier 2 can and cannot know, and it must say so:
- knows: qualified name, parameters and their kinds, `const` (`K`), ctor/dtor
  (`C1`/`D1`), primitive sizes on LP64.
- cannot know: whether a struct is trivial, real `sizeof` of user types,
  virtual-ness, and whether the symbol is a thunk.
- consequence: a class/struct return type is assumed memory-class (`HasSret`)
  unless it is a scalar; a virtual method is called **directly**, bypassing
  dispatch, which is only correct for a final class. Both are reported.

Tier 2 output is **unvalidated**: the generator cannot fail the build, so the
generated Go file carries a machine-readable note saying the binding was
inferred from the binary and not verified against headers.

### Tier 3 — written down by hand in Go

When neither tier can answer, the operator states the ABI next to the Go
declaration, and the generator emits exactly that:

```go
//cppgo:external _ZNK5mathx6Vector3dotEPKdm
//cppgo:abi method const this=arg0 args=ptr:f64,uint64 ret=f64
func Dot(v *Vector, x []float64) (float64, error)
```

The `abi` directive is the last resort and is emitted into the generated code as
a comment, so a reader can see the assumption without reading the directive.
Its grammar must be small, checked, and produce an error position on a typo:
`free|static|method|ctor|dtor`, `const`, `this=argN`, `sret`, `args=<type>,...`,
`ret=<type>`, with `<type>` in `i8,i16,i32,i64,u8..u64,f32,f64,ptr:<type>,bool`.

### Reporting

`cppgo gen` prints, per binding, which tier answered and what it assumed:

```
mathx::Vector::dot      mangled _ZNK5mathx6Vector3dotEPKdm   abi from compile_commands.json
mathx::Vector::x        inline, no symbol                    ERROR: define it out of line
libfoo::bar             abi from libfoo.so (unvalidated)     note emitted in generated code
libbar::baz             abi from //cppgo:abi directive       as stated
```
