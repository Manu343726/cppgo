# opencv

A slice of [OpenCV](https://opencv.org/) 4.10's `core` and `imgproc` headers,
bound to Go by `cppgo gen`. It is the worked example of binding a large,
*installed* C++ library: unlike [mathx](../mathx) there is no C++ source in this
directory to build, because the library is OpenCV itself, built and packaged by
the system.

The headers this example reads are
[`opencv2/core.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core.hpp)
and
[`opencv2/imgproc.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/imgproc/include/opencv2/imgproc.hpp),
which reach thousands of declarations through their includes. The binding covers
the image pipeline a Go program needs — a matrix, an input and an output array
around it, a colour conversion and a norm — and says below which declarations it
does not carry and why.

## What is bound

The declarations are copied from the installation's own headers, and each has a
link to the file it lives in.

An enum, with the values its declaration gives it
([`core/base.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/base.hpp)):

```cpp
enum NormTypes {
    NORM_INF       = 1,
    NORM_L1        = 2,
    NORM_L2        = 4,
    NORM_L2SQR     = 5,
    NORM_HAMMING   = 6,
    NORM_HAMMING2  = 7,
    NORM_TYPE_MASK = 7,
    NORM_RELATIVE  = 8,
    NORM_MINMAX    = 32
};
```

The colour-conversion enum, whose values are what `cvtColor` takes as its
`code` ([`imgproc.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/imgproc/include/opencv2/imgproc.hpp)):

```cpp
enum ColorConversionCodes {
    COLOR_BGR2BGRA     = 0,
    COLOR_BGR2RGBA     = 2,
    COLOR_RGB2GRAY     = 7,
    COLOR_BGR2GRAY     = 6,
    ...
};
```

A small record with two constructors, one method and three public fields
([`core/types.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/types.hpp)):

```cpp
class CV_EXPORTS TermCriteria
{
public:
    enum Type
    {
        COUNT=1,
        MAX_ITER=COUNT,
        EPS=2
    };

    TermCriteria();
    TermCriteria(int type, int maxCount, double epsilon);

    inline bool isValid() const;
    int type;
    int maxCount;
    double epsilon;
};
```

An object that holds state across calls, with scalar results
([`core/utility.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/utility.hpp)):

```cpp
class CV_EXPORTS_W TickMeter
{
public:
    TickMeter();
    void start();
    void stop();
    int64 getTimeTicks() const;
    double getTimeMicro() const;
    double getTimeMilli() const;
    double getTimeSec() const;
    int64 getCounter() const;
    double getFPS() const;
    double getAvgTimeSec() const;
    double getAvgTimeMilli() const;
    void reset();
};
```

The library's own configuration functions, all scalar
([`core/utility.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/utility.hpp)):

```cpp
CV_EXPORTS_W int getVersionMajor();
CV_EXPORTS_W int getVersionMinor();
CV_EXPORTS_W double getTickFrequency();
CV_EXPORTS_W int getNumThreads();
CV_EXPORTS_W void setNumThreads(int nthreads);
CV_EXPORTS_W int getNumberOfCPUs();
CV_EXPORTS_W bool useOptimized();
CV_EXPORTS_W void setUseOptimized(bool onoff);
```

### The image pipeline

`cv::Mat` is the type every image operation takes and returns
([`core/mat.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/mat.hpp)).
Its constructors are selected one by one, by full signature: the default one and
the `(rows, cols, type)` form are carryable, and the rest are reported, not
refused (see below). The public fields — `rows`, `cols`, `flags`, `dims`,
`data`, `datastart`, `dataend`, `datalimit` — become accessors, and the two
`total()` and `type()` methods are bound:

```cpp
Mat();
Mat(int rows, int cols, int type);
Mat& setTo(InputArray value, InputArray mask=noArray());
int type() const;
size_t total() const;
```

`cv::_InputArray` and `cv::_OutputArray` are the wrapper types the image
functions take instead of `Mat` around their data
([`core/mat.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/mat.hpp)):

```cpp
class CV_EXPORTS _InputArray
{
public:
    _InputArray();
    _InputArray(const Mat& m);
    _InputArray(const double& val);
    ...
};

class CV_EXPORTS _OutputArray : public _InputArray
{
public:
    _OutputArray();
    _OutputArray(Mat& m);
    ...
};
```

and the two functions the slice exists for
([`imgproc.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/imgproc/include/opencv2/imgproc.hpp)):

```cpp
CV_EXPORTS_W void cvtColor(InputArray src, OutputArray dst, int code, int dstCn = 0);
```

and ([`core.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core.hpp)):

```cpp
CV_EXPORTS_W double norm(InputArray src1, int normType = NORM_L2, InputArray mask = noArray());
```

`make gen` reads them:

```console
$ make gen
build/cppgo gen -header /usr/include/opencv4/opencv2/core.hpp \
                -header /usr/include/opencv4/opencv2/imgproc.hpp \
                -package opencv \
                -library /usr/lib/x86_64-linux-gnu/libopencv_imgproc.so \
                -compile-commands build/compile_commands.json -out . \
                -bind cv::NormTypes -bind cv::TermCriteria -bind cv::TickMeter \
                ...
                -bind 'cv::Mat::Mat()' -bind 'cv::Mat::Mat(int, int, int)' \
                -bind cv::Mat::total -bind cv::Mat::type -bind cv::Mat::setTo \
                -bind 'cv::_InputArray::_InputArray(const Mat &)' \
                -bind 'cv::_InputArray::_InputArray(const double &)' \
                -bind 'cv::_OutputArray::_OutputArray(Mat &)' \
                -bind cv::cvtColor -bind cv::ColorConversionCodes \
                -bind 'cv::norm(InputArray, int, InputArray)'
cv::Mat::Mat()                               direct _ZN2cv3MatC1Ev               noexcept, not virtual, register class; the library exports the symbol
cv::Mat::Mat(int, int, int)                  thunk  cppgo_thunk_95167f7605da     not noexcept: an exception would unwind into Go
cv::Mat::cols() const noexcept               thunk  cppgo_thunk_ecd878d5e515     a field accessor: the thunk reads or writes the member
cv::Mat::rows() const noexcept               thunk  cppgo_thunk_5d85f7e35c3b     a field accessor: the thunk reads or writes the member
cv::Mat::setTo(InputArray, InputArray)       thunk  cppgo_thunk_92864a2799f0     not noexcept: an exception would unwind into Go
cv::Mat::total() const                       thunk  cppgo_thunk_418abb6b61a8     not noexcept: an exception would unwind into Go
cv::Mat::type() const                        thunk  cppgo_thunk_ae2e7c20756e     not noexcept: an exception would unwind into Go
cv::_InputArray::_InputArray(const Mat &)    thunk  cppgo_thunk_788f0c922a56     not noexcept: an exception would unwind into Go
cv::_InputArray::_InputArray(const double &) thunk  cppgo_thunk_df895eeb7d91     not noexcept: an exception would unwind into Go
cv::_OutputArray::_OutputArray(Mat &)        thunk  cppgo_thunk_d167bb95f59b     not noexcept: an exception would unwind into Go
cv::cvtColor(InputArray, OutputArray, int)   thunk  cppgo_thunk_81b73d1d4521     default arguments: the caller's compiler applies them, so the call is written in C++
cv::norm(InputArray, int, InputArray)        thunk  cppgo_thunk_ebf8bf9df0        not noexcept: an exception would unwind into Go
...
cppgo: built libcppgo_opencv.so with /usr/bin/g++
```

One line per binding, with the carrier that carries it. [Architecture][arch] has
what the three carriers are; [Carriers][carriers] has what each line's reason
means. The shim links against `libopencv_imgproc.so`, the module `cvtColor`
lives in; OpenCV's imgproc names `libopencv_core.so` among its own dependencies,
so the core symbols `Mat`'s constructors call resolve too.

[arch]: https://manu343726.github.io/cppgo/explanation/architecture/
[carriers]: https://manu343726.github.io/cppgo/explanation/carriers/

## The generated API

[`zz_cppgo.gen.go`](zz_cppgo.gen.go) is the whole surface a caller sees. Its
image pipeline — the rest is unchanged and in the file — is:

```go
type InputArray struct{ /* opaque */ }
func NewInputArray() (*InputArray, error)
func NewInputArrayMatPtrRef(m *Mat) (*InputArray, error)
func NewInputArrayFloat64PtrRef(val *float64) (*InputArray, error)
func (i *InputArray) Close() error

type Mat struct{ /* opaque */ }
func NewMat() (*Mat, error)
func NewMatInt32Int32Int32(rows int32, cols int32, arg2 int32) (*Mat, error)
func (m *Mat) Close() error
func (m *Mat) SetToInputArrayPtrRef(value *InputArray) (*Mat, error)
func (m *Mat) SetTo(value *InputArray, mask *InputArray) (*Mat, error)
func (m *Mat) Type() (int32, error)
func (m *Mat) Total() (uint64, error)
func (m *Mat) TotalInt32(startDim int32) (uint64, error)
func (m *Mat) TotalInt32Int32(startDim int32, endDim int32) (uint64, error)
func (m *Mat) Flags() int32
func (m *Mat) SetFlags(flags int32)
func (m *Mat) Dims() int32
func (m *Mat) SetDims(dims int32)
func (m *Mat) Rows() int32
func (m *Mat) SetRows(rows int32)
func (m *Mat) Cols() int32
func (m *Mat) SetCols(cols int32)
func (m *Mat) Data() *uint8
func (m *Mat) SetData(data *uint8)
func (m *Mat) Datastart() *uint8
func (m *Mat) SetDatastart(datastart *uint8)
func (m *Mat) Dataend() *uint8
func (m *Mat) SetDataend(dataend *uint8)
func (m *Mat) Datalimit() *uint8
func (m *Mat) SetDatalimit(datalimit *uint8)

type OutputArray struct{ /* opaque */ }
func NewOutputArray() (*OutputArray, error)
func NewOutputArrayMatPtrRef(m *Mat) (*OutputArray, error)
func (o *OutputArray) Close() error
func (o *OutputArray) AsInputArray() *InputArray

type ColorConversionCodes uint32
const (
	ColorConversionCodesCOLORBGR2GRAY ColorConversionCodes = 6
	...
)

func CvtColor(src *InputArray, dst *OutputArray, code int32, dstCn int32) error
func CvtColorInputArrayPtrRefOutputArrayPtrRefInt32(src *InputArray, dst *OutputArray, code int32) error
func Norm(src1 *InputArray, normType int32, mask *InputArray) (float64, error)
```

Read the error results as a statement about the C++: `SetTo` has one because
`setTo` is not `noexcept`; `Rows` has none because a field read cannot fail
(and it returns zero on a closed value); `CvtColor` returns only an error because
the C++ declaration has a `void` result.

`_OutputArray` inherits `_InputArray`, so the binding gives it the only thing a
derived value can honestly expose: `AsInputArray`, a view of the same object at
the base address. `_OutputArray`'s constructor is the one that matters here:
`_OutputArray(Mat &)` stores the address of the caller's matrix, so the matrix
the Go value owns is the one `cvtColor` writes into.

## The call

From [`opencv_test.go`](opencv_test.go): two BGR pixels are converted to
luminance and read back.

```go
src, err := NewMatInt32Int32Int32(1, 2, cv8UC3) // 1x2, three channels
if err != nil {
    t.Fatalf("NewMatInt32Int32Int32: %v", err)
}
defer src.Close()
copy(unsafe.Slice(src.Data(), 6), []byte{0, 0, 255, 0, 255, 0}) // red, green

dst, err := NewMatInt32Int32Int32(1, 2, cv8UC1)
if err != nil {
    t.Fatalf("NewMatInt32Int32Int32: %v", err)
}
defer dst.Close()

in, err := NewInputArrayMatPtrRef(src)
if err != nil {
    t.Fatalf("NewInputArrayMatPtrRef: %v", err)
}
defer in.Close()
out, err := NewOutputArrayMatPtrRef(dst)
if err != nil {
    t.Fatalf("NewOutputArrayMatPtrRef: %v", err)
}
defer out.Close()

if err := CvtColor(in, out, int32(ColorConversionCodesCOLORBGR2GRAY), 0); err != nil {
    t.Fatalf("CvtColor: %v", err)
}
// dst now holds 76 (pure red) and 150 (pure green).
```

## What is not bound, and why

A refused declaration does not refuse its record. Since the generator visit is
per declaration, one uncarryable overload is skipped with a warning and the rest
of the record binds; the slice above is the proof. Selecting the whole `Mat`
constructor set is what shows it:

```console
$ cppgo gen -header /usr/include/opencv4/opencv2/core.hpp -package opencv \
            -library /usr/lib/x86_64-linux-gnu/libopencv_core.so \
            -bind 'cv::Mat::Mat' ...
cppgo: warning: cv::Mat::Mat(Size, int) is not bound: parameter size: "Size" is not part of the generated API; name it with -instantiate 'cv::Size_<int>'
cppgo: warning: cv::Mat::Mat(int, int, int, const Scalar &) is not bound: parameter s: "const Scalar" is not part of the generated API; name it with -instantiate 'cv::Scalar_<double>'
cppgo: warning: cv::Mat::Mat(int, int, int, void *, size_t) is not bound: parameter data: "void *" is a void pointer; expose a typed pointer at the C++ boundary
cppgo: warning: cv::Mat::Mat(const Mat &, const Range &) is not bound: parameter ranges: "Range" is not a POD; use scalar elements, or bind a C++ wrapper that copies the objects
cv::Mat::Mat()                               direct _ZN2cv3MatC1Ev               ...
cv::Mat::Mat(const Mat &)                    thunk  cppgo_thunk_648f34507efe     ...
```

`-instantiate` is not an escape here: the header declares no explicit
instantiation of `cv::Size_<int>` or `cv::Scalar_<double>`, so the flag answers
`it names no instantiation this header declares`. The constructors that remain
— the default one, the copy constructor, the `(rows, cols, type)` form — are
enough to allocate and populate an image, which is what the slice does.

Binding the record *by name* stops earlier, on a Go-name collision:

```console
$ cppgo gen ... -bind cv::Mat
cppgo gen: cppgo: cv::Mat::Mat(const Mat &) and cv::Mat::clone() const both map to Go name Clone; rename one with -rename '<signature>=<GoName>'
```

Both members are real and both map to `Clone`; `-rename` resolves it, and the
example avoids the collision by naming the members it uses. The same collision
exists for `Mat::ptr(int)` and `Mat::ptr(int) const`.

The rest of the matrix API, each with the declaration that stops it:

| declaration | refusal |
|---|---|
| `cv::Mat::at<_Tp>(int, int)` | `cv::Mat::at is a member function template and is not bound: name its concrete instantiation as a non-template member beside it` — element access is `data()`, a pointer to the matrix's own buffer |
| `cv::Mat::ptr<_Tp>(int)` | the same member-template warning |
| `cv::Mat::allocator`, `cv::Mat::u` | `is not bound: "MatAllocator *" is a pointer to a record, and nothing on the Go side owns it` (and `"UMatData *"` for `u`) |
| `cv::Mat::size`, `cv::Mat::step` | `is not bound: its type, cv::MatSize, is not part of the generated API` (and `cv::MatStep`) |
| `cv::mean(InputArray, InputArray)` | `result: "Scalar" is not part of the generated API; name it with -instantiate 'cv::Scalar_<double>'`, and `-instantiate` has no declaration to attach to |
| `cv::sum(InputArray)` | the same `Scalar` result; `cv::norm` is the scalar-returning reduction that binds |
| `cv::imread(const String &, int)` | `parameter filename: "const String" is not part of the generated API; name it with -instantiate 'std::__cxx11::basic_string<char,std::char_traits<char>,std::allocator<char>>'` — and that instantiation is not declared either, so the image-codec path does not bind today |
| `cv::imwrite(const String &, InputArray, const std::vector<int> &)` | the same `const String` parameter |
| `String cv::getVersionString()` | `result: "String" is not part of the generated API` |
| `cv::SVD` | the shim does not compile, as before |
| `cv::_InputArray::_InputArray(int, void *)` | `parameter _obj: "void *" is a void pointer` |

`imread`/`imwrite` are the one capability the issue's matrix names that this
slice cannot demonstrate: the codec API takes `cv::String`, a typedef of
`std::string`, and the generator maps the typedef only when the header declares
the standard-library instantiation. The file round trip is therefore absent,
not mocked; `cvtColor` and the buffer access cover the pixel path.

## Commands

```console
$ make        # gen, test, vet
$ make lib    # report the OpenCV shared object the binding links against
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
$ make test   # go test -tags cppgo_opencv, then the same under -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, libclang 20+ (generation only), and OpenCV's core,
imgproc and imgcodecs development files. `OPENCV_INCLUDE` and `OPENCV_LIB` are
the two paths a machine can differ in; the defaults are the Debian layout, with
`OPENCV_LIB` pointing at `libopencv_imgproc.so`. On a machine without OpenCV
every target prints the missing path and exits zero, which is what lets
[`scripts/build-examples.sh`](../../scripts/build-examples.sh) run there.

## Loading

The test names the shim it loads, because the loader does not search the current
directory; the shared object beside it names OpenCV's imgproc library as its
dependency, and the system loader resolves core through imgproc:

- `CppgoLibraryPath` is OpenCV's imgproc shared object, from `CPPGO_OPENCV_LIB`
  or the Debian default;
- `Open` is called with the absolute path of `libcppgo_opencv.so`.

## Tests

`make test` runs [`opencv_test.go`](opencv_test.go) twice. What it asserts, and
the declaration each value comes from:

| Test | Assertion, and its source |
|---|---|
| `TestVersion` | `GetVersionMajor()` = 4 and `GetVersionMinor()` = 10, the `CV_VERSION_MAJOR` and `CV_VERSION_MINOR` of [`core/version.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/version.hpp) in the 4.10 installation this binds |
| `TestNormTypes` | each constant equals the value `enum NormTypes` gives it in [`core/base.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/base.hpp), including `NORM_TYPE_MASK` = `NORM_HAMMING2` = 7 |
| `TestTermCriteria` | the three-argument constructor stores its arguments; the accessors and the setters agree; `IsValid()` is true when the type is `COUNT\|EPS` with a count, and false on the default-constructed record, as [`isValid`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/types.hpp) computes it |
| `TestTickMeter` | a fresh meter is zero; one start/stop gives `GetCounter()` = 1 and a non-negative duration; `GetTimeSec()` equals `GetTimeTicks()/GetTickFrequency()`; a second run gives 2; `Reset` clears both, per the inline definitions in [`core/utility.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/utility.hpp) |
| `TestRuntimeConfiguration` | the tick frequency is positive, at least one CPU is reported, `SetNumThreads(2)` then `GetNumThreads()` is 2, and `SetUseOptimized` round-trips through `UseOptimized` |
| `TestMatConstruction` | a 2×3 `CV_8UC3` matrix reports `Rows()` 2, `Cols()` 3, `Type()` 16, `Total()` 6 and `Dims()` 2 with a non-nil `Data()`; the default-constructed matrix reports `Total()` 0 and a nil `Data()` — the `CV_MAKETYPE` encoding of [`core/hal/interface.h`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/hal/interface.h) |
| `TestMatPixels` | two BGR pixels written through `Data()` are read back byte for byte: `(0, 0, 255)` red and `(0, 255, 0)` green |
| `TestSetTo` | `SetTo(7)` writes 7 into all nine bytes of a 1×3 `CV_8UC3` matrix, per `Mat::setTo`'s "assigns value to every channel of every pixel" |
| `TestCvtColorBGR2GRAY` | `COLOR_BGR2GRAY` is 6 and `cvtColor` turns pure red into 76 and pure green into 150: `Y = 0.299R + 0.587G + 0.114B`, the BT.601 luma formula OpenCV's `COLOR_BGR2GRAY` documents, rounded to 8 bits, and the destination is the matrix the `OutputArray` wraps |
| `TestNorm` | `Norm(NORM_L2)` of `[3 4 0 0]` is exactly 5 and `Norm(NORM_L1)` is 7 — `NormTypes` values 4 and 2, and the sum of squares and absolute values they name |
| `TestCvtColorError` | `cvtColor` on an empty matrix returns a non-nil error: OpenCV raises `cv::Exception` and the thunk hands it to Go |
| `TestCloseLifetime` | a second `Close` returns `cppgo.ErrClosed`, and a method on a closed value returns it rather than calling into freed memory |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` reproduces them byte for byte. `make check` is the gate:
it fails when a committed file differs from what `make gen` writes.
