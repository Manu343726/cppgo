# eigen

A slice of [Eigen](https://eigen.tuxfamily.org/) 3.4's Dense header, bound to Go
by `cppgo gen`. It is the worked example of a **header-only** C++ library: there
is no shared object to build, none to link, and no `-library` flag, because every
Eigen declaration is inline. The shim cppgo compiles from the generated sources is
the whole binary a caller loads.

The header this example reads is
[`Eigen/Dense`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/Dense), which
reaches most of Eigen through its includes. The binding covers the declarations
that cross as plain values, and says below which ones it does not carry and why.

## What is bound

Three enums from
[`Eigen/src/Core/util/Constants.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/util/Constants.h):

```cpp
enum StorageOptions {
  ColMajor = 0,
  RowMajor = 1,
  AutoAlign = 0,
  DontAlign = 2
};

enum ComputationInfo {
  Success = 0,
  NumericalIssue = 1,
  NoConvergence = 2,
  InvalidInput = 3
};

enum UpLoType {
  Lower           = 0x1,
  Upper           = 0x2,
  UnitDiag        = 0x4,
  ZeroDiag        = 0x8,
  UnitLower       = 0x5,
  UnitUpper       = 0x6,
  StrictlyLower   = 0x9,
  StrictlyUpper   = 0xa,
  SelfAdjoint     = 0x10,
  Symmetric       = 0x20
};
```

And the functions that report and set the thread count its parallel algorithms
use, from
[`Eigen/src/Core/products/Parallelizer.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/products/Parallelizer.h):

```cpp
/** Must be call first when calling Eigen from multiple threads */
inline void initParallel();

/** \returns the max number of threads reserved for Eigen */
inline int nbThreads();

/** Sets the max number of threads reserved for Eigen */
inline void setNbThreads(int v);
```

`make gen` reads them:

```console
$ make gen
build/cppgo gen -header /usr/include/eigen3/Eigen/Dense -package eigen \
                -compile-commands build/compile_commands.json -out . \
                -bind Eigen::StorageOptions -bind Eigen::ComputationInfo \
                -bind Eigen::UpLoType -bind Eigen::nbThreads \
                -bind Eigen::setNbThreads -bind Eigen::initParallel
Eigen::initParallel()       thunk  cppgo_thunk_54b1814a5d61  not noexcept: an exception would unwind into Go
Eigen::nbThreads()          thunk  cppgo_thunk_a31bc433fb13  not noexcept: an exception would unwind into Go
Eigen::setNbThreads(int)    thunk  cppgo_thunk_fc8637e263a2  not noexcept: an exception would unwind into Go
cppgo: built libcppgo_eigen.so with /usr/bin/g++
```

The three functions are inline, so the shim is where their code lives: `cppgo gen`
compiles it into `libcppgo_eigen.so`, and nothing else is loaded.

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

func InitParallel() error
func NbThreads() (int32, error)
func SetNbThreads(v int32) error
```

## The call

From [`eigen_test.go`](eigen_test.go), where the thread count is set and read
back:

```go
if err := InitParallel(); err != nil {
    t.Fatalf("InitParallel: %v", err)
}

before, err := NbThreads()
if err != nil {
    t.Fatalf("NbThreads: %v", err)
}
if before < 1 {
    t.Fatalf("NbThreads() = %d, want at least 1", before)
}

if err := SetNbThreads(3); err != nil {
    t.Fatalf("SetNbThreads(3): %v", err)
}
```

## What is not bound, and why

The matrix types are not bound, and the wall is Eigen's own. A C++ template only
exists where it is instantiated, so `cppgo gen` needs a concrete specialization
visible in the header it reads, and an *explicit instantiation* is the only
spelling libclang reports:

```cpp
template class Eigen::Matrix<double, -1, -1, 0, -1, -1>;
```

That line does not compile, for every `Matrix` specialization. Eigen's
constructors for the 3- and 4-element cases static-assert their own size
([`Matrix.h`](https://gitlab.com/libeigen/eigen/-/blob/3.4.0/Eigen/src/Core/Matrix.h)):

```cpp
EIGEN_STRONG_INLINE Matrix(const Scalar& x, const Scalar& y, const Scalar& z)
{
  Base::_check_template_params();
  EIGEN_STATIC_ASSERT_VECTOR_SPECIFIC_SIZE(Matrix, 3)
  ...
```

and an explicit class instantiation instantiates every constructor, so a 3×3
matrix fails on the three-element constructor and a 3-element vector fails on the
four-element one. The shim build reports it:

```console
cppgo: StaticAssert.h:158: zz_cppgo_shim.cpp: the generated shim does not compile;
static assertion failed: THIS_METHOD_IS_ONLY_FOR_VECTORS_OF_A_SPECIFIC_SIZE
```

Eigen documents the escape hatch — `#define EIGEN_NO_STATIC_ASSERT` turns those
compile-time checks into runtime ones — and with it the instantiation compiles.
The record is then refused on a constructor instead:

```console
cppgo: Matrix.h:267: Eigen::Matrix<double,-1,-1,0,-1,-1>::Matrix(
       internal::constructor_without_unaligned_array_assert): parameter :
       "internal::constructor_without_unaligned_array_assert" is passed by value
       but is not a POD; take it by const reference, or make it a POD that Go can
       mirror
```

That is the same refusal as `cv::Mat`: a record needs every constructor it
declares to be carryable, and `-exclude` cannot drop one overload of a shared
name.

The two-phase template flow meets the same wall, because it writes the same
explicit instantiation into the shim: `cppgo build -scan` emits
`template class ::Eigen::Matrix<double, -1, -1, 0, -1, -1>;` before the thunks.

Two more refusals, for completeness:

| declaration | refusal |
|---|---|
| `Eigen::IOFormat::IOFormat(int, int, const std::string&, …)` | `the call needs 16 INTEGER and 0 SSE argument registers; passing an argument block is not implemented by this build yet` |
| binding the whole header | `Eigen/src/Core/util/Memory.h:100: Eigen::internal::handmade_aligned_malloc(std::size_t, std::size_t): result: "void *" is a void pointer` |

## Commands

```console
$ make        # gen, test, vet
$ make lib    # Eigen is header-only: nothing to build or link
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
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
is loaded, because the bound declarations are inline.

## Tests

`make test` runs [`eigen_test.go`](eigen_test.go) twice. What it asserts, and the
declaration each value comes from:

| Test | Assertion, and its source |
|---|---|
| `TestStorageOptions` | `ColMajor` = 0, `RowMajor` = 1, `AutoAlign` = 0, `DontAlign` = 2, the values `enum StorageOptions` gives them in `Constants.h` |
| `TestComputationInfo` | `Success` = 0, `NumericalIssue` = 1, `NoConvergence` = 2, `InvalidInput` = 3 |
| `TestUpLoType` | the ten flags, including the compound `UnitLower` = 5, `UnitUpper` = 6, `StrictlyLower` = 9 and `StrictlyUpper` = 10 |
| `TestThreads` | `initParallel()` runs; `nbThreads()` is at least 1; after `setNbThreads(3)` it is 3 with OpenMP and 1 without, which is the branch `Parallelizer.h` documents; the value is put back |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` reproduces them byte for byte. `make check` is the gate:
it fails when a committed file differs from what `make gen` writes.
