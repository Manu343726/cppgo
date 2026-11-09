# eigen

A slice of [Eigen](https://eigen.tuxfamily.org/) 3.4's Dense header, bound to Go
by `cppgo gen`. It is the worked example of a **header-only** C++ library: there
is no shared object to build, none to link, and no `-library` flag, because every
Eigen declaration is inline. The shim cppgo compiles from the generated sources is
the whole binary a caller loads.

The header this example reads is
[`Eigen/Dense`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/Dense), which
reaches most of Eigen through its includes. The binding covers the declarations
that cross as plain values — the configuration enums, the thread controls, one
by-value record and the matrix class template — and says below, with the message
that stops each one, which declarations it does not carry and why.

## What is bound

Three enums from
[`Eigen/src/Core/util/Constants.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/util/Constants.h):

```cpp
enum StorageOptions {
  ColMajor = 0,
  RowMajor = 0x1,
  AutoAlign = 0,
  DontAlign = 0x2
};

enum ComputationInfo {
  Success = 0,
  NumericalIssue = 1,
  NoConvergence = 2,
  InvalidInput = 3
};

enum UpLoType {
  Lower=0x1,
  Upper=0x2,
  UnitDiag=0x4,
  ZeroDiag=0x8,
  UnitLower=UnitDiag|Lower,
  UnitUpper=UnitDiag|Upper,
  StrictlyLower=ZeroDiag|Lower,
  StrictlyUpper=ZeroDiag|Upper,
  SelfAdjoint=0x10,
  Symmetric=0x20
};
```

The functions that report and set the thread count its parallel algorithms use,
from
[`Eigen/src/Core/products/Parallelizer.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/products/Parallelizer.h):

```cpp
/** Must be call first when calling Eigen from multiple threads */
inline void initParallel();

/** \returns the max number of threads reserved for Eigen */
inline int nbThreads();

/** Sets the max number of threads reserved for Eigen */
inline void setNbThreads(int v);
```

A record whose constructor fills every member, from
[`Eigen/src/IO/IOFormat.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/IO/IOFormat.h):

```cpp
class IOFormat {
public:
  EIGEN_DEVICE_FUNC explicit IOFormat(int _precision = StreamPrecision, int _flags = 0,
      const std::string& _coeffSeparator = " ",
      const std::string& _rowSeparator = "\n", const std::string& _rowPrefix="",
      const std::string& _rowSuffix="", const std::string& _matPrefix="",
      const std::string& _matSuffix="", const char _fill=' ');
  ...
};
```

and the class template itself, from
[`Eigen/src/Core/Matrix.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/Matrix.h):

```cpp
template<typename _Scalar, int _Rows, int _Cols, int _Options, int _MaxRows, int _MaxCols>
class Matrix
  : public PlainObjectBase<Matrix<_Scalar, _Rows, _Cols, _Options, _MaxRows, _MaxCols> >
{
  ...
};
```

`make gen` writes the generic Go API, and `make scan` collects the
specializations the module uses:

```console
$ make gen scan
build/cppgo gen -header /usr/include/eigen3/Eigen/Dense -package eigen \
                -compile-commands build/compile_commands.json -out . \
                -bind Eigen::StorageOptions -bind Eigen::ComputationInfo \
                -bind Eigen::UpLoType -bind Eigen::nbThreads \
                -bind Eigen::setNbThreads -bind Eigen::initParallel \
                -bind Eigen::IOFormat -bind Eigen::Matrix
Eigen::IOFormat::IOFormat(int, int, const std::string &, ..., const char) thunk  cppgo_thunk_1b416b025213  more than 6 INTEGER or 8 SSE arguments: the thunk takes an argument block and unpacks it
Eigen::IOFormat::precision() const noexcept  thunk  cppgo_thunk_179001d9ec8e     a field accessor: the thunk reads or writes the member
Eigen::initParallel()                        thunk  cppgo_thunk_54b1814a5d61     not noexcept: an exception would unwind into Go
Eigen::Matrix<_Scalar, _Rows, _Cols, _Options, _MaxRows, _MaxCols> tmpl   -       generic API, 3 binding(s)
build/cppgo build ... -scan ./matrices -scan-dir .
Eigen::Matrix<double, 2, 2, 1, 2, 2>         tmpl   float64,eigen.D2,eigen.D2,eigen.D1,eigen.D2,eigen.D2 members instantiated where each thunk calls them
Eigen::Matrix<double, -1, -1, 1, -1, -1>     tmpl   float64,eigen.Dynamic,eigen.Dynamic,eigen.D1,eigen.Dynamic,eigen.Dynamic members instantiated where each thunk calls them
cppgo: built libcppgo_eigen.so with /usr/bin/g++
```

`Eigen::IOFormat` is the by-value record case. Its constructor takes two
integers, six `std::string`s and a `char` — sixteen INTEGER words with `this`,
past the six the register file holds — so the thunk takes an **argument block**
and unpacks it, and the object is constructed in Go's storage. What the
argument block is belongs to [Architecture][arch]; [Carriers][carriers] has the
carrier names.

The two `Matrix` lines are phase two. `-scan ./matrices` loads the package
[`matrices/`](matrices), finds every instantiation of the generic API, and
writes one thunk per member into the shim, where each thunk's own call
instantiates exactly the member it uses. **No `template class` line is written**:
a class-level explicit instantiation would also instantiate every member the
generic API skips, and Eigen's constructors static-assert on specializations that
do not satisfy their size checks.

[arch]: https://manu343726.github.io/cppgo/explanation/architecture/
[carriers]: https://manu343726.github.io/cppgo/explanation/carriers/

## The generated API

[`zz_cppgo.gen.go`](zz_cppgo.gen.go) is the whole surface a caller sees:

```go
type StorageOptions uint32
const (
	StorageOptionsColMajor  StorageOptions = 0
	StorageOptionsRowMajor  StorageOptions = 1
	StorageOptionsAutoAlign StorageOptions = 0
	StorageOptionsDontAlign StorageOptions = 2
)

type ComputationInfo uint32
const (
	ComputationInfoSuccess        ComputationInfo = 0
	ComputationInfoNumericalIssue ComputationInfo = 1
	ComputationInfoNoConvergence  ComputationInfo = 2
	ComputationInfoInvalidInput   ComputationInfo = 3
)

type UpLoType uint32
const (
	UpLoTypeLower         UpLoType = 1
	UpLoTypeUpper         UpLoType = 2
	UpLoTypeUnitDiag      UpLoType = 4
	UpLoTypeZeroDiag      UpLoType = 8
	UpLoTypeUnitLower     UpLoType = 5
	UpLoTypeUnitUpper     UpLoType = 6
	UpLoTypeStrictlyLower UpLoType = 9
	UpLoTypeStrictlyUpper UpLoType = 10
	UpLoTypeSelfAdjoint   UpLoType = 16
	UpLoTypeSymmetric     UpLoType = 32
)

type IOFormat struct{ /* opaque */ }
func NewIOFormat(_precision int32, _flags int32, _coeffSeparator string,
	_rowSeparator string, _rowPrefix string, _rowSuffix string,
	_matPrefix string, _matSuffix string, _fill int8) (*IOFormat, error)
func (i *IOFormat) Close() error
func (i *IOFormat) Precision() int32
func (i *IOFormat) SetPrecision(precision int32)
func (i *IOFormat) CoeffSeparator() (string, error)
func (i *IOFormat) RowSeparator() (string, error)
func (i *IOFormat) Fill() int8
...

func InitParallel() error
func NbThreads() (int32, error)
func SetNbThreads(v int32) error

// The matrix is the generic API: the type argument is the caller's.
type Matrix[_Scalar any, _Rows cppgoDimension, _Cols cppgoDimension,
	_Options cppgoDimension, _MaxRows cppgoDimension, _MaxCols cppgoDimension] struct{ /* opaque */ }

func NewMatrix[_Scalar any, _Rows cppgoDimension, _Cols cppgoDimension,
	_Options cppgoDimension, _MaxRows cppgoDimension, _MaxCols cppgoDimension]() (
	*Matrix[_Scalar, _Rows, _Cols, _Options, _MaxRows, _MaxCols], error)
func (m *Matrix[...]) Close() error
func (m *Matrix[...]) InnerStride() (int64, error)
func (m *Matrix[...]) OuterStride() (int64, error)

// The dimension type arguments: a fixed size is a phantom type, Dynamic is -1.
type D1 struct{}
func (D1) cppgoDimension() string { return "1" }
...
type Dynamic struct{}
func (Dynamic) cppgoDimension() string { return "-1" }
```

`Matrix` has three bindings because only three of its *own* declarations cross
as values: the constructor, the destructor and the two stride accessors. The
matrix operations are inherited, and the next section is what happens to them.

## The call

The by-value record, from [`eigen_test.go`](eigen_test.go): a format whose
constructor fills nine members across an argument block.

```go
format, err := NewIOFormat(3, 0, ", ", "\n", "<", ">", "[", "]", ' ')
if err != nil {
    t.Fatalf("NewIOFormat: %v", err)
}
defer format.Close()

if got := format.Precision(); got != 3 {
    t.Errorf("Precision() = %d, want 3", got)
}
if got, err := format.CoeffSeparator(); err != nil || got != ", " {
    t.Errorf("CoeffSeparator() = %q, %v, want %q, nil", got, err, ", ")
}
```

and the concrete specialization, from
[`matrices/matrices_test.go`](matrices/matrices_test.go): a fixed 2×2 row-major
matrix of doubles, whose strides are its shape.

```go
m, err := matrices.NewMatrix2d()
if err != nil {
    t.Fatalf("NewMatrix2d: %v", err)
}
defer m.Close()

if inner, err := m.InnerStride(); err != nil || inner != 1 {
    t.Errorf("InnerStride() = %d, %v, want 1", inner, err)
}
if outer, err := m.OuterStride(); err != nil || outer != 2 {
    t.Errorf("OuterStride() = %d, %v, want 2", outer, err)
}
```

`matrices.NewMatrix2d` is `NewMatrix[float64, D2, D2, D1, D2, D2]`, which is
`Eigen::Matrix<double, 2, 2, 1, 2, 2>`.

## What is not bound, and why

The matrix arithmetic is the part Eigen is used for, and it is the part this
slice cannot carry. Each declaration below is measured; the message is the
generator's own.

**The operations are inherited.** `rows()`, `cols()`, `norm()`, `inverse()`,
`transpose()`, `sum()` and `coeff()` are members of `PlainObjectBase`,
`MatrixBase` and `DenseBase`, not of `Matrix`. Binding the pattern
`Eigen::PlainObjectBase` gives a generic API with 13 bindings (`Rows`, `Cols`,
`Resize`, `SetZero`, `SetOnes`, `SetRandom`, …), but a concrete `Matrix`
specialization cannot reach them: the specializations are synthesized from the
pattern and carry no base list, so the module reports

```console
$ cppgo build ... -scan ./matrices
cppgo: warning: Eigen::Matrix<double,2,2,0,2,2> inherits 0 base class(es) and its inherited members are not copied: a specialization synthesized from a pattern cannot prove that a base member is valid for this instantiation; bind the members through As<Base>()
```

and `As<Base>()` cannot be generated either, because the base list it would cast
to is the same zero entries.

**The operations themselves return dependent types**, so even binding the base
patterns does not carry them:

| declaration | refusal |
|---|---|
| `Scalar DenseBase::sum() const` | `Eigen::DenseBase::sum() const is not bound: result: "Scalar" is not a type this build classifies; expose it as a scalar, a pointer, a reference or a supported std:: type` |
| `RealScalar MatrixBase::norm() const` | `Eigen::MatrixBase::norm() const is not bound: result: "RealScalar" is not a type this build classifies; …` — and `squaredNorm`, `stableNorm`, `blueNorm` and `hypotNorm` the same way |
| `const Inverse<Derived> MatrixBase::inverse() const` | `Eigen::MatrixBase::inverse() const is not bound: result: "const Inverse<Derived>" is not a type this build classifies; …` |
| `Scalar MatrixBase::determinant() const` | `result: "Scalar" is not a type this build classifies; …` |
| `TransposeReturnType DenseBase::transpose()` | `Eigen::DenseBase::transpose() is not bound: result: "TransposeReturnType" is not a type this build classifies; …` (and `ConstTransposeReturnType` for the `const` overload) |
| `CoeffReturnType DenseCoeffsBase::value() const` | `result: "CoeffReturnType" is not a type this build classifies; …`; binding `Eigen::DenseCoeffsBase` reports `generic API, 0 binding(s)`, so element access is not carried at all |
| `operator*(const MatrixBase<…> &, const PermutationBase<…> &)` | `Eigen::operator*(const MatrixBase<MatrixDerived> &, const PermutationBase<PermutationDerived> &): parameter : "const MatrixBase<MatrixDerived>" is not a type this build classifies; …` — addition and multiplication are free function templates over expression bases, and none of them crosses |
| the class template's own operators | `Eigen::Matrix::operator= … is not part of the generic API: an operator of a class template is bound on each concrete specialization instead` — and a specialization carries only the pattern's non-operator bindings, so no operator arrives that way either |

So the slice constructs a matrix and reads its shape, and cannot multiply it.
That is the wall the issue's Eigen matrix asks to cross; it is recorded here and
in the workstream's report rather than papered over with a C++ wrapper.

**Eigen's default storage order has no Go spelling.** The generic API names a
dimension value with a phantom type, and it generates `D1` … `D8` and `Dynamic`
(`-1`) only. Eigen's default `Options` is `0`, so `MatrixXf`/`MatrixXd` cannot be
named; `RowMajor` (`D1`) is the option that exists, and the module's two
specializations use it. The attempt to spell `Options = -1` as `Dynamic` is
refused by Eigen itself when the shim compiles:

```console
$ cppgo build ... -scan ./tmpprobe   # NewMatrix[float64, Dynamic, Dynamic, Dynamic, Dynamic, Dynamic]
cppgo build: cppgo: PlainObjectBase.h:978: zz_cppgo_shim.cpp: the generated shim does not compile;
static assertion failed: INVALID_MATRIX_TEMPLATE_PARAMETERS
```

**A `template class` instantiation does not help.** The older wall is still
there: `template class Eigen::Matrix<double, -1, -1, 0, -1, -1>;` does not compile
unless `EIGEN_NO_STATIC_ASSERT` is defined, and with it the record binds only its
constructors — the specialization's members are synthesized from the pattern, so
the arithmetic is exactly what is missing. The two-phase flow avoids the static
assertion by instantiating per member, which is why it is the flow this module
uses.

**The scan cannot read the generated package itself.** `-scan .` in this
directory fails with

```console
cppgo build: cppgo: Eigen::Matrix argument 1: the Go type _Scalar is not a scalar the two-phase flow carries yet; instantiate the template with a fixed-width Go scalar at the C++ boundary
```

because the generated constructor body instantiates `Matrix` with its own type
parameters (`&Matrix[_Scalar, …]`), and the scan reads that as an instantiation of
types rather than values. The module therefore scans [`matrices/`](matrices), a
package whose only instantiations are concrete, and
[`missing/`](missing) is outside the pattern because it deliberately names a
specialization the shim was not built for.

**The rest of the header**, for completeness:

| declaration | refusal |
|---|---|
| binding the whole header | `Eigen/src/Core/util/Memory.h:100: Eigen::internal::handmade_aligned_malloc(std::size_t, std::size_t): result: "void *" is a void pointer` |
| `Eigen::half`'s converting constructor | `Eigen::half::half is a member function template and is not bound: name its concrete instantiation as a non-template member beside it` |

## Commands

```console
$ make        # gen, scan, test, vet
$ make lib    # Eigen is header-only: nothing to build or link
$ make gen    # cppgo gen: the generic Go API, the C++ side, and the compiled C++ side
$ make scan   # cppgo build -scan ./matrices: the specializations the module uses
$ make test   # go test -tags cppgo_eigen, then the same under -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, libclang 20+ (generation only), and Eigen's headers.
`EIGEN_INCLUDE` is the one path a machine can differ in; the default is
`/usr/include/eigen3`. On a machine without Eigen every target prints the missing
path and exits zero, which is what lets
[`scripts/build-examples.sh`](../../scripts/build-examples.sh) run there.

## Loading

There is one object and no library: `Open` is called with the absolute path of
`libcppgo_eigen.so`, from `CPPGO_EIGEN_SHIM` or beside the package. Nothing else
is loaded, because the bound declarations are inline. Each package that uses the
binding opens the same shim itself (`eigen_test.go`'s `TestMain`, and the
subpackages' own), because the loader state is per Go package.

## Tests

`make test` runs the three test packages twice. What they assert, and the
declaration each value comes from:

| Test | Assertion, and its source |
|---|---|
| `TestStorageOptions` | `ColMajor` = 0, `RowMajor` = 1, `AutoAlign` = 0, `DontAlign` = 2, the values `enum StorageOptions` gives them in `Constants.h` |
| `TestComputationInfo` | `Success` = 0, `NumericalIssue` = 1, `NoConvergence` = 2, `InvalidInput` = 3 |
| `TestUpLoType` | the ten flags, including the compound `UnitLower` = 5, `UnitUpper` = 6, `StrictlyLower` = 9 and `StrictlyUpper` = 10 |
| `TestThreads` | `initParallel()` runs; `nbThreads()` is at least 1; after `setNbThreads(3)` it is 3 with OpenMP and 1 without, which is the branch `Parallelizer.h` documents; the value is put back |
| `TestIOFormat` | the nine-argument constructor stores every member: `Precision()` 3, `Flags()` 0, `CoeffSeparator()` `", "`, `RowSeparator()` a newline, the row and matrix prefixes and suffixes `"<"`, `">"`, `"["`, `"]"`, and `Fill()` a space; the setters write the same members — the constructor is copied from `IOFormat.h` |
| `TestMatrix2d` | `NewMatrix[float64, D2, D2, D1, D2, D2]` constructs `Eigen::Matrix<double, 2, 2, 1, 2, 2>`; `InnerStride()` is 1 element and `OuterStride()` 2 elements, the row-major strides `MatrixBase` documents (`innerStride` "between two consecutive entries within a row", `outerStride` "between two consecutive rows") |
| `TestMatrixXd` | `NewMatrix[float64, Dynamic, Dynamic, D1, Dynamic, Dynamic]` constructs the dynamic specialization; a default-constructed dynamic matrix has inner stride 1 and outer stride 0 |
| `TestCloseLifetime` (matrices) | a second `Close` returns `cppgo.ErrClosed`, and a stride accessor on a closed matrix returns it |
| `TestMissingInstantiation` (missing) | `NewMatrix[float64, D3, D3, D1, D3, D3]` — a specialization the shim was not built for — returns an error naming `::Eigen::Matrix<double, 3, 3, 1, 3, 3>` and the `cppgo build -scan` command, never a nil call |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` and `make scan` reproduce them byte for byte. `make
check` is the gate: it fails when a committed file differs from what the two
commands write.
