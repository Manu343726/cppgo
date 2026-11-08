+++
title = "When generation refuses"
description = "Read the error, decide whether it is a construct this build does not emit, a manifest entry that names nothing, or a shim the compiler rejected — and what to do about each."
weight = 40
+++

The generator refuses rather than emitting a call it cannot explain. Every
refusal writes nothing, names the file, the line and the declaration, and ends
with the thing to supply. The skill is reading which of three kinds it is.

**Audience:** a `cppgo gen` run that failed.

## The three kinds

1. **A construct this build does not emit yet.** Enums, inheritance, operators,
   variadic functions, nested types, templates without `-instantiate`. The fix
   is in the C++ surface (move it, wrap it, name it) or the construct's turn in
   the emitter.
2. **A manifest entry that names nothing.** `-bind`, `-exclude`,
   `-instantiate`, `-rename` and `-status` are checked against what the parse
   saw; an unmatched entry is an error.
3. **A shim the compiler rejected.** The C++ generated for the thunks did not
   compile, because the flags or the compiler do not match the header.

There is no fourth kind and no override. v4 has no `-force` and no mode to opt
into: a construct is emitted or it is refused.

## Case 1: an enum

```cpp
enum class Unit { Metre, Foot };
```

```
cppgo gen: cppgo: include/mathx/add.hpp:5: mathx::Unit: enums are not emitted
by this build yet; pass the value as its underlying integer at the C++ boundary
```

**What it means.** Enums have no Go type yet. The refusal is checked before
selection, so an enum anywhere in the parsed headers fails the run even if you
did not bind it. Splitting the header is the workaround that keeps the rest
bindable.

**The fix today.** Pass the value as an integer at the C++ boundary: change the
signature to take the underlying type, or wrap the call in a named function that
does.

## Case 2: inheritance

```cpp
class Base { public: virtual ~Base(); };
class Derived : public Base { public: ~Derived() override; };
```

```
cppgo gen: cppgo: include/inherit.hpp:4: refuse::Derived: inherited members and
base views are not emitted by this build yet; bind the base class directly, or
expose the derived members through a C++ wrapper
```

**What it means.** `As<Base>()`, virtual dispatch across the hierarchy and
`this` adjustment are designed, not emitted. The direct carrier cannot dispatch
a virtual call, and the generated API has no way to express the base view yet.

**The fix today.** Bind the base class directly (its methods are signatures on
their own), or expose the derived members through a named free function that
takes the concrete type. A wrapper function is one line of C++ and keeps the
generated package honest.

## Case 3: a class template

```
cppgo gen: cppgo: cell.hpp:9: tmpl::Cell: a class template is not a type until
it is instantiated; name a concrete instantiation with -instantiate 'tmpl::Cell<...>'
```

**What it means.** A C++ template has no layout and no symbol until it is
instantiated. Name the specialisation, and make it visible to the parse (an
explicit instantiation such as `template class Cell<int>;`):

```console
$ cppgo gen -header include/tmpl/cell.hpp -package tmpl \
      -library build/libcell.so -out ./tmpl \
      -instantiate 'tmpl::Cell<int>' -instantiate 'tmpl::Cell<double>'
```

The generated API is concrete:

```go
func NewCellInt(value int32) (*CellInt, error)
func (c *CellInt) Get() (int32, error)
func (c *CellInt) Set(value int32) error
func (c *CellInt) Close() error
```

The generic Go API (`Cell[T]`) and the collection of instantiations a Go program
uses are the next phase; the concrete package above is what this build emits.

## Case 4: an operator

```
cppgo gen: cppgo: operators.hpp:4: refuse::operator+(const Num &, const Num &):
operators and conversion operators are not emitted by this build yet; give it a
named member function or a named free function
```

**What it means.** `operator+` has a symbol, but the Go name it should take is a
design decision this build has not made. The fix is to name it:

```cpp
Num add(const Num &a, const Num &b);
```

## Case 5: a variadic function

```
cppgo gen: cppgo: variadic.hpp:3: refuse::sum(int): a variadic function has no
fixed argument list to lay out; replace the ellipsis with a fixed parameter list
or a span
```

**What it means.** `...` has no fixed register layout, so there is nothing to
emit. Take a slice (`const double *values, std::size_t n`) or a `std::span`.

## Case 6: a nested type

```
cppgo gen: cppgo: nested.hpp:3: refuse::Outer::Inner: a type nested inside a
record is not emitted by this build yet; move the nested type to namespace
scope, or bind the outer type through a C++ wrapper
```

**What it means.** The Go name of `Outer::Inner` and its relationship to
`Outer` are part of the same unmade design decision as inheritance. Move it to
namespace scope in the header, or expose what you need through a function.

## Case 7: the shim does not compile

A real one, from a header that declares `cppgo_last_error` itself for its own
status convention:

```
cppgo gen: cppgo: zz_cppgo_shim.cpp:35: cppgo_last_error: the generated shim
does not compile; invalid conversion from 'const char*' to 'const int8_t*'
{aka 'const signed char*'} [-fpermissive]
(compiler: g++ -std=c++17 … -shared -o …)
```

**What it means.** The header declares the accessor the shim itself defines, and
the generator tried to bind it as an ordinary function. The diagnostic is the
compiler's, and cppgo re-compiled the thunks one at a time to name the binding.

**The fix.** Exclude the infrastructure symbol:

```console
$ cppgo gen -header include/stats/stats.hpp -package stats \
      -library build/libstats.so -out ./stats \
      -status stats::histogram -exclude cppgo_last_error
```

More generally, a shim compile error means the flags or the compiler do not
match the header. Pass `-compile-commands`, choose the library's compiler with
`-cxx`, and check that the header is self-contained. See
[troubleshooting]({{< ref "manual/troubleshooting.md" >}}).

## Manifest mistakes

| symptom | cause | fix |
|---|---|---|
| *`-bind X: it names nothing this header declares`* | typo, namespace alias, or a declaration in a system header | check the qualified name; move the include |
| *`-instantiate X: it names no instantiation this header declares`* | the explicit instantiation is not visible to the parse | add `template class X<...>;` to the header |
| *`-rename X: it must be <qualified signature>=<GoName>`* | the entry has no `=` or an empty side | write both sides |
| *`-rename X: it names 2 declarations; use the full signature`* | the name is an overload set | name the full signature |
| *`A and B both map to Go name X; rename one`* | a collision the overload suffix did not separate | `-rename` one of them |
| *`-status X: the return type is double, and a status is a 32-bit integer`* | the function does not return `int32_t` | name the function that does |

## The decision procedure

When a run fails, ask in this order:

1. **Does the message name a construct from [the refusal
   list]({{< ref "manual/validation.md" >}})?** The second half of the message
   is the fix: an integer at the boundary, a named function, a concrete
   `-instantiate`, a slice instead of `...`.
2. **Does it name a flag entry?** The entry does not match the parse. Fix the
   name, or make the declaration visible (flags, include, explicit
   instantiation).
3. **Does it say the generated shim does not compile?** The parse and the
   compile disagree about flags, compiler or a self-contained header. Fix those
   first; do not change the C++ API to satisfy a guessed flag set.
4. **Does it name a file and line in a header you do not own?** The construct is
   in a third-party header. Wrap the part you need in a small header of your
   own — a named function, a concrete type — and bind that.

The report prints nothing for a refused run, because nothing was emitted. The
error is the whole answer.

Back to the [manual]({{< ref "manual/_index.md" >}}), or read the same failures
by symptom in [troubleshooting]({{< ref "manual/troubleshooting.md" >}}).
