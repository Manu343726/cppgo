# arrayfire

A slice of [ArrayFire](https://arrayfire.com/) 3.10's C++ headers, bound to Go
by `cppgo gen`. There is no C++ source in this directory: the library is
ArrayFire itself, and the shim links against its CPU backend.

The headers this example reads are
[`af/array.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/array.h)
for the array type,
[`af/data.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/data.h)
for the value-producing constructors,
[`af/algorithm.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/algorithm.h)
for the reductions,
[`af/blas.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/blas.h)
for the products and
[`af/lapack.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/lapack.h)
for the norm. The binding covers the path from a shape to a numeric result —
construct, reduce, multiply, read back — and says below which declarations it
does not carry and why.

## What is bound

The element-type enum, declared without explicit values in
[`af/defines.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/defines.h):

```cpp
typedef enum {
    f32,    ///< 32-bit floating point values
    c32,    ///< 32-bit complex floating point values
    f64,    ///< 64-bit floating point values
    c64,    ///< 64-bit complex floating point values
    b8 ,    ///< 8-bit boolean values
    s32,    ///< 32-bit signed integral values
    u32,    ///< 32-bit unsigned integral values
    u8 ,    ///< 8-bit unsigned integral values
    s64,    ///< 64-bit signed integral values
    u64     ///< 64-bit unsigned integral values
    , s16   ///< 16-bit signed integral values
    , u16   ///< 16-bit unsigned integral values
    , f16   ///< 16-bit floating point value
    , s8    ///< 8-bit signed integral values
} af_dtype;
```

The data-source enum, the norm enum and the matmul property, from the same file:

```cpp
typedef enum {
    afDevice,   ///< Device pointer
    afHost     ///< Host pointer
} af_source;

typedef enum {
    AF_NORM_VECTOR_1,      ///< treats the input as a vector and returns the sum of absolute values
    AF_NORM_VECTOR_INF,    ///< treats the input as a vector and returns the max of absolute values
    AF_NORM_VECTOR_2,      ///< treats the input as a vector and returns euclidean norm
    ...
    AF_NORM_EUCLID = AF_NORM_VECTOR_2
} af_norm_type;

typedef enum {
    AF_MAT_NONE = 0,
    AF_MAT_TRANS = 1,
    ...
} af_mat_prop;
```

The functions that report the library's own configuration, all scalar
([`af/device.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/device.h)):

```cpp
AFAPI int getDeviceCount();
AFAPI int getDevice();
AFAPI bool isDoubleAvailable(const int device);
AFAPI bool isHalfAvailable(const int device);
AFAPI size_t getMemStepSize();
```

and the one that ties the enum to a value
([`af/util.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/util.h)):

```cpp
/// Get the size of the type represented by an af_dtype enum
AFAPI size_t getSizeOf(af::dtype type);
```

`af::array` itself is selected one constructor at a time by full signature: the
default one, the one-dimensional and the two-dimensional allocation. Its public
methods that return a scalar or an enum — `type`, `elements`, `isempty`, `as`,
`dims` — bind, and `dims` gives the shape one dimension at a time
([`af/array.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/array.h)):

```cpp
array();
explicit array(dim_t dim0, dtype ty = f32);
explicit array(dim_t dim0, dim_t dim1, dtype ty = f32);
dtype type() const;
dim_t elements() const;
bool isempty() const;
array as(dtype type) const;
dim4 dims() const;
dim_t dims(unsigned dim) const;
```

`af::dim4`'s four-argument constructor is bound, so a shape can be built
([`af/dim4.hpp`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/dim4.hpp)):

```cpp
dim4(dim_t first, dim_t second = 1, dim_t third = 1, dim_t fourth = 1);
```

The two constructors that produce known values, and the reductions and products
the slice exists for
([`af/data.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/data.h),
[`af/algorithm.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/algorithm.h),
[`af/blas.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/blas.h)):

```cpp
AFAPI array identity(const dim_t d0, const dim_t d1, const dtype ty=f32);
AFAPI array range(const dim4 &dims, const int seq_dim = -1, const dtype ty=f32);
AFAPI array sum(const array &in, const int dim = -1);
AFAPI array sum(const array &in, const int dim, const double nanval);
AFAPI array min(const array &in, const int dim = -1);
AFAPI array max(const array &in, const int dim = -1);
AFAPI array matmul(const array &lhs, const array &rhs,
                   const matProp optLhs = AF_MAT_NONE,
                   const matProp optRhs = AF_MAT_NONE);
AFAPI array transpose(const array &in, const bool conjugate = false);
```

and the scalar reduction that reads a value back
([`af/lapack.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/lapack.h)):

```cpp
AFAPI double norm(const array &in, const normType type=AF_NORM_EUCLID,
                  const double p=1, const double q=1);
```

`af::seq` is the index range ArrayFire's array subscript takes. Its constructors
and the three public fields bind
([`af/seq.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/seq.h)):

```cpp
class AFAPI seq
{
public:
    af_seq s;
    size_t size;
    bool m_gfor;

    seq(double length = 0);
    seq(double begin, double end, double step = 1);
    ...
};
```

`make gen` reads them:

```console
$ make gen
build/cppgo gen -header /usr/local/include/af/array.h \
                -header /usr/local/include/af/data.h \
                -header /usr/local/include/af/algorithm.h \
                -header /usr/local/include/af/blas.h \
                -header /usr/local/include/af/lapack.h \
                -package arrayfire -library /usr/local/lib/libaf.so \
                -compile-commands build/compile_commands.json -out . \
                -bind af_dtype -bind af_source -bind af_norm_type \
                -bind 'af::array::array(dim_t, dim_t, dtype)' \
                -bind 'af::sum(const array &, const int)' \
                -bind 'af::matmul(const array &, const array &, const matProp, const matProp)' \
                -bind 'af::norm(const array &, const normType, const double, const double)' \
                -bind af::seq ...
af::array::array()                           thunk  cppgo_thunk_6dc2ae8a866b     not noexcept: an exception would unwind into Go
af::array::array(dim_t, dim_t, dtype)        thunk  cppgo_thunk_e7c5621124f1     not noexcept: an exception would unwind into Go
af::array::as(dtype) const                   thunk  cppgo_thunk_f8b21d7c68a9     a record passed or returned by value: the thunk copies it; the by-value copy runs in the shim, which catches a throwing copy
af::identity(const dim_t, const dim_t, const dtype) thunk  cppgo_thunk_34922b32838e a record passed or returned by value: the thunk copies it; the by-value copy runs in the shim, which catches a throwing copy
af::matmul(const array &, const array &, const matProp, const matProp) thunk  cppgo_thunk_de46a5c0f065  a record passed or returned by value: the thunk copies it; the by-value copy runs in the shim, which catches a throwing copy
af::norm(const array &, const normType, const double, const double) thunk  cppgo_thunk_1d1aa94f7a76  not noexcept: an exception would unwind into Go
af::sum(const array &, const int)            thunk  cppgo_thunk_723e0d57132c     a record passed or returned by value: the thunk copies it; the by-value copy runs in the shim, which catches a throwing copy
cppgo: built libcppgo_arrayfire.so with /usr/bin/g++
```

ArrayFire throws by design — every function above raises `af::exception` when
the call fails — so every binding is carried by a thunk that catches it and hands
Go an error. A `noexcept` declaration in this library would be a carrier change,
and there are none among the ones bound.

## The generated API

[`zz_cppgo.gen.go`](zz_cppgo.gen.go) is the whole surface a caller sees:

```go
type AfDtype uint32
const (
	AfDtypeF32 AfDtype = 0
	...
	AfDtypeS8  AfDtype = 13
)

type AfSource uint32
const (
	AfSourceAfDevice AfSource = 0
	AfSourceAfHost   AfSource = 1
)

type AfNormType uint32
const (
	AfNormTypeAFNORMVECTOR1 AfNormType = 0
	AfNormTypeAFNORMVECTOR2 AfNormType = 2
	...
)

type Array struct{ /* opaque */ }
func NewArray() (*Array, error)
func NewArrayInt64AfDtype(dim0 int64, ty AfDtype) (*Array, error)
func NewArrayInt64Int64AfDtype(dim0 int64, dim1 int64, ty AfDtype) (*Array, error)
func (a *Array) Close() error
func (a *Array) Elements() (int64, error)
func (a *Array) Type() (AfDtype, error)
func (a *Array) Dims() (*Dim4, error)
func (a *Array) DimsUint32(dim uint32) (int64, error)
func (a *Array) Isempty() (bool, error)
func (a *Array) As(arg0 AfDtype) (*Array, error)

type Dim4 struct{ /* opaque */ }
func NewDim4(first int64, second int64, third int64, fourth int64) (*Dim4, error)
func (d *Dim4) Close() error

type AfSeq struct {
	Begin float64
	End   float64
	Step  float64
}

type Seq struct{ /* opaque */ }
func NewSeqFloat64(length float64) (*Seq, error)
func NewSeqFloat64Float64(begin float64, end float64) (*Seq, error)
func NewSeqFloat64Float64Float64(begin float64, end float64, step float64) (*Seq, error)
func (s *Seq) Close() error
func (s *Seq) S() AfSeq
func (s *Seq) Size() uint64
...

func GetSizeOf(arg0 AfDtype) (uint64, error)
func GetDevice() (int32, error)
func GetDeviceCount() (int32, error)
func GetMemStepSize() (uint64, error)
func IsDoubleAvailable(device int32) (bool, error)
func IsHalfAvailable(device int32) (bool, error)

func Identity(d0 int64, d1 int64, ty AfDtype) (*Array, error)
func Range(dims *Dim4, seq_dim int32, ty AfDtype) (*Array, error)
func SumArrayPtrRefInt32(in *Array, dim int32) (*Array, error)
func SumArrayPtrRefInt32Float64(in *Array, dim int32, nanval float64) (*Array, error)
func Max(in *Array, dim int32) (*Array, error)
func Min(in *Array, dim int32) (*Array, error)
func Matmul(lhs *Array, rhs *Array, optLhs AfMatProp, optRhs AfMatProp) (*Array, error)
func Transpose(in *Array, conjugate bool) (*Array, error)
func Norm(in *Array, arg1 AfNormType, p float64, q float64) (float64, error)
```

`AfSeq` is `af_seq` itself, a plain struct of three doubles, so the values a
`seq` constructor computed cross by value and are read as fields.

## The call

From [`arrayfire_test.go`](arrayfire_test.go): a 4×1 column, its transpose and
the product, whose element `(i, j)` is `i*j`.

```go
dims, err := NewDim4(4, 1, 1, 1)
if err != nil {
    t.Fatalf("NewDim4: %v", err)
}
defer dims.Close()

v, err := Range(dims, 0, AfDtypeF64) // [0 1 2 3]
if err != nil {
    t.Fatalf("Range: %v", err)
}
defer v.Close()

vt, err := Transpose(v, false)
if err != nil {
    t.Fatalf("Transpose: %v", err)
}
defer vt.Close()

m, err := Matmul(v, vt, AfMatPropAFMATNONE, AfMatPropAFMATNONE)
if err != nil {
    t.Fatalf("Matmul: %v", err)
}
defer m.Close()

if got := norm1(t, m); got != 36 {
    t.Errorf("sum(|v * transpose(v)|) = %v, want 36 ((0+1+2+3)^2)", got)
}
```

## What is not bound, and why

A refused declaration no longer refuses its record: the generator visits each
declaration, so one uncarryable overload is skipped with a warning and the rest
of the record binds. That is what makes `af::array` usable at all. What the slice
still cannot carry, each with the exact message the generator prints:

| declaration | refusal |
|---|---|
| `template<typename T> array constant(T val, const dim4 &dims, ...)` | `af::constant(T, const dim4 &, const dtype): "const dim4 &" is a dependent type the generic API does not carry yet; take the type parameter by value, by pointer or by reference at the C++ boundary` — binding `af::constant` by name selects all five template overloads and stops here, so the value constructor is out; `af::identity` and `af::range` are the value sources this slice uses |
| `template<typename T> array(dim_t, const T *pointer, af::source)` | `af::array::array is a member function template and is not bound: name its concrete instantiation as a non-template member beside it` — host data cannot be copied in without a non-template wrapper |
| `template<typename T> T scalar() const` | `af::array::scalar is a member function template and is not bound` (and `-bind af::array::scalar` says `it names nothing this header declares`) — this is why the tests read values through `af::norm` rather than a scalar accessor |
| `void host(void *ptr) const` | `af::array::host(void *) const is not bound: parameter ptr: "void *" is a void pointer; expose a typed pointer at the C++ boundary` |
| `void set(af_array tmp)` | `af::array::set(af_array) is not bound: parameter tmp: "af_array" is a void pointer; expose a typed pointer at the C++ boundary` |
| binding `af::array` by name | `af::array::array_proxy::operator array() const and af::array::array_proxy::operator array() both map to Go name ArrayPtr; rename one with -rename '<signature>=<GoName>'` — the members this slice needs are named one by one instead |
| binding `af::dim4` by name | `af::dim4::elements() and af::dim4::elements() const both map to Go name Elements; rename one with -rename '<signature>=<GoName>'` (then `ndims` and `operator[]`, the same way); only the four-argument constructor is selected, and `dims(unsigned)` is how a shape is read |
| `af::dim4::dims()` | `is not bound: result: "dim_t[4]" is a C array; decay it to a pointer and a length at the C++ boundary` |
| binding `af::range` by name | the generator panics: `panic: runtime error: index out of range [6] with length 6` — one overload of the set lays out more argument registers than the carrier has slots. The single `range(const dim4 &, const int, const dtype)` signature binds; the set does not |
| binding `af::identity` by name | the shim does not compile: `conflicting declaration 'void** cppgo_result'` — the set mixes the by-value record result with the overloads that take `dim4`. The `identity(dim_t, dim_t, dtype)` signature binds on its own |

Two declarations are reported as bound by the generator but the generated Go
package does not compile, so the slice leaves them out:

- `af::exception`'s `const char *` constructors generate calls to helpers that
  are never emitted: `zz_cppgo.gen.go:812:78: undefined: cppgo_msg_cstr` (and
  `cppgo_file_cstr`, `cppgo_arg1_cstr`). `what()` itself is generated correctly,
  as a method that copies the C string out — but with no constructor there is no
  value to call it on, so the exception type is absent from this package.
- `af::infoString(bool)` — a free function with a `const char *` result —
  generates `zz_cppgo.gen.go:1623:9: cannot use cppgoResult (variable of type
  unsafe.Pointer) as string value in return statement`: the method path copies
  the C string out, the free-function path does not.

Both are generator findings, recorded here rather than worked around.

## Commands

```console
$ make        # gen, test, vet
$ make lib    # report the ArrayFire shared object the binding links against
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
$ make test   # go test -tags cppgo_arrayfire, then the same under -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, libclang 20+ (generation only), and ArrayFire's C++
headers and `libaf`. `AF_INCLUDE` and `AF_LIB` are the two paths a machine can
differ in; `make test` also puts the library's directory on `LD_LIBRARY_PATH` for
the loader. On a machine without ArrayFire every target prints the missing path
and exits zero, which is what lets
[`scripts/build-examples.sh`](../../scripts/build-examples.sh) run there.

## Loading

The test names the shim it loads, because the loader does not search the current
directory; the shim names `libaf.so` as its dependency:

- `CppgoLibraryPath` is ArrayFire's shared object, from `CPPGO_ARRAYFIRE_LIB` or
  the default `/usr/local/lib/libaf.so`;
- `Open` is called with the absolute path of `libcppgo_arrayfire.so`.

## Tests

`make test` runs [`arrayfire_test.go`](arrayfire_test.go) twice. What it asserts,
and the declaration each value comes from:

| Test | Assertion, and its source |
|---|---|
| `TestDtypeConstants` | each enumerator equals its position in [`af/defines.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/defines.h): `f32` = 0 through `s8` = 13 |
| `TestSourceConstants` | `afDevice` = 0 and `afHost` = 1, the order the same file declares them in |
| `TestSizeOf` | `getSizeOf` reports the width each enumerator's name states: `f32` 4, `c32` 8, `f64` 8, `c64` 16, `b8`/`s8`/`u8` 1, `s16`/`u16`/`f16` 2, `s32`/`u32` 4, `s64`/`u64` 8 |
| `TestDevice` | at least one device exists, the current one is 0, the memory manager's chunk size is non-zero, `isDoubleAvailable(0)` is true for the CPU backend, and `isHalfAvailable(0)` answers without an error |
| `TestArrayConstruction` | a 2×3 f64 array reports `Elements()` 6, `Type()` f64, `dims(0)` 2, `dims(1)` 3 and `Isempty()` false; the default-constructed array reports 0 elements and `Isempty()` true |
| `TestArrayLifetime` | a second `Close` is an error, and a method on a closed array returns an error rather than calling into freed memory |
| `TestRangeAndIdentity` | the 4×1 `range` holds `[0 1 2 3]` — its absolute sum is 6 — and the 2×2 `identity` holds two ones |
| `TestReductions` | on `m = v*transpose(v)` (`m[i][j] = i*j`): `sum(m, 0)` is `[0 6 12 18]` (absolute sum 36), the `nanval` overload agrees, `sum(m, 1)` is the same multiset, `max(m, 0)` is `[0 3 6 9]` (sum 18) and `min(m, 0)` is all zeros; the 1-norm of `[0 1 2 3]` is 6 and the 2-norm is `sqrt(14)` |
| `TestMatmul` | `v * transpose(v)` is 4×4, `dims(0)` and `dims(1)` are 4, and the sum of every element is `(0+1+2+3)^2 = 36`; an identity left operand returns the operand (sum 6), and `transpose(v) * v` is the 1×1 inner product 14 |
| `TestDtypePath` | an f64 array reports f64, `As(f32)` reports f32 with the same 4 elements, and `identity(2, 2, f32)` reports f32 |
| `TestSeq` | `seq(10, 20, 2)` has size 6 and `{begin 10, end 20, step 2}`; `seq(4)` is `{0, 3, 1}` with size 4; `seq(10, 20)` steps by one and has size 11 — the `[begin, end, step]` each constructor documents |
| `TestLibraryError` | `matmul` of a 4×1 by a 4×1 returns the dimension error, and `sum` along dimension 5 of a 4×1 column returns the out-of-range error: `af::exception` becomes a Go error |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` reproduces them byte for byte. `make check` is the gate:
it fails when a committed file differs from what `make gen` writes.
