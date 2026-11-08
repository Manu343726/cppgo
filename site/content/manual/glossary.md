+++
title = "Glossary"
description = "The terms this documentation uses, each defined once: ABI, calling convention, mangled name, symbol, translation unit, inline, vtable, sret, POD, libclang, trampoline, NOSPLIT, thunk, carrier, cgo, and the Go runtime words."
weight = 110
+++

**Audience:** anyone reading this site who meets a term they do not know. Each
entry is one or two sentences — enough to keep reading — and links to the page
that owns the mechanism when there is one.

## The C++ side

| term | what it means |
|---|---|
| **ABI** | Application binary interface. The agreement between compiled code and its caller: which register holds which argument, how a result comes back, how names are encoded in the binary. An API is what the compiler checks; an ABI is what already-compiled code assumes. |
| **calling convention** | The part of the ABI that says where arguments and results travel. On Linux/amd64 this is the [System V AMD64 ABI](#the-system-v-amd64-abi) — the same one C uses. |
| **header** | The file that *declares* a C++ API (`class Vector { ... };`) without the bodies. cppgo parses it; it is what `cppgo gen -header` names. |
| **declaration vs definition** | A *declaration* says a function exists and what it takes; a *definition* supplies the body. The header has declarations (usually), the `.cpp` has definitions. |
| **translation unit** | One `.cpp` file plus everything it `#include`s, as the compiler sees it. A header is not a program by itself; it only means something after preprocessing inside some translation unit. |
| **inline function** | A function whose body the compiler may paste at each call site. A member function defined *inside* its class definition is implicitly inline. Inline functions are not required to exist as a callable symbol in the compiled library, which is one reason a binding needs a thunk. |
| **out-of-line definition** | The opposite: the body lives outside the class definition, in a `.cpp`, so the compiler emits one callable symbol for it. A `noexcept`, out-of-line function is the direct carrier's favourite shape. |
| **internal linkage** | A name that is private to one translation unit (`static` at namespace scope, or an anonymous namespace). It is not exported, so nothing outside can call it. |
| **vtable** | Virtual table. For a class with virtual functions, the compiler puts a table of function pointers in each object, and a virtual call reads the table to find the override for the object's real type. A mangled symbol names one body, so it cannot dispatch like that; the generated thunk calls the method as C++ and lets the compiler emit the dispatch. |
| **virtual function** | A member function a derived class may override, dispatched through the [vtable](#the-c-side) at run time instead of called directly. |
| **`sret`** | "Structure return". A convention for returning a class or struct *by value*: the caller passes a hidden pointer to storage, and the function constructs the result there. It occupies an argument slot; the generated thunk passes that pointer into storage Go owns. |
| **POD** | Plain old data: a type whose bytes can be copied and whose lifetime needs no constructor — numbers, pointers, C structs. A POD whose fields are all public and whose layout matches Go's is *mirrored* as a Go struct; anything else is an opaque value with `Close`. |
| **`std::` type** | Anything from the C++ standard library (`std::string`, `std::vector`, `std::span`). They have their own layout, ownership and allocators, so they only cross inside a thunk, which constructs, copies and destroys them. |
| **`extern "C"`** | A C++ declaration that turns [name mangling](#the-call) off, giving the function a plain C name. The generated shim is a file of `extern "C"` thunks. |
| **shim** | In cppgo, the generated C++ translation unit `zz_cppgo_shim.cpp` holding one `extern "C"` thunk per binding the direct call cannot express. cppgo compiles it itself into `libcppgo_<package>.so`, linked against your library; your build never sees it. |

## The call

| term | what it means |
|---|---|
| **carrier** | How a bound declaration reaches C++. Every binding gets exactly one: **direct**, **thunk**, or **reentrant**. The carrier is invisible in the generated API and named in the report ([How it works]({{< ref "project/architecture.md" >}})). |
| **direct carrier** | The generated `NOSPLIT` trampoline calls the mangled symbol in your library. Reserved for `noexcept`, non-virtual, register-representable functions whose symbol the library exports. |
| **thunk** | The generated `extern "C"` function in the shim that calls the target as C++: an inline body, a virtual method, a template instantiation, a `std::` type, a record by value, an exception-catching wrapper. The Go side still calls it through a trampoline; the extra cost is one C++ frame. |
| **reentrant carrier** | The call path for a signature that passes a Go function to C++. It enters through `runtime.cgocall`, because the runtime only accepts a callback on a thread already inside a foreign call. One 16-byte allocation per call. |
| **register** | A small storage slot inside the CPU. Arguments to a function travel in registers, not on the stack, on x86-64. |
| **register class** | Which family of registers an argument uses: integers and pointers use the general-purpose ones (`RDI`, `RSI`, `RDX`, …), floats use the SSE ones (`XMM0`–`XMM7`). The two are counted independently ([register assignment]({{< ref "manual/calling-convention.md" >}})). |
| **callee-saved** | A register whose value the called function must preserve. `R12` and `R13` are callee-saved, which is why the trampoline may keep its bookkeeping in them across the call. |
| **name mangling / mangled name** | The encoding a C++ compiler applies to a function name so that namespaces, classes, `const` and parameter types fit in one symbol. `mathx::Vector::norm() const` becomes `_ZNK5mathx6Vector4normEv`. You never type these; cppgo reads them from libclang. |
| **Itanium C++ ABI** | The C++ ABI that GCC and Clang implement on Linux: mangling, `this` placement, virtual tables, `sret`. "The Itanium ABI" on this site means that document, not a CPU. |
| **symbol** | A name in a compiled object that refers to code or data. A *dynamic* symbol is one exported for the loader, which is what `dlsym` can find and what `-library` cross-checks. |
| **`.dynsym`** | The dynamic symbol table of an ELF shared object: the names and addresses the loader can resolve. When `-library` is a readable file, cppgo reads it to prove that a direct binding's symbol exists. |
| **`dlopen` / `dlsym`** | The C library functions that load a shared object at run time and look up a symbol in it. cppgo calls them through purego, without cgo ([installation]({{< ref "manual/installation.md" >}})). |
| **shared object (`.so`)** | The Linux shared library file the Go binary loads at run time. Your C++ build produces yours; cppgo compiles the shim into a second one. |
| **soname** | The name a shared object records as its own identity (`libmathx.so`), which is what a linker writes into a dependent object's `DT_NEEDED` entry. The generated shim names your library by soname, never by a build path. |
| **trampoline** | Here, a small generated assembly function per bound signature. It moves the arguments into the registers the calling convention expects, switches to an aligned stack, calls the C++ symbol or thunk, and returns the result. The name comes from "carrying the call across" into foreign code ([How it works]({{< ref "project/architecture.md" >}})). |
| **`NOSPLIT`** | A Go assembly directive meaning "this function does not allocate and must not be interrupted to grow a stack". It is what makes the trampoline safe to run without the runtime's cgo bookkeeping. |

## The System V AMD64 ABI

The calling convention used on Linux/x86-64, and the one every generated trampoline
implements. The parts that matter for reading this site:

- The first six integer-or-pointer arguments go in `RDI`, `RSI`, `RDX`, `RCX`,
  `R8`, `R9`; float arguments go in `XMM0`–`XMM7`, counted separately.
- A non-static member function receives the object pointer (`this`) in the first
  integer slot; a hidden `sret` pointer takes the slot before it.
- Results come back in `RAX` (integers, pointers) or `XMM0` (floats).
- `RSP` must be 16-byte aligned before a `CALL`.

The full table, including what happens when the registers run out, is
[register assignment]({{< ref "manual/calling-convention.md" >}}).

## The boundary

| term | what it means |
|---|---|
| **status convention** | How a thunk reports failure: return `int32_t`, `0` for success, non-zero for failure, with a message available from `cppgo_last_error()`. `-status` applies the same reading to a C++ function whose 32-bit return is a status ([error handling]({{< ref "manual/errors.md" >}})). |
| **libclang** | The clang C++ parser packaged as a shared library. cppgo loads it (through purego) to read your header: types, sizes, mangled names, virtual-ness, and whether a definition exists ([installation]({{< ref "manual/installation.md" >}})). |
| **`cppgo.Object`** | The Go allocation an opaque C++ value lives in: pinned so its address does not move, reference-counted so a view can share it, released by the destructor at the last reference. It is how a generated type keeps its C++ object without exposing a pointer ([object lifetimes]({{< ref "manual/lifetimes.md" >}})). |
| **`cppgo.Handle`** | A low-level `uintptr` that refers to a foreign object. It appears in the runtime package only; a generated API never names it. |

## The Go runtime side

| term | what it means |
|---|---|
| **goroutine** | A Go function running concurrently, multiplexed by the runtime onto a smaller number of operating-system threads. |
| **`G`, `M`, `P`** | The scheduler's three letters: a goroutine, an OS thread, and a *processor* — the token a thread needs in order to run Go code. There are `GOMAXPROCS` of them, so at most that many goroutines run Go code at once ([why cppgo]({{< ref "project/why.md" >}})). |
| **`g0` stack** | The per-thread stack the runtime uses for its own bookkeeping. cgo switches to it before running C; the direct carrier does not switch at all. |
| **cgo** | Go's supported way to call C: `import "C"`, a C compiler invoked by the toolchain, and a runtime handshake per call ([why cppgo]({{< ref "project/why.md" >}})). |
| **GC** | Garbage collector. It finds pointers by walking goroutine stacks, which it can only do for frames it understands. |
| **finalizer** | A function the collector may run when an object becomes unreachable. cppgo does not use them: they run at unpredictable times, on an arbitrary goroutine ([object lifetimes]({{< ref "manual/lifetimes.md" >}})). |
| **thread-local storage / TCB** | Data that belongs to one operating-system thread. glibc keeps `errno` and related state in a thread control block it initialises; a non-cgo Go program installs its own, which is why the loader needs purego's `internal/fakecgo` ([why cppgo]({{< ref "project/why.md" >}})). |

Back to the [manual]({{< ref "manual/_index.md" >}}).
