---
title: "The Go runtime and a native call"
description: "What a goroutine, its stack, the scheduler and the collector do around a C++ call, and what that forces cppgo's design to be."
weight: 15
---

A Go program can call a C++ function. The call crosses a line that no Go code
crosses on its own: the function was compiled by a different compiler, it knows
nothing about goroutines, and at the machine level it expects its arguments in
places Go never puts them. An interface like this is an **FFI**, a *foreign
function interface*: Go calling code that was not compiled as Go. A call that
leaves Go for such code is a **foreign call**. The rules that decide where
the arguments and the result travel are the **ABI**, the *application binary
interface* — which registers carry an integer, which carry a floating-point
value, where a value too big for the registers goes, and who puts the stack back
afterwards.

Go has its own way to do this: **cgo**, which needs a C toolchain at build time
(`CGO_ENABLED=1`) and writes a wrapper for every function it calls. cppgo is not
cgo. It calls the function directly from Go, with no C compiler in the build —
and that directness is what makes the rest of this page necessary.

The call the page follows is one method of a small C++ class. It is declared in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp)
and is the one member of that class that takes the cheapest route across that
line:

```cpp
    // Out of line and noexcept: the only member the direct carrier can take.
    // The generated Go signature has no error result at all.
    void scale(double k) noexcept;
```

`cppgo gen` reads that declaration and writes the Go method that calls it, in
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

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

Five things in that method are decisions about the Go runtime:

- `cppgoSym_5e3a9d471306` is the address of the C++ function in the loaded C++
  library, resolved by name before the call is made.
- `cppgoCall_5e3a9d471306` is a **trampoline**: a few instructions of assembly
  that load the arguments into the registers the C++ function expects, make the
  call, and come back. It is the assembly half of the direct carrier.
- `ProbeStack()` makes room on the goroutine's stack, because the C++ function
  will use that same stack and Go may not have left enough of it.
- `cppgo.KeepAlive(v)` tells the collector — Go's garbage collector, the runtime
  component that frees whatever nothing can reach — that the object must survive
  until after the call, even though nothing in Go reads it again.
- the receiver is the object's C++ address, not a Go pointer to a C++ value:
  `v.cppgoAddr()` is storage cppgo allocated outside the goroutine's stack and
  pinned, which means held at one address for as long as C++ uses it.

cppgo calls the mechanism that carries one call across the boundary a
**carrier**, and picks one per declaration. `scale` gets the *direct* carrier.
Why each step above is necessary, and why a C++ object cannot simply be a Go
local, is the order the runtime raises them in: a Go call first, then goroutines,
then growing stacks, then what the collector is allowed to move, then the points
where the runtime may stop Go code, then the foreign call itself.

## One Go function calling another

Start with the machine. A **register** is one of a small fixed set of named
storage cells inside the CPU; the code can read and write them in a single
instruction, which is why arguments travel in them. The rest of the state lives
in memory, and the part of memory a function call uses is the **stack**: a
region that grows downwards, towards lower addresses.

When a Go function calls another, the machine does three things:

- it writes the **return address** — the address of the instruction after the
  call — at the current bottom of the stack, so the callee's `RET` knows where to
  continue;
- it jumps to the callee, which subtracts from the **stack pointer** to make room
  for its own data. The stack pointer (`SP`, `RSP` on amd64) is the register that
  names the current bottom of the stack;
- it passes the arguments in registers, and reads the result out of a register
  when the callee returns.

The block of stack one call owns is its **frame**: the arguments that did not fit
in registers, the locals, the saved registers the callee must restore, and the
return address. A stack trace is a walk of frames, each one naming the function
that owns it — which is exactly what Go prints when a program panics.

Here is one Go function calling another, from the cppgo runtime
([`internal/asm/call_amd64.go`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/call_amd64.go)):

```go
func Call(fn unsafe.Pointer, a *Args) (uintptr, float64) {
	if a == nil {
		panic("cppgo: asm.Call: nil Args")
	}
	return call(fn, a)
}
```

