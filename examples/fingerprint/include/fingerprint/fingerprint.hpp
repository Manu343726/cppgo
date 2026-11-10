#pragma once

// fingerprint — the binding surface of the example: everything the Go program
// can call, and nothing else.
//
// This header is the whole of the boundary. It includes no OpenCV, no spdlog
// and no other third-party header: the Go side hands the library the bytes of
// an encoded frame and gets a verdict, a template or a string back, and the
// image work — decoding, colour conversion, the cheap prefilter, segmentation,
// enhancement, thinning, minutiae extraction and the template's own layout —
// happens on the C++ side of the call. `pipeline.hpp` is the C++ half, over
// `cv::Mat`; this one is the crossing.
//
// One call per frame. `Trigger::evaluate` decodes the frame, decides, and
// returns the next state; `enroll` runs the full pipeline and returns the
// template's bytes. Neither hands Go a pixel, a matrix, a minutia or a field of
// the template.
//
// Two things this header's declarations rely on, said here because they are
// properties of the whole example and not of one function:
//
//   - the frame bytes are *borrowed* for the duration of the call. The
//     `(const std::uint8_t *, std::size_t)` pair is the one shape the binding
//     passes without copying, and the library copies nothing out of it except
//     the decoder's own output;
//   - the log callback is called only while C++ is inside a call made from Go.
//     The library starts no thread, and logging from a C++ thread the Go
//     runtime has never seen would enter the callback on a thread that cannot
//     receive it.

#include <cstddef>
#include <cstdint>
#include <string>
#include <vector>

namespace fingerprint {

// --- the trigger ------------------------------------------------------------

// Which test decided the verdict. A frame that fires says NONE (the evidence
// itself) or HOLD (the hold window is still open); a frame that does not fire
// names the first test that failed, so a stream that never fires says why.
enum TriggerReason {
    NONE = 0,              // fired: every test of this frame passed
    HOLD = 1,              // fired from the hold window: this frame had no evidence of its own
    DECODE_FAILED = 2,     // the bytes are not a frame the image decoder accepts
    NO_SKIN = 3,           // no skin-like pixel in the whole frame
    NO_BLOB = 4,           // skin pixels, but no blob large enough to be a finger
    SHAPE = 5,             // the blob's proportions are not a finger's
    LOW_CONTRAST = 6,      // the blob is too flat to carry ridges
    NO_RIDGES = 7,         // contrast, but no ridge-scale gradient
    BELOW_THRESHOLD = 8,   // all tests passed and the evidence is still under the fire threshold
};

// The trigger's memory of the stream. The caller owns one value per stream and
// passes the previous one in on every frame; a zero value is the start of a
// stream, and `initial_trigger_state()` states that explicitly.
//
// The rule the counters implement, with the constants this example uses:
//
//   * every frame's evidence `score` is folded into `score_ema` with weight
//     kTriggerEmaWeight, and the decisions below read the smoothed value — one
//     noisy frame moves it by at most half;
//   * `above` counts consecutive frames at or above kTriggerHigh (0.55),
//     `below` consecutive frames below kTriggerLow (0.30);
//   * a frame fires when `above` reaches kTriggerConsecutive (2), and each
//     frame that fires this way re-arms `hold` to kTriggerHoldFrames (5);
//   * a frame that does not reach kTriggerHigh fires anyway while `hold` is
//     non-zero, and spends one frame of it — so a finger leaving the scene
//     keeps the stream alive for five frames and a single noisy frame still
//     cannot start it, because a start needs two.
struct TriggerState {
    std::int32_t frame_index;   // frames evaluated before this one
    std::int32_t above;         // consecutive frames at or above the high threshold
    std::int32_t below;         // consecutive frames below the low threshold
    std::int32_t hold;          // frames of hold left
    float score_ema;            // smoothed evidence
    float skin_ema;             // smoothed skin sub-score
    float contrast_ema;         // smoothed contrast sub-score
    float ridge_ema;            // smoothed ridge sub-score
    std::int32_t roi_count;     // regions of the last frame that fired
    std::int32_t roi_x[4];      // ...and their boxes, in source pixels
    std::int32_t roi_y[4];
    std::int32_t roi_w[4];
    std::int32_t roi_h[4];
};

inline constexpr float kTriggerEmaWeight = 0.5f;
inline constexpr float kTriggerHigh = 0.55f;
inline constexpr float kTriggerLow = 0.30f;
inline constexpr std::int32_t kTriggerConsecutive = 2;
inline constexpr std::int32_t kTriggerHoldFrames = 5;
inline constexpr std::int32_t kTriggerWidth = 160;
inline constexpr std::int32_t kTriggerHeight = 120;

// The state of a stream that has not started.
TriggerState initial_trigger_state() noexcept;

// What the trigger saw in one frame.
struct TriggerVerdict {
    bool fire;                  // whether the frame is worth the full pipeline
    TriggerReason reason;       // the decisive test
    float score;                // combined evidence, 0..1
    float skin;                 // skin sub-score, 0..1
    float contrast;             // contrast sub-score, 0..1
    float ridge;                // ridge sub-score, 0..1
    std::int32_t roi_count;     // one box per finger seen, up to 4
    std::int32_t roi_x[4];
    std::int32_t roi_y[4];
    std::int32_t roi_w[4];
    std::int32_t roi_h[4];
};

// One frame's crossing: the verdict, the state to pass in next, and where the
// time went inside C++ (the Go side compares it with the call's own duration,
// which is how the report separates the two).
struct TriggerOutcome {
    TriggerState state;
    TriggerVerdict verdict;
    double decode_micros;       // inside imdecode and the resize to the working size
    double prefilter_micros;    // the rest: mask, components, statistics
};

// The real-time trigger: a cheap, per-frame verdict over the frames of a
// stream, with the expensive pipeline left to `enroll`.
//
// The object owns the working buffers of the prefilter and reuses them on every
// frame (see the README's cost section); the *state* is the caller's, which is
// what lets one Trigger serve several streams. The decoder allocates its own
// output on each call — that is the one per-frame allocation this side cannot
// remove — and everything after the decode writes into a buffer that already
// exists.
//
// The prefilter is O(N) on a kTriggerWidth x kTriggerHeight image: one reduced
// decode, one resize, one YCrCb conversion, one threshold into a mask, one
// two-pass connected-components labelling, one Sobel pass and one pass per blob
// for its statistics. No Gabor, no morphology, no thinning, no contour search,
// and no floating-point convolution over the full frame.
class Trigger {
public:
    Trigger();
    ~Trigger();

