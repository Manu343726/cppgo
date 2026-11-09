---
title: cppgo
description: Call C++ from Go with no cgo at build time.
---

cppgo generates a Go package from a C++ header. You point it at the header and
at the way your project compiles it; it writes the Go package you call. Your
library, your build and your sources are not modified, and the Go side needs no
cgo and no `import "C"`.

The input is the C++ header — here, the class the quick start binds, in
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

You generate from it:

```console
$ cppgo gen -header include/mathx/vector.hpp -package mathx \
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
if err != nil { /* ... */ }
defer v.Close()

norm, err := v.Norm()   // norm() can throw, so the Go call returns an error
v.Scale(2)              // scale() is noexcept, so there is no error to check
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

The generator, the runtime and the two examples are implemented and tested.
Three things are designed and not shipped yet, and every page marks them where
they appear: the full error projection, the wider operators/inheritance work, and
the OpenCV, ArrayFire and Eigen target slices.
