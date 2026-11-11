// The calls the Go program makes, over the pipeline the other files implement:
// `enroll` and `dump_ansi378`, plus the library's version.
//
// Everything a caller sees crosses here: the bytes of an encoded frame go in,
// the pipeline runs — decode, segment, normalise, enhance, binarise, thin,
// extract, serialise — and the bytes of an ANSI/INCITS 378 subset come out,
// with the timing of each stage beside them. No `cv::Mat`, no minutia and no
// template field leaves this file except as an opaque string.
//
// This is also where the example's records are written, one per stage of one
// enrolment: trace for the timings, debug for the frame and the region, info for
// the enrolment, warn for a frame with no finger in it and error for one that
// does not decode. They reach the Go side through the sink the caller stored,
// while this call is running — see log.cpp for why that is the only moment a
// record may cross.

#include "fingerprint/fingerprint.hpp"

#include "fingerprint/pipeline.hpp"
#include "log.hpp"

#include <algorithm>
#include <chrono>
#include <stdexcept>
#include <string>
#include <vector>

#include <opencv2/core/version.hpp>
#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>

namespace fingerprint {
namespace {

using Clock = std::chrono::steady_clock;

double micros(Clock::time_point from, Clock::time_point to) {
    return std::chrono::duration<double, std::micro>(to - from).count();
}

} // namespace

Enrollment enroll(const std::uint8_t *encoded, std::size_t length) {
    const auto log = detail::logger();
    if (encoded == nullptr || length == 0) {
        log->error("enroll: there are no bytes to decode");
        throw std::invalid_argument("there are no bytes to decode");
    }

    const Clock::time_point start = Clock::now();
    // The frame's bytes are borrowed for the duration of the decode; nothing
    // below holds them.
    const cv::Mat view(1, static_cast<int>(length), CV_8U, const_cast<std::uint8_t *>(encoded));
    const cv::Mat photo = cv::imdecode(view, cv::IMREAD_COLOR);
    const Clock::time_point decoded = Clock::now();
    if (photo.empty()) {
        log->error("enroll: the frame did not decode ({} bytes)", length);
        throw std::invalid_argument("the frame did not decode");
    }
    log->debug("enroll: decoded {}x{} ({} bytes)", photo.cols, photo.rows, length);

    const cv::Mat mask = segment(photo);
    const Clock::time_point segmented = Clock::now();
    const Region roi = largestRegion(mask);
    if (roi.width <= 0 || roi.height <= 0) {
        log->warn("enroll: no finger was found in the {}x{} frame", photo.cols, photo.rows);
        throw std::invalid_argument("no finger was found in the frame");
    }
    log->debug("enroll: roi {}x{} at ({}, {})", roi.width, roi.height, roi.x, roi.y);

    cv::Mat gray;
    cv::cvtColor(photo(cv::Rect(roi.x, roi.y, roi.width, roi.height)), gray, cv::COLOR_BGR2GRAY);
    const cv::Mat normalized = normalize(gray, kNormalizeMean, kNormalizeSigma);
    const cv::Mat orientation = orientationField(normalized, kOrientationBlock);
    // The frequency is estimated from the normalised ridge pattern before the
    // Gabor bank is tuned to it: the bank's wavelength is the estimate.
    const double frequency = ridgeFrequency(normalized);
    const cv::Mat enhanced = gaborEnhance(normalized, orientation, frequency);
    const Clock::time_point enhancedAt = Clock::now();

    std::vector<Minutia> minutiae = extractMinutiae(enhanced);
    // The extractor works inside the ROI; the template's coordinates are the
    // frame's.
    for (Minutia &minutia : minutiae) {
        minutia.x += roi.x;
        minutia.y += roi.y;
    }
    const Clock::time_point extracted = Clock::now();
    log->info("enroll: {}x{} frame, roi {}x{}, {} minutiae", photo.cols, photo.rows, roi.width, roi.height,
              minutiae.size());

    TemplateRecord record{};
    record.width = photo.cols;
    record.height = photo.rows;
    record.ridge_frequency = frequency;
    record.minutiae = std::move(minutiae);
    const std::vector<std::uint8_t> bytes = toAnsi378(record);
    const Clock::time_point encodedAt = Clock::now();
    log->trace("enroll: decode {:.2f} ms, segment {:.2f} ms, enhance {:.2f} ms, extract {:.2f} ms, encode {:.2f} ms",
               micros(start, decoded) / 1000.0, micros(decoded, segmented) / 1000.0,
               micros(segmented, enhancedAt) / 1000.0, micros(enhancedAt, extracted) / 1000.0,
               micros(extracted, encodedAt) / 1000.0);

    Enrollment result{};
    result.template_bytes.assign(bytes.begin(), bytes.end());
    result.minutiae = static_cast<std::int32_t>(record.minutiae.size());
    result.image_width = record.width;
    result.image_height = record.height;
    result.roi_x = roi.x;
    result.roi_y = roi.y;
    result.roi_width = roi.width;
    result.roi_height = roi.height;
    result.ridge_frequency = frequency;
    result.decode_micros = micros(start, decoded);
    result.segment_micros = micros(decoded, segmented);
    result.enhance_micros = micros(segmented, enhancedAt);
    result.extract_micros = micros(enhancedAt, extracted);
    result.encode_micros = micros(extracted, encodedAt);
    result.total_micros = micros(start, encodedAt);
    return result;
}

std::string dump_ansi378(const std::uint8_t *bytes, std::size_t length) {
    if (bytes == nullptr || length == 0) {
        throw std::invalid_argument("there are no bytes to read");
    }
    const std::vector<std::uint8_t> record(bytes, bytes + length);
    return describeAnsi378(record);
}

std::string version() { return std::string("fingerprint 0.1.0 (OpenCV ") + CV_VERSION + ")"; }

} // namespace fingerprint
