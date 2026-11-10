// The log sink's assertions: the records arrive in Go through the callback,
// carry their level, and their text is read from the buffer the sink fills.

#include "harness.hpp"

#include "fingerprint/fingerprint.hpp"

#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <utility>
#include <vector>

namespace {

using namespace fingerprint;

std::vector<std::pair<std::int32_t, std::int32_t>> deliveries;

void record(std::int32_t level, std::int32_t pending) { deliveries.emplace_back(level, pending); }

std::vector<std::uint8_t> readFile(const std::string &name) {
    std::ifstream file(harness::fixturePath(name), std::ios::binary);
    return std::vector<std::uint8_t>(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
}

} // namespace

void logTests() {
    SECTION("one record per level reaches the callback, with its spdlog level");
    deliveries.clear();
    drain_log(); // start from an empty buffer
    set_log_callback(&record);
    emit_log_samples();
    CHECK(deliveries.size() == 7);
    for (std::int32_t level = 0; level < 6; ++level) {
        CHECK(deliveries[static_cast<std::size_t>(level)].first == level);
    }
    CHECK(deliveries[6].first == 2); // info
    CHECK(deliveries[6].second == 7);

    SECTION("the text of each record, including the one with several arguments");
    const std::string text = drain_log();
    CHECK(text.find("[trace] trace sample") != std::string::npos);
    CHECK(text.find("[debug] debug sample") != std::string::npos);
    CHECK(text.find("[info] info sample") != std::string::npos);
    CHECK(text.find("[warning] warn sample") != std::string::npos);
    CHECK(text.find("[error] error sample") != std::string::npos);
    CHECK(text.find("[critical] critical sample") != std::string::npos);
    CHECK(text.find("frame 640x480 at (12, 34) with 7 minutiae") != std::string::npos);
    // A drain empties the buffer.
    CHECK(drain_log().empty());

    SECTION("clearing the callback stops the delivery, and the buffer keeps filling");
    clear_log_callback();
    deliveries.clear();
    emit_log_samples();
    CHECK(deliveries.empty());
    CHECK(!drain_log().empty());
    // A null pointer is the same thing said differently.
    set_log_callback(nullptr);
    deliveries.clear();
    emit_log_samples();
    CHECK(deliveries.empty());
    drain_log();

    SECTION("the pipeline's own records arrive through the same sink");
    set_log_callback(&record);
    deliveries.clear();
    const std::vector<std::uint8_t> photo = readFile("print.png");
    const Enrollment enrollment = enroll(photo.data(), photo.size());
    CHECK(enrollment.minutiae > 0);
    const std::string pipelineLog = drain_log();
    CHECK(pipelineLog.find("enroll: decoded 320x320") != std::string::npos);
    CHECK(pipelineLog.find("minutiae") != std::string::npos);
    CHECK(!deliveries.empty());
    bool sawInfo = false;
    for (const auto &delivery : deliveries) {
        sawInfo = sawInfo || delivery.first == 2;
    }
    CHECK(sawInfo);
    clear_log_callback();
}
