// The real-time trigger: the cheap per-frame decision that guards the full
// pipeline.
//
// The contract this file implements is in fingerprint.hpp (the rule, the
// constants, the state), and it is cheap by construction:
//
//   * the frame is decoded at a reduced scale and resized to a fixed
//     160x120 working image, so neither the decode nor anything after it grows
//     with the camera's resolution;
//   * every pass over that image is O(N): one YCrCb conversion, one threshold
//     into a mask, one two-pass connected-components labelling with a
//     union-find, one Sobel pair, and one accumulation pass for the blob
//     statistics;
//   * there is no morphology, no Gabor bank, no thinning, no contour search and
//     no floating-point convolution — those are the pipeline's work, and the
//     whole point is that they do not run on frames the trigger rejects;
//   * the buffers are members and are reused: the only allocation per frame is
//     the decoder's own output, which the image-codec API does not let us
//     supply.
//
// The trigger logs, but never delivers: its entry point takes no callback
// parameter, so it is not a reentrant call, and the sink therefore buffers what
// it writes and leaves it for drain_log(). A record delivered from here would
// be a call into Go on a thread the runtime cannot account for.

#include "fingerprint/fingerprint.hpp"

#include "fingerprint/pipeline.hpp"
#include "log.hpp"

#include <algorithm>
#include <chrono>
#include <cmath>
#include <vector>

#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>

namespace fingerprint {
namespace {

using Clock = std::chrono::steady_clock;

double micros(Clock::time_point from, Clock::time_point to) {
    return std::chrono::duration<double, std::micro>(to - from).count();
}

// The skin test of segment(), as integers, for the one-pass mask below. The
// values are the YCrCb box of Chai & Ngan (see pipeline.hpp).
constexpr int kCrMin = 133;
constexpr int kCrMax = 173;
constexpr int kCbMin = 77;
constexpr int kCbMax = 127;

// A blob smaller than this cannot be a finger at 160x120: it is 1.25% of the
// working frame.
constexpr int kMinBlobArea = 240;

// A finger's box is elongated; a face, a door or a wall is not. The bounds are
// wide on purpose — a foreshortened finger seen end-on is still a finger.
constexpr double kMinAspect = 0.25;
constexpr double kMaxAspect = 6.0;

// What each sub-score saturates at. A finger's blob covers at least a twentieth
// of the working frame; ridges carry a local standard deviation of about 25
// grey levels and a Sobel response of about 100 counts.
constexpr double kSkinSaturation = 0.05;
constexpr double kContrastSaturation = 25.0;
constexpr double kRidgeSaturation = 100.0;

// A sub-score under this floor is the decisive failure, and the verdict names
// it rather than reporting a low total.
constexpr double kSubScoreFloor = 0.20;

struct Blob {
    int area;
    int x0;
    int y0;
    int x1;
    int y1;
    double sum;
    double sumSquares;
    double sumGradient;
};

std::uint32_t bigEndian32(const std::uint8_t *bytes) {
    return (static_cast<std::uint32_t>(bytes[0]) << 24) | (static_cast<std::uint32_t>(bytes[1]) << 16) |
           (static_cast<std::uint32_t>(bytes[2]) << 8) | static_cast<std::uint32_t>(bytes[3]);
}

// The frame's own size, read from the encoded header.
//
// The reduced decode returns a smaller image than the frame — that is the point
// of the flag — so the decoded Mat cannot say where a region lies in the source.
// The header can: a PNG's IHDR carries the size at a fixed offset, and a JPEG's
// SOF marker carries it wherever the marker happens to be. A format neither
// recognises falls back to the decoded size, which is only wrong for a codec
// that honoured the flag without being one of these two.
bool encodedSize(const std::uint8_t *bytes, std::size_t length, int &width, int &height) {
    if (bytes == nullptr || length < 8) {
        return false;
    }
    // PNG: the eight-byte signature, then the IHDR chunk's width and height.
    static const std::uint8_t png[8] = {0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A};
    if (length >= 24 && std::equal(std::begin(png), std::end(png), bytes) && bytes[12] == 'I' && bytes[13] == 'H' &&
        bytes[14] == 'D' && bytes[15] == 'R') {
        width = static_cast<int>(bigEndian32(bytes + 16));
        height = static_cast<int>(bigEndian32(bytes + 20));
        return width > 0 && height > 0;
    }
    // JPEG: walk the markers until a start-of-frame one, whose length is
    // followed by the precision, the height and the width.
    if (bytes[0] == 0xFF && bytes[1] == 0xD8) {
        std::size_t at = 2;
        while (at + 4 <= length) {
            if (bytes[at] != 0xFF) {
                ++at;
                continue;
            }
            const std::uint8_t marker = bytes[at + 1];
            if (marker == 0xFF) {
                ++at;
                continue;
            }
            if (marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7)) {
                at += 2;
                continue;
            }
            const std::size_t size = (static_cast<std::size_t>(bytes[at + 2]) << 8) | bytes[at + 3];
            const bool frameHeader = marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC;
            if (frameHeader && at + 9 <= length) {
                height = (static_cast<int>(bytes[at + 5]) << 8) | bytes[at + 6];
                width = (static_cast<int>(bytes[at + 7]) << 8) | bytes[at + 8];
                return width > 0 && height > 0;
            }
            if (size < 2) {
                return false;
            }
            at += 2 + size;
        }
    }
    return false;
}

} // namespace

