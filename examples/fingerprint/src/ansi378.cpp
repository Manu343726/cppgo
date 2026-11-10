// The ANSI/INCITS 378 subset this example serialises, and why it is a subset.
//
// The standard's record is a CBEFF-wrapped, multi-view structure. This example
// carries one finger view and the fields a matcher built on this pipeline can
// fill, and it says so here rather than pretending to write the standard.
//
// ---------------------------------------------------------------------------
// THE LAYOUT
// ---------------------------------------------------------------------------
//
// All multi-byte integers are big-endian, which is the standard's byte order.
//
//   offset size field
//   ------ ---- ---------------------------------------------------------------
//        0    4 format identifier: 'F' 'M' 'R' 0x00
//        4    4 version: ' ' '2' '0' 0x00  — the value ANSI/INCITS 378-2004 uses
//        8    4 record length in bytes, including this header: 20 + 8 * count
//       12    2 image width in pixels
//       14    2 image height in pixels
//       16    2 number of minutiae
//       18    2 ridge frequency in hundredths of a cycle per pixel
//       20    8 * count minutiae, ordered by (y, x)
//
// and one minutia record:
//
//   offset size field
//   ------ ---- ---------------------------------------------------------------
//        0    1 type: 0 ridge ending, 1 ridge bifurcation
//        1    2 x in image pixels
//        3    2 y in image pixels
//        5    1 angle: 0..255 maps to [0, 2*pi)
//        6    1 quality: 0..100
//        7    1 ridge count (255 means "255 or more")
//
// ---------------------------------------------------------------------------
// WHAT THE SUBSET CARRIES FROM ANSI/INCITS 378-2004
// ---------------------------------------------------------------------------
//
//   * the format identifier and version, so a reader can tell the record apart
//     from another fingerprint format;
//   * the record length, so the record can be skipped inside a container;
//   * the image size, so a minutia's coordinates are interpretable;
//   * the minutiae count, and per minutia its type (the standard's two
//     MINUTIAE_TYPE values), its position and its direction quantised to one
//     byte (the standard's ANGLE field, 1/256 turn units), and a quality;
//   * the ridge count of a minutia.
//
// ---------------------------------------------------------------------------
// WHAT IT OMITS, AND WHAT THAT MEANS
// ---------------------------------------------------------------------------
//
//   * the CBEFF product identifier, capture-equipment compliance and capture
//     equipment identifier — this pipeline reads a frame, not a certified
//     scanner;
//   * the finger position, view number, impression type and finger quality that
//     make a record a finger *view*: the subset carries exactly one view and
//     does not say which finger it is;
//   * the image resolution fields (pixels per centimetre), which is why the
//     ridge frequency here is in cycles per *pixel*: without a resolution the
//     standard's cycles-per-millimetre value cannot be computed. This is the
//     one field whose unit differs from the standard's, and it is an extension
//     of the subset, stored in the two reserved bytes the standard does not
//     define at this offset;
//   * the extended data block: minutia direction in the standard's own units,
//     the ridge counts to the nearest neighbouring minutiae, and the core and
//     delta records. The ridge count above is this subset's own single field,
//     which the standard carries in that extended block instead;
//   * the count of finger views, and therefore the multi-view layout.
//
// A record this file writes is therefore *not* an ANSI/INCITS 378 record a
// certified matcher would accept. It is the header and minutia shape of the
// standard, with the fields a reader of this example needs, and it is named a
// subset wherever it appears.

#include "fingerprint/pipeline.hpp"

#include <cmath>
#include <cstdio>
#include <sstream>
#include <stdexcept>
#include <string>
#include <vector>

