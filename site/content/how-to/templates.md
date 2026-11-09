---
title: "Bind a C++ template library"
description: "The two-phase flow: cppgo gen writes the generic API, cppgo build -scan emits the instantiations the program uses."
weight: 20
---

A C++ template only exists where it is instantiated, so a template library takes
two commands: one writes the generic Go API, and one reads your program and
provides the concrete types it uses. The reason for the split is
[in Architecture]({{< ref "explanation/architecture.md" >}}#templates-why-two-phases);
this page is the procedure.

## The header

The template is declared in a header, and that header is what `cppgo gen` reads.
This is `box::Box`, the fixture the two-phase flow is measured on, in
[`internal/gen/testdata/box/box.hpp`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/testdata/box/box.hpp):

```cpp
namespace box {

template <typename T>
class Box {
public:
    Box() : value_() {}
    Box(T value) : value_(value) {}
    Box(const Box &other) = delete;
    ~Box() {}
    T get() const { return value_; }
    void set(T value) { value_ = value; }
    T add(T other) const { return value_ + other; }
    T *echo(T *value) const { return value; }
    const T *peek(const T *value) const { return value; }
    void consume(const T &value) { value_ = value; }
    static T identity(T value) { return value; }

private:
    T secret() const { return value_; }
    T value_;
};

} // namespace box
```

## Phase one: generate the generic API

```console
$ cppgo gen -header include/box.hpp -package box -out ./box
```

For a class template the generated package declares a Go generic type; for a
function template, a generic function:

```go
// Box is box::Box<T>: the generic API. The concrete specializations are the ones
// the program instantiates; `cppgo build` collects them and emits the shim.
type Box[T any] struct {
	obj *cppgo.Object
}

// NewBox constructs a box::Box<T>.
//
// C++: box::Box::Box<T>(T), two-phase template
func NewBox[T any](value T) (*Box[T], error)

// Get calls box::Box::get.
func (b *Box[T]) Get() (T, error)

// Set calls box::Box::set.
func (b *Box[T]) Set(value T) error
```

This package compiles and `go vet` passes with no instantiation at all.

A non-type template parameter has no Go equivalent, so a fixed dimension is a
*phantom type*: `D1` through `D8` name the C++ values 1 to 8, and `Dynamic`
names a runtime dimension. `Eigen::Matrix<double, 4, 4>` is
`Matrix[float64, D4, D4]` in the generated API.

## Phase two: the call, and the instantiations it needs

The program instantiates the generic API, and that is the input the second
command needs. These are the first calls of the measured program in
[`internal/gen/template_e2e_test.go`](https://github.com/Manu343726/cppgo/blob/main/internal/gen/template_e2e_test.go),
which `cppgo build -scan` loads as a Go package:

```go
func Run() (int32, float64, int64, int32, error) {
	b, err := box.NewBox[int32](7)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer b.Close()
	if err := b.Set(9); err != nil {
		return 0, 0, 0, 0, err
	}
	added, err := b.Add(4)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	got, err := b.Get()
	if err != nil {
		return 0, 0, 0, 0, err
	}
```

`box::Box<int32>` is not in the C++ side yet, so the second command is what
provides it:

```console
$ cppgo build -header include/box.hpp -package box -out ./box -scan ./cmd/mytool
```

`cppgo build` reads the Go package named by `-scan`, finds every use of the
generic API, and provides each concrete type to the C++ side. `-scan` is
repeatable and `-scan-dir` sets the directory the patterns resolve against.

If your program uses a type that the C++ side does not have, the failure names
the line to add:

```
cppgo: box::Box[int32] has no C++ instantiation in the shim: add
"template struct ::box::Box<int32>;" to the header and run
`cppgo build -scan <package>`: cppgo: symbol "cppgo_tmpl_...": not found
```

Add that line to the header, or pass the same specialisation as
`-instantiate 'box::Box<int32>'` and make the header declare it; the two are the
same input, and the flag exists for a header you would rather not edit.

## Checking in CI

`cppgo check` accepts the same `-scan` patterns, because the generated C++ side
depends on the program's instantiations. Compare like with like:

```console
$ cppgo check -header include/box.hpp -package box -out ./box -scan ./cmd/mytool
```

## What limits the generic API today

| Limit | What it means |
|---|---|
| Type arguments are scalars and pointers to scalars | `bool`, the sized integers, `float32`, `float64`, and pointers to them. `string` is not a type argument |
| Member function templates are refused | "the member function template X is not emitted by this build yet" — expose the concrete instantiation as a non-template member |
| Operators and conversion operators are refused | a template whose API is `operator+` cannot be bound yet; give it a named member |
| Inheritance is refused | a template that derives from a base is out of this build |
| A template with a dependent, unclassified field is refused | "not a type this build classifies" |

Those are the generics/operators/inheritance items of the wider generator work,
not a configuration you have missed. The
[limits page]({{< ref "explanation/limits.md" >}}) lists each one, and the Eigen
target slice, which the two-phase flow exists for, is designed and not shipped.

## Related

- The command and its flags: [Command line]({{< ref "reference/cli.md" >}}).
- The rest of the generated shapes: [Generated API]({{< ref "reference/generated-api.md" >}}).
