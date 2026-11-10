# opencv

A slice of [OpenCV](https://opencv.org/) 4.10 bound to Go by `cppgo gen` from the
installation's own headers: `core`, `imgproc` and `imgcodecs`. It is the worked
example of binding a large, *installed* C++ library — there is no C++ source in
this directory to build, because the library is OpenCV itself, built and
packaged by the system.

The binding goes through the image pipeline a program actually uses: a
`cv::Mat` built from a shape or from raw bytes, the `_InputArray`/`_OutputArray`
proxies around it, image files through `imgcodecs`, and the algorithms of
`imgproc` and `core` — colour conversion, thresholding, resizing, blurring, edge
detection, contours and reductions. Every test reads or writes a real matrix or
the committed PNG under [`testdata/`](testdata) and asserts the value.

## What is bound

The declarations are copied from the installation's headers; the *generator*
features that carry them are described in [§ What the generator learned](#what-the-generator-learned).

### The matrix

`cv::Mat` ([`core/mat.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/mat.hpp))
is bound as a whole record. The constructors that carry are the default one, the
`(rows, cols, type)` form, the `(rows, cols, type, void *, size_t)` raw-data
form (a `void *` is Go's `unsafe.Pointer`), the `Scalar`-filled form and the
copy constructor; the public fields — `rows`, `cols`, `flags`, `dims`, `data`,
`datastart`, `dataend`, `datalimit` — become accessors, and the methods the
pipeline needs are methods: `total`, `type`, `channels`, `empty`, `setTo`,
`clone`, `copy`, `convertTo`, `reshape`. The copy constructor and `clone()` both
map to Go's `Clone`; the collision resolves as `Copy` (the copy constructor,
which shares the buffer) and `Clone` (the deep copy), which is what the two C++
names actually mean.

Element access is the two explicit instantiations of the member function
template:

```cpp
template<typename _Tp> _Tp& at(int i0, int i2);
```

The manifest names `at<unsigned char>(int, int)` and `at<double>(int, int)`, and
the generated methods are `AtUint8(i, j) (*uint8, error)` and
`AtFloat64(i, j) (*float64, error)`: the reference the C++ side returns is the
element, so a write through it changes the matrix.

### The proxies

`cv::_InputArray` and `cv::_OutputArray`
([`core/mat.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/mat.hpp))
are the wrapper types the algorithms take instead of `Mat`. The constructors
that carry are `()`, `(const Mat &)` and `(const double &)` for the input side,
and `()` and `(Mat &)` for the output side — the last one is what makes an
algorithm write into the matrix the Go value owns:

```go
in, _ := NewInputArrayMatPtrRef(src)
out, _ := NewOutputArrayMatPtrRef(dst)
CvtColor(in, out, int32(ColorConversionCodesCOLORBGR2GRAY), 0)
```

### The algorithms

| Module | Bound declarations |
|---|---|
| `imgproc` | `cvtColor`, `threshold`, `resize`, `blur`, `GaussianBlur`, `Canny`, `contourArea`, `boundingRect` |
| `core` | `norm`, `mean`, `sum`, `minMaxLoc`, `add`, `subtract`, `multiply`, `divide` |
| `imgcodecs` | `imread`, `imwrite`, `imencode` |

and the enums their codes come from: `ColorConversionCodes`, `ThresholdTypes`,
`InterpolationFlags` and `BorderTypes`. An enum alias keeps one Go constant:
`BORDER_REFLECT101` and `BORDER_DEFAULT` are the same value, and the report says
so.

The two geometry records the algorithms take by value are synthesised from their
patterns and are Go structs of the C layout:
`PointInt{X, Y}`, `SizeInt{Width, Height}`, `RectInt{X, Y, Width, Height}` and
`ScalarFloat64{Val [4]float64}`. Their operations that need no dependent type
are methods (`Dot`, `Cross`, `Inside`, `Area`, `Empty`, `Contains`), and their
fields are read and written directly, which is what a mirrored record is.

### The file paths

`imread` and `imwrite` take a `cv::String`, an alias of `std::string`; the
generator resolves the alias to the standard type the emitted table names, and
the parameter crosses as a Go `string`. `imencode` writes its bytes through
`std::vector<uchar> &`, a non-const standard-library reference the generator now
copies back into the caller's Go slice (`*[]uint8`).

The test fixture is [`testdata/pixels.png`](testdata/pixels.png), a 4×3 BGR
image whose twelve pixels are the values the tests assert.

## What the generator learned

The declarations above needed features the emitter did not have. Each is now a
mechanism with tests, not a documented exception:

- **typedef aliases of standard types.** `cv::String` is
  `std::__cxx11::basic_string<char, …>`, and the spelling carries no clue. The
  type map reads the canonical record name behind a typedef and normalises the
  standard-library aliases (`basic_string` → `std::string`, an allocator
  argument is dropped from `std::vector`).
- **name collisions on a whole record.** Two declarations that map to one Go
  name no longer refuse the run: the declared member keeps the name, the copy
  constructor becomes `Copy`, a const overload takes a `Const` suffix, and
  anything still colliding takes a short signature hash. `-rename` still wins.
- **an uncarryable declaration in a selected set.** A class template the generic
  API cannot model, or an overload of a named function, is a warning naming the
  declaration; the rest of the set binds. That is what makes `-bind cv::Mat` a
  usable command.
- **concrete instantiations of function and member templates.** A member
  function template has no Go spelling (Go has no type parameters on methods),
  so the manifest names the instantiation — `-instantiate
  'cv::Mat::at<unsigned char>(int, int)'` — and the emitter resolves it against
  the pattern, substitutes the arguments into the signature, and writes the
  explicit template arguments into the call.
- **specializations libclang does not visit.** `cv::Scalar_<double>`,
  `cv::Point_<int>`, `cv::Size_<int>` and `cv::Rect_<int>` have no definition
  cursor in the translation unit. `-instantiate 'cv::Size_<int>'` now builds the
  record from its pattern: substituted fields, standard-layout sizes and
  offsets, the pattern's members, and a base's fields flattened into the derived
  layout (`Scalar_` derives from `Vec`, which derives from `Matx`, which carries
  `val[4]`).
- **a `std::vector<T>&` out-parameter.** The Go side passes `*[]T`; the shim
  builds the vector from the caller's slice, calls, and copies the callee's
  result into a block the Go side reads and frees. `imencode` is the case that
  needed it.
- **`void *`.** An untyped pointer is Go's `unsafe.Pointer`, which is the C API's
  own shape. It is what makes the `Mat` raw-data constructor bind. Nothing on
  the Go side owns the memory; the caller keeps it alive, exactly as the C++
  declaration says. A *result* of type `void *` crosses as `unsafe.Pointer` too.

## What is not bound, and what the coverage report says

`cppgo gen` writes [`coverage.txt`](coverage.txt) when the Makefile passes
`-coverage`. It is a generated output — `make check` regenerates it and fails on
a difference — so the numbers below are a measurement, not a claim. The report
has one summary row per module and one line per declaration that is not bound,
with the generator's reason and a classification.

| module | selected | bound | refused | skipped | templates | bound ratio |
|---|---|---|---|---|---|---|
| core | 236 | 159 | 5 | 23 | 49 | 0.75 |
| imgproc | 9 | 9 | 0 | 0 | 0 | 1.00 |
| imgcodecs | 4 | 4 | 0 | 0 | 0 | 1.00 |

The ratio is bound / (bound + refused + templates): the *declarations the
manifest selected*, not the whole header. A slice that names three modules does
not claim the other two thousand declarations, and the report lists those as
`not-selected` so the count is visible rather than silent.

The 5 refusals of `core`, each classified in the file:

- **untyped-pointer** — three `Mat` constructors taking a `void *` plus
  `push_back_(const void *)`; two of them also take a `size_t *` step array.
  The raw-data `(rows, cols, type, void *, size_t)` form now binds, so these are
  the remaining untyped-pointer declarations that need a different shape (an
  `const int *` shape array).
- **by-value-record** — two `Mat` constructors/operators taking
  `const std::vector<Range> &`: `cv::Range` is a record with a constructor, and
  the vector-of-record element path is the same one `findContours` needs (below).
- **unowned-pointer** — `Mat::getDefaultAllocator()` and `getStdAllocator()`
  return a `MatAllocator *` nobody owns.

The 49 templates are the member function templates of `cv::Mat` (`at`, `ptr`,
`push_back`, `forEach`, the conversion operators). Each is bound by naming its
instantiation; the manifest names the two the tests use, and the rest are the
matrix's generic surface.

The 23 skipped declarations are explained, not refused: they are the C++
constructors, destructor and move overloads of the *mirrored* records
(`Point`, `Size`, `Rect`, `Scalar`), whose Go value is the struct itself. A
mirrored record has no `cppgo.Object`, no factory and no `Close`; the struct is
the value.

### The one algorithm still out: contours

`findContours`, `drawContours`, `split` and `merge` take an
`InputArrayOfArrays`/`OutputArrayOfArrays`, which is an `_OutputArray`
constructed from `std::vector<std::vector<Point>>` (or `std::vector<Mat>`), and
the mapping of a vector of vectors is the one shape this build does not have.
`contourArea` and `boundingRect` *do* bind and are tested on a contour built the
way `findContours` produces one — an N×1 two-channel `CV_32SC2` matrix.

This is a genuine design decision rather than a construct to patch, and it has
two honest options:

1. **A generated record-vector handle.** For `std::vector<T>` where `T` is an
   opaque record, emit a small Go type (`VecMat`) owning the C++ vector, with
   `Len`, `At(i)` returning a view, and `Close`; the thunk runs the call into the
   vector and hands the block to that owner. This keeps ownership exact and costs
   a new emitter shape.
2. **A flattened nested-vector mapping.** Map `std::vector<std::vector<T>>` to a
   Go `[][]T` marshalled as a flat array plus offsets per call, with the shim
   building and dismantling the nested vectors. This keeps the Go API the natural
   one and costs two extra wire words and a copy-in/copy-out per nested vector.

The first is the smaller change to the emitter and the larger change to the Go
API; the second is the opposite. The workstream's report records the choice as
open.

## Commands

```console
$ make        # gen, test, vet
$ make lib    # report the OpenCV shared object the binding links against
$ make gen    # cppgo gen: the Go API, the C++ side, the compiled C++ side and coverage.txt
$ make test   # go test -tags cppgo_opencv, then the same under -race
$ make check  # cppgo check: are the committed generated sources (and coverage.txt) current?
$ make coverage  # the same check, run as part of make test
$ make clean
```

Requirements: Go 1.25+, libclang 20+ (generation only), and OpenCV's core,
imgproc and imgcodecs development files. `OPENCV_INCLUDE` and `OPENCV_LIB` are
the two paths a machine can differ in; the defaults are the Debian layout, with
`OPENCV_LIB` pointing at `libopencv_imgcodecs.so` (which names imgproc and core
among its dependencies). On a machine without OpenCV every target prints the
missing path and exits zero, which is what lets
[`scripts/build-examples.sh`](../../scripts/build-examples.sh) run there.

## Loading

The test names the shim it loads, because the loader does not search the current
directory; the shared object beside it names OpenCV's imgcodecs library as its
dependency, and the system loader resolves imgproc and core through imgcodecs:

- `CppgoLibraryPath` is OpenCV's imgcodecs shared object, from `CPPGO_OPENCV_LIB`
  or the Debian default;
- `Open` is called with the absolute path of `libcppgo_opencv.so`.

## Tests

`make test` runs [`opencv_test.go`](opencv_test.go) twice. Every test asserts a
value; the source of each value is named beside it.

| Test | Assertion, and its source |
|---|---|
| `TestVersion` | `GetVersionMajor()` = 4 and `GetVersionMinor()` = 10, the macros of [`core/version.hpp`](https://github.com/opencv/opencv/blob/4.10.0/modules/core/include/opencv2/core/version.hpp) |
| `TestNormTypes`, `TestTermCriteria`, `TestTickMeter`, `TestRuntimeConfiguration` | the enum values, the criteria record, the meter's counters and durations, and the runtime configuration of the library |
| `TestMatConstruction` | a 2×3 `CV_8UC3` matrix reports rows 2, cols 3, type 16, channels 3, total 6 and a non-nil buffer; the default matrix is empty |
| `TestMatPixels`, `TestSetTo` | two BGR pixels round-trip through `data()`; `setTo(7)` fills all nine bytes |
| `TestMatElementAccess` | `at<unsigned char>` reads and writes a byte (`uchar&`), and `at<double>` reads a `CV_64F` matrix at its element width |
| `TestMatCopyAndClone` | `Clone()` is independent (a write does not change the source) and `Copy()` shares the buffer (a write does) |
| `TestMatSetToConvertReshape` | `convertTo(CV_64F, 0.5)` turns 7 into 3.5 in every element; `reshape(2)` turns a 1×4 vector into a 1×2 two-channel matrix |
| `TestMatFromRawData` | a `Mat` built over a Go buffer reads the same bytes and writes back into the buffer: no copy |
| `TestImread` | the committed 4×3 PNG reads as rows 3, cols 4, channels 3, `CV_8UC3`, and all twelve BGR triples are asserted |
| `TestImwriteRoundTrip` | the fixture is written to a temp directory and read back; every pixel survives the PNG round trip |
| `TestImencode` | the same matrix encoded in memory starts with the PNG signature `89 50 4E 47 0D 0A 1A 0A` |
| `TestCvtColorBGR2GRAY`, `TestCvtColorPixelwise` | `COLOR_BGR2GRAY` is 6; pure red is 76 and pure green 150; every pixel of the fixture matches `(B*1868 + G*9617 + R*4899 + 2^13) >> 14`, the fixed-point coefficients of OpenCV 4.10's `color_rgb.simd.hpp` |
| `TestThreshold` | `THRESH_BINARY` at 127.5 turns `{0,127,128,255}` into `{0,0,255,255}` and `THRESH_BINARY_INV` into `{255,255,0,0}`; the returned level is 127, the 8-bit truncation OpenCV applies |
| `TestResize` | a 2×2 matrix resized to 4×4 with `INTER_NEAREST` is each source pixel four times |
| `TestBlur` | `blur` and `GaussianBlur` with a 3×3 kernel keep a constant 100 image at 100 — the `Size` record crossing by value |
| `TestCanny` | a white square on black has edges on its border and none in its flat interior or at the far corner |
| `TestArithmetic` | `add`, `subtract`, `multiply` and `divide` on known bytes, saturated to `CV_8U` (`200+100 = 255`, `0-1 = 0`, …) |
| `TestReductions` | on 1..9: L2 norm `sqrt(285)`, L1 norm 45, mean 5, sum `{45,0,0,0}`, min 1 at (0,0), max 9 at (2,2) |
| `TestContourAreaAndBoundingRect` | a `CV_32SC2` contour of the square (0,0)(4,0)(4,4)(0,4) has area 16 and bounding rectangle (0,0,5,5) |
| `TestGeometryRecords` | `Point::Dot` = 11, `Point::Cross` = 2, `Size::Area` = 20 and `Size::Empty` = false, `Rect::Contains` true inside and false outside — the C++ inline definitions |
| `TestCvtColorError` | `cvtColor` on an empty matrix returns the library's own error |
| `TestCloseLifetime` | a second `Close` returns `cppgo.ErrClosed` and a method on a closed value returns it |

## The committed generated sources

[`zz_cppgo.gen.go`](zz_cppgo.gen.go), [`zz_cppgo_amd64.s`](zz_cppgo_amd64.s),
[`cppgo_shim/`](cppgo_shim/) and [`coverage.txt`](coverage.txt) are committed on
purpose: the example is teaching material, and `make gen` reproduces them byte
for byte. `make check` is the gate: it fails when a committed file differs from
what `make gen` writes, which is also how a declaration that stops binding
becomes a review comment instead of a silent loss.
