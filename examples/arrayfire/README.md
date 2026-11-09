# arrayfire

A slice of [ArrayFire](https://arrayfire.com/) 3.10's C++ header, bound to Go by
`cppgo gen`. There is no C++ source in this directory: the library is ArrayFire
itself, and the shim links against its CPU backend.

The header this example reads is
[`af/array.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/array.h),
which pulls in the type and device declarations through its includes. The binding
covers the declarations that cross as plain values, and says below which ones it
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

The data-source enum, from the same file:

```cpp
typedef enum {
    afDevice,   ///< Device pointer
    afHost     ///< Host pointer
} af_source;
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

And the one that ties the enum to a value
([`af/util.h`](https://github.com/arrayfire/arrayfire/blob/v3.10.0/include/af/util.h)):

```cpp
/// Get the size of the type represented by an af_dtype enum
AFAPI size_t getSizeOf(af::dtype type);
```

`make gen` reads them:

```console
$ make gen
build/cppgo gen -header /usr/local/include/af/array.h -package arrayfire \
                -library /usr/local/lib/libaf.so \
                -compile-commands build/compile_commands.json -out . \
                -bind af_dtype -bind af_source -bind af::getDevice \
                -bind af::getDeviceCount -bind af::getMemStepSize \
                -bind af::getSizeOf -bind af::isDoubleAvailable \
                -bind af::isHalfAvailable
af::getDevice()                  thunk  cppgo_thunk_357aaf090e93  not noexcept: an exception would unwind into Go
af::getDeviceCount()             thunk  cppgo_thunk_c3104da706f4  not noexcept: an exception would unwind into Go
af::getMemStepSize()             thunk  cppgo_thunk_b4d2e58ce60b  not noexcept: an exception would unwind into Go
af::getSizeOf(af::dtype)         thunk  cppgo_thunk_20ceac09288a  not noexcept: an exception would unwind into Go
af::isDoubleAvailable(const int) thunk  cppgo_thunk_c03708c6be2b  not noexcept: an exception would unwind into Go
af::isHalfAvailable(const int)   thunk  cppgo_thunk_211589553e43  not noexcept: an exception would unwind into Go
cppgo: built libcppgo_arrayfire.so with /usr/bin/g++
```

ArrayFire throws by design — every function above raises `af::exception` when the
call fails — so every binding is carried by a thunk that catches it and hands Go
an error. A `noexcept` declaration in this library would be a carrier change, and
there is none.

## The generated API

[`zz_cppgo.gen.go`](zz_cppgo.gen.go) is the whole surface a caller sees:

```go
type AfDtype uint32
const (
	AfDtypeF32 AfDtype = 0
	AfDtypeC32 AfDtype = 1
	AfDtypeF64 AfDtype = 2
	AfDtypeC64 AfDtype = 3
	AfDtypeB8  AfDtype = 4
	AfDtypeS32 AfDtype = 5
	AfDtypeU32 AfDtype = 6
	AfDtypeU8  AfDtype = 7
	AfDtypeS64 AfDtype = 8
	AfDtypeU64 AfDtype = 9
	AfDtypeS16 AfDtype = 10
	AfDtypeU16 AfDtype = 11
	AfDtypeF16 AfDtype = 12
	AfDtypeS8  AfDtype = 13
)

type AfSource uint32
const (
	AfSourceAfDevice AfSource = 0
	AfSourceAfHost   AfSource = 1
)

func GetSizeOf(arg0 AfDtype) (uint64, error)
func GetDevice() (int32, error)
func GetDeviceCount() (int32, error)
func GetMemStepSize() (uint64, error)
func IsDoubleAvailable(device int32) (bool, error)
func IsHalfAvailable(device int32) (bool, error)
```

## The call

From [`arrayfire_test.go`](arrayfire_test.go), where the element type and the
width it names are checked against each other:

```go
for _, tc := range []struct {
    dtype AfDtype
    want  uint64
}{
    {AfDtypeF32, 4},
    {AfDtypeC64, 16},
    {AfDtypeU8, 1},
} {
    got, err := GetSizeOf(tc.dtype)
    if err != nil {
        t.Errorf("GetSizeOf(%d): %v", tc.dtype, err)
        continue
    }
    if got != tc.want {
        t.Errorf("GetSizeOf(%d) = %d, want %d", tc.dtype, got, tc.want)
    }
}
```

## What is not bound, and why

`af::array` is the type every ArrayFire operation takes and returns, and it
cannot be bound today. Its first member is

```cpp
class AFAPI array {
    af_array   arr;
public:
    void set(af_array tmp);
    ...
};
```

where `af_array` is `typedef void * af_array` in `af/defines.h`. The generator
refuses a `void *` parameter:

```console
$ cppgo gen -header /usr/local/include/af/array.h -package arrayfire -bind af::array ...
cppgo gen: cppgo: /usr/local/include/af/array.h:48: af::array::set(af_array):
parameter tmp: "" is not a type this build classifies; expose it as a scalar, a
pointer, a reference or a supported std:: type
```

`-exclude` cannot narrow that either: the declaration is a member of the record,
and the record needs every selected member to be carryable.

The reductions the array work is built on are refused for a second, independent
reason. `af::sum`, `af::min`, `af::max`, `af::constant` and `af::randu` are not
declared in `af/array.h` at all — they are in `af/algorithm.h` and `af/data.h`.
Adding that header makes them visible, and the generic template API then refuses
their parameter:

```console
$ cppgo gen -header /usr/local/include/af/array.h \
            -header /usr/local/include/af/algorithm.h -package arrayfire -bind af::sum
cppgo gen: cppgo: /usr/local/include/af/algorithm.h:350: af::sum(const array &):
"const array &" is a dependent type the generic API does not carry yet; take the
type parameter by value, by pointer or by reference at the C++ boundary
```

The remaining refusals, each with the declaration it stops on:

| declaration | refusal |
|---|---|
| `af::dim4` | `af/dim4.hpp:53: af::dim4::operator=(dim4) noexcept: parameter other: "dim4" is passed by value but is not a POD` |
| `af::seq` | `af/seq.h:117: af::seq::seq(seq, bool): parameter other: "seq" is passed by value but is not a POD` |
| `af::exception` | the shim does not compile: `invalid conversion from 'const int8_t*' to 'const char*'` |
| `af::infoString(bool)` | it binds, as `func InfoString(verbose bool) (*int8, error)`: the `const char*` result is a pointer to C memory, not a Go string |

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

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` reproduces them byte for byte. `make check` is the gate:
it fails when a committed file differs from what `make gen` writes.
