# opencv

A slice of [OpenCV](https://opencv.org/) 4.10's core header, bound to Go by
`cppgo gen`. It is the worked example of binding a large, *installed* C++
library: unlike [mathx](../mathx) there is no C++ source in this directory to
build, because the library is OpenCV itself, built and packaged by the system.

The header this example reads is
[`opencv2/core.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core.hpp),
which reaches thousands of declarations through its includes. The binding covers
the ones the generator carries today, and says below which ones it does not carry
and why.

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

And the library's own configuration functions, all scalar
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

`make gen` reads them:

```console
$ make gen
build/cppgo gen -header /usr/include/opencv4/opencv2/core.hpp -package opencv \
                -library /usr/lib/x86_64-linux-gnu/libopencv_core.so \
                -compile-commands build/compile_commands.json -out . \
                -bind cv::NormTypes -bind cv::TermCriteria -bind cv::TickMeter \
                -bind cv::getVersionMajor -bind cv::getVersionMinor \
                -bind cv::getTickFrequency -bind cv::getNumThreads \
                -bind cv::setNumThreads -bind cv::getNumberOfCPUs \
                -bind cv::useOptimized -bind cv::setUseOptimized
cv::TermCriteria::TermCriteria()             thunk  cppgo_thunk_d6a8db8dcb53  not noexcept: an exception would unwind into Go
...
cppgo: built libcppgo_opencv.so with /usr/bin/g++
```

One line per binding, with the carrier that carries it. [Architecture][arch] has
what the three carriers are; [Carriers][carriers] has what each line's reason
means.

[arch]: https://manu343726.github.io/cppgo/explanation/architecture/
[carriers]: https://manu343726.github.io/cppgo/explanation/carriers/

## The generated API

[`zz_cppgo.gen.go`](zz_cppgo.gen.go) is the whole surface a caller sees. Its
declarations:

```go
type NormTypes uint32
const (
	NormTypesNORMINF      NormTypes = 1
	NormTypesNORML1       NormTypes = 2
	NormTypesNORML2       NormTypes = 4
	NormTypesNORML2SQR    NormTypes = 5
	NormTypesNORMHAMMING  NormTypes = 6
	NormTypesNORMHAMMING2 NormTypes = 7
	NormTypesNORMTYPEMASK NormTypes = 7
	NormTypesNORMRELATIVE NormTypes = 8
	NormTypesNORMMINMAX   NormTypes = 32
)

type TermCriteriaType uint32
const (
	TermCriteriaTypeCOUNT   TermCriteriaType = 1
	TermCriteriaTypeMAXITER TermCriteriaType = 1
	TermCriteriaTypeEPS     TermCriteriaType = 2
)

type TermCriteria struct{ /* opaque */ }
func NewTermCriteria() (*TermCriteria, error)
func NewTermCriteriaInt32Int32Float64(arg0 int32, maxCount int32, epsilon float64) (*TermCriteria, error)
func (t *TermCriteria) Clone() (*TermCriteria, error)
func (t *TermCriteria) Close() error
func (t *TermCriteria) IsValid() (bool, error)
func (t *TermCriteria) Type() int32
func (t *TermCriteria) SetType(arg0 int32)
func (t *TermCriteria) MaxCount() int32
func (t *TermCriteria) SetMaxCount(maxCount int32)
func (t *TermCriteria) Epsilon() float64
func (t *TermCriteria) SetEpsilon(epsilon float64)

type TickMeter struct{ /* opaque */ }
func NewTickMeter() (*TickMeter, error)
func (t *TickMeter) Clone() (*TickMeter, error)
func (t *TickMeter) Close() error
func (t *TickMeter) Start() error
func (t *TickMeter) Stop() error
func (t *TickMeter) GetTimeTicks() (int64, error)
func (t *TickMeter) GetTimeMicro() (float64, error)
func (t *TickMeter) GetTimeMilli() (float64, error)
func (t *TickMeter) GetTimeSec() (float64, error)
func (t *TickMeter) GetCounter() (int64, error)
func (t *TickMeter) GetFPS() (float64, error)
func (t *TickMeter) GetAvgTimeSec() (float64, error)
func (t *TickMeter) GetAvgTimeMilli() (float64, error)
func (t *TickMeter) Reset() error

func GetVersionMajor() (int32, error)
func GetVersionMinor() (int32, error)
func GetTickFrequency() (float64, error)
func GetNumThreads() (int32, error)
func SetNumThreads(nthreads int32) error
func GetNumberOfCPUs() (int32, error)
func UseOptimized() (bool, error)
func SetUseOptimized(onoff bool) error
```

Read the error results as a statement about the C++: `Start` has one because
`TickMeter::start` is not `noexcept` and an exception must not unwind into Go;
`TermCriteria.Type` has none because a field read cannot fail; `SetNumThreads`
returns only an error because it has no value.

## The call

From [`opencv_test.go`](opencv_test.go), where a meter accumulates one run and
reads it back:

```go
tm, err := NewTickMeter()
if err != nil {
    t.Fatalf("NewTickMeter: %v", err)
}
defer tm.Close()

if err := tm.Start(); err != nil {
    t.Fatalf("Start: %v", err)
}
if err := tm.Stop(); err != nil {
    t.Fatalf("Stop: %v", err)
}

if got, err := tm.GetCounter(); err != nil || got != 1 {
    t.Errorf("GetCounter() after one start/stop = %d, %v, want 1, nil", got, err)
}
```

## What is not bound, and why

The generator refuses a declaration it cannot carry rather than binding it
silently, and a refused declaration inside a record fails the whole record. That
is why the image work is not here: `cv::Mat`, the type every image operation
takes and returns, cannot be bound today. Its constructor set includes

```cpp
Mat(Size size, int type);
```

whose `cv::Size` parameter is passed **by value** and is not a POD that Go can
mirror (`cv::Size_` has user-declared constructors, so it is not plain old data).
The generator's error:

```console
$ cppgo gen -header /usr/include/opencv4/opencv2/core.hpp -package opencv \
            -instantiate 'cv::Size_<int>' -bind cv::Mat ...
cppgo gen: cppgo: /usr/include/opencv4/opencv2/core/mat.hpp:836:
cv::Mat::Mat(Size, int): parameter size: "Size" is passed by value but is not a
POD; take it by const reference, or make it a POD that Go can mirror
```

`-exclude` cannot narrow that: every constructor of a class shares one qualified
name, so excluding `cv::Mat::Mat` removes the copy constructor, the default
constructor and `Mat(int, int, int)` together. A record therefore needs every one
of its constructors to be carryable, and `cv::Mat`'s are not.

The same rule leaves the following out, each with the declaration and the error
that stop it:

| declaration | refusal |
|---|---|
| `String cv::getVersionString()` | `result: "String" is not part of the generated API; name it with -instantiate 'std::__cxx11::basic_string<…>'` |
| `cv::PCA::project(InputArray) const` | `result: "Mat" is passed by value but is not a POD` |
| `cv::SVD` | the shim does not compile: `SVD(const _InputArray&)` is lowered to a call through a pointer |
| `cv::norm`, `cv::mean`, `cv::sum` | `norm` stops on its `const Matx<_Tp,m,n>&` overload (`"const Matx<_Tp, m, n>" is not a type this build classifies`); `mean` and `sum` return `Scalar`, which the generator asks for as `-instantiate 'cv::Scalar_<double>'` |
| `cv::_InputArray` itself | `_InputArray(int, void *): parameter _obj: "void *" is a void pointer` |

## Commands

```console
$ make        # gen, test, vet
$ make lib    # report the OpenCV shared object the binding links against
$ make gen    # cppgo gen: the Go API, the C++ side, and the compiled C++ side
$ make test   # go test -tags cppgo_opencv, then the same under -race
$ make check  # cppgo check: are the committed generated sources current?
$ make clean
```

Requirements: Go 1.25+, libclang 20+ (generation only), and OpenCV's core
development files. `OPENCV_INCLUDE` and `OPENCV_LIB` are the two paths a machine
can differ in; the defaults are the Debian layout. On a machine
without OpenCV every target prints the missing path and exits zero, which is what
lets [`scripts/build-examples.sh`](../../scripts/build-examples.sh) run there.

## Loading

The test names the shim it loads, because the loader does not search the current
directory; the shared object beside it names OpenCV's core library as its
dependency, and the system loader resolves it:

- `CppgoLibraryPath` is OpenCV's core shared object, from `CPPGO_OPENCV_LIB` or
  the Debian default;
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
| `TestCloseLifetime` | a second `Close` returns `cppgo.ErrClosed`, and a method on a closed value returns it rather than calling into freed memory |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s) and
[`cppgo_shim/`](cppgo_shim/) are committed on purpose: the example is teaching
material, and `make gen` reproduces them byte for byte. `make check` is the gate:
it fails when a committed file differs from what `make gen` writes.