struct Trigger::Impl {
    cv::Mat small;      // 160x120 BGR, the working image
    cv::Mat bgr;        // 160x120 BGR, from a camera's YUYV frame
    cv::Mat ycc;        // 160x120 YCrCb
    cv::Mat gray;       // 160x120 grey
    cv::Mat mask;       // 160x120, 255 where the pixel is skin
    cv::Mat gx;         // CV_16S
    cv::Mat gy;         // CV_16S
    std::vector<std::int32_t> labels;
    std::vector<std::int32_t> parent;
    std::vector<Blob> blobs;
    // The labels the previous frame used, so this one resets those and nothing
    // else: sizing a blob table by the pixel count and clearing it every frame
    // is a megabyte of memset per frame, which is not a hot path.
    std::vector<std::int32_t> touched;

    // The decision over the working image, and where the time inside C++ went.
    TriggerOutcome decide(const TriggerState &previous, double scaleX, double scaleY, double decodeMicros);

    // The union-find root of a provisional label, with path halving.
    std::int32_t find(std::int32_t label) {
        while (parent[static_cast<std::size_t>(label)] != label) {
            parent[static_cast<std::size_t>(label)] = parent[static_cast<std::size_t>(parent[static_cast<std::size_t>(label)])];
            label = parent[static_cast<std::size_t>(label)];
        }
        return label;
    }

    void join(std::int32_t a, std::int32_t b) {
        a = find(a);
        b = find(b);
        if (a == b) {
            return;
        }
        if (a < b) {
            parent[static_cast<std::size_t>(b)] = a;
        } else {
            parent[static_cast<std::size_t>(a)] = b;
        }
    }
};

Trigger::Trigger() : impl_(new Impl()) {}

Trigger::~Trigger() { delete impl_; }

std::int32_t Trigger::width() const noexcept { return kTriggerWidth; }

std::int32_t Trigger::height() const noexcept { return kTriggerHeight; }

// The temporal rule, applied to whatever the frame's evidence was: the EMA
// update, the two consecutive counters, and the hold window. It is the same
// code for a frame that decoded and one that did not, so a broken frame spends
// the hold window instead of cutting the stream.
void applyTemporal(TriggerVerdict &verdict, TriggerState &state, const TriggerState &previous) {
    const float weight = kTriggerEmaWeight;
    state.score_ema = weight * verdict.score + (1.0f - weight) * previous.score_ema;
    state.skin_ema = weight * verdict.skin + (1.0f - weight) * previous.skin_ema;
    state.contrast_ema = weight * verdict.contrast + (1.0f - weight) * previous.contrast_ema;
    state.ridge_ema = weight * verdict.ridge + (1.0f - weight) * previous.ridge_ema;

    if (state.score_ema >= kTriggerHigh) {
        state.above = previous.above + 1;
        state.below = 0;
        state.hold = kTriggerHoldFrames;
        if (state.above >= kTriggerConsecutive) {
            verdict.fire = true;
            verdict.reason = NONE;
            state.roi_count = verdict.roi_count;
            for (int i = 0; i < 4; ++i) {
                state.roi_x[i] = verdict.roi_x[i];
                state.roi_y[i] = verdict.roi_y[i];
                state.roi_w[i] = verdict.roi_w[i];
                state.roi_h[i] = verdict.roi_h[i];
            }
            return;
        }
    } else {
        state.above = 0;
        state.below = state.score_ema < kTriggerLow ? previous.below + 1 : 0;
    }
    if (previous.hold > 0) {
        state.hold = previous.hold - 1;
        verdict.fire = true;
        verdict.reason = HOLD;
        // The hold reports where the fingers were: this frame had none.
        verdict.roi_count = previous.roi_count;
        for (int i = 0; i < 4; ++i) {
            verdict.roi_x[i] = previous.roi_x[i];
            verdict.roi_y[i] = previous.roi_y[i];
            verdict.roi_w[i] = previous.roi_w[i];
            verdict.roi_h[i] = previous.roi_h[i];
        }
        return;
    }
    verdict.fire = false;
    state.hold = 0;
}

