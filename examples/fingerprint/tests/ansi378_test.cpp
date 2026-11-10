// The template's assertions: the exact bytes of a known minutia set, a
// byte-for-byte round trip, the fields that survive it, and the refusals.

#include "harness.hpp"

#include "fingerprint/fingerprint.hpp"
#include "fingerprint/pipeline.hpp"

#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

namespace {

using namespace fingerprint;

std::string hex(const std::vector<std::uint8_t> &bytes) {
    static const char *digits = "0123456789abcdef";
    std::string out;
    for (std::uint8_t byte : bytes) {
        out.push_back(digits[(byte >> 4) & 0xF]);
        out.push_back(digits[byte & 0xF]);
    }
    return out;
}

std::vector<std::uint8_t> readFile(const std::string &path) {
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        std::fprintf(stderr, "the fixture %s could not be read\n", path.c_str());
        std::exit(1);
    }
    return std::vector<std::uint8_t>(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
}

// The three minutiae the serialisation test is written against: two endings and
// a bifurcation, sorted by row then column, with angles at the quarter turns so
// the quantisation is exact.
TemplateRecord knownRecord() {
    TemplateRecord record{};
    record.width = 640;
    record.height = 480;
    record.ridge_frequency = 0.10;
    record.minutiae = {
        Minutia{100, 200, 0.0, MINUTIA_ENDING, 80, 5},
        Minutia{150, 210, 3.14159265358979323846 / 2.0, MINUTIA_BIFURCATION, 60, 3},
        Minutia{200, 220, 3.14159265358979323846, MINUTIA_ENDING, 40, 1},
    };
    return record;
}

} // namespace

