# fingerprint

A fingerprint biometrics application: a C++ library that does the image work and
a Go program that orchestrates it. It is the complete-project example that also
has a real subject — segmentation, ridge enhancement, minutiae extraction, an
ANSI/INCITS 378 template and a real-time trigger that decides which frames of a
stream are worth the pipeline.

The split is the point and it is structural, not a matter of taste:

| Side | Owns |
|---|---|
| C++ (`include/`, `src/`) | decoding, colour conversion, segmentation, Gabor enhancement, binarisation, thinning, minutiae, the template's layout, the trigger's prefilter, the logger |
| Go (`cmd/fingerprint`) | the frame source, the loop, the concurrency, file IO, the CLI, the numbers |

No Go file decodes an image, reads a pixel, walks a minutia or parses a
template. The Go side hands over bytes and gets a verdict, a template or a
string back.

## The boundary

Two calls, and they are the whole surface (`include/fingerprint/fingerprint.hpp`):

```cpp
// One frame: is it worth the pipeline?  Cheap, per frame.
TriggerOutcome Trigger::evaluate(const std::uint8_t *encoded, std::size_t length,
                                 const TriggerState &state);

// One frame through everything: decode, segment, enhance, extract, serialise.
Enrollment enroll(const std::uint8_t *encoded, std::size_t length);
```

Both take the frame's bytes as a `(pointer, length)` pair, which is the one
shape the binding passes **without copying**: the Go slice is the C++ pointer,
valid for the call. `enroll` returns an `Enrollment` whose fields are counts, a
region, its per-stage microseconds and the template's bytes (a `std::string`).
Nothing on the Go side can see what is inside those bytes.

Copies per frame, said plainly:

- the trigger: **none** on the way in. The decoder allocates its own output —
  the one allocation per frame the image-codec API does not let us supply — and
  everything after it is written into buffers the `Trigger` object owns;
- an enrolment: one copy of the template's bytes on the way out, into the block
  the generated binding hands to Go. The encoded frame is still borrowed.

The header includes no OpenCV and no spdlog. `include/fingerprint/pipeline.hpp`
is the C++ half — the same stages over `cv::Mat` — and it is what the C++ tests
call.

## The pipeline

| Stage | What it does, and the value it is built on |
|---|---|
| `segment` | the skin mask of a finger in YCrCb, then the largest connected component. The box is Chai & Ngan's (*Face segmentation using skin-color map in videophone applications*, IEEE TCSVT 9(4), 1999): Cr 133..173, Cb 77..127 |
| `normalize` | mean/variance normalisation to the values of Hong, Wan and Jain (*Fingerprint image enhancement: algorithm and performance evaluation*, IEEE TPAMI 20(8), 1998): mean 100, standard deviation 100. Floating point: rounding to eight bits here clips the tails |
| `orientationField` | the doubled-angle trick — `0.5·atan2(2·gx·gy, gx²−gy²)` — averaged over a 16-pixel block, so a ridge and its valley agree |
| `gaborEnhance` | an eight-direction even-symmetric Gabor bank whose wavelength is the measured ridge frequency; each pixel keeps the one kernel the orientation field selects. The kernel is made zero-mean first, or the image's own mean leaks through it and the response stops being a ridge signal |
| `binarize` | Otsu's threshold; the result is 0 and 255 and nothing else |
| `thin` | Zhang-Suen, to a one-pixel skeleton |
| `extractMinutiae` | the Rutovitz crossing number on the skeleton: 1 is a ridge ending, 3 a bifurcation. Angle from the neighbours, quality from the enhanced image, ridge count along the direction across the ridges |
| `ridgeFrequency` | ridge starts per pixel along the axis that crosses the ridges, averaged over both axes and the middle half of the image |
| `toAnsi378` / `fromAnsi378` | the template, below |

## The template: a documented subset of ANSI/INCITS 378

The record is 20 bytes of header and 8 per minutia, big-endian throughout:

| Offset | Size | Field |
|---|---|---|
| 0 | 4 | format identifier `"FMR\0"` |
| 4 | 4 | version `" 20\0"` — the value ANSI/INCITS 378-2004 uses |
| 8 | 4 | record length, the whole record |
| 12 | 2 | image width |
| 14 | 2 | image height |
| 16 | 2 | minutiae count |
| 18 | 2 | ridge frequency, hundredths of a cycle per pixel |
| 20 | 8·n | minutiae, ordered by (y, x) |