// The decision itself, over the working image. `scaleX`/`scaleY` map a working
// pixel back into the source frame: src = work / scale.
TriggerOutcome Trigger::Impl::decide(const TriggerState &previous, double scaleX, double scaleY, double decodeMicros) {
    Impl &impl = *this;
    const Clock::time_point afterDecode = Clock::now();
    const int width = impl.small.cols;
    const int height = impl.small.rows;
    const int pixels = width * height;

    cv::cvtColor(impl.small, impl.ycc, cv::COLOR_BGR2YCrCb);
    cv::cvtColor(impl.small, impl.gray, cv::COLOR_BGR2GRAY);
    cv::Sobel(impl.gray, impl.gx, CV_16S, 1, 0, 3);
    cv::Sobel(impl.gray, impl.gy, CV_16S, 0, 1, 3);

    // One pass, one threshold: the mask the components are labelled from.
    impl.mask.create(height, width, CV_8U);
    int skinPixels = 0;
    for (int y = 0; y < height; ++y) {
        const unsigned char *row = impl.ycc.ptr<unsigned char>(y);
        unsigned char *out = impl.mask.ptr<unsigned char>(y);
        for (int x = 0; x < width; ++x) {
            const int cr = row[x * 3 + 1];
            const int cb = row[x * 3 + 2];
            const bool skin = cr >= kCrMin && cr <= kCrMax && cb >= kCbMin && cb <= kCbMax;
            out[x] = skin ? 255 : 0;
            skinPixels += skin ? 1 : 0;
        }
    }

    // Two-pass connected components with a union-find: 8-connected, so a ridge
    // pattern with a one-pixel valley does not split the finger in two.
    //
    // The buffers are grown once and then written, never cleared: every label
    // is assigned below (0 for a background pixel), and a union-find parent is
    // set when its label is created.
    if (impl.labels.size() != static_cast<std::size_t>(pixels)) {
        impl.labels.resize(static_cast<std::size_t>(pixels));
    }
    if (impl.parent.size() < static_cast<std::size_t>(pixels) + 2) {
        impl.parent.resize(static_cast<std::size_t>(pixels) + 2);
    }
    std::int32_t next = 1;
    for (int y = 0; y < height; ++y) {
        const unsigned char *maskRow = impl.mask.ptr<unsigned char>(y);
        std::int32_t *labelRow = impl.labels.data() + static_cast<std::size_t>(y) * static_cast<std::size_t>(width);
        for (int x = 0; x < width; ++x) {
            if (!maskRow[x]) {
                labelRow[x] = 0;
                continue;
            }
            std::int32_t label = 0;
            if (x > 0 && labelRow[x - 1]) {
                label = impl.find(labelRow[x - 1]);
            }
            if (y > 0) {
                const std::int32_t *up = impl.labels.data() + static_cast<std::size_t>(y - 1) * static_cast<std::size_t>(width);
                if (up[x] != 0) {
                    if (label == 0) {
                        label = impl.find(up[x]);
                    } else {
                        impl.join(label, up[x]);
                    }
                }
                if (x > 0 && up[x - 1] != 0) {
                    if (label == 0) {
                        label = impl.find(up[x - 1]);
                    } else {
                        impl.join(label, up[x - 1]);
                    }
                }
                if (x + 1 < width && up[x + 1] != 0) {
                    if (label == 0) {
                        label = impl.find(up[x + 1]);
                    } else {
                        impl.join(label, up[x + 1]);
                    }
                }
            }
            if (label == 0) {
                label = next++;
                impl.parent[static_cast<std::size_t>(label)] = label;
            }
            labelRow[x] = label;
        }
    }

    // The blob table is grown once and reset only where the last frame wrote:
    // an area of zero is what "untouched" means below.
    if (impl.blobs.size() < static_cast<std::size_t>(next)) {
        impl.blobs.resize(static_cast<std::size_t>(next));
    }
    for (const std::int32_t label : impl.touched) {
        impl.blobs[static_cast<std::size_t>(label)] = Blob{0, 0, 0, 0, 0, 0.0, 0.0, 0.0};
    }
    impl.touched.clear();
    for (int y = 0; y < height; ++y) {
        const unsigned char *maskRow = impl.mask.ptr<unsigned char>(y);
        const unsigned char *grayRow = impl.gray.ptr<unsigned char>(y);
        const short *gxRow = impl.gx.ptr<short>(y);
        const short *gyRow = impl.gy.ptr<short>(y);
        std::int32_t *labelRow = impl.labels.data() + static_cast<std::size_t>(y) * static_cast<std::size_t>(width);
        for (int x = 0; x < width; ++x) {
            if (!maskRow[x]) {
                continue;
            }
            const std::int32_t label = impl.find(labelRow[x]);
            Blob &blob = impl.blobs[static_cast<std::size_t>(label)];
            if (blob.area == 0) {
                blob.x0 = x;
                blob.x1 = x;
                blob.y0 = y;
                blob.y1 = y;
                impl.touched.push_back(label);
            } else {
                blob.x0 = std::min(blob.x0, x);
                blob.x1 = std::max(blob.x1, x);
                blob.y0 = std::min(blob.y0, y);
                blob.y1 = std::max(blob.y1, y);
            }
            ++blob.area;
            const double value = grayRow[x];
            blob.sum += value;
            blob.sumSquares += value * value;
            blob.sumGradient += std::abs(static_cast<int>(gxRow[x])) + std::abs(static_cast<int>(gyRow[x]));
        }
    }

    // The fingers: every blob that could be one, largest first, and at most
    // kMaxFingers of them.
    struct Candidate {
        const Blob *blob;
        double aspect;
        int area;
    };
    std::vector<Candidate> candidates;
    candidates.reserve(impl.blobs.size());
    int biggestArea = 0;
    for (std::size_t i = 1; i < impl.blobs.size(); ++i) {
        const Blob &blob = impl.blobs[i];
        if (blob.area < kMinBlobArea) {
            continue;
        }
        biggestArea = std::max(biggestArea, blob.area);
        const double boxWidth = blob.x1 - blob.x0 + 1;
        const double boxHeight = blob.y1 - blob.y0 + 1;
        candidates.push_back(Candidate{&blob, boxWidth / boxHeight, blob.area});
    }
    std::sort(candidates.begin(), candidates.end(), [](const Candidate &a, const Candidate &b) {
        return a.area > b.area;
    });

    TriggerOutcome out{};
    out.state = previous;
    out.state.frame_index = previous.frame_index + 1;
    out.decode_micros = decodeMicros;

    TriggerVerdict &verdict = out.verdict;
    verdict.fire = false;
    verdict.reason = NONE;
    verdict.roi_count = 0;

    if (skinPixels == 0) {
        verdict.reason = NO_SKIN;
    } else if (candidates.empty()) {
        verdict.reason = NO_BLOB;
    } else {
        std::vector<const Blob *> fingers;
        for (const Candidate &candidate : candidates) {
            if (candidate.aspect < kMinAspect || candidate.aspect > kMaxAspect) {
                continue;
            }
            fingers.push_back(candidate.blob);
        }
        if (fingers.empty()) {
            verdict.reason = SHAPE;
        } else {
            // The frame's evidence is the largest finger's; the regions are
            // then reported left to right, so a stream's boxes do not swap
            // places when two blobs are one pixel apart in area.
            const Blob &best = *fingers.front();
            std::sort(fingers.begin(), fingers.end(), [](const Blob *a, const Blob *b) {
                return a->x0 < b->x0 || (a->x0 == b->x0 && a->y0 < b->y0);
            });
            const double area = static_cast<double>(best.area);
            const double mean = best.sum / area;
            const double variance = std::max(0.0, best.sumSquares / area - mean * mean);
            const double spread = std::sqrt(variance);
            const double gradient = best.sumGradient / area;

            const double skinScore = std::min(1.0, area / (kSkinSaturation * pixels));
            const double contrastScore = std::min(1.0, spread / kContrastSaturation);
            const double ridgeScore = std::min(1.0, gradient / kRidgeSaturation);
            const double score = 0.35 * skinScore + 0.30 * contrastScore + 0.35 * ridgeScore;

            verdict.skin = static_cast<float>(skinScore);
            verdict.contrast = static_cast<float>(contrastScore);
            verdict.ridge = static_cast<float>(ridgeScore);
            verdict.score = static_cast<float>(score);

            // The regions, in source coordinates, of every finger this frame
            // shows.
            verdict.roi_count = static_cast<std::int32_t>(std::min<std::size_t>(fingers.size(), 4));
            for (std::int32_t i = 0; i < verdict.roi_count; ++i) {
                const Blob &blob = *fingers[static_cast<std::size_t>(i)];
                verdict.roi_x[i] = static_cast<std::int32_t>(std::lround(blob.x0 / scaleX));
                verdict.roi_y[i] = static_cast<std::int32_t>(std::lround(blob.y0 / scaleY));
                verdict.roi_w[i] = static_cast<std::int32_t>(std::lround((blob.x1 - blob.x0 + 1) / scaleX));
                verdict.roi_h[i] = static_cast<std::int32_t>(std::lround((blob.y1 - blob.y0 + 1) / scaleY));
            }

            if (contrastScore < kSubScoreFloor) {
                verdict.reason = LOW_CONTRAST;
            } else if (ridgeScore < kSubScoreFloor) {
                verdict.reason = NO_RIDGES;
            } else {
                verdict.reason = BELOW_THRESHOLD;
            }
        }
    }

    applyTemporal(verdict, out.state, previous);

    out.prefilter_micros = micros(afterDecode, Clock::now());
    return out;
}

