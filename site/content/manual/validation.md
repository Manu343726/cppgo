+++
title = "Validation and the report"
description = "What cppgo gen checks before it emits a trampoline, where the compilation flags come from, how to read the per-binding report, and what -force does and does not downgrade."
weight = 80
+++

## Why validation exists

A generated trampoline is a promise that a symbol exists and that its arguments
fit the registers. Both can be checked before the program runs, and in the
direct ABI mode they are: `cppgo gen` fails the build rather than emit something
that segfaults in production.

The header is not enough on its own. It **declares** `Vector(double, double);`
and the definition lives in a `.cpp`. So the generator parses the header for
the API surface and the mangled names, and every translation unit that includes
it for the bodies, then merges the sightings by mangled name. The merged symbol
answers both questions: what the call looks like, and whether a symbol exists.

Which sources are parsed:

- the header itself, when it is a source file;
- source files in the header's directory;
- entries in the compilation database that include the header.

At most eight including sources are parsed; any one that defines the out-of-line
members answers the question. If your definitions live somewhere the discovery
does not look, the generator sees a declaration with no body and refuses the
binding as inline — even though the built library does export the symbol.

## Where the compilation flags come from

A translation unit is only meaningful with the flags it was compiled with:
without them a header may not even find its own includes. cppgo takes them from
clangd's configuration instead of inventing them:

1. `-compile-commands <path>` when given (a `compile_commands.json` or its
   directory), else the `CompileFlags.CompilationDatabase` named by `.clangd`,
   else the nearest `compile_commands.json` walking up from the header.
2. `.clangd` files in the header's directory and its ancestors, merged as clangd
   does: `CompileFlags.Compiler`, then `CompileFlags.Remove`, then
   `CompileFlags.Add`.
3. Otherwise `-std=c++17 -I<header dir>`, **reported as an assumption**.

The language is always made explicit (`-x c++-header` for headers), because
libclang would otherwise treat a `.h` as C and produce unmangled names. The
input file, `-c` and `-o` are dropped from the discovered arguments.

When the flags are the fallback, the report says
`abi from fallback flags (assumed)` and a warning goes to stderr:

```
cppgo: warning: the compilation flags for /src/mathx/vector.hpp are an
assumption (assumed: -std=c++17 -I/src/mathx); a validation this misses comes
from a parse without the header's real flags
```

That warning is serious for a header with its own includes: a failed include
means libclang sees a partial declaration, and the validation is only as good
as what it saw. Point `-compile-commands` at your build's database.

## Reading the per-binding report

One line per binding, on stdout, ordered by qualified C++ name so an overload
set reads together:

```
mathx::Vector::Vector       mangled _ZN5mathx6VectorC1Edd      abi from compile_commands.json; note: the object is allocated in Go storage owned by this package (16 bytes, alignment 8) and released by //cppgo:dtor
mathx::Vector::dot          mangled _ZNK5mathx6Vector3dotEPKdm  abi from compile_commands.json; note: the trailing error is always nil: a direct C++ call has no status channel (return an int32 status and declare only error to get one)
mathx::Vector::norm         mangled _ZNK5mathx6Vector4normEv    abi from compile_commands.json
mathx::Vector::~Vector      mangled _ZN5mathx6VectorD1Ev       abi from compile_commands.json
```

Read the third column first. It names the tier that answered:

| value | meaning |
|---|---|
| `abi from compile_commands.json` | flags came from a compilation database, with the entry it used in the detail |
| `abi from .clangd` | flags came from `.clangd` files |
| `abi from arguments` | flags were supplied by a caller of the generator API, not by the CLI |
| `abi from fallback flags (assumed)` | flags were guessed; the parse may be incomplete |

Then the notes. Two of them are load-bearing:

- **`the object is allocated in Go storage ... (N bytes, alignment A)`** — the
  size the constructor allocates. If it looks wrong, the probe translation unit
  did not see the real class (an `#ifdef` that was not set, a different header
  with the same name).
- **`the trailing error is always nil ...`** — the binding declared an `error`
  that the C++ function cannot produce, because it does not return an integer
  status. The code is correct; the error is always `nil`. If that is not what
  you wanted, change the C++ function to return `int32_t`.

`-force` adds a third kind of note: `forced with -force, NOT validated: <why>`.
It appears both in the report and as a `// NOT VALIDATED` comment in the
generated file, so an accepted-but-unchecked binding is never invisible.

## The checks

Each of these is a build failure with a message that names the binding, the
file and the line:

| check | message (short form) |
|---|---|
| declaration not found | *is not declared in \<header\> as a bindable function (a declaration in a system header, or one with internal linkage, is invisible)* |
| no mangled name | *it has no mangled name, so it is a template ... only an explicit instantiation emits a symbol* |
| inline / internal linkage | *defined inside its class definition or has internal linkage ... move the definition out into a source file* |
| virtual | *it is virtual: the call has to go through the vtable ... use the shim mode* |
| wrong arity | *the binding describes N arguments but the C++ signature takes M* |
| width mismatch | *the Go type is int (8-byte) but the C++ type is "int" (4-byte)* |
| register class mismatch | *the Go type is float64, a float (XMM), but the C++ type is "int", which travels in the other register class* |
| struct by value | *a struct passed by value ... take it by const reference or use the shim mode* |
| `std::` type | *a standard-library type: its ABI (and its lifetime) is the shim mode's business* |
| by-value class result | *returns X by value ... use the shim mode for this binding* |
| register exhaustion | *needs N integer argument registers, but the SysV ABI only provides 6 ...* |
| duplicate symbol | *X and Y both bind \<mangled\>* |
| ambiguous overload | *N overloads accept the Go argument types ... rename the Go function* |

`-force` downgrades the non-fatal ones — inline, virtual, width/class mismatch,
by-value struct parameter, arity, overload ambiguity, and a binding that names
the wrong C++ entity — to warnings, for the case where you know better than the
parse. It does **not** downgrade a missing mangled name (there is no address to
call) or a class returned by value (the call cannot be emitted at all): those
remain build errors, because no code could be generated.

<div class="callout callout-danger">
<span class="callout-title">-force does not fix the ABI</span>
<p>
It emits the binding exactly as your Go declaration describes it. If the Go
declaration is wrong, the call is wrong: a width mismatch accepted with
<code>-force</code> still passes a 4-byte value in an 8-byte slot, and the C++
side reads whatever the upper bits happen to be. Use it when the <em>parse</em>
is wrong, not when the code is.
</p>
</div>

## Warnings on stderr

Two package-level warnings, printed as `cppgo: warning: ...`:

- the assumed-flags warning above;
- a leftover `cppgo_shim.h` in the output directory from an earlier shim-mode
  run. ABI mode does not write that file and does not delete it, because the
  generator only owns the files it wrote. Leaving it behind invites the C++
  side to keep implementing entries nothing calls.

## Determinism

The generated bytes are a pure function of the Go sources, the header and the
flags. Running `cppgo gen` twice on the same input produces the same output,
which is what makes [`cppgo check`]({{< ref "manual/ci.md" >}}) a usable CI
gate: it regenerates in memory and compares with what is on disk.