and one minutia: type (0 ending, 1 bifurcation), x, y, angle (0..255 = 0..2π),
quality (0..100), ridge count (255 means 255 or more).

**Carried from the standard**: the format identifier and version, the record
length, the image size, the minutiae count, and per minutia its type, position,
direction, quality and ridge count.

**Omitted, and why** (the full list is in `src/ansi378.cpp`): the CBEFF product
identifier, capture-equipment compliance and identifier, the finger position,
view number, impression type and finger quality that make a record a finger
*view*, the image resolution — which is why the ridge frequency here is cycles
per **pixel** and not per millimetre, the one field whose unit differs — the
extended data block (neighbouring ridge counts, cores, deltas), and the
multi-view layout. A record this file writes is therefore not a record a
certified matcher would accept, and it is called a subset wherever it appears.

## The real-time trigger

The trigger runs on every frame of a stream and answers one question cheaply:
should the pipeline see this one? It works at a fixed **160×120**, whatever the
camera's resolution.

Its own cost, by construction:

- one reduced decode (`IMREAD_REDUCED_COLOR_4`) and one resize to the working
  size;
- O(N) passes only: one YCrCb conversion, one threshold into the mask, one
  two-pass connected-components labelling with a union-find, one Sobel pair, one
  accumulation pass for the blob statistics;
- **no** Gabor, no morphology, no thinning, no contour search, no
  floating-point convolution, and nothing over the full frame;
- the buffers are members of the `Trigger` object and are reused. Labels and the
  blob table are grown once and written, not cleared: a megabyte of memset per
  frame is not a hot path.

The decision is temporal, and the rule is in the header beside the constants
that implement it:

- every frame's evidence is an EMA with weight 0.5 — one noisy frame moves it by
  at most half;
- a frame fires when the *smoothed* evidence has been at or above 0.55 for two
  consecutive frames, and each such frame re-arms a hold window of five frames;
- while the hold window is open the stream keeps firing, spending one frame of
  it, so a finger leaving does not cut the stream instantly — and a single
  convincing frame in an empty stream still cannot start it.

The verdict carries the evidence (skin, contrast, ridge sub-scores), one region
per finger seen — up to four, in source coordinates — and the decisive test's
name, so a stream that never fires says why: `NO_SKIN`, `NO_BLOB`, `SHAPE`,
`LOW_CONTRAST`, `NO_RIDGES` or `BELOW_THRESHOLD`.

### What it costs, measured

640×480 MJPEG frame, in the CI image (see § Commands for the exact run):

| | per frame |
|---|---|
| trigger | **1.33 ms**, of which ~1.0 ms is the JPEG decode and ~0.3–0.6 ms the prefilter |
| full pipeline | **39.5 ms** |
| ratio | **~30×** |
| 30 fps budget | 33 ms — the trigger spends ~4% of it; the pipeline spends 120% of it and cannot run at frame rate at all |

The decode is most of the trigger, and it is the one cost a cheaper prefilter
cannot remove: `IMREAD_REDUCED_COLOR_4` still reads the whole JPEG stream. The
fixtures are PNGs, which is why a `watch` run over `testdata/` reports ~14 ms per
frame instead of 1.3 — libpng inflates the full image whatever the reduction
flag says. A camera hands MJPEG, and MJPEG is the case the numbers above are
about.

The trigger's floor is asserted where it cannot be flaky: `TestTriggerIsCheaper-
ThanThePipeline` takes the median of several runs of each and requires the
trigger to be at least ten times cheaper, against a measured 28–33×. The
absolute numbers come from the benchmarks below and not from prose.

## Attaching it to a live stream

`FrameSource` is one interface — `Next() (Frame, error)`, `io.EOF` at the end —
and two implementations:

- **`dir:<path>`** replays a directory's images in name order. It is what CI and
  the tests use, and what a recording is.
- **`camera:<index>`** reads `/dev/video<index>` over V4L2 with no cgo, asking
  for MJPEG first (the format whose decode the trigger pays at a reduced scale)
  and falling back to YUYV, whose conversion to BGR is C++'s work. Go only reads
  the device's buffer; the pixels are C++'s. A camera that is not there is not a
  failure: `watch` says there is nothing to watch and stops.

The trigger algorithm knows neither: it receives a frame.

The loop runs the trigger at the source's pace and hands the frames it selects
to **one** worker through a one-slot hand-off:

```
source ──▶ trigger ──fire──▶ [ one slot ] ──▶ the pipeline ──▶ template
              │                     
              └──gate──▶ counted as gated: the pipeline never ran