    // The trigger owns the prefilter's buffers; a copy would free them twice.
    // The deletion is what says so, and the generator reads it.
    Trigger(const Trigger &) = delete;
    Trigger &operator=(const Trigger &) = delete;

    // The working size of the prefilter, in pixels. Fixed, whatever the frame's
    // own size: the resize is what makes the cost independent of the camera.
    std::int32_t width() const noexcept;
    std::int32_t height() const noexcept;

    // One frame of an encoded stream (JPEG, PNG or any format the decoder
    // reads). Decoded at a reduced scale, so the decode itself is cheaper than
    // the full one `enroll` pays.
    TriggerOutcome evaluate(const std::uint8_t *encoded, std::size_t length, const TriggerState &state);

    // One frame read off a camera in raw YUYV (the format a V4L2 device hands
    // out when it has no MJPEG): the YUV-to-BGR conversion is this library's
    // work, not the frame source's, and `state` carries across the two forms so
    // a stream may switch between them.
    TriggerOutcome evaluate_yuyv(const std::uint8_t *frame, std::size_t length, std::int32_t width, std::int32_t height,
                                 const TriggerState &state);

private:
    struct Impl;
    Impl *impl_;
};

// --- the full pipeline ------------------------------------------------------

// What one enrolment produced, and where its time went. The template's bytes
// are the ANSI/INCITS 378 subset of `pipeline.hpp`/`ansi378.cpp`; the Go side
// writes them to a file and never reads a field of them.
struct Enrollment {
    std::string template_bytes;
    std::int32_t minutiae;
    std::int32_t image_width;
    std::int32_t image_height;
    std::int32_t roi_x;
    std::int32_t roi_y;
    std::int32_t roi_width;
    std::int32_t roi_height;
    double ridge_frequency;
    double decode_micros;
    double segment_micros;
    double enhance_micros;
    double extract_micros;
    double encode_micros;
    double total_micros;
};

// The whole pipeline on one encoded frame: decode, segment, normalise,
// enhance, binarise, thin, extract, serialise. Throws std::invalid_argument
// when the frame does not decode or holds no finger — the Go side gets an
// error and the reason is in the log.
Enrollment enroll(const std::uint8_t *encoded, std::size_t length);

// A textual listing of a template: the header's fields, then one line per
// minutia. The format is the C++ side's, so the CLI prints it unchanged.
std::string dump_ansi378(const std::uint8_t *bytes, std::size_t length);

// --- the log sink -----------------------------------------------------------

// Where the library's spdlog records go: a sink that formats the record, adds
// it to the buffer `drain_log()` reads, and calls `fn` with the record's spdlog
// level (trace 0, debug 1, info 2, warn 3, err 4, critical 5) and the number of
// records the buffer holds.
//
// `fn` is called on the thread that made the outermost call — the library
// creates no thread — so the callback is always a re-entry the Go runtime
// accepts. Passing a null pointer, or calling clear_log_callback(), stops the
// delivery; the buffer keeps filling either way.
void set_log_callback(void (*fn)(std::int32_t level, std::int32_t pending)) noexcept;
void clear_log_callback() noexcept;

// The records buffered since the last drain, one per line, and clears the
// buffer. This is how a Go program reads the text: the callback reports that a
// record arrived, and this reads it, because the callback's arguments are
// scalars (see the README for why).
std::string drain_log();

// One record per spdlog level plus one record with several arguments, so a test
// can assert the level, the text and the count against a single call.
void emit_log_samples();

// The library's own version line, with the OpenCV build it links against.
std::string version();

} // namespace fingerprint
