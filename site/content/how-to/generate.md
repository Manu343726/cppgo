---
title: "Generate a binding for your own library"
description: "The command, the compilation configuration, and what a generation error looks like."
weight: 10
---

You have a C++ library with headers and you want a Go package that calls it.
This guide assumes you have read
[Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}) or already
know what `cppgo gen` produces.

The commands use the example library in
[`examples/mathx`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx);
substitute your own paths where it has its own.

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
it.

## 2. State how the header is compiled

`cppgo gen` needs the include paths and the language standard your project uses.
There are two ways to supply them, in this order of preference:

- **A compilation database.** CMake, Meson and most large projects produce
  `compile_commands.json`. Point at it and cppgo reads the entry that covers each
  header:

  ```console
  $ cppgo gen -header include/mathx/vector.hpp -package mathx \
              -compile-commands build/compile_commands.json -out .
  ```

- **A `.clangd` file.** For a project with no compilation database, the
  `CompileFlags` block in `.clangd` is enough, and the same file keeps clangd's
  editor behaviour correct. The example's
  [`examples/mathx/.clangd`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/.clangd)
  is the whole of its compilation configuration:

  ```yaml
  CompileFlags:
    Add: [-std=c++17, -Iinclude]
  ```

Do not skip this because the header compiles: a missing include path produces a
wrong binding rather than an error. When it finds no configuration at all, cppgo
says so and assumes a default:

```
cppgo: warning: the compilation flags for api.hpp are an assumption
(assumed: -std=c++17 -Iinclude); a parse without the project's real flags can
misread the header
```

## 3. Decide what to bind

By default, the header is the unit of selection: every public declaration in it
is bound, plus the types those declarations use. For a large library, narrow the
set with roots and exclusions:

```console
$ cppgo gen -header include/opencv2/core.hpp -package core \
            -bind cv::Mat -bind 'cv::cvtColor' -exclude 'cv::detail' -out ./cvcore
```

A root is a namespace, a class, a member or a function. An entry that names
nothing is an error, never a silent no-op:

```console
cppgo: -bind cv::Maat: it names nothing this header declares
```

That is deliberate. A typo that silently shrinks the API is the failure mode
worth an error, because nobody notices a missing function until it is needed.

## 4. Run it

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
            -library build/libmathx.so -out .
```

| Flag | What it does |
|---|---|
| `-header <path>` | the C++ header to bind; repeatable, and each header is a selection unit |
| `-package <name>` | the Go package name to write. There is no default; pass it |
| `-out <dir>` | where the generated package and the C++ files are written (default `.`) |
| `-library <file>` | your shared object. cppgo links the C++ side it builds against it, and checks that every function it binds is really exported by it. **Omit it for a header-only library** |
| `-compile-commands <path>` | the database, or its directory |
| `-bind`, `-exclude` | add or remove a root |
| `-rename 'sig=GoName'` | resolve a Go name collision |
| `-instantiate '<specialization>'` | name a concrete template specialisation |
| `-status <qualified>` | a declaration whose `int32` return is a status code |
| `-cxx <compiler>` | the C++ compiler for the generated side (default `g++`, then `clang++`) |
| `-check` | report staleness instead of writing |
| `-q` | suppress the report |

The complete reference is [Command line]({{< ref "reference/cli.md" >}}).

## 5. Read the report

The report has one line per binding, and a last line naming the compiler that
built the C++ side:

```
mathx::Vector::scale(double) noexcept        direct _ZN5mathx6Vector5scaleEd     noexcept, not virtual, register class; the library exports the symbol
mathx::Vector::norm() const                  thunk  cppgo_thunk_49993cda6560     not noexcept: an exception would unwind into Go
mathx::midpoint(Point, Point)                thunk  cppgo_thunk_a66614728a62     a record passed or returned by value: the thunk copies it
cppgo: built libcppgo_mathx.so with /usr/bin/g++
```

Check the last line against the compiler that built your library. The middle
columns are explained in [Architecture]({{< ref "explanation/architecture.md" >}}#generation-parse-selection-determinism);
you do not need them to use the package.

## 6. The generated Go, and the call

The declarations in step 1 became these Go declarations, copied from the
generated
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
func (v *Vector) Close() error
```

`Norm` returns an error because `mathx::Vector::norm()` can throw; `Scale`
returns nothing because `mathx::Vector::scale(double) noexcept` cannot fail. A
program calls the package the way it calls any Go package:

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

## 7. Commit the sources and check them

The generated sources are reproducible — the same inputs produce the same files
— so commit them with your project and let a check catch drift:

```console
$ cppgo check -header include/mathx/vector.hpp -package mathx \
              -library build/libmathx.so -out .
./: generated files are up to date
$ echo $?
0
```

`cppgo check` writes nothing and builds nothing. It exits `0` when everything
matches, `1` when a file is missing or stale, and `2` when the command line
itself is wrong. The exact invocation is written into the banner of
`zz_cppgo.gen.go`, so the command that produced a file lives beside it.

[Gate a generated package in CI]({{< ref "how-to/ci.md" >}}) puts the check in a
pipeline.

## When it refuses

A generation error names the file, the line, the declaration and the fix, and
nothing is written. These are the ones you are most likely to meet:

| Message contains | What happened | What to do |
|---|---|---|
| `it names nothing this header declares` | a `-bind`, `-exclude`, `-rename` or `-status` entry matches no declaration | fix the qualified name or the signature |
| `operators and conversion operators are not emitted by this build yet` | the declaration is an operator | give it a named member or free function |
| `enums are not emitted by this build yet` | the header declares an enum | pass the value as its underlying integer at the C++ boundary |
| `inherited members and base views are not emitted by this build yet` | a class has base classes | bind the base directly, or expose the derived members through a wrapper |
| `the call needs N INTEGER and M SSE argument registers` | more arguments than the call can carry | take a pointer to a struct |
| `no Go value type` | `long double`, `__int128` or `nullptr_t` in a signature | wrap it at the C++ boundary |
| `the generated shim does not compile` | the generated C++ failed to compile with your flags | the error carries the compiler's diagnostic |

The full list, with a workaround for each, is
[Limits]({{< ref "explanation/limits.md" >}}).

If a construct you need is in that list, it is not a flag you have missed: it is
work this build has not done yet.

## Related

- A template-heavy library: [Bind a C++ template library]({{< ref "how-to/templates.md" >}}).
- What the generated package looks like: [Generated API]({{< ref "reference/generated-api.md" >}}).
- Moving the result to a deployment: [Ship a program that uses a binding]({{< ref "how-to/deploy.md" >}}).
