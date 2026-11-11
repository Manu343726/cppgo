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

Both take the frame's bytes as a `(pointer, length)` pair. `enroll` returns an
`Enrollment` whose fields are counts, a region, its per-stage microseconds and
the template's bytes (a `std::string`). Nothing on the Go side can see what is
inside those bytes.

Copies, said plainly — and one of them is the price of the log sink:

- **the frame is copied in, once per call.** The package registers a retained
  callback (§ *The log sink*), so every binding enters C++ through the reentrant
  carrier, and a call that may run a Go callback works on its own **pinned copy**
  of every Go value it hands over: the callee reads the same bytes whether or not
  the callback moved the goroutine's stack. Measured on a 16 KB MJPEG frame, the
  copy and the pinning are inside the benchmark's noise — 1.305 ms/frame, and the
  allocations the benchmark reports per trigger call (16.8 KB, 5 allocations)
  are that copy;
- the decoder allocates its own output — the one allocation per frame the
  image-codec API does not let us supply — and everything after it is written
  into buffers the `Trigger` object owns;
- an enrolment copies the template's bytes once on the way out, into the
  `std::string` the binding hands to Go.

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

The sink is an `spdlog::sinks::base_sink` that formats each record and calls the
function pointer the caller stored with `set_log_callback`. There is no buffer
and no pump: a record reaches Go where it is written.

### The retained registration

`set_log_callback` *stores* its argument, which no C++ signature can say, so the
Makefile states it:

```console
$ grep retain -A1 examples/fingerprint/Makefile
            -retain 'fingerprint::set_log_callback'
```

That one word is what makes the generated Go binding return a registration whose
lifetime is the caller's:

```go
func SetLogCallback(fn func(level int32, message string)) (*CallbackRegistration, error)
func (r *CallbackRegistration) Close() error
```

and the CLI uses exactly that, once per invocation:

```go
logs, stop, err := withLogs(verbose, stderr)   // registers, and returns the release
if err != nil {
	return err
}
defer stop()                                    // SetLogCallback(nil), then Close
```

The release does the two steps in the documented order: **clear the C++ side
first** (`SetLogCallback(nil)`, which returns no registration), **then close the
registration**. Delivering after a `Close` without clearing is the released
slot's panic in Go — measured, and asserted by a test — because the C++ side
holds a raw pointer that nothing tells it about.

The records go to `log/slog`: the callback maps spdlog's six levels onto slog's
and writes the message, which is the text spdlog formatted (the pattern is `%v`,
so the level is not written twice). `-v` lowers the handler to show the trace and
debug records the pipeline writes; the summary line beside a run's numbers counts
every record the callback received, whatever the handler printed.

### The threading rule

- the library starts no thread and writes from none: every record is written
  inside a call made from Go, and the callback runs there — on the goroutine
  that made the call, while it is still inside it;
- a worker goroutine's calls deliver on the worker's own stack, to the same
  registration (there is one sink per process; the last registration wins, which
  a test pins);
- the callback must not log: it runs while the C++ sink holds its mutex, and a
  record written from inside the handler would re-enter it and deadlock;
- a library that delivered from a thread of its own could not be bound this way:
  the runtime has no goroutine there to run the callback on.

### What it cost to get here

The first version of this example could not use a retained sink: the generator's
reentrant carrier emitted uncompilable Go for a class with a destructor, for a
record with a `std::string` member, and for a record returned by value. That is
issue #142, fixed in `9aee1cd`; the example's three shapes are what it was
missing, and `internal/gen/testdata/retained/shapes.hpp` is the fixture that now
holds them.

One API consequence stayed: with every binding reentrant, a call's arguments must
fit the register file, and `evaluate_yuyv(const std::uint8_t *, std::size_t,
std::int32_t width, std::int32_t height, const TriggerState &)` needs seven
words. `the callback call needs 7 INTEGER and 0 SSE words, past the register file`.
The geometry crosses as an 8-byte `FrameSize` record instead, which is one word.

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
BenchmarkTrigger-16         	     868	   1305474 ns/op	   16848 B/op	       5 allocs/op
BenchmarkFullPipeline-16    	      30	  40064333 ns/op	   16802 B/op	       7 allocs/op
$ ./build/fingerprint_tests testdata
252 checks, 0 failed
```

The recipe the gate runs is `cppgo-ci:recipe-5fd24d63715d`, which carries Python
3, Conan 2.28.1 and the headless OpenCV modules: `scripts/build-examples.sh`
builds this example there and does not skip it.

## The fixtures, and what the tests assert

`testdata/` is committed and `tools/make_fixtures.cpp` regenerates it byte for
byte: a desk, a hand, one finger, two fingers, a print with deliberate ridge
breaks, a JPEG of the finger frame, and a hand-drawn one-pixel skeleton.

The C++ tests (252 checks) assert values, not the absence of a crash:

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
| the log sink | one record per level with its level and its text, the arguments already formatted, a null pointer stopping delivery, a second registration replacing the first, and the pipeline's own records arriving during the call that wrote them |

The Go tests add the loop: the entering and leaving sequences again, the
accounting (`fired == processed + dropped`), one template per processed frame,
a peak in flight of one, a missing camera skipping cleanly, and the cost ratio.

## What the generator needed

Two things this example asked of `cppgo` were not there when it was written, and
both are in now:

- **a retained callback** (#139): a `set_log_callback` whose pointer the callee
  stores. The Makefile's `-retain` is the declaration, the generated call returns
  a registration, and every binding of the package enters C++ through the
  reentrant carrier, because any call may deliver a record;
- **the reentrant carrier's three missing shapes** (#142): a class with a
  destructor, a record with a `std::string` member, and a record returned by
  value did not compile in a `-retain` package. The example is what needed them —
  the trigger owns its buffers and the enrolment is a record with the template's
  bytes in it.

What remains is a limit, not a gap: `-retain` puts every call on the register
file, so a signature with more than six INTEGER words is refused and the
geometry crosses as a `FrameSize` record.