`Call` is ordinary Go. Its frame holds `fn`, `a` and its own saved state, and
when it calls `call` the machine writes a return address just below that frame.
`call` is the assembly trampoline explained in
[The trampoline, line by line](#the-trampoline-line-by-line), and it receives its
arguments the way any Go function does: in registers, moved onto the stack by a
wrapper the toolchain generates, because assembly addresses them there.

That is where Go assembly names its arguments. `fn+0(FP)` is not the first
argument as a register, it is the first argument as the caller left it in the
argument area of the frame, and `FP` is the pseudo-register for that area: an
offset such as `+0(FP)` is measured from the caller's stack pointer at the
moment of the call. The assembler's generated wrapper copies the incoming
argument registers into that area, which is why the trampoline can name its
arguments `fn`, `a`, `r` and `f` by offset even though Go passed them in
registers.

Two more facts about this machine are needed before a C++ call can be explained.
The first is which registers a call may destroy: under the System V AMD64
convention — the ABI that Linux, the C++ compiler and Go's own C entry points
share — `RAX`, `RCX`, `RDX`, `RSI`, `RDI`, `R8`–`R11` and every `XMM` register
belong to the caller and may be overwritten by the callee, while `RBX`, `RBP`,
`R12`–`R15` are **callee-saved**: the callee must give them back unchanged.
The second is the order of the arguments: the first six integer or pointer
arguments go in `RDI`, `RSI`, `RDX`, `RCX`, `R8`, `R9`, the first eight
floating-point arguments in `XMM0`–`XMM7`, anything beyond that on the stack, and
the result comes back in `RAX` (integer) or `XMM0` (floating point).

## Goroutines and OS threads

A goroutine's stack is not the whole picture. Three runtime objects sit under
every running Go function, and the rest of the page uses all three.

A **goroutine** is one Go function running concurrently, with its own stack that
starts small and grows. It is not an operating-system thread: the runtime can run
many of them on a handful of threads, and the runtime — not the kernel — decides
which one runs. `g` is the runtime's bookkeeping struct for one goroutine: its
stack bounds and its saved registers when it is not running. While the goroutine
runs, `g` itself is never far away: the Go ABI keeps a pointer to it in a fixed
register. The trampoline says so in a comment, and then respects it:

```asm
// R14 holds g in the Go ABI and is deliberately never touched here.
TEXT ·call(SB), NOSPLIT, $0-32
```

`R14` is that register on amd64: whatever the trampoline does, it must give `g`
back.

An **M** is an operating-system thread — the runtime calls it a machine — which
is what the kernel schedules and what the CPU actually executes. When goroutine
code has to block in a way the runtime cannot see around (a file read, a network
wait), the M blocks and the runtime starts or reuses another one.

A **P** is the permission an M needs in order to run Go code. It is not a thread
and it does not execute anything: it is a token, like the badge a worker needs
before entering a workshop. The badge also carries what the holder needs while it
is inside — a short queue of goroutines to run next and a per-P cache of the
allocator — so the fast path of scheduling never takes a global lock.
`GOMAXPROCS` is the number of badges: by default the number of logical CPUs, and
changing it changes how many Ms may be inside Go at the same time. An M without a
P can still be inside the runtime or blocked in a system call; it just cannot run
Go code.

A goroutine therefore runs when an M holds a P and picks it from a queue, and it
runs on that goroutine's own stack. That is how a cppgo call starts: the
trampoline names no scheduler function, takes no lock and asks for no thread. It
runs on the goroutine's stack, with the P held, in the middle of ordinary Go
execution. Everything that goes wrong later in this page is a consequence of
that one sentence.

## Why a Go stack grows by copying

A goroutine starts with a small stack, and a deep call chain needs more. The
stack is contiguous, so "more" cannot mean extending it in place: the runtime
allocates a new, larger stack and copies the old one into it. The check that
triggers this sits at the top of every Go function — the compiler emits it in Go
functions, and the assembler emits it in assembly functions except the ones that
opt out. It compares `SP` against the **stack guard**, the lowest address the
current stack may use, with the function's frame size already subtracted, and
jumps to `runtime.morestack` when the frame would not fit. `morestack` allocates
the new stack and calls `runtime.copystack`
([`runtime/stack.go`](https://github.com/golang/go/blob/master/src/runtime/stack.go)),
which copies the frames over and then walks the new stack fixing up pointers.

It cannot fix up every pointer, because it does not know where they are. What it
knows is the **stack map**: a table the compiler emits for each function, which
says, for a given instruction, which words of the frame and which registers hold
pointers, and what those pointers point at. `copystack` adjusts the pointers the
maps describe and leaves every other word alone.

That single sentence decides what a pointer is worth in Go:

- a pointer in a Go variable on the stack **is** described by a stack map, so it
  follows the copy;
- a pointer in a register, at the moment of a copy, is not in a frame and is not
  adjusted unless the runtime has a register map for that exact instruction;
- a pointer held as a `uintptr` — an integer wide enough to hold an address — is
  an integer as far as the maps are concerned: it is not adjusted, and the value
  silently becomes an address into the old stack;
- a pointer held inside a C++ frame is not described by anything Go knows, so it
  is not adjusted either.

The consequence is the first rule of the whole design: **an address into a
goroutine frame is valid only until that stack next grows.** A C++ object cannot
live in a Go frame. A growth would copy its bytes without running any C++ code
at all — not even a *move constructor*, the constructor C++ calls when an object
changes address — and without telling the object it had moved; any pointer to it
that C++ had stored would now name the old copy.

## The heap, the collector, and pinning

Heap memory is different. The **heap** is the memory the collector manages:
Go allocates an object there, and the collector finds the objects that are still
reachable by following pointers from the goroutine stacks and the globals. An
object nobody can reach is freed eventually.

The collector is free to move a heap object while it does this, and a C++ object
has to keep one address for its whole life. The runtime's answer is **pinning**:
pinning an object tells the collector that this object's address is in use
outside Go, so it must not be moved or freed until the pin is released. In Go
that is `runtime.Pinner`, with `Pin` and `Unpin`.

cppgo's `Object` is built on exactly that, in
[`object.go`](https://github.com/Manu343726/cppgo/blob/main/object.go):

```go
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
	// refs counts the Go-side values sharing the object.
	refs atomic.Int64
	// release is the concrete destructor, run at the last reference.
	release func()
}
```

and `NewObject` allocates the storage and pins it:

```go
	storage := make([]byte, size+align-1)
	o := &Object{storage: storage, release: release}
	// Pin the first byte: the pinner holds the whole allocation in place.
	o.pinner.Pin(&storage[0])
```

Both fields are needed, and they do different jobs. The pinner asks the runtime
not to move the object; `storage` keeps the allocation reachable, so the
collector has something to trace and cannot free it while C++ is using it. A
bare `uintptr` in a struct would look like a plain integer to the collector: the
object could be freed, and the address could later belong to something else.

The current collector does not compact the heap, so pinning changes nothing
observable for an object that is alive anyway. cppgo does not rely on that: the
pin is the runtime's stated guarantee, and the code would stay correct if the
collector started moving objects.

A frame address is not an option at any price. `runtime.Pinner` pins a heap
object: given a pointer that is not in the heap it does nothing at all, and
silently — so pinning an address on a goroutine stack protects nothing. Even if
it did, the stack copy described in the previous section would move the object
and leave the C++ side holding a stale address. A C++ value therefore lives in
Go-allocated, pinned heap storage, never in a local variable.

`cppgo.KeepAlive(v)` in the generated `Scale` closes the remaining hole. After
`v.cppgoAddr()` has been read, Go never touches `v` again, so the collector may
consider the object dead *while the C++ call is still running*. `KeepAlive` makes
the reference live until after the call returns, which is why it is the last
statement of every generated call.

## Safepoints, preemption, and what the runtime cannot see

The runtime needs to stop Go code from time to time: to grow a stack, to scan
the stacks for the collector, or to **preempt** a goroutine — stop it from the
outside — that has run too long. It may only do so at a **safepoint**: an
instruction at which it can describe every frame of the goroutine. At a safepoint
the compiler has recorded stack maps for the whole stack and register maps for
the registers in use, so the runtime can walk the frames, find every pointer, and
copy or scan the stack if it has to.

Being allowed to stop only at a safepoint is a trade the runtime makes on purpose.
A goroutine running a tight loop with no calls would never reach one, so since
Go 1.14 the runtime also preempts asynchronously
([the release notes](https://go.dev/doc/go1.14)): it sends a signal, and the
signal handler asks `runtime.isAsyncSafePoint` whether the interrupted program
counter is an acceptable place. That function accepts Go code with a stack map,
and rejects anything else
([`runtime/preempt.go`](https://github.com/golang/go/blob/master/src/runtime/preempt.go)).

A foreign call presents the runtime with exactly the state it cannot describe.
The trampoline is `NOSPLIT` assembly — `NOSPLIT` being the attribute that forbids
the function from growing the stack, explained in
[Why `NOSPLIT`](#why-nosplit) — with no stack map and no local frame: the only
words it declares are the argument slots its caller filled. The C++ frames below
it are not Go frames at all: no stack map, no Go type information, and a layout
the runtime has no way to read.

So the rule falls out of the definition rather than out of a special case: while
a direct call runs, the goroutine reaches no safepoint, cannot be preempted, and
cannot have its stack copied or scanned. The frame is therefore stable for the
duration of the call, which is what makes passing a pointer to C++ meaningful at
all. The price is on the other side: a **stop-the-world** collection — the
collector pausing every goroutine to take a consistent snapshot — waits for that
call to return, and the goroutine is not preemptible while it runs. A direct call
must be short, and that is a property of the declaration, not something the
runtime can enforce.

## Entering a foreign call

The runtime does have an entry for "this thread is now inside foreign code", and
cgo-generated code uses it. cppgo reaches the same entry without cgo, by link
name, in
[`internal/asm/reentrant_amd64.go`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/reentrant_amd64.go):

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

`//go:linkname` is the compiler directive that binds this Go declaration to a
symbol in another package — here one of the runtime's own functions, which is not
exported and could not be imported. It is what lets a module call the runtime's
internal entry without an `import "C"` and without a C compiler.

`runtime.cgocall` only answers when the runtime believes the program uses cgo:
its entry check reads `runtime.iscgo`, a flag normally set by linking
`runtime/cgo`. The dynamic loader cppgo is built on — `purego`, wrapped by
[`internal/rtl`](https://github.com/Manu343726/cppgo/blob/main/internal/rtl/rtl.go)
— brings in its `fakecgo` package, which sets that flag without a C toolchain.
That is what makes the runtime's foreign-call entry reachable from a program
built with `CGO_ENABLED=0`.

`runtime.cgocall` does four things that the direct trampoline does not:

- it sets `m.incgo`, a flag on the M saying "this thread is executing foreign
  code". The runtime's own callback entry requires that flag, and
  [The other direction](#the-other-direction) depends on it;
- it calls `entersyscall()` before the foreign function and `exitsyscall()`
  after, so the **P is released** while the thread is out of Go and acquired
  again on the way back. That is the whole cost of the carrier: another goroutine
  can use the P meanwhile, and the return may land on a different M
  ([`runtime/cgocall.go`](https://github.com/golang/go/blob/master/src/runtime/cgocall.go));
- it runs the foreign function on the **g0** stack. Every M has two stacks: it
  runs one goroutine at a time on that goroutine's stack, and it has its own
  system stack, `g0`, for runtime work that must not run on a goroutine stack —
  the scheduler, signal handling, and entries into foreign code. `g0` is the
  thread's own working bench, separate from the bench a goroutine brings with it.
  Because the foreign call runs there, its frames are not bounded by the
  goroutine's stack or by the 32 KiB the probe reserves; what limits them is the
  g0 stack the runtime sized for its own use. The generated Go still probes
  before a reentrant call, for the pointers it forms into its own frame — the
  probe is simply not what makes room for C++ here;
- it enters the C ABI with the M in a state the runtime understands, so the
  runtime knows this thread is not running Go and does not try to stop it.

The direct trampoline does none of that. It calls C++ on the goroutine's own
stack, keeps the P, and pays for it with exactly the constraints the earlier
sections derived: no growth, no preemption, no stack map, no callback. That is
also why it is cheap: no system call, no P handoff, no scheduler transition in
either direction. The same crossing is available both ways, and cppgo uses the
cheap one wherever the declaration allows it.

## The trampoline, line by line

The runtime's general trampoline is
[`internal/asm/call_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/call_amd64.s).
Its signature is not known when it is written — it takes the address of the
foreign function and a block of arguments — so it loads both register banks the
System V convention defines:

```asm
TEXT ·call(SB), NOSPLIT, $0-32
	MOVQ fn+0(FP), R12
	MOVQ a+8(FP), AX

	// Integer/pointer bank.
	MOVQ 0(AX), DI
	MOVQ 8(AX), SI
	MOVQ 16(AX), DX
	MOVQ 24(AX), CX
	MOVQ 32(AX), R8
	MOVQ 40(AX), R9

	// Floating point bank.
	MOVSD 48(AX), X0
	MOVSD 56(AX), X1
	MOVSD 64(AX), X2
	MOVSD 72(AX), X3
	MOVSD 80(AX), X4
	MOVSD 88(AX), X5
	MOVSD 96(AX), X6
	MOVSD 104(AX), X7

	// Enter the C ABI.
	MOVQ SP, R13
	ANDQ $~15, SP
	CALL R12
	MOVQ R13, SP

	// Results, once FP is meaningful again.
	MOVQ AX, r+16(FP)
	MOVSD X0, f+24(FP)
	RET
```

The syntax first. `TEXT ·call(SB), NOSPLIT, $0-32` declares a function: `·call`
is its name in the current package, `(SB)` says the name is relative to the
static base the linker fixes up, `NOSPLIT` is the attribute explained in
[the next section](#why-nosplit), and `$0-32` is the frame: `0` bytes of local
variables, `32` bytes of arguments and results that the caller reserves. `FP`,
`SP`, `MOVQ`, `MOVSD`, `ANDQ`, `CALL` and `RET` are the whole vocabulary the code
below uses:

- `MOVQ` copies 64 bits (a "quad word") between a register and memory;
  `MOVSD` copies one 64-bit floating-point value, and its two operands are the
  memory address and the `XMM` register, in either order;
- `ANDQ $~15, SP` clears the low four bits of `SP`, rounding it down to a
  multiple of 16;
- `CALL` pushes the return address and jumps; `RET` pops the address and jumps
  back;
- `R12`, `R13` and `R14` are callee-saved registers, and `R14` is special
  because it holds `g`; `DI`, `SI`, `DX`, `CX`, `R8`, `R9` are the integer
  argument registers, `X0` is `XMM0`, and `AX` is `RAX`.

Now the instructions, in the order they run:

- `MOVQ fn+0(FP), R12` reads the foreign function's address from the first
  argument slot and puts it in `R12`, which is callee-saved, so the register
  still holds the address after the call returns.
- `MOVQ a+8(FP), AX` loads the address of the argument block into `AX`, a
  caller-saved register, to use as the base for the offsets that follow.
- the six `MOVQ` from `0(AX)` to `40(AX)` load the integer and pointer arguments
  into `DI`, `SI`, `DX`, `CX`, `R8`, `R9`; the eight `MOVSD` load the
  floating-point arguments into `X0`–`X7`. Every argument is placed exactly where
  the System V convention puts it, which is the only thing "calling C++" means at
  this level.
- `MOVQ SP, R13` saves the Go stack pointer in `R13`, also callee-saved.
- `ANDQ $~15, SP` aligns it. The System V convention requires `RSP` to be a
  multiple of 16 immediately before a `CALL`, because the callee expects
  `RSP+8` to be a multiple of 16 at its first instruction — the `CALL` has just
  pushed the 8-byte return address. Go makes no such promise to a function it did
  not compile, so the trampoline establishes the alignment itself; Go's
  `runtime.asmcgocall` does the same thing with the same instruction.
- `CALL R12` makes the call. From here until it returns, C++ code is running on
  the goroutine's stack.
- `MOVQ R13, SP` puts the stack pointer back, and only now is `FP` meaningful
  again.
- `MOVQ AX, r+16(FP)` and `MOVSD X0, f+24(FP)` store the results: `RAX` carries
  the integer result and `XMM0` the floating-point one.
- `RET` returns to Go.

The order is not stylistic. `FP` is measured from `SP`, so the moment `SP`
changes, every `fn+0(FP)` in the function means something else. Hence the rule
the file states in its header comment: **every argument is read before `SP` is
switched, and every result is stored after it is restored.** It also explains
why `R12` and `R13` are used for the state that has to survive the call: both are
callee-saved, so the C++ function is obliged to preserve them, whatever it does
with the rest of the register file. And it explains why `R14` is never touched:
it holds `g`, and a trampoline that clobbered it would leave the runtime unable
to say which goroutine it is running.

The generated trampoline for the page's example is the same code with the
signature baked in
([`examples/mathx/zz_cppgo_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo_amd64.s)):

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

`scale` takes one `double` and returns nothing, so the generator baked the
register assignment into the trampoline instead of passing an argument block:
`this` goes straight into `DI`, `k` into `X0`, there is no result to store, and
the frame is 24 bytes instead of 32. The trampoline is generated
per signature because the register assignment is a property of the declaration;
the general one above exists for calls whose signature cppgo does not know when
it writes the Go, such as reading the shim's error message.

## Why `NOSPLIT`

Without `NOSPLIT`, the assembler would put the stack-split preamble in front of
the function, the same check every Go function gets: compare `SP` against the
stack guard with the frame size subtracted, and call `runtime.morestack` when it
does not fit. `morestack` allocates a larger stack, copies the frames, fixes up
the pointers its stack maps describe, and restarts the function on the new stack.

None of that may happen here. The trampoline is about to move `SP` into the C++
frame; its arguments are addressed through `FP`, which is relative to `SP`; a
growth between reading an argument and switching `SP` would move the frame and
leave the offsets pointing at the wrong bytes — and a growth during the call
could not even describe the C++ frames it would have to walk over. `NOSPLIT`
means what it says: this function runs within the stack it was entered with, and
never asks for another one. `ProbeStack()` in the generated Go method is the
other half of that sentence: the stack is grown in Go, before the call, or
nowhere.

The probe is this function
([`internal/asm/probe_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/probe_amd64.s)):

```asm
TEXT ·probe(SB), $32768-0
	MOVB	AX, 0(SP)
	RET
```

It is not `NOSPLIT`, and that is the point: entering it runs the preamble, and
the preamble sees a frame of `32768` bytes, so the goroutine's stack is grown
whenever fewer than 32 KiB are left below the caller. The single store writes one
byte at the bottom of that frame — the byte a foreign call would reach first.
The stored value is never read; the store exists so that the frame is real code
touching real memory. It is deliberately not a loop over every page: a Go stack
is already mapped, so there is nothing to fault in, and one store at the bottom
is enough to make the frame exist.

When `probe` returns, `SP` is back where it was, but the stack itself is larger:
the goroutine now has at least 32 KiB below its current frame, and those are the
bytes the C++ prologue will consume when the trampoline calls into C++. The
amount is the constant the probe's frame must match
([`internal/asm/probe.go`](https://github.com/Manu343726/cppgo/blob/main/internal/asm/probe.go)):

```go
// StackReserve is the goroutine stack headroom, in bytes, that Probe guarantees
// below the point it is called from: enough for the deepest generated C++ shim
// frame, and small enough that the probe costs a frame adjustment rather than a
// copy.
//
// It must match the frame size of probe in probe_amd64.s. The deep-stack test in
// the cppgo package fails on a mismatch.
const StackReserve = 32 << 10
```

The frame is written in assembly rather than as a Go array for a reason that is
not about the ABI: a Go function with a 32 KiB local array zeroes the array on
entry, and zeroing 32 KiB costs more than the call the probe protects. Assembly
has no such initialization, so the probe costs a frame adjustment and a single
store.

Every generated method calls `ProbeStack()` before the call and before any
pointer into its own frame is formed — because the growth it may trigger moves
that frame, and a pointer formed before the growth would be a pointer into the
old copy.

## The C++ side

What the address in `R12` points at is a function whose name in the library's
symbol table is not the name anyone wrote. C++ has namespaces, classes and
overloads, so the compiler encodes the declaration into the symbol, and the
encoding is the **name mangling** — a **symbol** being the string the linker and
the dynamic loader use to find a function. On Linux and most other Unix systems
the encoding follows the Itanium C++ ABI.

`mathx::Vector::scale(double)` becomes `_ZN5mathx6Vector5scaleEd`, and every part
of that string is the declaration:

| Part | Meaning |
|---|---|
| `_Z` | the prefix that marks a mangled name |
| `N` … `E` | a nested name: namespace and class, up to the terminating `E` |
| `5mathx` | a name of 5 characters: `mathx` |
| `6Vector` | a name of 6 characters: `Vector` |
| `5scale` | a name of 5 characters: `scale` |
| `E` | end of the nested name |
| `d` | one parameter of type `double` |

`const` and the parameter types are part of the encoding too — a `const` member
mangles as `_ZNK…`, which is why `norm()` is `_ZNK5mathx6Vector4normEv`. cppgo
computes this string, resolves it with `dlsym` — the lookup by name that the
dynamic loader performs, the loader being the part of the system that opens a
shared library at run time — and calls the resulting address; the C++ compiler's
own parser is never involved at run time.

The call itself is the System V convention described above, applied to a member
function: `this` is the first argument, so it arrives in `DI`; the declared
arguments follow in `SI`, `DX`, `CX`, `R8`, `R9` for integers and pointers and in
`X0`–`X7` for floating-point values; the result comes back in `RAX` or `XMM0`.
`scale` is a non-const member taking one `double`, so its two arguments are the
object address in `DI` and `k` in `X0` — exactly what the generated trampoline
loads. A free function has no `this` and starts filling the argument registers
with its first parameter instead; that is the only difference, and it is why one
trampoline shape serves both.

`noexcept` changes no register and no calling convention. What it changes is
control flow: it is the callee's promise that it will not throw. That promise is
what makes the direct call legal, because the direct path has no C++ frame above
the call that could catch anything: an escaping exception would terminate the
process (see [The shim](#the-shim)). A declaration that is not `noexcept` is
therefore never called directly, whatever its signature would allow.

Two more conditions come from the linker rather than the language. The function
must have an out-of-line definition, because an inline function has no symbol in
the library; and the symbol must be exported, because `dlsym` can only find what
the library publishes. `scale` satisfies both, which is why it is the one member
of `mathx::Vector` the direct carrier can take.

## The shim

A register call carries bytes. What it cannot carry is a C++ declaration whose
meaning lives in the compiler: an inline body with no symbol to call, a
constructor or destructor, a `std::string` whose layout belongs to the standard
library, a record passed by value that needs its copy constructor, or a call that
may throw. Those need C++ code, generated and compiled.

cppgo writes that code. The **shim** is one C++ translation unit — one source
file as the compiler sees it — compiled by cppgo into `libcppgo_<package>.so`, a
shared library the Go program loads at run time. A **thunk** is one `extern "C"`
function in it that receives the Go call and calls the real declaration.
`extern "C"` is the linkage that turns off name mangling, so the symbol in the
shim really is `cppgo_thunk_49993cda6560` and `dlsym` finds it by that exact
string. For `mathx::Vector::norm`, whose declaration may throw, the generated
thunk is this one
([`examples/mathx/cppgo_shim/zz_cppgo_shim.cpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/cppgo_shim/zz_cppgo_shim.cpp)):

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

Read it as three decisions:

- **The status is the return value.** `norm` returns a `double`, but the thunk
  returns an `int32_t`: `0` for success, non-zero for a caught exception. The
  actual result leaves through `cppgo_result`, a pointer into Go storage the
  caller provides. That keeps one shape for every thunk: the status is always in
  `RAX` and the value always goes through storage, so the same generated Go
  carries a `double`, a `std::string` or a record.
- **The message is thread-local.** `cppgo_error_message` is declared in the same
  file as a `thread_local std::string` and cleared at the top of every thunk, so
  a concurrent call on another thread cannot see this one's message and a
  successful call can never report a stale error. It is read back through
  `cppgo_last_error`, which returns its `c_str()`.
- **The call is inside `try`.** Everything that may throw is inside the block,
  including the copy of `error.what()`.

Why the `try` has to be here, and not in Go, is **unwind information**. A C++
exception does not "jump" to a handler: the unwinder walks the stack frame by
frame, and for each frame it needs the compiler's record of how to undo it —
where the return address is, which registers were saved, which objects to
destroy. That record is emitted by the C++ compiler into the object file's
exception tables. Go frames have no such records: the Go compiler emits its own
metadata for the Go runtime's stack walking, and nothing DWARF-readable for a
foreign unwinder. An exception that reaches a Go frame therefore does not unwind
into Go; the process terminates. The shim catches at the language boundary,
which is the last place where a C++ frame is still on top and a handler still
exists, and Go only ever sees a status and a message.

The same rule runs in the other direction. A panic raised inside a Go callback is
not allowed to reach C++, for the symmetric reason: a panic unwinds Go frames
using Go's own metadata, and there is nothing for it to unwind into once the
stack becomes C++. It is captured at the trampoline, the callback's result is
zeroed, and the panic is re-raised in Go once the foreign call returns — the
first moment a panic may propagate. That path is in
[The other direction](#the-other-direction); the user-facing consequences are on
[Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}).

One more term is worth having here: a **POD** — *plain old data* — is a struct
with no constructors, no virtual functions and a standard layout, such as
`mathx::Point`. A 16-byte POD still goes through a thunk when it crosses by
value, because copying it is C++'s job even when the bytes are simple; what makes
it cheap is that the copy is a memcpy between two 16-byte blocks with the same
layout.

Which constructs go through the shim at all, and which are refused, is the
[carrier and construct table]({{< ref "explanation/carriers.md" >}}#the-construct-table).

## The other direction

So far C++ code only ever runs because Go called it. A **callback** reverses
that: C++ is given the address of a Go function and calls it, while the Go call
that led there is still on the stack.

The address C++ receives is not a Go function pointer — C++ could not call one.
It is the address of a slot in a table of five-byte `CALL` instructions, in
[`internal/callback/trampoline_amd64.s`](https://github.com/Manu343726/cppgo/blob/main/internal/callback/trampoline_amd64.s):

```asm
TEXT ·callbackTable(SB), NOSPLIT|NOFRAME, $0
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
	CALL ·callbackasm1(SB)
```

The file continues that instruction up to 64 slots, the table's whole capacity.
Each `CALL` is five bytes of relative call, so the slots are five bytes apart, and
a slot's address is the table's address plus five times its index. That is the
point of the trick: when C++ calls the slot, the `CALL` inside it pushes an
address inside the table, and the trampoline recovers the index from that address
without any argument:

```asm
	// slot = (return address - callbackTable) / 5 - 1
	MOVQ R11, AX
	LEAQ ·callbackTable(SB), CX
	SUBQ CX, AX
	XORQ DX, DX
	MOVQ $5, CX
	DIVQ CX
	DECQ AX
```

`R11` was loaded, a few instructions earlier, with the return address the slot's
`CALL` pushed. Subtracting the table's base gives the byte offset of the *next*
slot's instruction, and dividing by five gives the index plus one, which is what
the `DECQ` removes.

Before that, the trampoline saves the register file: the six integer argument
registers and the eight `XMM` registers go into the same 128-byte argument block
the call trampoline reads from. It then fills a small record with the index and a
pointer to that block, and enters the runtime through `crosscall2`, which is the
runtime's own C-to-Go entry:

```asm
	MOVQ ·dispatch(SB), DI
	MOVQ (DI), DI
	LEAQ 128(SP), SI
	MOVQ $0, CX
	CALL crosscall2(SB)
```

`DI` is the address of the Go dispatcher, `SI` points at the record describing
the call, and `crosscall2` reaches `runtime.cgocallback`. That entry is the only
door by which C code may enter Go with the runtime's knowledge: it switches to
the stack of the goroutine that owns the call, locks the M to that goroutine, and
runs the registered Go function there, with the C frames left exactly where they
are. When the Go function returns, the result is written into the block, and
`callbackasm1` moves it to `RAX` or `XMM0`, restores the C caller's return
address where `RET` will find it, and returns to C++ — a normal C function
return, from C++'s point of view.

And this is the constraint that decides a carrier. `runtime.cgocallback` only
works on a thread that is inside a foreign call, because that is the state it
pairs with: the `entersyscall` that dropped the P, the `m.incgo` flag, the g0
stack, and the `exitsyscall` it performs to run Go code again. On the direct
trampoline none of that is true: the M is in the middle of Go code, holds its P,
and is not in a foreign call. The callback then arrives in a state the runtime
cannot account for, and the runtime dies at the `exitsyscall` that follows with
its fatal error `morestack on g0`. It is not a panic a program can recover from;
the process is gone. The test that pins this behaviour is in
[`internal/callback`](https://github.com/Manu343726/cppgo/tree/main/internal/callback).

The consequence is structural rather than a caveat: a binding whose signature
passes a Go function to C++ must be entered through the reentrant carrier,
whatever else its signature would allow. The carrier is a property of the
declaration, decided at generation time, and the table above is why.

## Summary

Each section above paired one assumption of the Go runtime with one fact about
C++, and each pair forces a decision:

| What the runtime does | What that forces |
|---|---|
| A goroutine stack is copied, and only stack-mapped pointers follow the copy | No C++ object and no long-lived pointer in a Go frame; `ProbeStack` before forming one |
| A foreign call has no stack map, so it is no safepoint | A `NOSPLIT` trampoline with no local frame, so nothing in it is stopped or moved |
| The stack cannot grow during the call, and the C++ frame uses it | The probe grows it in Go first, with 32 KiB of headroom |
| A C++ exception needs unwind information for every frame it crosses | The shim catches at the boundary; an exception never reaches Go |
| A callback is only legal inside a foreign call | A third carrier, entered through `runtime.cgocall` |
| A direct call keeps the P and cannot be preempted | The cheapest crossing, and the reason a direct call must stay short |

The three carriers are the smallest set that satisfies those rows:

- **direct** — the declaration is `noexcept`, out-of-line, non-virtual and its
  arguments fit the registers, so the trampoline calls its mangled symbol: no
  cgo, no scheduler transition, one register call, `this` in `DI`;
- **thunk** — the call the C ABI cannot express goes through a generated
  `extern "C"` function compiled into the shim, which returns a status and a
  message;
- **reentrant** — the same call entered through `runtime.cgocall`, which is the
  only state in which C++ may call back into Go.

What each carrier costs, and the measured difference between them, is on
[Performance]({{< ref "explanation/benchmarks.md" >}}). Everything else in
[Architecture]({{< ref "explanation/architecture.md" >}}) is a consequence of one
row of the table above.

## Related

- The design built on this model: [Architecture]({{< ref "explanation/architecture.md" >}}).
- Which carrier each declaration gets: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
- What a call costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
- What the mechanisms stop at: [Limits]({{< ref "explanation/limits.md" >}}).
