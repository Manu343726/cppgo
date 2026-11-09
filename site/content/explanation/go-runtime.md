---
title: "The Go runtime and a native call"
description: "What a goroutine, its stack, the scheduler and the collector do around a C++ call, and what that forces cppgo's design to be."
weight: 15
---

A binding is a native call made from a language whose runtime was designed
without one. Before the design in [Architecture]({{< ref "explanation/architecture.md" >}})
makes sense, the pieces it works around have to be on the table: what a
goroutine's stack is allowed to do, what the scheduler and the collector see
during a foreign call, and what a foreign frame on a Go stack costs.

This page builds that model from the code cppgo emits. Each section starts with
the real source of the mechanism it explains — the trampoline, the probe, the
callback table, the generated call — and then says what it is doing.

## A goroutine, an M and a P

The trampoline that calls C++ says one thing about the Go ABI in a comment:

```asm
// R14 holds g in the Go ABI and is deliberately never touched here.
TEXT ·call(SB), NOSPLIT, $0-32
```

`g` is the **goroutine**: the stack, the frame chain and the saved registers of
one lightweight thread. An **M** is an operating-system thread that executes it.
A **P** is the scheduling context an M needs in order to run Go code: the local
run queue, the allocator cache, the state the scheduler hands between threads.
`GOMAXPROCS` Ps exist, so that many Ms can run Go code at once.

A cppgo call is made by the goroutine itself: the trampoline names no scheduler
function, takes no lock and asks for no thread. Whether that is enough depends on
what the runtime does with the goroutine and the P while the C++ frame runs, which
is the rest of this page.

## The stack is copied, not extended

