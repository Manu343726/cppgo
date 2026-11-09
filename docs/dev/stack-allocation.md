# Stack storage, zero-copy parameters and manual declarations

This page records a research question that decides part of cppgo's emitter: can a
C++ object live in a Go stack frame, can a parameter cross the boundary without a
copy, and can a hand-written Go declaration choose the representation of a C++
declaration? It is for the contributor who has to change the object model or the
parameter mapping and needs the measurements that make the current answers stand,
not for a user of the library. The prototypes it describes sit behind the
`stackalloc` build tag and change neither the runtime nor the emitter; shipping
any of them is separate work ([#43](https://git.manu343726.com/manu343726/cppgo/issues/43),
[#44](https://git.manu343726.com/manu343726/cppgo/issues/44)).

The research was asked in [#40](https://git.manu343726.com/manu343726/cppgo/issues/40),
with two later requirements: the C++ standard-library ⇄ Go input mapping and
zero-copy inputs ([#43](https://git.manu343726.com/manu343726/cppgo/issues/43)),
and hand-written Go declarations that force a representation
([#44](https://git.manu343726.com/manu343726/cppgo/issues/44)).

## The words this page uses

The page is written for a Go developer who has not worked on the Go runtime or on
C++ object lifetimes. These are the terms it needs, defined once:

| Term | What it means here |
|---|---|
| **FFI** | foreign function interface: Go calling code that was not compiled as Go |
| **ABI** | application binary interface: which registers and which memory carry the arguments and the result of a compiled call |
| **C++ object** | a value of a class type. It has a constructor and a destructor, and its bytes can be more than its fields: a pointer to its virtual-function table (the **vtable**), an internal pointer to one of its own members, an address registered elsewhere |
| **constructor, destructor** | the member functions that establish an object's invariant and release what it owns. The destructor runs when the object's lifetime ends |
| **copy constructor** | the constructor that builds a new object as a copy of another. C++ runs it on a by-value argument, a return, and some copies the compiler inserts |
| **special member functions** | the constructor, copy/move constructor, assignment operators and destructor the compiler may generate. A type whose author wrote one of them is "user-provided" for that member, and the compiler no longer treats the type as plain bytes |
| **POD** | plain old data: a record with no constructors, no virtual functions and no destructor, whose fields Go can mirror and whose bytes can be copied |
| **trivially copyable** | the C++ trait saying an object's bytes may be copied with `memcpy` (C's byte-copy function) and the copy is still a valid object. It is necessary for a value form (see § 2.1) and not sufficient |
| **trivially relocatable** | the C++26 proposal (P2786) for types whose bytes may be moved to a new address and the old storage discarded without running a destructor. It is a stronger promise than "trivially copyable" once a user-provided member exists |
| **placement new** | constructing an object in storage the caller supplies — `new (address) T(...)` — instead of allocating it |
| **`this`** | the pointer to the object a member function was called on. A method registers it if the object stores it somewhere |
| **borrow, own** | a **borrow** is a pointer and a length whose memory belongs to the caller and is valid only during the call. An **owning** type holds memory through a destructor and may reallocate it |
| **`std::span`, `std::string_view`** | non-owning views from the C++ standard library: a pointer and a length over, respectively, a range of `T` and a range of characters |
| **`std::vector`, `std::string`, `std::map`, `std::set`** | owning standard-library types: each holds memory, has a destructor and may reallocate on growth |
| **`std::optional`, `std::pair`, `std::tuple`, `std::function`** | standard-library wrappers: a value that may be absent, a two-field record, an N-field record, and a callable. Each has a layout of its own, not a Go pointer's |
| **`noexcept`** | a C++ declaration that promises not to throw. A thrown exception in a `noexcept` function terminates the process |
| **goroutine, frame, stack** | a goroutine is a Go function running with its own stack; a frame is the part of that stack holding one call's arguments and locals |
| **stack growth, stack copy** | Go grows a goroutine's stack by allocating a larger one and copying the old contents into it. The copy is `runtime.copystack`; it adjusts only pointers its stack maps describe |
| **stack map** | a per-frame list the runtime keeps of which words hold pointers, so the collector and the stack copier can adjust them. A pointer in a C++ frame, a register or a `uintptr` is not in a stack map |
| **safepoint, preemption** | a safepoint is a point in Go code where the runtime may stop a goroutine because every live value there is described by a stack map. Preemption is the runtime taking a goroutine off its OS thread there — to schedule another goroutine or to grow the stack |
| **`NOSPLIT`** | an assembler directive on a Go function that omits the stack-growth preamble, so the function can be entered without a stack check and the runtime cannot grow the stack at that point |
| **escape analysis** | the compiler pass that decides whether a value may live in its frame or must be moved to the heap. `go build -gcflags=-m` prints its decisions, including `moved to heap: x` |
| **pinning, `runtime.Pinner`** | telling the collector that a heap object must not move. `runtime.Pinner.Pin` records that promise; a stack address is not a heap object, so pinning one does nothing |
| **`g`, `M`, `P`** | the runtime's names for a goroutine (`g`), the OS thread it runs on (`M`) and the permission a thread must hold to run Go code (`P`). A `P` carries a local run queue and an allocator cache |
| **`uintptr`, `unsafe.Pointer`** | `uintptr` is an integer wide enough to hold a pointer; converting a pointer to it hides the pointer from the compiler and the collector. `unsafe.Pointer` is the pointer type they track |
| **`checkptr`** | the instrumentation behind `-race` and `-d=checkptr` that reports pointer arithmetic producing an address outside a live allocation, as a fatal error |
| **trampoline, thunk, shim, carrier** | the trampoline is generated assembly that calls the C++ symbol directly; a thunk is generated C++ that adapts a declaration the trampoline cannot carry; the shim is the C++ library cppgo compiles from the thunks; the carrier is the route one binding takes (direct, thunk or reentrant) |
| **`alloca`** | the C library function that allocates a frame object whose size is only known at run time. Go has no equivalent: every Go frame has a size fixed at compile time |
| **arena** | a block of memory from which objects are carved without an individual allocation. Go's arena experiment is on hold, and its objects cannot be pinned |
| **cgo** | Go's built-in way to call C, enabled with `CGO_ENABLED=1`. Its rules for Go pointers handed to C are quoted in § 5.3; cppgo's own calls go through assembly and are not checked, so the generator must keep them |
| **libclang** | the C library of the Clang compiler front end. The generator reads a header with it; `sizeof(T)` arrives from the same parse |
| **libstdc++** | the GNU implementation of the C++ standard library, the one `g++` uses |
| **`static_assert`** | a C++ compile-time assertion: the build fails when its condition is false. The shim uses it to prove a layout matches Go's |
| **`//go:noescape`** | a Go directive on a function declaration telling the compiler the function does not keep a pointer passed to it, so the argument may stay in the caller's frame |
| **`//go:noinline`** | a Go directive forbidding the compiler from inlining a function, used here to keep an allocation opaque to the optimiser |
| **`sync.Pool`** | a standard-library pool of reusable objects, shared per `P` |
| **build tag** | a `//go:build` line that selects which files a build includes. `stackalloc` is the tag that keeps the prototypes out of the production build |
| **`-run '^$'`, `-benchtime`** | `-run '^$'` runs no tests, only benchmarks; `-benchtime 1s` asks each benchmark to run for at least one second |
| **`-race`, `-benchmem`, ns/op, allocs/op** | `-race` is the race detector; `-benchmem` adds bytes and allocations per operation; `ns/op` is nanoseconds per operation; `allocs/op` is heap allocations per operation. [Benchmarks](benchmarks.md) defines the whole harness |
| **qualified name** | a C++ name with its namespace and class: `mathx::Vector::dot` rather than `dot` |
| **`gofmt`** | Go's source formatter; `make fmt-check` fails when a file is not formatted with it |

## The question and the verdicts

Three questions, three answers. The evidence for each follows in the rest of the
page:

- **Can a C++ object live on the Go stack?** Not in general, and the subset where
  it is safe is the subset that needs no object at all. A C++ object in Go frame
  storage is moved by every goroutine stack growth — the runtime copies the bytes
  and adjusts only the pointers its stack maps describe — so a type whose bytes
  may not be copied (no trivial copy constructor, or a destructor that must run,
  or an internal pointer to itself, or a registered `this`) cannot live there, and
  `runtime.Pinner` cannot help because pinning a non-heap address is a documented
  no-op that the compiler will not even let you reach without moving the object
  to the heap first. Measured: the emitted lifecycle allocates 7 times and costs
  782 ns against 97 ns for the value form, and the whole difference is bought
  back by a pinned slab with no stack semantics at all (81 ns, 0 allocations).
- **Can a parameter be zero-copy?** Yes, and it already is for the shapes whose
  C++ type is a borrow: `(const T *, size_t)`, `std::span<T>` and
  `std::string_view` are a pointer and a length, so the Go backing array is the
  callee's argument. Everything that owns memory (`std::string`, `std::vector`,
  `std::map`, …) is a copy, and no declaration can make it anything else.
  Measured: 1 KiB of `[]byte` through `const std::vector<uint8_t> &` costs +76 ns
  over the borrow, 1 KiB of `[]int` +114 ns, and a 51-byte `std::string` +26 ns.
- **Can a manual Go declaration force a representation?** It can force the Go
  *shape* — slice vs string vs pointer, one parameter vs two — and it can decide
  nothing about whether the C++ side copies: a borrow is only accepted where the
  C++ type is a borrow, and an `error` result only where the C++ declaration has
  a channel to report it through (`noexcept` has none; a throwing declaration
  must keep one). The prototype model refuses the six cases named in the issue
  and accepts the two zero-copy overrides.

Machine for every number here: AMD Ryzen 7 6800HS, Linux/amd64, Go 1.27.0-X,
g++ 16.2.1, 2026-11-09. Commands are in § 8, and every quoted figure is a
`-benchmem` run of the command named beside it.

## 1. What the runtime guarantees

### 1.1 A goroutine stack is copied, and only stack-mapped pointers follow it

Go stacks are contiguous and grown by reallocation and copy. The design document
states the invariant the copy relies on: escape analysis guarantees that pointers
to stack data are only ever passed down the call tree, so "the only pointers to
stack data are in the stack itself", and "only real pointers can be adjusted — we
need to avoid integers that look like pointers"
([contigstacks](https://go.dev/s/contigstacks)). It lists the consequence as a
disadvantage of the design: "pointer-into-stack restrictions … there may be
situations in the future where we wish to allow pointers into the stack, and this
implementation will preclude them".

The implementation is `runtime.copystack`
([`src/runtime/stack.go:929`](https://go.dev/src/runtime/stack.go)): it memmoves
the used part of the stack, then walks the new stack with `adjustframe`
(`stack.go:701`) and `adjustpointers`, which use each frame's stack map
(`stack.go:762`). Nothing else is adjusted. A pointer that lives in a C++ frame,
in a register, or in a Go variable typed `uintptr` is not in a stack map and does
not move with the frame.

The runtime has a comment about exactly this hazard, in `shrinkstack`
(`stack.go:1286`): "Check for self-shrinks while in a libcall. These may have
pointers into the stack disguised as uintptrs, but these code paths should all be
nosplit." The runtime protects its own libcalls from the problem; cppgo's
trampolines are the same shape of call and get no such protection.

### 1.2 A goroutine inside a foreign call is not preempted, so the frame is stable *during* the call

Async preemption requires the program counter to be in Go code with a stack map:
`isAsyncSafePoint` returns `false` when `!f.valid()` with the comment "Not Go
code" ([`src/runtime/preempt.go`](https://go.dev/src/runtime/preempt.go)). Through
the direct carrier the goroutine is `_Grunning` — the state "running on an M and
executing Go code" — executing a `NOSPLIT` assembly trampoline and then C; it
reaches no safepoint while the C frame runs, so its stack cannot be copied in
that window. That is why a *leaf* use of a stack object is safe: the address is
used only while C++ is running and nothing can move it.

It is also why the danger is entirely in the two other places:

- **the frame's lifetime after the call** — the object is still in the frame, and
  the next growth moves it (measured in § 3.3);
- **a callback**, because the reentrant carrier runs Go frames inside the foreign
  call.

`runtime.cgocall`'s own comment
([`src/runtime/cgocall.go:164`](https://go.dev/src/runtime/cgocall.go)) says the
second one in as many words: "fn may call back into Go code, in which case we'll
exit the 'system call', run the Go code (which may grow the stack), and then
re-enter the 'system call'". And `cgocallbackg` (`cgocall.go:344`) drops a warning
that is the whole analysis in one sentence: "When we call into Go, the stack is
free to be moved. If these locals aren't visible in the stack maps, they won't
get updated properly, and will end up being stale."

### 1.3 `runtime.Pinner` pins heap objects, and nothing else

`go doc runtime.Pinner.Pin` says: "Pin pins a Go object, preventing it from being
moved or freed by the garbage collector until the Pinner.Unpin method has been
called." and "It's safe to call Pin on non-Go pointers, in which case Pin will do
nothing."

The source says why a frame is one of those non-Go pointers. `pinnerGetPtr`
([`src/runtime/pinner.go:125`](https://go.dev/src/runtime/pinner.go)) rejects
nil, a non-pointer kind and an arena object (it panics on all three), then
`setPinned` (`pinner.go:169`) looks the address up with `spanOfHeap(uintptr(ptr))`;
for a stack address that returns nil, so `setPinned` returns `false` and `Pin`
records nothing. **A pin of a stack address is a silent no-op** — no panic, no
warning, and the stack copy proceeds.

The API defeats the attempt one step earlier anyway: `Pin` takes `any`, so a
pointer converted to an interface escapes. The compiler says so itself; see the
measurement in § 3.3.

Arena-allocated objects are not pinnable either: `pinnerGetPtr` panics with
"object was allocated into an arena" (`pinner.go:134`). The arena experiment is
also the closest thing Go has to bulk allocation, and it is on hold indefinitely
([golang/go#51317](https://github.com/golang/go/issues/51317)).

### 1.4 A stack frame has a fixed size chosen at compile time

"Go stacks do not have any `alloca`-style mechanism for dynamically-sized stack
frames. All Go stack frames are constant sized."
([Allocating on the Stack](https://go.dev/blog/allocation-optimizations), Keith
Randall, 2026-02-27). A local larger than `ir.MaxStackVarSize` — 128 KiB in this
toolchain ([`src/cmd/compile/internal/ir/cfg.go:11`](https://go.dev/src/cmd/compile/internal/ir/cfg.go))
— is moved to the heap by the compiler: measured, a `var big [1 << 20]byte`
reports `moved to heap: big`.

So a value form of a C++ object works only for a size the generator knows at
generation time, which it does: `sizeof(T)` from libclang. Size is not the
obstacle; lifetime is.

## 2. The cases

| Case | Verdict | Why |
|---|---|---|
| A trivially copyable C++ record held in a Go value, constructor's address irrelevant, no self-reference | **Safe** | The copy is defined and nothing observes the address. This is what cppgo already emits for a mirrorable record (`Point` in [`examples/mathx`](../../examples/mathx)): 30.8 ns/op, 0 allocations. |
| A C++ object with a non-trivial copy constructor or destructor (declared out of line, like `mathx::Vector`), or with an internal pointer to itself or a registered `this` | **Unsafe** | Every stack growth copies its bytes without running any special member and without telling the object. P2786 defines the property precisely and names the exceptions: "types that maintain an internal pointer to a data member and types that register their presence in an external registry that must point back to the object" (§ 7.1 of [P2786R13](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2025/p2786r13.html)). Measured in § 3.3. |
| The same object, in a **leaf** scope with no callback and no growth until the destructor's address is recomputed | **Unsafe in practice, safe only under a rule the language cannot state** | The call itself is stable (§ 1.2), and every later call recomputes `&v` because a Go pointer is adjusted by the copier. What is not stable is the object's *bytes*: any growth between construction and destruction relocates them. A value API would need "the frame must not move while the object is alive", which is a property of the whole call graph, not of the declaration. |
| A C++ object in a **local `[N]byte`** whose address is only ever passed as a `uintptr` into the argument block | **Unsafe** | The compiler does not consider the object reachable, so it may cache or re-materialise it (measured in § 3.3), and the object still moves. |
| A C++ object explicitly heap-allocated in the shim (`new` in a thunk) with a Go handle | **Safe** | The storage never moves. Costs one C++ allocation plus the Go wrapper; see § 4. |
| A pinned slab block | **Safe** | One `Pinner.Pin` per chunk pins the whole allocation (`setPinned` records the pin per heap object). 81 ns/op for the same lifecycle, 0 allocations (§ 4). |

### 2.1 The condition C++ itself states

`std::is_trivially_copyable` is the necessary condition: "Objects of
trivially-copyable types that are not potentially-overlapping subobjects are the
only C++ objects that may be safely copied with `std::memcpy`"
([cppreference](https://en.cppreference.com/w/cpp/types/is_trivially_copyable)).
Two traps in reading it, both measured in § 3.3:

- a user-provided **ordinary** constructor does not affect the trait —
  cppreference's own example has a class with `D(int)` and asserts
  `is_trivially_copyable_v<D> == true`; what matters is the copy/move
  constructor, assignment and destructor;
- a class with a **virtual function** is never trivially copyable
  (`is_trivially_copyable_v<C> == false` in the same example), so polymorphic
  types are out of the value form entirely.

C++26 would not change the verdict for a real class. `is_trivially_relocatable`
is deduced only when "none of its eligible special member functions are user
provided"; a class that declares a copy constructor or a destructor needs
`trivially_relocatable_if_eligible`, which is a declaration its author makes, not
something cppgo can infer
([P2786R13 § 5.3](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2025/p2786r13.html)).
The vendor does not have it yet: g++ 16.2.1 with `-std=c++26` defines neither
`__cpp_trivial_relocatability` nor `std::is_trivially_relocatable` (measured: the
fixture reports `-1`). The competing proposal, P1144, treats "trivially
relocatable" as a holistic property rather than P2786's verb-like one; the
difference, and the objections P2786's reading drew, are laid out in
[P3233R0](https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2024/p3233r0.html)
and in [O'Dwyer's comparison](https://quuxplusone.github.io/blog/2024/06/15/who-uses-trivial-relocation/).

## 3. Measurements

### 3.1 The lifecycle, both forms

`go test -tags stackalloc ./internal/stackalloc -run '^$' -bench . -benchmem -benchtime 1s`

```
BenchmarkCellLifecycle-16       12584227       96.83 ns/op       0 B/op    0 allocs/op
BenchmarkObjectLifecycle-16      1561030      782.1  ns/op     152 B/op    7 allocs/op
BenchmarkObjectNewRelease-16     3640912      358.3  ns/op      88 B/op    2 allocs/op
BenchmarkCellZero-16           535954143        2.325 ns/op       0 B/op    0 allocs/op
BenchmarkSlabLifecycle-16       14397060       80.82 ns/op       0 B/op    0 allocs/op
BenchmarkCellNorm-16            38313097       31.14 ns/op       0 B/op    0 allocs/op
BenchmarkObjectNorm-16          15528859       73.17 ns/op       8 B/op    1 allocs/op
BenchmarkPointByValue-16        39347692       30.80 ns/op       0 B/op    0 allocs/op
```

`CellLifecycle` is a `Cell` in the calling frame: placement-new through a thunk,
one method call, destructor — the value form. `ObjectLifecycle` is the emitted
shape, reproduced statement for statement
([`internal/stackalloc/value.go`](../../internal/stackalloc/value.go),
`NewVectorShape`): `NewObject`, the release closure, `&Vector`, one accessor,
`Release`.

The 7 allocations are the ones the compiler reports for the **real generated
file**:

```
$ cd examples/mathx && CGO_ENABLED=0 go build -gcflags=-m .
zz_cppgo.gen.go:271:2:  moved to heap: constructed
zz_cppgo.gen.go:272:6:  moved to heap: obj
zz_cppgo.gen.go:273:31: func literal escapes to heap
zz_cppgo.gen.go:285:9:  &Vector{...} escapes to heap
zz_cppgo.gen.go:342:6:  moved to heap: cppgoResult
```

four for the constructor, plus `make([]byte, size+align-1)` and `&Object{}` inside
`NewObject`, plus one per accessor for the result slot. `ObjectNorm` isolates the
last of them: 73.17 ns and 8 B against 31.14 ns and 0 B, because the emitted
accessor passes `unsafe.Pointer(&cppgoResult)` to a body-less trampoline with no
`//go:noescape`, which the compiler must treat as leaking
([`src/cmd/compile/internal/escape/escape.go:455`](https://go.dev/src/cmd/compile/internal/escape/escape.go):
"External functions are assumed unsafe, unless //go:noescape is given before the
declaration"). The prototype's trampoline
([`internal/stackalloc/callout_amd64.s`](../../internal/stackalloc/callout_amd64.s))
reproduces that declaration on purpose, so the 1 alloc/op is the emitted shape and
not an artefact.

`ObjectNewRelease` prices the storage alone, with no destructor and no C++ call:
358 ns, 88 B, 2 allocations. That is the cost the question is about, and it is
not one allocation — it is two, and the pin/unpin pair around them.

### 3.2 The value form really is in the frame

```
$ go build -tags stackalloc -gcflags=-m ./internal/stackalloc | grep -v 'nil Args'
value.go:33:7:  c does not escape
value.go:42:7:  c does not escape
value.go:55:7:  c does not escape
value.go:102:14: construct does not escape
```

`asm.Call`'s assembly back end carries `//go:noescape`
([`internal/asm/call_amd64.go`](../../internal/asm/call_amd64.go)), so the `Args`
block stays in the caller's frame, and the object address is passed as a
`uintptr` inside it, which escape analysis does not track. That is the mechanism
a value API would rely on — and it is also the reason a stack object is invisible
to the compiler's memory model (§ 3.3).

### 3.3 What a stack copy does to the object

All of these are in [`internal/stackalloc`](../../internal/stackalloc):
`stack_test.go` and `relocate_test.go` grow the stack by recursion (100 KiB
frames, under the 128 KiB limit) and fail loudly if the storage escaped to the
heap and therefore did not move.

| Test | Measured |
|---|---|
| [`TestFrameMovesAcrossStackGrowth`](../../internal/stackalloc/stack_test.go) | `pre != post`: a local's address changes across one growth. |
| [`TestPinTakesAnInterfaceSoTheStorageEscapes`](../../internal/stackalloc/stack_test.go) | `p.Pin(&c.buf[0])` reports `moved to heap: c` and the address does **not** change: the object is a heap object before the runtime sees it. |
| [`TestPinnerIgnoresAStackAddress`](../../internal/stackalloc/stack_test.go) | `p.Pin(unsafe.Pointer(pre))` — the same address, forced through `uintptr` so nothing escapes — leaves the object in the frame and it moves anyway. A pin of a frame is a silent no-op. |
| [`TestCallbackGrowthLeavesTheThisPointerStale/no_growth`](../../internal/stackalloc/stack_test.go) | No growth: the pointer C++ received and the pointer C++ wrote into the caller's frame both equal the object's address — the control. |
| [`TestCallbackGrowthLeavesTheThisPointerStale/growth`](../../internal/stackalloc/stack_test.go) | Growth: the heap out-pointer still holds the pre-call address, the in-frame out-pointer stays zero (**the write was lost to the copy**), and the live object is unchanged. |
| [`TestCallbackGrowthLosesAWriteThroughThis`](../../internal/stackalloc/stack_test.go) | C++ writes `this->x = 999` through the pre-call pointer, in a child process: the live object still reads 1 (`LIVE=1`, exit 0) — the write landed in the stack copy that is no longer live rather than faulting, because the freed small stack span was still mapped. The test tolerates a fault as well: both outcomes are the same use-after-free. |
| [`TestValueReturnRelocatesTheConstructor`](../../internal/stackalloc/relocate_test.go) | `NewCell` returns a value; `cppgo_construct_this` (recorded by the C++ constructor) differs from the address of the value the caller got. |
| [`TestSelfReferenceBreaksOnALegalByteCopy`](../../internal/stackalloc/relocate_test.go) | `cppgo_selfref` (trivially copyable, holds `me = this`) is byte-copied; at the new address `me != this`. The copy is defined and the object is broken. |

The last row is the reason "trivially copyable" is necessary and not sufficient:
the trait describes the copy, not the invariant. P2786's FAQ reaches the same
place from the other side (§ 2.1).

Two consequences for the emitted code:

- the accessor's 8-byte result allocation is load-bearing, not waste. A value API
  that passes `&result` as a `uintptr` and reads it afterwards may be reading a
  value the compiler has decided cannot have changed. Measured twice: a
  `sink := new(uintptr)` whose address is only ever passed as a `uintptr` stays in
  the frame, and C++'s write to it is lost
  (`TestCallbackGrowthLeavesTheThisPointerStale`); and a slice literal read back
  its original contents after the callee had written to it, while the same test
  passed when the package was compiled on its own
  ([`internal/zerocopy`](../../internal/zerocopy), `newInts`). The fix in both
  cases was to make the allocation opaque — `newHeapUintptr` and `newInts` are
  `//go:noinline` — and the emitted code happens to do it already, by leaking the
  out pointer into a non-`noescape` trampoline;
- `cppgo.KeepAlive(...any)` boxes what it is given. It costs nothing in the
  generated file because the boxing is eliminated there
  (`zz_cppgo.gen.go:344: … argument does not escape`), but a value API should use
  `runtime.KeepAlive` instead, which is an intrinsic and cannot move the receiver.

Under `-race` the deliberate `uintptr` round-trip is a **fatal error**, which is
the toolchain agreeing with the analysis:

```
fatal error: checkptr: pointer arithmetic result points to invalid allocation
stackalloc.addressAcrossGrowthWhilePinnedAsUintptr()
```

The function carries `//go:nocheckptr` (which the compiler honours outside the
runtime) so the experiment stays runnable under `-race`; without it the run stops
there.

The heap side of the same report, for the alternatives: `cppgo.Object` is two
heap objects (`make([]byte, …)` and `&Object{}`) plus the two boxed locals and the
closure of the emitted constructor; a slab block is one allocation per chunk of
`perChunk` objects.

## 4. The alternatives that remove the allocation without stack semantics

| Alternative | What it costs | Verdict |
|---|---|---|
| **Pinned slab, freelist** ([`internal/stackalloc/slab.go`](../../internal/stackalloc/slab.go)) | 81 ns/op, 0 allocs/op for the same lifecycle; one `make` and one `Pinner.Pin` per chunk, because a pin is recorded per heap object, so pinning `&chunk[0]` pins the chunk | The best of the four. Needs a size class per record and a free list; a `sync.Pool` is the same idea with the runtime's per-P machinery, and one lock or one pool per P is the concurrency price. |
| **Per-package arena (bump allocation)** | One allocation per chunk, no free list, so memory grows to the peak | Dead end on two counts: Go's arena experiment is on hold ([#51317](https://github.com/golang/go/issues/51317)), and `Pinner.Pin` panics on an arena object (`pinner.go:134`), so an arena block cannot be handed to C even if it existed. A private bump allocator over a `[]byte` chunk is the slab above with no reuse. |
| **Value API for trivially relocatable types** | 97 ns/op, 0 allocs | Safe only for the subset in § 2 that already needs no object. It is not an optimisation of the general path: it is a second, restricted API whose safety rests on a trait plus a hand-written promise about self-references and registration. |
| **C++-side handle** (`new` in the shim, Go holds an index) | One C++ allocation + one Go wrapper allocation today; with an integer handle, one process-global slot table | Same allocation count as today unless the slot table is global, and a global table is package-level mutable state — the one thing the design already excludes (except the callback table, with its argument). The handle also changes the ownership story: the C++ object outlives the Go value, so a missed `Close` leaks. |
| **Per-call result slot** (the 8-byte allocation) | 73.17 → ~31 ns if removed | Independent of the object question, and the cheapest single win in the emitted code: `//go:noescape` on the generated trampolines, or `runtime.KeepAlive` instead of `cppgo.KeepAlive`, removes one allocation per accessor call without touching the object model. **Not done here**; it belongs in its own issue. |

**Recommendation.** Keep `Object` as it is; do not add a value or stack form of a
non-trivial C++ type. If the per-object cost becomes the measured bottleneck, the
change to make is a pinned slab behind `NewObject` (same API, one allocation per
chunk, one pin per chunk), which is measured at 81 ns/op against 782 ns/op for
the lifecycle and needs no change to the emitter's contract. The 8-byte per-call
result allocation is a separate, smaller, safer win and should be its own issue.

## 5. `std` ⇄ Go parameters: borrowed or copied

`std` here means the types of the C++ standard library, and the arrow means the
direction of the mapping: a C++ parameter becomes a Go argument.

### 5.1 The rule

A parameter is **borrowed** when the C++ type is a pointer and a length, or a type
whose object representation is a pointer and a length — `std::span`,
`std::string_view`. It is **copied** when the C++ type owns memory or has an
invariant only its own constructor can establish. There is no third option:
adopting Go memory into an owning C++ type would give the C++ object a destructor
that frees Go's memory (§ 5.3).

Measured for `std::span`: a `std::span<const int>` parameter arrives in the same
two registers as `(const int *, size_t)` — the fixture's span form and its
pointer+length form return the same result from the same arguments
([`TestSpanIsPointerAndLength`](../../internal/zerocopy/zerocopy_test.go)) — and a
modification through `std::span<int>` is visible in the Go slice
([`TestSpanWritesThroughToTheGoSlice`](../../internal/zerocopy/zerocopy_test.go)).

### 5.2 The table

| C++ input | Go | Borrow? | Conditions |
|---|---|---|---|
| `const char *` + `size_t` | `[]byte` | **Borrow** | The bytes are Go's; no copy; the callee may not retain, and may not write. Measured 0 allocations. |
| `const char *` + `size_t` | `string` | **Borrow** | Same; `string` is read-only on the Go side, so it fits a `const` parameter only. |
| `char *` + `size_t` | `[]byte` | **Borrow**, writable | Writes are visible in the slice (§ 5.4). |
| `std::span<const T>` | `[]T` | **Borrow** | The two-register ABI above. `T` must be a scalar kind the mapping has: `int32`, `int64`, `float64`, … |
| `std::span<T>` | `[]T` | **Borrow**, writable | |
| `std::string_view` | `string` | **Borrow** | Pointer + length; the view does not own. A `std::string_view` result is **not** a Go string: the bytes it names belong to whatever it pointed at, which may die. cppgo's table already says "borrowed for the call; designed, not measured". |
| `std::array<T, N>` | `[N]T` | **Borrow**, by reference | Layout-compatible in libstdc++: measured `sizeof(std::array<int,4>) == 16` and `alignof == 4`, equal to `[4]int32` ([`TestArrayLayoutIsTheGoArrayLayout`](../../internal/zerocopy/zerocopy_test.go)). The standard specifies `std::array` as an aggregate with a `T[N]` member, not a byte-for-byte layout, so the generator must assert the size and alignment at compile time in the shim — which is the same `static_assert` the `Point` binding already uses. |
| `std::vector<T>`, `std::string`, `std::map`, `std::set` | — | **Copy** | Owns memory, has a destructor, and may reallocate. Declaring it a borrow is refused (§ 5.4). |
| `std::vector<std::string>` | `[]string` | **Copy**, and the leaves decide | A Go `string` is a pointer and a length; reinterpreting `[]string` as C strings is not a copy of the strings, and a Go pointer in C memory is forbidden (§ 5.3). The copy is per element. |
| `std::pair<A,B>` / `std::tuple<…>` | a two-field / N-field struct | **Copy by value**, layout-checked | The Go struct can mirror the layout when every element is a scalar and the padding matches; the shim's `static_assert`s are the check. There is no borrow: a `pair` parameter is passed by value or by reference to a Go-mirrored struct, never to a Go struct with different padding. |
| `std::optional<T>` | `*T` (parameter) / `(T, bool)` (result) | **Copy** | `optional` has a layout of its own (a value and a discriminator); it is not a Go pointer. |
| `std::function<…>`, function pointer | `func(…)` | n/a | The callback table's address; not a data borrow. |

Nested containers follow the leaf: `std::vector<std::vector<T>>` can never be
borrowed because the inner vectors own. Only a container whose leaves are all
trivially copyable scalars could in principle be traversed without copying, and
even then its *own* metadata (size, capacity, allocation) is not the Go slice
header, so there is nothing to adopt.

### 5.3 The constraints, and where they come from

- **No Go pointers in raw memory handed to C.** The cgo rules state it directly:
  "All Go pointers passed to C must point to pinned Go memory", and "Go code may
  pass a Go pointer to C provided the memory to which it points does not contain
  any Go pointers to memory that is unpinned"
  ([`src/cmd/cgo/doc.go:368`](https://go.dev/src/cmd/cgo/doc.go), `:382`).
  `[]byte`, `[]int32`, `[]float64` are raw; `[]string`, `[]any`, `[]*T` are not.
- **No retention past the call.** "C code may not keep a copy of a Go pointer
  after the call returns, unless the memory it points to is pinned with
  `runtime.Pinner` and the Pinner is not unpinned while the Go pointer is stored
  in C memory" (`src/cmd/cgo/doc.go:392`). A borrow is for the call.
- **The stack can move; a slice's backing array usually does not.** A `[]T` that
  escapes is heap-allocated and stable; a small slice whose backing array stays
  in a frame moves with it, and then the callee's pointer is stale — the same
  mechanism as § 3.3, and the reason the borrowed-slice test above has to
  allocate through an opaque function to observe the write.
- **The trampoline has no cgocheck.** cgo's rules are enforced dynamically
  ("These rules are checked dynamically at runtime", `src/cmd/cgo/doc.go:415`);
  cppgo's calls go through raw assembly
  ([`internal/asm/call_amd64.s`](../../internal/asm/call_amd64.s)) and are not
  checked at all. The rules are the generator's to keep, which is why they belong
  in the emitter and not in a comment.

### 5.4 Measurements

`go test -tags stackalloc ./internal/zerocopy -run '^$' -bench . -benchmem -benchtime 1s`

```
BenchmarkBytesBorrowed-16        2388750   499.3 ns/op   0 B/op  0 allocs/op
BenchmarkBytesCopied-16          2094072   575.2 ns/op   0 B/op  0 allocs/op
BenchmarkIntsBorrowed-16         3634624   324.6 ns/op   0 B/op  0 allocs/op
BenchmarkIntsCopied-16           2748700   438.5 ns/op   0 B/op  0 allocs/op
BenchmarkIntsSpan-16             3671978   327.0 ns/op   0 B/op  0 allocs/op
BenchmarkIntsSpanFromVector-16   2650146   449.9 ns/op   0 B/op  0 allocs/op
BenchmarkStringViewLength-16    50328328    24.07 ns/op   0 B/op  0 allocs/op
BenchmarkStringCopyLength-16    23765680    49.70 ns/op   0 B/op  0 allocs/op
```

Read the deltas, not the absolute numbers: the callee sums the payload in every
row, so the loop dominates and the difference is the copy.

| Payload | Borrow | Copy | Delta |
|---|---|---|---|
| 1 KiB `[]uint8` | 499.3 | 575.2 | **+75.9 ns** |
| 1 KiB `[]int32` | 324.6 | 438.5 | **+113.9 ns** |
| 51-byte `string` (length only) | 24.07 | 49.70 | **+25.6 ns** |
| `std::span<const int>` vs `(const int *, size_t)` | 327.0 vs 324.6 | — | **+2.4 ns**: constructing the span in the callee, no thunk, no copy |

`-benchmem` reports 0 allocations for the copy rows because the allocation
happens in C++, which the Go allocator does not account for. That is a limitation
of the measurement, not a claim that the copy is free.

`BenchmarkIntsSpanFromVector` (449.9 ns) is the span with the copy in front of
it: it matches `IntsCopied`, which is how one can tell the span itself costs
nothing.

Does the emitter support `span` today? [`DESIGN.md`](../../DESIGN.md)'s construct
table lists `std::span<T>` → `[]T` as **"borrowed; designed, not measured"**, and
`std::string_view` the same. The measurements above are therefore at the ABI
level — pointer and length in two registers, the span constructed by the callee —
and the emitter work to bind a `span` parameter is
[#43](https://git.manu343726.com/manu343726/cppgo/issues/43).

## 6. Manual Go declarations (#44)

### 6.1 What a declaration can force, and what it cannot

A hand-written Go signature decides **the Go shape**: one parameter or two, slice
or string or pointer, and whether an `error` result is present. It cannot decide
**what the C++ side does**, and it cannot invent a channel that does not exist:

- it cannot turn a copy into a borrow: a `std::vector<T>` parameter is a copy in
  the shim whatever the Go side says, because the shim has to construct the
  vector;
- it cannot drop an `error` result from a declaration that can throw —
  `try`/`catch` in the thunk is the only thing between a C++ exception and
  `std::terminate`;
- it cannot add an `error` result to a `noexcept` declaration: nothing can report
  through it, so the Go result would always be nil and the signature would lie;
- it cannot change the C++ ABI: argument count, order and grouping are the C++
  declaration's.

### 6.2 Recommended syntax

A hand-written file beside the generated package, keyed by the C++ *qualified*
name with the C++ parameter types, exactly as the error configuration is keyed by
the declaration. The file is YAML, the indentation-based configuration format:

```yaml
# cppgo.decls.yaml, beside the header, read by `cppgo gen` and diffed by `cppgo check`
decls:
  - cxx: "mathx::Vector::dot(const double *, size_t) const"
    go:  "func (v *Vector) Dot(ys []float64) (float64, error)"   # what is emitted today
  - cxx: "user::checked_mul(int, int)"
    go:  "func CheckedMul(a, b int32) (int32, error)"
  - cxx: "user::sum(const int *, size_t) noexcept"
    go:  "func Sum(values []int32) int32"                        # borrow, no error
```

Why not a `.go` file that the Go compiler checks, which is what "a Go override
file" would suggest: a declaration in the generated package's own directory
collides with the generated symbol (the two files declare the same function), and
a build tag does not help because the generator must read the file as an *input*,
which means the file exists in the build that generates the package it collides
with. A separate package avoids the collision but imports the generated types
(`*Vector`), which is the chicken-and-egg that makes the override unreadable on a
first generation. The key + textual signature keeps the generator's validation
exact, and `cppgo check` can report a stale override the same way it reports a
stale generated file. The parser cost is one grammar the generator already has to
own.

Where the generator resolves it: after libclang gives the declaration model
(kinds, grouping, `noexcept`, by-value layout) and **before** carrier selection,
because the carrier depends on the signature the override may change (`[]T`
keeps a shim thunk for a `std::span`; the direct carrier needs
register-representable scalars). A rejection is a generation error naming the
declaration, the parameter index, and the fix — the same shape as
`rejectUnimplemented`.

### 6.3 Validation cases

[`internal/declspec`](../../internal/declspec) implements the rules as a model
over the declaration facts (it is not the emitter) and
`go test -tags stackalloc ./internal/declspec -v` runs them. The six cases from
the issue, plus the boundaries:

| C++ | Declared Go | Result |
|---|---|---|
| `void f(const bool) noexcept` | `F(s string)` | **Rejected**: "is bool, not string" |
| `void f(const bool) noexcept` | `F(b bool) error` | **Rejected**: "is noexcept: it has no channel to report a failure through" |
| `void g(const char *, size_t) noexcept` | `G([]byte)` | **Accepted**: borrow |
| `void g(const char *, size_t) noexcept` | `G(s string)` | **Accepted**: borrow, read-only |
| `int h(std::span<int>) noexcept` | `H([]int) int32` | **Accepted**: borrow |
| `void k(const std::vector<int> &)` | `K([]int)` | **Rejected**: "owns memory: a Go slice or string cannot be adopted by it" |
| `void k(const std::vector<std::string> &)` | `K([]string)` | **Rejected**: same; the leaf being a Go pointer does not change the container |
| `void m(std::string_view) noexcept` | `M([]byte)` | **Rejected**: "must be a Go string" |
| `void t(const char *, size_t)` (throwing) | `T([]byte)` | **Rejected**: "can throw, so its Go signature must carry an error result" |
| `void t(const char *, size_t)` (throwing) | `T([]byte) error` | **Accepted** |
| `void a(const unsigned char *, size_t)` | `A([]int32)` | **Rejected**: element kind mismatch |
| `void b(const char *, size_t)` | `B(data *byte, n uint64)` | **Rejected**: the pair groups into one Go parameter |
| `int c() noexcept` | `C()` | **Rejected**: "returns one value, the Go signature returns 0" |

Two rules the table encodes that are worth stating plainly:

- **`noexcept` and `error` are two-way exclusive.** A throwing declaration must
  keep the error result; a `noexcept` one must not have it. This is the only place
  where the Go signature is decided by something other than a type.
- **Ownership beats preference.** The `std::vector` row is refused for *any* Go
  signature, not because a slice is wrong but because no Go signature can make
  the shim stop copying. Refusing it in the override is how the user learns that
  the copy is in the binding, not in their declaration.

### 6.4 What #44 still has to decide

- the file name and whether it is YAML (above) or a `.go`-shaped text grammar;
  either way it is an input to `cppgo check`;
- whether an override may *narrow* a want (for example a `[]T` parameter declared
  `*T` to pass a single element) or only re-shape what the C++ side already
  permits. The model here allows only the latter;
- how an override interacts with the error projection configuration (a
  `cppgo.yaml` channel and a declared `error` result are the same question asked
  twice);
- what `cppgo check` reports when an override names a declaration that no longer
  exists — the same class of staleness the generated files already have.

## 7. What is in the branch

| Path | What |
|---|---|
| [`internal/stackalloc`](../../internal/stackalloc) | the value form (`Cell`), the emitted shape (`NewVectorShape`), the slab, the benchmarks, and the stack-copy demonstrations. `//go:build stackalloc`. |
| [`internal/zerocopy`](../../internal/zerocopy) | the parameter-mapping benchmarks and the span/array/string-view ABI tests. `//go:build stackalloc`. |
| [`internal/declspec`](../../internal/declspec) | the manual-declaration validation model and its cases. `//go:build stackalloc`. |
| [`docs/dev/stack-allocation.md`](stack-allocation.md) | this document. |

None of the three packages is in the production build: the tag keeps them out of
`go build ./...`, `go vet ./...`, `go test ./...` and the coverage gate, and the
production tree is unchanged. `gofmt` still sees them, and `make fmt-check`
covers them.

## 8. Reproduce

```sh
# the production gates (unchanged by this branch)
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
CGO_ENABLED=1 go test -race -count=1 ./...
CGO_ENABLED=0 go vet ./...
make fmt-check

# the experiment
CGO_ENABLED=0 go test -tags stackalloc -count=1 ./internal/stackalloc ./internal/zerocopy ./internal/declspec
CGO_ENABLED=1 go test -tags stackalloc -race -count=1 ./internal/stackalloc ./internal/zerocopy ./internal/declspec
CGO_ENABLED=0 go test -tags stackalloc -run '^$' -bench . -benchmem -benchtime 1s ./internal/stackalloc ./internal/zerocopy
CGO_ENABLED=0 go build -tags stackalloc -gcflags=-m ./internal/stackalloc
cd examples/mathx && CGO_ENABLED=0 go build -gcflags=-m .   # the emitted shapes
```

The fixture needs `g++`; every test and benchmark skips when it is absent, like
[`internal/testlib`](../../internal/testlib). `go vet -tags stackalloc
./internal/stackalloc` reports one `unsafeptr` finding — `go vet` is Go's static
checker, and `unsafeptr` is its check for a pointer converted through `uintptr`
— at `stack_test.go`'s deliberate round-trip into `runtime.Pinner.Pin`, the line
that demonstrates the pin is a no-op. It is expected, and it is why the
experiment is outside the untagged `go vet ./...`.

## 9. Sources

1. Go, *Contiguous stacks* design document — <https://go.dev/s/contigstacks>
2. Go, `runtime` sources, this toolchain (`/usr/lib/go/src`):
   [`runtime/pinner.go`](https://go.dev/src/runtime/pinner.go),
   [`runtime/stack.go`](https://go.dev/src/runtime/stack.go),
   [`runtime/cgocall.go`](https://go.dev/src/runtime/cgocall.go),
   [`runtime/preempt.go`](https://go.dev/src/runtime/preempt.go),
   [`cmd/compile/doc.go`](https://go.dev/src/cmd/compile/doc.go),
   [`cmd/compile/internal/escape/escape.go`](https://go.dev/src/cmd/compile/internal/escape/escape.go),
   [`cmd/compile/internal/ir/cfg.go`](https://go.dev/src/cmd/compile/internal/ir/cfg.go),
   [`cmd/cgo/doc.go`](https://go.dev/src/cmd/cgo/doc.go). Line numbers quoted
   above.
3. Keith Randall, *Allocating on the Stack*, 2026-02-27 —
   <https://go.dev/blog/allocation-optimizations>
4. golang/go#51317, *proposal: arena: new package providing memory arenas* —
   <https://github.com/golang/go/issues/51317>
5. Alisdair Meredith et al., *Trivial Relocatability For C++26*, P2786R13 —
   <https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2025/p2786r13.html>
6. *Issues with P2786 ("Trivial Relocatability For C++26")*, P3233R0 —
   <https://www.open-std.org/jtc1/sc22/wg21/docs/papers/2024/p3233r0.html>
7. Arthur O'Dwyer, *Who uses P2786 and P1144 for trivial relocation?*, 2024-06-15 —
   <https://quuxplusone.github.io/blog/2024/06/15/who-uses-trivial-relocation/>
8. cppreference, *std::is_trivially_copyable* —
   <https://en.cppreference.com/w/cpp/types/is_trivially_copyable>
9. cppgo [`DESIGN.md`](../../DESIGN.md) §§ *Carriers*, *The trampoline*, *The
   mapping* — the direct and reentrant carriers, the probe-before-pointer rule,
   and the "designed, not measured" rows this document fills in for `span` and
   `string_view`.
