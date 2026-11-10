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
    emit_log_samples(&record);
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

    SECTION("a call with no callback delivers nothing, and the buffer keeps filling");
    deliveries.clear();
    emit_log_samples(nullptr);
    CHECK(deliveries.empty());
    CHECK(!drain_log().empty());
    // The scope ends with the call: the next one that passes a callback
    // delivers again, and a plain one does not.
    deliveries.clear();
    emit_log_samples(&record);
    CHECK(deliveries.size() == 7);
    deliveries.clear();
    emit_log_samples(nullptr);
    CHECK(deliveries.empty());
    drain_log();

    SECTION("the pipeline's own records arrive through the same sink");
    deliveries.clear();
    drain_log();
    const std::vector<std::uint8_t> photo = readFile("print.png");
    const Enrollment enrollment = enroll(photo.data(), photo.size());
    CHECK(enrollment.minutiae > 0);
    // Enrolling takes no callback: what it wrote is buffered, and nothing was
    // delivered while it ran.
    CHECK(deliveries.empty());
    // Delivering is the second step, and it is the call that takes the
    // callback: one call per buffered record, in order, with its level.
    // Three records: the decoded size, the region, and the enrolment itself.
    const std::size_t delivered = deliver_log(&record);
    CHECK(delivered == 3);
    CHECK(deliveries.size() == 3);
    CHECK(deliveries[0].first == 1); // debug: the decoded size
    CHECK(deliveries[1].first == 1); // debug: the region
    CHECK(deliveries[2].first == 2); // info: the frame and the minutiae
    const std::string pipelineLog = drain_log();
    CHECK(pipelineLog.find("enroll: decoded 320x320") != std::string::npos);
    CHECK(pipelineLog.find("minutiae") != std::string::npos);
    CHECK(pipelineLog.find("[info] enroll: 320x320 frame") != std::string::npos);
    CHECK(pipelineLog.find("\n\n") == std::string::npos); // one line per record
    // A null callback delivers nothing and reports nothing.
    CHECK(deliver_log(nullptr) == 0);
    // The trigger is the call that takes no callback: its records are buffered
    // and drained, and no delivery is attempted (its call is not reentrant).
    deliveries.clear();
    Trigger trigger;
    const TriggerState state = initial_trigger_state();
    const std::vector<std::uint8_t> fingerprint = readFile("frame-background.png");
    const TriggerOutcome outcome = trigger.evaluate(fingerprint.data(), fingerprint.size(), state);
    CHECK(outcome.verdict.fire == false);
    CHECK(deliveries.empty());
    drain_log();
}