TriggerState initial_trigger_state() noexcept { return TriggerState{}; }

TriggerOutcome Trigger::evaluate(const std::uint8_t *encoded, std::size_t length, const TriggerState &state) {
    const Clock::time_point begin = Clock::now();
    if (encoded == nullptr || length == 0) {
        TriggerOutcome out{};
        out.state = state;
        out.state.frame_index = state.frame_index + 1;
        out.verdict.reason = DECODE_FAILED;
        out.prefilter_micros = micros(begin, Clock::now());
        detail::logger()->warn("trigger: an empty frame was handed in");
        applyTemporal(out.verdict, out.state, state);
        return out;
    }

    // The frame's bytes are borrowed, not copied: the Mat header only names
    // them, and the decoder reads them during this call.
    const cv::Mat encodedView(1, static_cast<int>(length), CV_8U, const_cast<std::uint8_t *>(encoded));
    const cv::Mat decoded = cv::imdecode(encodedView, cv::IMREAD_REDUCED_COLOR_4);
    const Clock::time_point afterDecode = Clock::now();
    if (decoded.empty()) {
        TriggerOutcome out{};
        out.state = state;
        out.state.frame_index = state.frame_index + 1;
        out.verdict.reason = DECODE_FAILED;
        out.decode_micros = micros(begin, afterDecode);
        out.prefilter_micros = micros(afterDecode, Clock::now());
        detail::logger()->warn("trigger: the frame did not decode ({} bytes)", length);
        applyTemporal(out.verdict, out.state, state);
        return out;
    }

    // The working image is a fixed size whatever the camera's, and the resize
    // writes into the buffer the previous frame left behind.
    cv::resize(decoded, impl_->small, cv::Size(kTriggerWidth, kTriggerHeight), 0.0, 0.0, cv::INTER_AREA);

    // A reduced decode returns a smaller image than the frame, so the frame's
    // own size comes from its header; the decoded size is the fallback.
    int sourceWidth = decoded.cols;
    int sourceHeight = decoded.rows;
    int headerWidth = 0;
    int headerHeight = 0;
    if (encodedSize(encoded, length, headerWidth, headerHeight)) {
        sourceWidth = headerWidth;
        sourceHeight = headerHeight;
    }
    return impl_->decide(state, static_cast<double>(impl_->small.cols) / sourceWidth,
                         static_cast<double>(impl_->small.rows) / sourceHeight, micros(begin, afterDecode));
}

