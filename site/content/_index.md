---
title: cppgo
description: Generate a Go package that calls a C++ library, with no cgo at build time.
---

cppgo gives a Go program an ordinary Go package that calls a C++ library. It
writes the **binding** for you: the Go code that calls a library written in
another language. You give it the library's **header** — the C++ file a program
includes to see its classes and functions — and the **compilation
configuration** for that header, the directories your C++ compiler searches for
included files and the language standard it compiles with. It writes a Go
package that calls the library; this page calls it the **generated package**.

Your C++ sources, your build and your library are not modified, and the
generated package needs no **cgo at build time** and no `import "C"`. cgo is
Go's built-in way to call C: it needs a C compiler at build time and a special
`import "C"` block, which lets Go compile C code written beside it. cppgo loads
the compiled library at run time through a small Go library — the **runtime** —
that does not depend on cgo.

The input is the C++ header — here, the class the example binds, in
[`examples/mathx/include/mathx/vector.hpp`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/include/mathx/vector.hpp):

```cpp
#include <cstddef>

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

Build the generator and run the example's generation step:

```console
$ make build
$ make -C examples/mathx gen
```

The example builds its own copy of the generator; from inside `examples/mathx`,
the generation command is:

```console
$ build/cppgo gen -header include/mathx/vector.hpp -package mathx \
                  -library build/libmathx.so -out .
```

The result is an ordinary Go package. These are the declarations that class
became, copied from
[`examples/mathx/zz_cppgo.gen.go`](https://github.com/Manu343726/cppgo/blob/main/examples/mathx/zz_cppgo.gen.go):

```go
func NewVector(x float64, y float64) (*Vector, error)
func (v *Vector) Norm() (float64, error)
func (v *Vector) Scale(k float64)
func (v *Vector) Close() error
```

And you call it:

```go
v, err := mathx.NewVector(3, 4)
if err != nil {
	log.Fatal(err)
}
defer v.Close()

norm, err := v.Norm()   // norm() can fail, so the Go call returns an error
if err != nil {
	log.Fatal(err)
}
fmt.Println(norm)       // 5

v.Scale(2)              // scale() cannot fail, so there is no error to check
```

## Start here

If you have never called C++ from Go before, read
[Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}}). It takes
one small C++ class from a header to a passing Go test, and explains each step.

The sections on the left follow the four kinds of documentation: a
[tutorial]({{< ref "tutorials/_index.md" >}}) to learn,
[how-to guides]({{< ref "how-to/_index.md" >}}) for a task,
[reference]({{< ref "reference/_index.md" >}}) for the exact signature, and
[explanation]({{< ref "explanation/_index.md" >}}) for how it works and why it
is built this way.

## Maturity

The generator, the runtime and the examples are implemented and tested. Three
things are designed and not shipped yet, and every page marks them where they
appear: the full set of C++ failure conventions, the remaining C++ construct
support, and the OpenCV, ArrayFire and Eigen target slices.