Go stacks are contiguous and grow by allocating a larger stack and copying the
old one. The copy is the operation that decides what a pointer is worth:
`runtime.copystack` moves the bytes and then walks the new stack adjusting only
the pointers its stack maps describe
([`runtime/stack.go`](https://github.com/golang/go/blob/master/src/runtime/stack.go)).
A pointer in a register, in a C++ frame, or in a `uintptr` is not in a stack map
and does not follow the copy.

Every Go function starts with the check that triggers that copy. So does a Go
assembly function, unless it is marked `NOSPLIT` — the assembler emits the same
preamble, comparing `SP` against the stack guard with the frame size included and
calling `runtime.morestack` when the frame does not fit. cppgo uses that preamble
deliberately instead of avoiding it:

```asm
TEXT ·probe(SB), $32768-0
	MOVB	AX, 0(SP)
	RET
```

`probe` is a 32 KiB frame with one store in it
([`internal/asm/probe_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/probe_amd64.s)).
Entering it runs the stack check; the frame stays allocated below the caller's
`SP` when it returns. That is `ProbeStack`, and the 32 KiB is the reserve a
foreign call may use — enough for the deepest generated shim frame:

```go
// StackReserve is the goroutine stack headroom, in bytes, that Probe guarantees
// below the point it is called from: enough for the deepest generated C++ shim
// frame, and small enough that the probe costs a frame adjustment rather than a
// copy.
const StackReserve = 32 << 10
```

The frame is written in assembly rather than as a Go local because a Go array of
that size is zeroed on entry, which would cost more than the call it protects.
The store at `0(SP)` is a single byte, not a page loop: the memory is already
mapped, and the store is what makes the frame real.

The consequence for a binding is the first rule of the design: **an address into
a goroutine frame is only valid until the next growth.** A C++ object cannot live
in a Go frame, because a growth would copy its bytes without running any special
member and without telling the object.

## The heap does not move, and a frame cannot be pinned

The collector traces the heap concurrently, and it does not move heap objects.
What it guarantees for a Go object handed to C is written on `runtime.Pinner`, and
cppgo's `Object` is built on it:

```go
// Object is storage for a C++ value whose layout Go cannot see.
type Object struct {
	// pinner keeps the storage from moving while C++ holds its address. It is
	// the mechanism, not a policy: the collector is free to move anything it
	// does not know is pinned, and the C++ side is not traced.
	pinner runtime.Pinner
	// storage is the allocation the pinned address points into. Keeping the
	// slice reachable gives the collector something to trace; a bare uintptr
	// would not.
	storage []byte
	// addr is the aligned address the C++ side sees.
	addr uintptr
	...
}
```

`NewObject` makes the allocation, pins its first byte and records the aligned
address inside it
([`object.go`](https://github.com/Manu343726/cppgo/blob/main/object.go)). Two
properties follow, and both are needed: the pin is what tells the runtime the
address is in use by foreign code, and the `storage` slice keeps the allocation
reachable so the collector has something to trace. A bare `uintptr` would look
like an integer.

The pin is the runtime's stated guarantee, even though the collector of today
does not compact the heap; cppgo does not depend on it staying that way.

A frame address is not an option here at any price: `Pinner.Pin` does nothing for
an address that is not in the heap, so a value on the goroutine stack cannot be
pinned. That is why a bound C++ object lives in storage Go allocated on the heap
and never in a local variable.

## Safepoints, preemption, and what the runtime cannot see

The runtime stops a goroutine at a **safepoint**: a point in Go code where the
stack maps describe every frame, so the stacks can be scanned and, if needed,
copied. Since Go 1.14 the runtime can also preempt asynchronously, by signal. It
only does so where `isAsyncSafePoint` accepts the program counter, and that
function rejects anything that is not Go code with a stack map
([`runtime/preempt.go`](https://github.com/golang/go/blob/master/src/runtime/preempt.go)).

A foreign call presents the runtime with exactly the state it cannot describe:

- the trampoline is `NOSPLIT` assembly with no stack map and no local frame —
  the only words it declares are the argument slots in `$0-…`, which the caller
  filled;
- the C++ frames below it are not Go frames at all: no stack map, no type
  information, no return address the runtime knows.

So while a direct call runs, the goroutine stays `_Grunning`, reaches no
safepoint, and cannot be preempted or have its stack copied. The frame is stable
for the duration of the call — which is what makes passing a pointer to C++ safe
at all — and the runtime waits for the call to return before it can stop that P
for a collection.

## Entering and leaving a foreign call

The runtime's own entry for "this thread is inside foreign code" is
`runtime.cgocall`, and cppgo reaches it without cgo, by link name:

```go
// cgocall is runtime.cgocall: it enters a foreign call (m.incgo, the g0 stack,
// the C ABI) and calls its first argument with its second as the argument. It is
// the same entry cgo-generated code uses, and reaching it by linkname is what
// keeps cgo out of the build: no import "C", and no toolchain cgo at compile
// time.
//
//go:linkname cgocall runtime.cgocall
func cgocall(fn, arg unsafe.Pointer) int32
```

That comment is the whole difference between the two carriers. A `cgocall` entry:

- marks the M as being in a cgo call (`m.incgo`, which the callback path
  requires);
- runs the function on the **g0** stack, the M's system stack, not on the
  goroutine's stack;
- goes through the scheduler's system-call accounting, which drops the P while
  the thread is out of Go and picks one up again on return.

The direct trampoline does none of that. It calls C++ on the goroutine's own
stack, keeps the P, and pays for it with the constraints above: no growth, no
preemption, no callback.

## cgo's path, and cppgo's

### cgo

A cgo binding writes the call in Go and lets the toolchain build the wrapper:

```go
func Add(a, b int64) int64 {
	return int64(C.bench_add(C.int64_t(a), C.int64_t(b)))
}
```

`C.bench_add` is a generated Go wrapper; behind it the runtime enters the C ABI
through `runtime.cgocall`, which calls `runtime.asmcgocall`
([`runtime/asm_amd64.s`](https://github.com/golang/go/blob/master/src/runtime/asm_amd64.s)):
that function switches to the g0 stack, aligns it and calls the wrapper there.
The C function therefore runs on the system stack, and the call costs a scheduler
transition in each direction.

### cppgo

cppgo loads the library and resolves the symbol with the dynamic loader, no cgo
and no compiler in the Go build:

```go
func (l *Library) Symbol(name string) (uintptr, error) {
	...
	addr, err := dlsym(l.handle, name)
	if err != nil {
		return 0, &Error{Op: "dlsym", Path: l.path, Name: name, Err: err}
	}
	return addr, nil
}
```

The generated package resolves every symbol it needs before the first call — the
thunks from the shim, and the direct entry points from your library by their
mangled names:

```go
// _ZN5mathx6Vector5scaleEd
cppgoSym_5e3a9d471306 uintptr
```

then calls the address from generated assembly. The next section is that
assembly.

## What `NOSPLIT` is

Once a declaration qualifies for the direct carrier — `mathx::Vector::scale`, in
the example:

```cpp
void scale(double k) noexcept;
```

the generated Go method is a probe, an address check, and a call:

```go
func (v *Vector) Scale(k float64) {
	if v == nil || v.obj == nil {
		panic("cppgo: mathx::Vector::scale(double) noexcept on a closed Vector")
	}
	if err := cppgoOpen(""); err != nil {
		panic(err)
	}
	cppgo.ProbeStack()
	cppgoCall_5e3a9d471306(cppgoSym_5e3a9d471306, v.cppgoAddr(), k)
	cppgo.KeepAlive(v)
}
```

and the assembly it calls is this
([`zz_cppgo_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo_amd64.s)):

```asm
// func cppgoCall_5e3a9d471306(fn uintptr, this uintptr, a1 float64)
TEXT ·cppgoCall_5e3a9d471306(SB), NOSPLIT, $0-24
	MOVQ	fn+0(FP), R12
	MOVQ	this+8(FP), DI
	MOVSD	a1+16(FP), X0
	MOVQ	SP, R13
	ANDQ	$~15, SP
	CALL	R12
	MOVQ	R13, SP
	RET
```

Four things in those seven instructions are the whole mechanism.

**`NOSPLIT`.** Without the flag the assembler would put the stack-split preamble
in front of the function: compare `SP` against the stack guard minus the frame
size, and call `runtime.morestack` when it does not fit. `morestack` allocates a
new stack, copies the goroutine's frames and restarts the function. That cannot
happen here: the trampoline is about to move `SP` to a foreign frame, its
arguments are addressed through `FP`, which is `SP`-relative, and a growth
between reading an argument and switching `SP` would leave the offsets pointing
at the wrong bytes. `NOSPLIT` says the function runs within the stack it was
entered with; the headroom it needs is why `ProbeStack` runs first, from Go, where
growing is still allowed.

**`$0-24`.** Zero bytes of local frame, 24 bytes of arguments and results: `fn`,
`this` and `a1`. `FP` is the pseudo-register for the argument area; with a
zero-sized frame it coincides with `SP` at entry, which is why the real
trampoline in [`internal/asm/call_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/call_amd64.s)
carries the rule in its comment: *every argument is read before `SP` is switched,
and every result is stored after `SP` is restored.*

**The alignment sequence.** In Go, `SP` is not required to be 16-byte aligned at
a call. The System V AMD64 ABI does require it: the callee expects `RSP+8` to be
16-aligned at its entry point, which means `RSP` is 16-aligned immediately before
`CALL`. So the trampoline saves `SP` in `R13`, rounds it down, calls, and puts it
back — `MOVQ SP, R13; ANDQ $~15, SP; CALL R12; MOVQ R13, SP`. `R12` holds the
target and `R13` the saved stack pointer because both are callee-saved by SysV,
so the callee must preserve them; `R14`, which holds `g`, is never touched.

**The argument registers.** `this` goes in `DI` first, then the declared
arguments in `SI`, `DX`, `CX`, `R8`, `R9`, and floating-point arguments in
`X0`–`X7`. That is the same convention the shim's thunks use, and it is why the
same trampoline can carry a C++ method or a free function.

`ProbeStack` and the trampoline are two halves of one rule: **the stack is grown
in Go, before the call, or not at all.** The probe is called before any pointer
into the frame is formed, because the growth it may trigger moves that frame.

## The C++ side

### The Itanium ABI

A C++ symbol is not a name, it is an encoding. `mathx::Vector::scale(double)`
`noexcept` is `_ZN5mathx6Vector5scaleEd`: namespace, class, member and parameter
types, in the Itanium C++ ABI's mangling. `dlsym` resolves exactly that string,
and the trampoline calls the resulting address with the SysV register convention.
The `this` pointer is the first argument, the object's pinned storage address; the
`double` arrives in `XMM0`; `noexcept` means the callee promises not to throw, so
the call needs no error channel at all.

### What the ABI cannot express: the shim

Everything a raw register call cannot carry — an inline body with no exported
symbol, a `std::string`, a record passed by value, a constructor, or a function
that may throw — goes through a generated `extern "C"` function compiled by
cppgo. For `mathx::Vector::norm`:

```cpp
double norm() const;
```

becomes, in [`examples/mathx/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp):

```cpp
extern "C" int32_t cppgo_thunk_49993cda6560(const void * self, double * cppgo_result) {
	cppgo_error_message.clear();
	try {
		*cppgo_result = static_cast<const ::mathx::Vector *>(self)->norm();
		return 0;
	} catch (const std::exception &error) {
		cppgo_error_message = error.what();
		return 1;
	} catch (...) {
		cppgo_error_message = "unknown C++ exception";
		return 1;
	}
}
```

The status return is the error channel: `0` for success, non-zero for a caught
exception, with the message retrievable through `cppgo_last_error`. The same
thunk is what makes the carrier cost what it costs — a status store and an
out-parameter allocation — which is the difference between 40.9 ns and 94.2 ns on
the same arithmetic in [Performance]({{< ref "explanation/benchmarks.md" >}}).

### Exceptions cannot cross a Go frame

C++ unwinding walks the stack looking for a handler, and it needs unwind
information for every frame it passes. Go frames have none: a C++ exception that
reaches a Go frame does not unwind, it terminates the process. The shim therefore
catches at the language boundary — the last place where a C++ frame is still on
top — and the Go side only ever sees a status and a message.

The same rule shows up in the other direction: a panic inside a Go callback does
not reach C++. It is captured, the result is zeroed, and the panic is re-raised
in Go after the foreign call returns, because that is the first moment a panic may
propagate.

### The P is not released

The direct trampoline never enters the scheduler, so the P stays attached to the
M for the whole call. That is the price of the crossing being cheap: no system
call, no P handoff, no scheduler transition — and, while the C++ frame runs, that
P is not available to run other goroutines, the goroutine is not preemptible, and
a stop-the-world collection waits for the call to return. The design assumes the
calls are short. A long-running C++ function in a direct binding stalls
scheduling on that P for its duration, and no amount of it being `noexcept` helps.

## The other direction: the reentrant carrier

A callback reverses the direction: C++ calls a Go function. The runtime only
allows that entry on a thread that is already inside a foreign call, which is what
`cgocall` establishes. The mechanism is a table of call slots:

```asm
// callbackTable is the address space C calls into. Each slot is one five-byte
// relative CALL to callbackasm1, so slot i's address is callbackTable + 5*i and
// callbackasm1 recovers i by dividing the return address by the instruction
// length. Nothing here returns in the usual sense: callbackasm1 leaves the
// C-ABI frame untouched and returns straight to the C caller, so a slot that
// returns means the table was entered at a bad address, which the Go
// dispatcher reports as a released slot.
TEXT ·callbackTable(SB), NOSPLIT|NOFRAME, $0
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
```

The trampoline saves the fourteen argument registers, works out which slot it was
entered through from the return address, and enters the runtime with
`runtime.cgocallback`, which runs the Go function on the goroutine that owns the
call. The result comes back in `RAX`/`XMM0`, and the C caller's return address is
put back where `RET` will find it.

That path is only legal through `cgocall`. On the direct trampoline the same
callback arrives on an M that is not in a foreign call, and the runtime dies at
the next scheduler transition — `morestack on g0`. It is why the carrier is a
property of the declaration: a binding that passes a Go function must be entered
through the reentrant carrier, whatever its signature would otherwise allow.

## Why the design is what it is

| Constraint from the runtime | What it forces |
|---|---|
| A goroutine stack is copied; only stack-mapped pointers follow | No C++ object or long-lived pointer in a Go frame; `ProbeStack` before forming one |
| A foreign call cannot be preempted or have its stack copied | A `NOSPLIT` trampoline with no local frame, so nothing in it can be moved or interrupted |
| C++ needs unwind information for every frame it crosses | The generated shim catches at the boundary; an exception never reaches Go |
| The runtime only accepts a callback inside a foreign call | A third carrier, entered through `cgocall`, for declarations that pass a Go function |
| A direct call keeps the P and cannot be preempted | Cheap crossing, and the reason the calls must stay short |

The three carriers are the smallest set that satisfies those constraints:

- **direct** — no cgo, no scheduler transition, one register call: 40.9 ns
  against cgo's 107.8 ns on the same arithmetic
  ([Performance]({{< ref "explanation/benchmarks.md" >}}));
- **thunk** — a call the C ABI cannot express, carried by compiled C++ with a
  status return, at close to cgo's price;
- **reentrant** — the only shape in which C++ may call back into Go.

Everything else in [Architecture]({{< ref "explanation/architecture.md" >}}) is a
consequence of one row in that table.

## Related

- The design built on this model: [Architecture]({{< ref "explanation/architecture.md" >}}).
- Which carrier each declaration gets: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What a call costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
