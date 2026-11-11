// The sink's assertions: every level reaches the caller's function, the text is
// the formatted line, a null pointer stops delivery, and the pipeline's own
// records arrive while the call that writes them is still running.

#include "harness.hpp"

#include "fingerprint/fingerprint.hpp"
#include "log.hpp" // detail::write_log_samples: the sink's diagnostic, not bound

#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <utility>
#include <vector>

namespace {

using namespace fingerprint;

std::vector<std::pair<std::int32_t, std::string>> deliveries;

void record(std::int32_t level, const char *message) { deliveries.emplace_back(level, std::string(message)); }

std::vector<std::uint8_t> readFile(const std::string &name) {
    std::ifstream file(harness::fixturePath(name), std::ios::binary);
    return std::vector<std::uint8_t>(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
}

bool sawLevel(std::int32_t level) {
    for (const auto &delivery : deliveries) {
        if (delivery.first == level) {
            return true;
        }
    }
    return false;
}

std::string textOf(std::int32_t level) {
    for (const auto &delivery : deliveries) {
        if (delivery.first == level) {
            return delivery.second;
        }
    }
    return "";
}

} // namespace

void logTests() {
    SECTION("one record per level reaches the callback, with its spdlog level");
    deliveries.clear();
    set_log_callback(&record);
    detail::write_log_samples();
    CHECK(deliveries.size() == 7);
    for (std::int32_t level = 0; level < 6; ++level) {
        CHECK(deliveries[static_cast<std::size_t>(level)].first == level);
    }
    CHECK(deliveries[6].first == 2); // info

    SECTION("the text is the formatted line, the arguments already substituted");
    CHECK(deliveries[0].second == "trace sample");
    CHECK(deliveries[1].second == "debug sample");
    CHECK(deliveries[2].second == "info sample");
    CHECK(deliveries[3].second == "warn sample");
    CHECK(deliveries[4].second == "error sample");
    CHECK(deliveries[5].second == "critical sample");
    CHECK(deliveries[6].second == "frame 640x480 at (12, 34) with 7 minutiae");
    // One line per record: no trailing newline travels with it.
    CHECK(deliveries[0].second.find('\n') == std::string::npos);

    SECTION("a null pointer stops delivery, and the library keeps working");
    set_log_callback(nullptr);
    deliveries.clear();
    detail::write_log_samples();
    CHECK(deliveries.empty());
    const std::vector<std::uint8_t> photo = readFile("print.png");
    const Enrollment enrollment = enroll(photo.data(), photo.size());
    CHECK(enrollment.minutiae > 0);
    CHECK(deliveries.empty());

    SECTION("the pipeline's records arrive during the call that writes them");
    set_log_callback(&record);
    deliveries.clear();
    const Enrollment second = enroll(photo.data(), photo.size());
    CHECK(second.minutiae == enrollment.minutiae);
    // debug (the decoded frame), debug (the region), info (the enrolment) and
    // trace (the per-stage times), in the order the pipeline reached them.
    CHECK(deliveries.size() == 4);
    if (deliveries.size() == 4) {
        CHECK(deliveries[0].first == 1);
        CHECK(deliveries[0].second == "enroll: decoded 320x320 (3642 bytes)");
        CHECK(deliveries[1].first == 1);
        CHECK(deliveries[1].second == "enroll: roi 219x301 at (51, 10)");
        CHECK(deliveries[2].first == 2);
        CHECK(deliveries[2].second == "enroll: 320x320 frame, roi 219x301, 33 minutiae");
        CHECK(deliveries[3].first == 0);
        CHECK(deliveries[3].second.find("enroll: decode ") == 0);
    }

    SECTION("a frame with no finger is a warning, and one that does not decode an error");
    deliveries.clear();
    const std::vector<std::uint8_t> desk = readFile("frame-background.png");
    CHECK_THROWS(enroll(desk.data(), desk.size()), std::invalid_argument);
    CHECK(deliveries.size() == 2); // the decode's debug line, then the warning
    CHECK(sawLevel(3));
    CHECK(textOf(3) == "enroll: no finger was found in the 640x480 frame");

    deliveries.clear();
    const std::vector<std::uint8_t> garbage{1, 2, 3, 4, 5, 6, 7, 8};
    CHECK_THROWS(enroll(garbage.data(), garbage.size()), std::invalid_argument);
    CHECK(deliveries.size() == 1);
    CHECK(sawLevel(4));
    CHECK(textOf(4) == "enroll: the frame did not decode (8 bytes)");

    deliveries.clear();
    CHECK_THROWS(enroll(nullptr, 0), std::invalid_argument);
    CHECK(sawLevel(4));

    SECTION("a second registration replaces the first");
    deliveries.clear();
    set_log_callback([](std::int32_t, const char *) {});
    detail::write_log_samples();
    CHECK(deliveries.empty());
    // ...and the first one still receives once it is stored again.
    set_log_callback(&record);
    detail::write_log_samples();
    CHECK(deliveries.size() == 7);

    SECTION("the trigger's records arrive through the same sink");
    set_log_callback(&record);
    deliveries.clear();
    const std::vector<std::uint8_t> broken{0, 1, 2, 3};
    Trigger trigger;
    const TriggerState state = initial_trigger_state();
    const TriggerOutcome outcome = trigger.evaluate(broken.data(), broken.size(), state);
    CHECK(outcome.verdict.reason == DECODE_FAILED);
    CHECK(deliveries.size() == 1);
    CHECK(sawLevel(3)); // a warning: the decoder refused the frame
    set_log_callback(nullptr);
}
