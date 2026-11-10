---
title: "Limits"
description: "What cppgo cannot do, and what to do instead."
weight: 30
---

There are three kinds of limit, and they behave differently:

- **hard limits** — properties of the platform or the ABI, the *application
  binary interface*: how a compiled call is laid out, which registers and which
  memory carry its arguments and its result. No amount of generator work removes
  them.
- **not supported yet** — the construct does not bind, and the generator says so
  with a message naming the declaration and the fix. A declaration is left out
  with a warning; a template pattern that stops the run is an error. The
  construct never silently degrades.
- **designed** — stated in the design, not in this build. Marked as such wherever
  it appears in the documentation.

## Hard limits

Four things in this table need saying first. A **binding** is one C++ declaration
turned into a callable Go function. An exception *unwinds*: it walks the frames
above the `throw`, looking for a `catch` (a handler), and needs unwind information
for every frame it crosses. A *foreign call* is a call from Go into machine code
the Go toolchain never compiled. The *register file* is the fixed set of CPU
registers a calling convention passes arguments in — nothing on the stack counts,
and the callback path reads only those registers.

| Limit | Why | What you can do |
|---|---|---|
| An exception with no C++ frame terminates the process | the [direct path]({{< ref "explanation/carriers.md" >}}#direct) has no handler above it, and unwinding through Go frames is not supported, because a Go frame carries no unwind information | give the library a `noexcept` entry point — one that promises not to throw — that reports failure as a status |
| A callback cannot be called from a direct binding | the runtime only accepts a callback on a thread already inside a foreign call | none: bindings that take a Go function use the [reentrant carrier]({{< ref "explanation/carriers.md" >}}#reentrant) automatically |
| A callback carries at most 6 integer and 8 floating-point arguments, and one result | the callback entry receives the register file and nothing else | take a pointer to a struct |
| A `std::` layout appears only in C++ | constructing a `std::string` or `std::function` (C++'s wrapper around any callable) from a Go-side layout — the type's in-memory representation, the bytes and their order — works only for instantiations the library happens to export | let cppgo construct it (it does, for a parameter or a result) |
| A function-like macro that *is* the API cannot be bound | a macro is a text substitution performed before compilation, and its expansion is not a declaration | ask the library for a function |
| A template type nobody asked for does not exist | a template only exists where it is instantiated | run `cppgo build -scan` with the program that uses it |
| `long double` and `__int128` have no Go value type | they are the two C++ types wider than 64 bits, and there is no Go type of that width | widen or narrow at the C++ boundary |
| The C++ side must share your library's ABI and standard library | the two call each other across that boundary | build with the same compiler family and standard library; nothing detects a mismatch for you |

## Not supported yet

Two things can happen, and they cost different amounts. A **declaration the
build cannot carry** is a warning: the rest of the header binds, the package is
written, and one line on stderr names the declaration and the fix. A **pattern
the generic API cannot model** stops the whole header with an error: nothing is
written, because the pattern has no shape the two-phase flow can emit.

A warning that leaves one declaration out has this shape, and a row whose
message differs quotes the whole line:

```
cppgo: warning: <declaration> is not bound: <what is wrong>; <fix>
```

An error names the file and the line of the declaration:

```
cppgo: <file>:<line>: <declaration>: <what is wrong>; <fix>
```

Most rows quote the `<what is wrong>; <fix>` half, which is what you read after
`is not bound:`. A handful of terms need defining first. A **record** is a struct or
class used as a bundle of fields, and it is **mirrorable** when Go can hold its
bytes directly — every field public, the natural Go layout, and no pointer,
reference or other field Go cannot represent. A **POD** — *plain old data* — is
the strongest form of that: a record with no constructor, no virtual function
and no private members, whose bytes are its whole representation. An **opaque
record** is the other extreme: a C++ struct whose layout Go cannot see, so Go
holds only a pointer to storage C++ owns. A **template pattern** is a class or
function template before any concrete type is substituted, such as
`template <typename T> class Box`; a **dependent type** is a type that mentions
a template parameter, such as `std::vector<T>`, and a **parameter pack** is the
`...` that stands for an argument list of unknown length. The messages also name
the convention's two classes of argument register: **INTEGER**, the six
general-purpose ones, and **SSE**, the eight floating-point ones beside them.

### A declaration the build does not bind (warning)

| Construct | Message | Workaround |
|---|---|---|
| a variadic function (a parameter list ending in `...`) | `a variadic function has no fixed argument list to lay out; replace the ellipsis with a fixed parameter list or a span` | a fixed list, or a pointer and a length |
| a member function template | `<declaration> is a member function template and is not bound: name its concrete instantiation as a non-template member beside it` | a non-template member |
| an operator outside the naming table | `"<operator>" is an operator outside the naming table; rename it with -rename '<signature>=<GoName>'` | `-rename`, or a named function |
| an operator of a class template | `<signature> is not part of the generic API: an operator of a class template is bound on each concrete specialization instead` | bind the specialization |
| a member of a class template whose signature names a type declared inside it | `<signature> is not part of the generic API: its signature names a type declared inside <template>, and each instantiation has its own; the specialization carries it` | bind the specialization |
| a `std::unique_ptr` or `std::shared_ptr` parameter | `"std::unique_ptr<T>" is a smart-pointer parameter; take the object by reference or by pointer at the C++ boundary` | a reference or a pointer |
| a `std::span` result | `"std::span<int>" is a span result; take the span as a parameter, or return the container it views` | a container result, or the span as a parameter |
| a `std::vector<std::string>` | `"std::string" is a standard-library element this build does not cross inside a container; use a scalar, a POD record, or a std::vector of those as the element` | a `std::vector` of scalars or records, or a wrapper |
| a function pointer or `std::function` result | `"std::function<...>" is a function or std::function result; take it as a callback parameter, or expose a C++ wrapper that owns it` | an out-parameter, or a wrapper |
| a C array | `"T [N]" is a C array; decay it to a pointer and a length at the C++ boundary` | a pointer and a length |
| a non-`const` `char *` parameter | `"char *" is a non-const char pointer; take it as const char *: the Go string is immutable, so the callee's writes would not reach it` | `const char *` |
| a pointer to an opaque record returned by a free function | `"T *" is a pointer to an opaque record; return it through a reference at the C++ boundary` | a reference; a method's borrowed pointer is a view and binds |
| `void *` | `"void *" is a void pointer; expose a typed pointer at the C++ boundary` | a typed pointer |
| `std::nullptr_t` | `"std::nullptr_t" is nullptr_t; pass a typed null pointer at the C++ boundary` | a typed pointer |
| a non-`const` reference to a `std::` value | `"std::string &" is a non-const standard-library reference; take it by value or by const reference; copy-back is not emitted by this build yet` | a value or a `const` reference |
| a standard-library type outside the emitted table (`std::deque`, `std::map`, ...) | `"std::deque<int>" is a standard-library type outside the emitted table; cross it as a scalar, T*/length or POD, or bind a C++ wrapper with a supported signature` | a wrapper with a supported signature |
| a pointer to a `std::` value | `"std::string *" is a pointer to a standard-library type; take it by value or by reference at the C++ boundary` | a value or a reference |
| `long double` | `cppgo: long double: long double and __int128 have no Go value type; widen or narrow it at the C++ boundary to double or int64, or wrap it in a representable type` | widen or wrap |
| `__int128`, or an integer width the type map does not carry | `"__int128" has a 16-byte width with no Go value type; widen or narrow it to a fixed-width integer at the C++ boundary` | widen or narrow |
| an incomplete type, or a record the selection did not keep | `"T" is not part of the generated API; include its complete definition in the header` — a specialization says `name it with -instantiate '<Specialization>'` | include it, or `-instantiate` |
| a record passed by value whose layout is unknown | `"T" is passed by value but its layout is unknown; make the type complete in the header, or take it by reference at the C++ boundary` | a complete type or a reference |
| a callback taking more than 6 INTEGER arguments | `"int (*)(...)" takes more than 6 INTEGER arguments; take a pointer to a struct instead` | a struct pointer |
| a callback taking more than 8 SSE arguments | `"double (*)(...)" takes more than 8 SSE arguments; take a pointer to a struct instead` | a struct pointer |
| a callback argument that is not a scalar | `the callback argument "T" is not a scalar; take an integer, a double or a pointer instead` — a `float` says `the callback argument "float" is a float; take a double instead: a float argument is not carried by this build yet` | scalar arguments |
| a pointer parameter beside a callback | `the parameter "<name>" of a callback binding is a <Go type>; pass a scalar: a pointer into Go memory would move when the callback grows the stack` | scalars only |
| a callback binding that returns a standard-library value | `"<signature>" returns a standard-library value and takes a callback; return the value through an out parameter the callee fills, or drop the callback from this declaration` | an out-parameter, or no callback |
| a call whose out-parameters fill the register file after the argument block | `the call needs N words after the argument block, past the register file` — a word the block cannot carry says `the argument block cannot carry the word "<type>"` | fewer out-parameters |
| a callback call past the register file | `the callback call needs N INTEGER and M SSE words, past the register file` | a smaller signature |
| a public field whose type the selection dropped | `<Record>::<field> is not bound: its type, <Type>, is not part of the generated API` | bind the type too |
| a public field that is a raw pointer to a record | `<Record>::<field> is not bound: "T *" is a pointer to a record, and nothing on the Go side owns it; expose the pointed-to object through a method, or take the address only inside C++` | a method |
| a public field whose value cannot be assigned | `<Record>::<field> has no setter: a std::string_view does not own its storage` — a record field says `its type's copy assignment is not public` | the getter only |
| a record whose `sizeof` or `alignof` is unknown | `<Record> has no layout the Go side can allocate (sizeof or alignof is unknown), so no factory is generated for it; reach it as a view of a field or of a borrowed result` | a view |
| an enum of a template specialization whose values cannot be read | `<Enum> is not bound: the enumerator values of a template specialization's enum cannot be read from the pattern; give them literal initialisers` | literal initialisers |
| a specialization synthesized from a pattern that has base classes | `<Record> inherits N base class(es) and its inherited members are not copied: a specialization synthesized from a pattern cannot prove that a base member is valid for this instantiation; bind the members through As<Base>()` | `As<Base>()` |

### A run the build refuses (error)

These stop the run instead of leaving one declaration out: a template pattern
the generic API cannot model, two declarations that need the same Go name, or a
`-status` entry the declaration cannot satisfy. Nothing is written.

| Construct | Message | Workaround |
|---|---|---|
| a public field on a class template | `cppgo: <file>:<line>: <record>: the public field <name> of a template is not emitted by this build yet; make the field private and expose an accessor in C++, or bind the instantiation with -instantiate` | a private field with an accessor, or `-instantiate` |
| a function template whose signature names a dependent type — a parameter pack such as `af::eval`, or a type such as `Eigen::NoAlias` | `cppgo: <file>:<line>: <function>: result: "<type>" is not a type this build classifies; expose it as a scalar, a pointer, a reference or a supported std:: type` — a parameter says `parameter <name>:` instead of `result:` | write the signature with a scalar, a pointer or a reference |
| a template parameter that is not a type or an integer | `the <kind> template parameter <name> is not a type or an integer; expose a template-template parameter as a concrete type at the C++ boundary` | a concrete type |
| a non-type template parameter that is not an integer | `the non-type template parameter <name> is not an integer; expose the value as an integral template parameter at the C++ boundary` | an integer parameter |
| a class template with no parameters | `a class template with no parameters is not a generic type; name a concrete instantiation with -instantiate` | `-instantiate` |
| two declarations that map to one Go name, when no overload suffix separates them | `cppgo: <signature> and <signature> both map to Go name <Name>; rename one with -rename '<signature>=<GoName>'` — the type forms say `two records map to Go type <Name>` or `two types map to Go type <Name>`, and two templates say `both map to the Go name <Name>` | `-rename` |
| `-status` on a return that is not a 32-bit integer | `cppgo: -status <signature>: the return type is <type>, and a status is a 32-bit integer` — a `void` return says `the declaration returns nothing, so there is no status to read` | drop `-status`, or bind the declaration without it |

### `char *` parameters

An unpaired `const char *` crosses as a Go `string` in both directions: a
parameter is copied into a NUL-terminated buffer for the call, and a result is
copied out of the library's storage.

A `(const char *, size_t)` pair does not become a `string`: the pointer and the
length are recognised as a pair first, so the parameter is a Go `[]int8` — a
borrowed byte slice the callee reads during the call. That is the pair form of
the type map, and it is the one shape where the generated Go and the `string`
the map states for a C string differ.

A non-`const` `char *` parameter is the one form that does not bind: the callee
would write into the Go string, which is immutable, so the warning in the table
above asks for `const char *`. Use `std::string` or `std::string_view` — a
non-owning pointer-and-length view of a string — when the C++ side needs the
pointer and a length as one argument.

## Designed, not shipped

The first row is the *error projection*: turning each of the ways a C++ library
reports failure into a Go `error`.

| Item | What is missing | What you can rely on today |
|---|---|---|
| the full error projection | `std::error_code` (a library's own error value), `std::expected` (a value, or the error instead), `errno` (C's per-thread error number), `std::variant` (one of several types), a configured `bool` + out-parameter, custom result types | exceptions and `-status` become Go errors; a declaration returning `std::error_code` is left out with a warning |
| generics breadth | class templates bind for scalar type arguments and non-type dimensions (a template parameter that is a value rather than a type); a public field on a class template, a function template whose signature names a dependent type, and a parameter pack are not part of the generic API | every refusal names the declaration and the fix |
| the image, array and matrix work of the three target libraries | `cv::Mat`, `af::array` and Eigen's matrix types: a class binds only up to what can cross, so a constructor the generator cannot carry is left out with a warning, and Eigen's usual `Options=0` has no phantom type in the generic API | each library ships a slice that binds and tests the declarations the generator carries today: [OpenCV](https://github.com/Manu343726/cppgo/tree/main/examples/opencv), [ArrayFire](https://github.com/Manu343726/cppgo/tree/main/examples/arrayfire), [Eigen](https://github.com/Manu343726/cppgo/tree/main/examples/eigen) |
| embedded generated packages | `cppgo.OpenFS` works; `Open`, the load function `cppgo gen` writes, does not call it yet | deploy the two C++ files, or use the runtime directly |

## What is not a limit

Three things look like limits and are not:

- **A record Go cannot mirror.** A bitfield (several fields packed into the bits
  of one integer), a packed struct (its padding removed), a union (several
  members sharing one storage), a private or mixed-access field, or a field Go
  cannot represent makes the record *opaque*, not refused: Go holds a pointer to
  C++ storage, and every public field keeps a getter and, when the C++
  assignment is public, a setter. What you lose is the Go struct whose layout is
  the C++ layout, not the type.
- **A missing declaration in the report.** The *report* is the list of bindings
  `cppgo gen` prints. If a declaration is absent from it, the declaration was not
  selected ([`-bind` / `-exclude`]({{< ref "reference/cli.md" >}}#cppgo-gen)) or
  the *parse* — the generator's reading of your headers — did not see it. A parse
  without the project's real flags says so with a warning; a wrong include path
  binds a different program.
- **A call that cannot use the direct path.** It is not a degradation of the API:
  the shim — the C++ file cppgo generates — carries the call, and the Go-side
  cost of it is the same
  ([Performance]({{< ref "explanation/benchmarks.md" >}})).

## Related

- What each construct costs: [Performance]({{< ref "explanation/benchmarks.md" >}}).
- Why these limits exist: [Architecture]({{< ref "explanation/architecture.md" >}}).
- Which construct gets which carrier: [Carriers and the construct table]({{< ref "explanation/carriers.md" >}}).