void ansi378Tests() {
    SECTION("toAnsi378: the header and minutiae of a known set, byte for byte");
    const std::vector<std::uint8_t> bytes = toAnsi378(knownRecord());
    // 20 bytes of header and 8 per minutia. Every multi-byte field is
    // big-endian, and the values are the ones knownRecord() sets.
    const std::string expected =
        // "FMR\0", version " 20\0"
        "464d5200" "2032300" "0"
        // record length 44 = 0x0000002c
        "0000002c"
        // width 640 = 0x0280, height 480 = 0x01e0
        "0280" "01e0"
        // minutiae count 3, ridge frequency 10 hundredths
        "0003" "000a"
        // ending, x 100, y 200, angle 0, quality 80, ridge count 5
        "00" "0064" "00c8" "00" "50" "05"
        // bifurcation, x 150, y 210, angle 64 (a quarter turn), quality 60, 3
        "01" "0096" "00d2" "40" "3c" "03"
        // ending, x 200, y 220, angle 128 (a half turn), quality 40, 1
        "00" "00c8" "00dc" "80" "28" "01";
    CHECK(bytes.size() == 44);
    if (hex(bytes) != expected) {
        std::fprintf(stderr, "  got      %s\n  expected %s\n", hex(bytes).c_str(), expected.c_str());
    }
    CHECK(hex(bytes) == expected);

    SECTION("fromAnsi378: the fields of the known set, and a byte-for-byte round trip");
    const TemplateRecord decoded = fromAnsi378(bytes);
    CHECK(decoded.width == 640);
    CHECK(decoded.height == 480);
    CHECK(decoded.minutiae.size() == 3);
    CHECK_NEAR(decoded.ridge_frequency, 0.10, 1e-9);
    CHECK(decoded.minutiae[0].type == MINUTIA_ENDING);
    CHECK(decoded.minutiae[0].x == 100 && decoded.minutiae[0].y == 200);
    CHECK(decoded.minutiae[0].quality == 80 && decoded.minutiae[0].ridge_count == 5);
    CHECK(decoded.minutiae[1].type == MINUTIA_BIFURCATION);
    CHECK(decoded.minutiae[1].x == 150 && decoded.minutiae[1].y == 210);
    CHECK_NEAR(decoded.minutiae[1].angle, 3.14159265358979323846 / 2.0, 2.0 * 3.14159265358979323846 / 256.0);
    CHECK(decoded.minutiae[2].x == 200 && decoded.minutiae[2].y == 220);
    CHECK_NEAR(decoded.minutiae[2].angle, 3.14159265358979323846, 2.0 * 3.14159265358979323846 / 256.0);
    CHECK(hex(toAnsi378(decoded)) == hex(bytes));

    SECTION("the order is canonical: the same minutiae in another order serialise alike");
    TemplateRecord shuffled = knownRecord();
    std::swap(shuffled.minutiae[0], shuffled.minutiae[2]);
    CHECK(hex(toAnsi378(shuffled)) == hex(bytes));

    SECTION("fromAnsi378 refuses what is not this subset's record");
    CHECK_THROWS(fromAnsi378({}), std::invalid_argument);
    std::vector<std::uint8_t> wrongMagic = bytes;
    wrongMagic[0] = 'X';
    CHECK_THROWS(fromAnsi378(wrongMagic), std::invalid_argument);
    std::vector<std::uint8_t> shortRecord{bytes.begin(), bytes.end() - 1};
    CHECK_THROWS(fromAnsi378(shortRecord), std::invalid_argument);
    std::vector<std::uint8_t> wrongCount = bytes;
    wrongCount[17] = 4;
    CHECK_THROWS(fromAnsi378(wrongCount), std::invalid_argument);

    SECTION("describeAnsi378: the header and one line per minutia");
    const std::string description = describeAnsi378(bytes);
    CHECK(description.find("image: 640x480") != std::string::npos);
    CHECK(description.find("minutiae: 3") != std::string::npos);
    CHECK(description.find("bifurcation") != std::string::npos);
    CHECK(description.find("0.10 cycles/pixel") != std::string::npos);

    SECTION("enroll: the pipeline's own template, with the stages timed");
    const std::vector<std::uint8_t> photo = readFile(harness::fixturePath("print.png"));
    const Enrollment enrollment = enroll(photo.data(), photo.size());
    std::printf("   enroll: %d minutiae, roi %dx%d at (%d,%d), %.1f ms total\n", enrollment.minutiae,
                enrollment.roi_width, enrollment.roi_height, enrollment.roi_x, enrollment.roi_y,
                enrollment.total_micros / 1000.0);
    std::printf("   stages: decode %.2f ms, segment %.2f ms, enhance %.2f ms, extract %.2f ms, encode %.2f ms\n",
                enrollment.decode_micros / 1000.0, enrollment.segment_micros / 1000.0,
                enrollment.enhance_micros / 1000.0, enrollment.extract_micros / 1000.0,
                enrollment.encode_micros / 1000.0);
    CHECK(enrollment.image_width == 320);
    CHECK(enrollment.image_height == 320);
    CHECK(enrollment.roi_width > 180 && enrollment.roi_height > 250);
    CHECK(enrollment.minutiae >= 2);
    CHECK(enrollment.ridge_frequency > 0.05 && enrollment.ridge_frequency < 0.2);
    CHECK(enrollment.template_bytes.size() == 20 + 8 * static_cast<std::size_t>(enrollment.minutiae));
    CHECK(enrollment.template_bytes.compare(0, 4, std::string("FMR\0", 4)) == 0);
    CHECK(enrollment.total_micros > 0.0);
    CHECK(enrollment.total_micros >= enrollment.decode_micros + enrollment.segment_micros);

    // The coordinate space is the frame's, not the ROI's: a minutia's position
    // is inside the image's bounds.
    const TemplateRecord record = fromAnsi378(
        std::vector<std::uint8_t>(enrollment.template_bytes.begin(), enrollment.template_bytes.end()));
    CHECK(record.minutiae.size() == static_cast<std::size_t>(enrollment.minutiae));
    for (const Minutia &minutia : record.minutiae) {
        CHECK(minutia.x >= 0 && minutia.x < enrollment.image_width);
        CHECK(minutia.y >= 0 && minutia.y < enrollment.image_height);
    }

    SECTION("enroll refuses a frame that is not an image, and a frame with no finger");
    const std::vector<std::uint8_t> garbage{1, 2, 3, 4, 5, 6, 7, 8};
    CHECK_THROWS(enroll(garbage.data(), garbage.size()), std::invalid_argument);
    const std::vector<std::uint8_t> desk = readFile(harness::fixturePath("frame-background.png"));
    CHECK_THROWS(enroll(desk.data(), desk.size()), std::invalid_argument);

    SECTION("dump_ansi378 prints the template enroll produced");
    const std::string dumped = dump_ansi378(reinterpret_cast<const std::uint8_t *>(enrollment.template_bytes.data()),
                                            enrollment.template_bytes.size());
    CHECK(dumped.find("image: 320x320") != std::string::npos);
    CHECK(dumped.find("minutiae: " + std::to_string(enrollment.minutiae)) != std::string::npos);

    SECTION("version names the library and the OpenCV it links");
    const std::string version = fingerprint::version();
    CHECK(version.find("fingerprint 0.1.0") == 0);
    CHECK(version.find("OpenCV 4.") != std::string::npos);
}
