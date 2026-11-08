+++
title = "Generation input"
description = "What cppgo gen reads and every flag that changes it: the headers, the compilation flags, -bind and -exclude, -instantiate, -rename, -status and the C++ compiler. Includes the naming rules and the report."
weight = 30
+++

**Audience:** you have generated one package and need to shape a larger one, or
you are reading a `cppgo gen` invocation in a `Makefile`.

## One command

```console
$ cppgo gen -header <path> [-header <path>]... -package <name> \
      [-library <file>] [-out <dir>] [-compile-commands <path>] \
      [-bind <qualified>]... [-exclude <qualified>]... \
      [-instantiate '<specialization>']... \
      [-rename '<signature>=<GoName>']... [-status <qualified>]... \
      [-cxx <compiler>] [-check] [-q]
```

- `-header` is required and repeatable. One header or twenty. They are parsed
  together and form one package.
- `-package` is the Go package name.
- `-out` is where the files are written (default `.`). It is also where the shim
  is compiled, beside the generated Go.
- `-library` is your shared object. cppgo links the shim against it and, when it
  is a readable file, cross-checks every direct binding against its dynamic
  symbol table. Omit it for a header-only library. The shim is then the whole
  binary.
- `-compile-commands` is a `compile_commands.json` or the directory holding it.
  Use it. Without it the parser falls back to `-std=c++17 -I<header dir>` and
  warns that it is guessing.

The user writes no bindings. The input is C++. The output is a Go package, one
assembly file, and a C++ translation unit cppgo compiles.

## What is bound by default

**Every public declaration in the headers**, plus the type closure of every kept
signature. A header is the unit of selection. If
`vector.hpp` declares the watcher, the watcher is in the package.

That is deliberate. The alternative, binding only what a Go file names, makes
the Go file a second, stale description of the C++ API, which is the problem
header-first generation exists to remove.

What is *not* a declaration:

- **Macros.** libclang sees a macro's expansion. A macro that *is* the API (a function-like macro that expands to a call) is invisible and cannot be bound. The fix is a function, which every large library provides.
- **Constructs this build omits today** — enums, inheritance, static data
  members, member function templates, operators, nested types, variadic
  functions. They are refusals. The [validation
  page]({{< ref "manual/validation.md" >}}) lists every one with its message.

## `-bind` and `-exclude`: narrow the API

Both name a **root**: a namespace, a class, a member, or a function. A root keeps
itself and everything inside it. A name that matches nothing is an error. A typo cannot silently shrink the API.

```console
$ cppgo gen -header include/opencv2/opencv.hpp -package cv \
      -library build/libopencv_core.so -out ./cv \
      -bind cv::Mat -bind cv::cvtColor -exclude cv::detail
```

The type closure still applies. If a kept signature mentions `cv::Size`, `Size`
is emitted even when it is not a root. That keeps a narrowed package compiling.

## `-instantiate`: name a template specialisation

A C++ class template becomes a type only when it is instantiated, and a symbol appears at the same point. Name the concrete specialisation you want:

```console
$ cppgo gen -header include/tmpl/cell.hpp -package tmpl \
      -library build/libcell.so -out ./tmpl \
      -instantiate 'tmpl::Cell<int>' -instantiate 'tmpl::Cell<double>'
```

The header must make the instantiation visible, an explicit instantiation such
as `template class Cell<int>;` is the usual way. The generated types are named
`CellInt` and `CellFloat64`, and their constructors and methods are ordinary Go
functions:

```go
func NewCellInt(value int32) (*CellInt, error)
func (c *CellInt) Get() (int32, error)
func (c *CellInt) Set(value int32) error
func (c *CellInt) Close() error
```

Without `-instantiate`, a header that declares a class template is refused:

```
cppgo gen: cppgo: cell.hpp:9: tmpl::Cell: a class template is not a type until
it is instantiated; name a concrete instantiation with -instantiate 'tmpl::Cell<...>'
```

The generic Go API (`Matrix[float64,4,4]`) and the collection of the
instantiations a Go program uses are the next phase of the design. The concrete
API above is what the emitter produces today.

## `-rename`: resolve a Go name collision

Two C++ declarations can map to one Go identifier. Different namespaces with the
same class name, a method and a free function, or two overloads that no
parameter-type suffix separates. The generator refuses the collision and asks
for a name:

```console
$ cppgo gen ... -rename 'cv::detail::Mat=DetailMat'
```

An entry is `<qualified name or full signature>=<GoName>`. A name that matches no declaration is an error. So is a name that matches several declarations by qualified name. Name the full signature instead.

## `-status`: read a 32-bit return as a status

Some C libraries (and C++ ones that follow their style) report failure in the
return value: `0` is success, anything else is an error. Name the declaration
and the Go signature returns only `error`:

```console
$ cppgo gen -header include/stats/stats.hpp -package stats \
      -library build/libstats.so -out ./stats -status stats::ensure
```

```go
func Ensure() error
```

The C++ return has to be a 32-bit integer. A function that returns `void`, or a
`double`, is refused with the type it found. The status covers only part of the error story, see [error handling]({{< ref "manual/errors.md" >}}) for exceptions and
for what is designed but not emitted yet.

## Naming

The rules are fixed. A generated package reads like a Go package:

| C++ | Go |
|---|---|
| `mathx::Vector` | `Vector` |
| `mathx::Vector::Vector(...)` | `NewVector(...)` |
| `mathx::Vector::~Vector()` | `(*Vector) Close() error` |
| copy constructor | `(*Vector) Clone() (*Vector, error)` |
| `mathx::Vector::norm()` | `(*Vector) Norm()` |
| `mathx::Vector::dims()` (static) | `VectorDims()` |
| a free function `mathx::saxpy` | `Saxpy` |
| an overload set | the bare name for the zero-argument one, a parameter-type suffix otherwise (`Dot`, `DotFloat64Slice`) |

Namespaces leave the Go name unchanged. A name is CamelCase, split on
underscores. `Open` is reserved for the package entry point.

## The report and `check`

`cppgo gen` prints one line per binding on stdout, the signature, the carrier,
the symbol, and the reason, and ends with the shim it built:

```
mathx::Vector::scale(double) noexcept   direct _ZN5mathx6Vector5scaleEd   noexcept, out of line, not virtual, register class
cppgo: built mathx/libcppgo_mathx.so with /usr/bin/g++
```

`-q` suppresses it. `cppgo check` runs the same work with the writing turned
off. It compares the generated sources with the files on disk and exits non-zero
when one is stale. The compiled shim is a build artefact.
`check` compares the sources and leaves the binary to `gen`.

Next is [the type mapping]({{< ref "manual/type-mapping.md" >}}).