namespace fingerprint {
namespace {

constexpr std::size_t kHeaderSize = 20;
constexpr std::size_t kMinutiaSize = 8;
constexpr std::uint16_t kMaxCoordinate = 0xFFFF;
constexpr int kMaxRidgeCount = 255;
constexpr int kAngleUnits = 256;

const std::uint8_t kFormatIdentifier[4] = {'F', 'M', 'R', 0x00};
const std::uint8_t kVersion[4] = {' ', '2', '0', 0x00};

void putU16(std::vector<std::uint8_t> &out, std::size_t at, std::uint16_t value) {
    out[at] = static_cast<std::uint8_t>((value >> 8) & 0xFF);
    out[at + 1] = static_cast<std::uint8_t>(value & 0xFF);
}

std::uint16_t getU16(const std::vector<std::uint8_t> &in, std::size_t at) {
    return static_cast<std::uint16_t>((static_cast<std::uint16_t>(in[at]) << 8) | in[at + 1]);
}

void putU32(std::vector<std::uint8_t> &out, std::size_t at, std::uint32_t value) {
    out[at] = static_cast<std::uint8_t>((value >> 24) & 0xFF);
    out[at + 1] = static_cast<std::uint8_t>((value >> 16) & 0xFF);
    out[at + 2] = static_cast<std::uint8_t>((value >> 8) & 0xFF);
    out[at + 3] = static_cast<std::uint8_t>(value & 0xFF);
}

std::uint32_t getU32(const std::vector<std::uint8_t> &in, std::size_t at) {
    return (static_cast<std::uint32_t>(in[at]) << 24) | (static_cast<std::uint32_t>(in[at + 1]) << 16) |
           (static_cast<std::uint32_t>(in[at + 2]) << 8) | static_cast<std::uint32_t>(in[at + 3]);
}

std::uint16_t clampCoordinate(int value) {
    if (value < 0) {
        return 0;
    }
    if (value > kMaxCoordinate) {
        return kMaxCoordinate;
    }
    return static_cast<std::uint16_t>(value);
}

std::uint8_t encodeAngle(double radians) {
    double turn = radians / (2.0 * CV_PI);
    turn -= std::floor(turn); // [0, 1)
    int units = static_cast<int>(std::lround(turn * kAngleUnits));
    if (units >= kAngleUnits) {
        units = 0;
    }
    return static_cast<std::uint8_t>(units);
}

double decodeAngle(std::uint8_t units) {
    return units * (2.0 * CV_PI) / kAngleUnits;
}

} // namespace

std::vector<std::uint8_t> toAnsi378(const TemplateRecord &record) {
    if (record.minutiae.size() > 0xFFFF) {
        throw std::invalid_argument("more minutiae than the count field holds");
    }
    std::vector<Minutia> minutiae = record.minutiae;
    // One canonical order, so the same logical template serialises to the same
    // bytes whatever order the extractor produced.
    std::sort(minutiae.begin(), minutiae.end(), [](const Minutia &a, const Minutia &b) {
        return a.y < b.y || (a.y == b.y && a.x < b.x);
    });

    std::vector<std::uint8_t> out(kHeaderSize + kMinutiaSize * minutiae.size(), 0);
    for (std::size_t i = 0; i < sizeof(kFormatIdentifier); ++i) {
        out[i] = kFormatIdentifier[i];
        out[4 + i] = kVersion[i];
    }
    putU32(out, 8, static_cast<std::uint32_t>(out.size()));
    putU16(out, 12, clampCoordinate(record.width));
    putU16(out, 14, clampCoordinate(record.height));
    putU16(out, 16, static_cast<std::uint16_t>(minutiae.size()));
    const double hundredths = record.ridge_frequency * 100.0;
    putU16(out, 18, static_cast<std::uint16_t>(std::clamp(hundredths, 0.0, 65535.0)));

    std::size_t at = kHeaderSize;
    for (const Minutia &minutia : minutiae) {
        out[at] = static_cast<std::uint8_t>(minutia.type == MINUTIA_BIFURCATION ? MINUTIA_BIFURCATION : MINUTIA_ENDING);
        putU16(out, at + 1, clampCoordinate(minutia.x));
        putU16(out, at + 3, clampCoordinate(minutia.y));
        out[at + 5] = encodeAngle(minutia.angle);
        out[at + 6] = static_cast<std::uint8_t>(std::clamp(minutia.quality, 0, 100));
        out[at + 7] = static_cast<std::uint8_t>(std::clamp(minutia.ridge_count, 0, kMaxRidgeCount));
        at += kMinutiaSize;
    }
    return out;
}

TemplateRecord fromAnsi378(const std::vector<std::uint8_t> &bytes) {
    if (bytes.size() < kHeaderSize) {
        throw std::invalid_argument("the record is shorter than the 20-byte header");
    }
    for (std::size_t i = 0; i < sizeof(kFormatIdentifier); ++i) {
        if (bytes[i] != kFormatIdentifier[i]) {
            throw std::invalid_argument("the record does not start with FMR\\0");
        }
        if (bytes[4 + i] != kVersion[i]) {
            throw std::invalid_argument("the version field is not this subset's ' 20\\0'");
        }
    }
    const std::uint32_t length = getU32(bytes, 8);
    if (length != bytes.size()) {
        throw std::invalid_argument("the record length field and the byte count disagree");
    }
    const std::uint16_t count = getU16(bytes, 16);
    if (kHeaderSize + kMinutiaSize * count != bytes.size()) {
        throw std::invalid_argument("the minutiae count and the record length disagree");
    }

    TemplateRecord record{};
    record.width = getU16(bytes, 12);
    record.height = getU16(bytes, 14);
    record.ridge_frequency = getU16(bytes, 18) / 100.0;
    record.minutiae.reserve(count);
    for (std::uint16_t i = 0; i < count; ++i) {
        const std::size_t at = kHeaderSize + kMinutiaSize * i;
        Minutia minutia{};
        minutia.type = bytes[at] == MINUTIA_BIFURCATION ? MINUTIA_BIFURCATION : MINUTIA_ENDING;
        minutia.x = getU16(bytes, at + 1);
        minutia.y = getU16(bytes, at + 3);
        minutia.angle = decodeAngle(bytes[at + 5]);
        minutia.quality = bytes[at + 6];
        minutia.ridge_count = bytes[at + 7];
        record.minutiae.push_back(minutia);
    }
    return record;
}

std::string describeAnsi378(const std::vector<std::uint8_t> &bytes) {
    const TemplateRecord record = fromAnsi378(bytes);
    std::ostringstream out;
    char line[160];
    std::snprintf(line, sizeof(line), "format: FMR 0 (ANSI/INCITS 378 subset, see ansi378.cpp)\n");
    out << line;
    std::snprintf(line, sizeof(line), "version: ' 20'\n");
    out << line;
    std::snprintf(line, sizeof(line), "bytes: %zu\n", bytes.size());
    out << line;
    std::snprintf(line, sizeof(line), "image: %dx%d\n", record.width, record.height);
    out << line;
    std::snprintf(line, sizeof(line), "ridge frequency: %.2f cycles/pixel\n", record.ridge_frequency);
    out << line;
    std::snprintf(line, sizeof(line), "minutiae: %zu\n", record.minutiae.size());
    out << line;
    std::snprintf(line, sizeof(line), "  %-12s %6s %6s %8s %8s %6s\n", "type", "x", "y", "angle", "quality", "ridges");
    out << line;
    for (const Minutia &minutia : record.minutiae) {
        std::snprintf(line, sizeof(line), "  %-12s %6d %6d %8.3f %8d %6d\n",
                      minutia.type == MINUTIA_BIFURCATION ? "bifurcation" : "ending", minutia.x, minutia.y,
                      minutia.angle, minutia.quality, minutia.ridge_count);
        out << line;
    }
    return out.str();
}

} // namespace fingerprint