```

Single-flight is the policy, and it is counted: a frame that fires while the
worker is busy **replaces** the pending one (the newest frame is the one worth
segmenting) and the replaced frame is counted as dropped. Nothing queues, so the
stream never falls minutes behind the camera. The run reports `frames`, `fired`,
`processed`, `gated`, `dropped`, the peak in flight, and the time each stage
spent **inside C++** beside the time the call took from Go — which is the claim
that the expensive work is C++'s, as a number:

```
frames     7
fired      5
processed  1
gated      2  (the full pipeline never ran)
dropped    3  (fired while the worker was busy)
empty      1  (selected, and the pipeline found no finger)
max in flight 1
trigger    3483 µs/frame in C++, 56 µs/frame in Go
segment    47.89 ms/frame in C++, 257 µs/frame in Go
minutiae   17.0 per processed frame
saved      86% of the frames never reached the pipeline
wall       126.05 ms for 7 frames
```

`empty` is not a failure: the hold window selects a few frames after the finger
goes, and a frame the scene has already left has no finger in it. It is counted
apart from a failure of the call itself.

The Go column is what the boundary and the orchestration cost: the call, the
mutex around the hand-off, the counter. It is three orders of magnitude below
the C++ column, which is the lesson of the example.

## The log sink and the callback

The library logs through **spdlog**, fetched by Conan and pinned:

```console
$ cat conanfile.txt
[requires]
spdlog/1.14.1

