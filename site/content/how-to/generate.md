---
title: "Generate a binding for your own library"
description: "The command, the compilation configuration, and what a generation error looks like."
weight: 10
---

You have a C++ library that builds, and you want a Go package that calls it.
Binding it means three things: name the header, tell cppgo how that header is
compiled, and run one command. This guide is those three steps, what the command
reports back, and what it refuses.

A C++ *header* is the file that declares a library's types and functions — the
C++ counterpart of a Go package's exported declarations, except that it is a file
textually included by whoever uses the library, so its text is all the compiler is
told. A *binding* is the Go package cppgo generates from it; *generating* means
running the command that writes that package.

Use it when you are binding your own library. If you have not bound anything
yet, [Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}) is the
same flow at teaching pace, with every output explained as it appears.

The scenario is the `mathx` library in
[`examples/mathx`](https://github.com/Manu343726/cppgo/tree/main/examples/mathx):
one header, one source file, a `Makefile`, and the generated package committed
beside them. Substitute your own paths where the guide uses its own.

## 1. The header you bind

cppgo reads a header, never a compiled library: the declarations are the source
of truth. This is the header the guide binds, `mathx::Vector` in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
namespace mathx {

class Vector {
public:
    Vector(double x, double y);

    Vector(const Vector &other);

    ~Vector();

    double x() const { return x_; }

    double y() const;

    double norm() const;

    double dot(const double *ys, std::size_t n) const;

    double at(std::size_t index) const;

    void validate() const;

    void scale(double k) noexcept;

    static int dims();

private:
    double x_;
    double y_;
};

} // namespace mathx
```

Nothing in it knows that cppgo exists, and nothing cppgo writes is compiled into
it. Every line is nevertheless something the generator has a rule for, so here is
the C++ it uses, in the order the file uses it:

- `namespace mathx { … }` wraps every name in `mathx`, the way a Go package
  qualifies a type: outside the block the class is `mathx::Vector`.
- `class Vector { public: … private: … }` declares a *class*, C++'s
  type-with-functions. The `public:` members are the API; the `private:` ones are
  state the Go side can neither see nor touch, which is why a method exists for
  each of them.
- `Vector(double x, double y);` is a *constructor*: the function that creates an
  object. It has no return type, and it becomes `NewVector` in Go.
- `Vector(const Vector &other);` is the *copy constructor*. `&` means
  *reference* — another name for an object that already exists, not a copy of it —
  and `const` in front of it means the call will not modify what `other` names.
- `~Vector();` is the *destructor*: the function the C++ runtime calls when an
  object is destroyed. In the Go package it runs from `Close`.
- `double x() const { … }` is a *member function* (a method). The `const` after
  the parameter list is a promise that the call does not modify the object.
- `void scale(double k) noexcept;` has two qualifiers: `void` says it returns
  nothing, and `noexcept` is C++'s promise that the call never *throws* an
  exception — C++'s way of reporting a failure, which is not a return value
  ([Report failures from C++ to Go]({{< ref "how-to/errors.md" >}}) is about it).
- `std::size_t` is C++'s unsigned type for sizes and indices, the width of a
  pointer — what Go calls `uint`. `const double *ys` is a *pointer*: the address
  of the first element of an array of doubles, which is how Go hands a slice's
  backing array to C++ without copying it.
- `static int dims();` is a *static member*: a function that belongs to the class
  but needs no object to run, so Go gets it as a package-level function.
- `double x_;` and `double y_;` are the object's two fields. The trailing `_` is a
  naming convention, not syntax.

## 2. State how the header is compiled

`cppgo gen` needs the include paths and the language standard your project uses.
There are two ways to supply them, and cppgo uses whichever it finds first:

- **A compilation database.** CMake, Meson and most large projects produce
  `compile_commands.json`, a file that records the exact compiler command line of
  every source file. Point at it and cppgo takes the entry that covers each
  header:

  ```console
  $ cppgo gen -header include/myproject/api.hpp -package myproject \
              -compile-commands build/compile_commands.json -out .
  ```

  Without `-compile-commands`, cppgo also looks for a `compile_commands.json` in
  the header's directory and in each directory above it, and uses the nearest
  one.

- **A `.clangd` file.** `.clangd` is the per-project configuration file of clangd,
  clang's language server, and cppgo reads the same file, so the flags the
  generator uses are the flags the editor uses. Its `CompileFlags.Add` list holds
  compiler arguments to add, and `CompileFlags.Remove` holds arguments to drop. The
  example's
  [`examples/mathx/.clangd`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/.clangd)
  is the whole of its compilation configuration:

  ```yaml
  CompileFlags:
    Add: [-std=c++17, -Iinclude]
  ```

  `-std=c++17` selects the C++ language version (C++17) the header is written
  for, and `-Iinclude` adds the directory `include` to the list of directories a
  `#include` is looked for in. cppgo merges every `.clangd` from the header's
  directory upwards, the nearer file winning.

A compilation database supplies the base command line and a `.clangd` layers its
`Remove` and `Add` on top of it. With neither, cppgo guesses and says so.

Do not skip this because the header compiles: a missing include path produces a
wrong binding rather than an error. When it finds no configuration at all, cppgo
prints a warning and assumes `-std=c++17` plus the directory the header is in:

```
cppgo: warning: the compilation flags for include/myproject/api.hpp are an assumption
(assumed: -std=c++17 -Iinclude/myproject); a parse without the project's real flags
can misread the header
```

Read it in three parts: the header the flags are for, the fragment the parser was
given in parentheses (`assumed:` — the language standard and one include path), and
the consequence. Note that the include path is the header's own directory and not
your project root, which is why a header that includes a sibling by a
project-relative name can still fail to parse.

## 3. Decide what to bind

By default, the header is the unit of selection: every public declaration in it
is bound, plus the types those declarations use. For a large library, narrow the
set with roots and exclusions:

```console
$ cppgo gen -header include/opencv2/core.hpp -package core \
            -bind cv::Mat -bind 'cv::cvtColor' -exclude 'cv::detail' -out ./cvcore
```

A *root* is one entry of that selection — a namespace, a class, a member or a
free function, named the way C++ names it. `-bind` adds a root and `-exclude`
removes one; both are repeatable, and `-exclude` wins over `-bind` for the
subtree it names. An entry that names nothing is an error, never a silent no-op:

```
cppgo gen: cppgo: -bind cv::Maat: it names nothing this header declares
```

The leading `cppgo gen:` is the command reporting, the second `cppgo:` belongs to
the error. That is deliberate. A typo that silently shrinks the API is the failure
mode worth an error, because nobody notices a missing function until it is needed.
The same check applies to `-rename` and `-status` entries.

## 4. Run it

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -out .
```

The command writes the Go package, the assembly and the C++ sources, compiles the
shim, and prints one report line per binding (step 5). Every flag it takes:

| Flag | What it does |
|---|---|
| `-header <path>` | the C++ header to bind; repeatable, and each header is a selection unit |
| `-package <name>` | the Go package name to write. There is no default; pass it |
| `-out <dir>` | where the generated package and the C++ files are written (default `.`) |
| `-library <file>` | your shared object — the `.so` file your C++ build produced. cppgo links the C++ side it builds against it, and when the file is readable it checks that every symbol behind a *direct* binding is really exported by it. **Omit it for a header-only library** |
| `-compile-commands <path>` | the compilation database, or the directory that holds it |
| `-bind`, `-exclude` | add or remove a selection root |
| `-rename 'sig=GoName'` | rename one declaration whose Go name would collide; `sig` is the qualified name or the full C++ signature |
| `-instantiate '<specialization>'` | name a concrete template specialisation, for a header you would rather not edit ([Bind a C++ template library]({{< ref "how-to/templates.md" >}})) |
| `-status <qualified>` | a declaration whose `int32` return is a status code rather than a value |
| `-cxx <compiler>` | the C++ compiler for the generated side (default: `g++`, then `clang++`) |
| `-check` | report staleness instead of writing (what `cppgo check` runs) |
| `-q` | suppress the per-binding report and the `cppgo: built …` line |
| `-scan`, `-scan-dir` | phase two's input, accepted by `cppgo build` and `cppgo check`; `cppgo gen` ignores them |

The files it writes, in `<out>`:

| File | Contents |
|---|---|
| `zz_cppgo.gen.go` | the Go API, the generated `Open` and the declarations of the trampolines |
| `zz_cppgo_amd64.s` | one assembly trampoline per binding |
| `cppgo_shim/zz_cppgo_shim.h` | the `extern "C"` declarations of the thunks |
| `cppgo_shim/zz_cppgo_shim.cpp` | the thunks themselves — generated C++ that carries what the direct call cannot |
| `libcppgo_<package>.so` | the shim, compiled by cppgo and linked against your library; the generated Go loads it at run time |

The complete reference is [Command line]({{< ref "reference/cli.md" >}}).

## 5. Read the report

The report has one line per binding, and a last line naming the compiler that
built the shim. Three of `mathx`'s lines and the last one, as the command prints
them:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

Read a line left to right; it has four columns:

1. **The declaration** — the C++ signature the line is about, as the header wrote
   it.
2. **The carrier** — how the call crosses the boundary. `direct` means the
   assembly trampoline calls the C++ function itself. `thunk` means it calls a
   small C++ function cppgo generated into the shim, which then makes the real
   call; every argument, result or exception the direct call cannot express takes
   this route. (`reentrant` is the third carrier, for a call that passes a Go
   function to C++.)
3. **The symbol** — the name the compiled object gives the function, here
   `_ZN5mathx6Vector5scaleEd` for a declaration you wrote as
   `mathx::Vector::scale(double)`. C++ encodes namespaces, classes and parameter
   types into that name; the encoding is called *mangling*, and a *mangled name*
   is what it produces. A `-` sits in this column for a template, because no
   concrete function exists yet.
4. **The reason** — the sentence that decided the carrier.

The last line is about the shim: `cppgo: built <file> with <compiler>` names the
shared object it compiled and the compiler it used. That compiler makes up the
other half of every call, so it must be the same family and the same standard
library as the one that built yours; this line is where you check it.

What the carriers mean for a call's speed, and why the choice exists, is in
[Architecture]({{< ref "explanation/architecture.md" >}}#generation-parse-selection-determinism);
you do not need it to use the package.

## 6. The generated Go, and the call

Four of the declarations in step 1 became these Go declarations, copied from the
generated
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
func (v *Vector) Close() error
```

`*Vector` is a handle to the C++ object, not a copy of it: the object lives on
the C++ side and `Close` destroys it. `Norm` returns an error because
`mathx::Vector::norm()` can throw; `Scale` returns nothing because
`mathx::Vector::scale(double) noexcept` cannot fail. A program calls the package
the way it calls any Go package:

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

norm, err := v.Norm()
if err != nil {
	log.Fatal(err)
}
fmt.Println(norm)          // 5

v.Scale(2)                 // no error to check: this call cannot fail
```

`defer v.Close()` runs the C++ destructor when the function returns, which is the
counterpart of the constructor `NewVector` ran. Nothing is loaded yet at
`NewVector`: the first call loads the shim and your library, and a failed load is
the error that call returns.

## 7. Commit the sources and check them

The generated sources are reproducible — the same inputs produce the same files
— so commit them with your project and let a check catch drift:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
              -library build/libmathx.so -out .
.: generated files are up to date
$ echo $?
0
```

`cppgo check` writes nothing and builds nothing: it regenerates the sources in
memory and compares them with the files on disk. It exits `0` when everything
matches, `1` when a file is missing or stale, and `2` when the command line
itself is wrong. The exact invocation is written into the banner of
`zz_cppgo.gen.go` — the `//` comment block at the top of the file — so the
command that produced a file lives beside it.

[Gate a generated package in CI]({{< ref "how-to/ci.md" >}}) puts the check in a
pipeline.

## When it refuses

A generation error names the file, the line, the declaration and the fix, and
nothing is written — not even a partial package, because the shim is compiled in
a scratch directory first. These are the ones you are most likely to meet:

| Message contains | What happened | What to do |
|---|---|---|
| `it names nothing this header declares` | a `-bind`, `-exclude`, `-rename` or `-status` entry matches no declaration | fix the qualified name or the signature |
| `operators and conversion operators are not emitted by this build yet` | the declaration is an operator | give it a named member or free function |
| `enums are not emitted by this build yet` | the header declares an enum | pass the value as its underlying integer at the C++ boundary |
| `inherited members and base views are not emitted by this build yet` | a class has base classes | bind the base directly, or expose the derived members through a wrapper |
| `the call needs N INTEGER and M SSE argument registers` | more arguments than the call can carry | take a pointer to a struct |
| `no Go value type` | `long double`, `__int128` or `nullptr_t` in a signature | wrap it at the C++ boundary |
| `and a status is a 32-bit integer` | a `-status` entry names a declaration that does not return a 32-bit integer | drop the flag, or bind the declaration without it |
| `the generated shim does not compile` | the generated C++ failed to compile with your flags | the error carries the compiler's diagnostic |

The full list, with a workaround for each, is
[Limits]({{< ref "explanation/limits.md" >}}).

If a construct you need is in that list, it is not a flag you have missed: it is
work this build has not done yet.

## Related

- A template-heavy library: [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).
- What the generated package looks like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Moving the result to a deployment: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