TriggerOutcome Trigger::evaluate_yuyv(const std::uint8_t *frame, std::size_t length, std::int32_t width,
                                      std::int32_t height, const TriggerState &state) {
    const Clock::time_point begin = Clock::now();
    if (frame == nullptr || width <= 0 || height <= 0 ||
        length < static_cast<std::size_t>(width) * static_cast<std::size_t>(height) * 2) {
        TriggerOutcome out{};
        out.state = state;
        out.state.frame_index = state.frame_index + 1;
        out.verdict.reason = DECODE_FAILED;
        out.prefilter_micros = micros(begin, Clock::now());
        detail::logger()->warn("trigger: a YUYV frame of {}x{} bytes={} is not whole", width, height, length);
        applyTemporal(out.verdict, out.state, state);
        return out;
    }

    // YUYV packs two pixels into four bytes; the conversion is this side's
    // work, so the frame source only has to read the device.
    const cv::Mat packed(height, width, CV_8UC2, const_cast<std::uint8_t *>(frame));
    cv::cvtColor(packed, impl_->bgr, cv::COLOR_YUV2BGR_YUYV);
    cv::resize(impl_->bgr, impl_->small, cv::Size(kTriggerWidth, kTriggerHeight), 0.0, 0.0, cv::INTER_AREA);
    return impl_->decide(state, static_cast<double>(impl_->small.cols) / width,
                         static_cast<double>(impl_->small.rows) / height, 0.0);
}

} // namespace fingerprint