[options]
spdlog/*:header_only=True
fmt/*:header_only=True
```

(`conan.lock` pins the recipe revisions; the header-only package has no
compiler-specific binary in it, so the lockfile is portable across machines.)

The sink is an `spdlog::sinks::base_sink` that formats each record, appends it to
a buffer, and calls a registered C function pointer with the record's level. On
the Go side that function is `bindingLogs.record`, and the text is read with
`DrainLog`.

**The threading rule, and why the callback is a parameter rather than a
registration.** Three properties of this build decide the shape, and each was
measured rather than assumed:

1. the Go runtime accepts a callback only inside a call it entered through
   `cgocall`. A binding is routed that way when its signature takes a function
   pointer. A record delivered from a call that does not is a crash —
   `fatal error: morestack on g0`, reproduced from a worker goroutine;
2. the generated binding registers the Go function for the duration of the call
   that hands it over and **releases it when that call returns**, so a sink
   registered once and kept names a slot that is no longer the callback's: the
   next delivery is a jump through `0xfffffffffffffff8`. A registered-and-kept
   sink cannot exist in this build;
3. a binding that takes a callback may take **nothing but scalars** beside it —
   "a pointer into Go memory would move when the callback grows the stack" — so
   the call that delivers records cannot also be the call that carries the
   frame's bytes, and the callback's argument cannot be a string.

Hence the two steps, and only two:

```go
enrollment, err := fingerprint.Enroll(photo)          // buffers its records
fingerprint.DeliverLog(bindingLogs.record)            // delivers them, one call per record
text, err := fingerprint.DrainLog()                   // reads the text
```

`SetLogCallback` as a registration therefore does not exist here, and the
generator's refusal of the shape the tutorial would like is recorded below. The
library starts no thread: every record is written from inside a call Go made, on
the goroutine that made it.

## Commands

```console
$ make            # conan install, cmake, the C++ tests, cppgo gen, go test, vet, benchmarks
$ make conan      # conan install only: the pinned spdlog, into build/conan
$ make lib        # the C++ library, by CMake with Conan's toolchain
$ make fixtures   # regenerate testdata/ from tools/make_fixtures.cpp
$ make cpp-test   # the C++ tests, through CTest
$ make gen        # cppgo gen: the Go API, the assembly, the shim sources
$ make test       # the C++ tests, then go test twice (no cgo, then -race)
$ make bench      # the trigger and the full pipeline, ns per frame
$ make check      # cppgo check: are the committed generated sources current?
$ make run        # build and start the program
$ make clean
```

Requirements: Go 1.25+, CMake 3.16+, Conan 2 (the profile is detected on first
use), libclang 20+ for generation, and OpenCV 4.10's core, imgproc and imgcodecs
development files. `OPENCV_INCLUDE` and `OPENCV_LIB_DIR` are the two paths a
machine can differ in.

The measured numbers above come from:

```console
$ make bench
BenchmarkTrigger-16         	    1760	   1326790 ns/op	     208 B/op	       1 allocs/op
BenchmarkFullPipeline-16    	      60	  39512113 ns/op	     210 B/op	       3 allocs/op
$ ./build/fingerprint_tests testdata
243 checks, 0 failed
```

The CI image carries no Python and no Conan until #135 publishes the recipe that
adds them; until then every target prints why it is skipping and exits zero, so
`scripts/build-examples.sh` stays green, and the example is verified on a host
with Conan and in a local image built from the pinned recipe plus Python 3 and
Conan 2.28.1.

## The fixtures, and what the tests assert

`testdata/` is committed and `tools/make_fixtures.cpp` regenerates it byte for
byte: a desk, a hand, one finger, two fingers, a print with deliberate ridge
breaks, a JPEG of the finger frame, and a hand-drawn one-pixel skeleton.

The C++ tests (243 checks) assert values, not the absence of a crash:

| Test | Assertion |
|---|---|
| segmentation | the mask is the photo's size, `CV_8UC1`, only 0 and 255; the desk frame has no region at all |
| normalisation | mean 100 and deviation 100 to 1e-3; a flat image stays flat |
| orientation | the print's ridges run along y, so the central field's mean is 0 mod π within 0.15 |
| ridge frequency | the fixture's period is 10 px, so 0.1 cycles/pixel within 0.02 |
| binarisation, thinning | only 0/255, then only 0/1; over 90% of the skeleton's pixels are one pixel wide; thinning a drawn skeleton changes nothing |
| **minutiae** | the drawn skeleton's four minutiae at exactly (60,60) ending, (140,60) bifurcation, (200,60) ending, (140,160) ending — by position, type and order |
| **the template** | the exact 44 bytes of a known three-minutia record; the fields that survive the round trip; a byte-for-byte re-encode; four refusals; the canonical order |
| the trigger | which of six deterministic sequences fires, at which index: an empty stream never, a hand never (and says `LOW_CONTRAST`), a finger entering at 5, a steady finger from 2, a finger leaving through 10 and not 11, one convincing frame never, two fingers in one region-pair |
| the log sink | one record per level with its level and count, the multi-argument text, a call with no callback delivering nothing, and the pipeline's own records delivered afterwards |

The Go tests add the loop: the entering and leaving sequences again, the
accounting (`fired == processed + dropped`), one template per processed frame,
a peak in flight of one, a missing camera skipping cleanly, and the cost ratio.

## What the generator refused, and the shape it forced

Three declarations the application wanted are not bindable in this build. Each
is reported here with the generator's own message, and none is worked around by
hand-writing a binding:

```
cppgo: warning: fingerprint::set_log_callback(void (*)(std::int32_t, const char *)) noexcept is not bound:
  parameter fn: "char *" is not a scalar or a known record; use a scalar element type at the C++ boundary

cppgo: warning: fingerprint::enroll(const std::vector<std::uint8_t> &,
  void (*)(std::int32_t, std::int32_t)) is not bound:
  the parameter "encoded" of a callback binding is a []uint8;
  pass a scalar: a pointer into Go memory would move when the callback grows the stack
```

- **a callback argument cannot be a C string**, so `SetLogCallback(func(level
  int32, message string))` — the shape the tutorial wants — cannot be generated.
  The record's text travels through `DrainLog` and the callback carries its
  level;
- **a callback binding may take nothing but scalars**, so the frame's bytes and
  the record callback cannot be parameters of one call. Delivering is its own
  call (`DeliverLog`), which is why the pipeline's records are buffered by
  `enroll` and delivered after it;
- **a registered-and-kept callback does not exist**: the generated binding
  releases the Go function when the registering call returns, and a delivery
  afterwards is a call through a released address (measured: `morestack on g0`
  from a worker goroutine). The callback is therefore a parameter of the call
  that fires it.

A `using LogCallback = void (*)(...)` alias is refused too — `"LogCallback" is a
void pointer; expose a typed pointer at the C++ boundary` — so the function
pointer is spelled out at each use.
