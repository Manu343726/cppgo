// The trigger's assertions: which frame of a deterministic sequence fires, and
// why the ones that do not fire do not.
//
// The sequences are the ones the rule in fingerprint.hpp describes: an empty
// stream, a hand that is not a finger, a finger entering, a finger held still, a
// finger leaving, one frame of noise in an empty stream, and two fingers at
// once. Each expected index below is the rule's own arithmetic — the EMA with
// weight 0.5, two consecutive frames at or above 0.55, five frames of hold —
// and not a recording of what the implementation happened to do.

#include "harness.hpp"

#include "fingerprint/fingerprint.hpp"

#include <opencv2/imgproc.hpp>

#include <cstdint>
#include <fstream>
#include <iterator>
#include <string>
#include <vector>

namespace {

using namespace fingerprint;

std::vector<std::uint8_t> readFile(const std::string &name) {
    const std::string path = harness::fixturePath(name);
    std::ifstream file(path, std::ios::binary);
    if (!file) {
        std::fprintf(stderr, "the fixture %s could not be read\n", path.c_str());
        std::exit(1);
    }
    return std::vector<std::uint8_t>(std::istreambuf_iterator<char>(file), std::istreambuf_iterator<char>());
}

std::vector<std::uint8_t> frame(const std::string &name) { return readFile(name); }

struct Run {
    std::vector<TriggerOutcome> outcomes;

    std::vector<int> fired() const {
        std::vector<int> indices;
        for (std::size_t i = 0; i < outcomes.size(); ++i) {
            if (outcomes[i].verdict.fire) {
                indices.push_back(static_cast<int>(i));
            }
        }
        return indices;
    }

    void print(const char *name) const {
        std::printf("   %s:", name);
        for (std::size_t i = 0; i < outcomes.size(); ++i) {
            std::printf(" %d:%s%.2f/%s", static_cast<int>(i), outcomes[i].verdict.fire ? "F" : "-",
                        outcomes[i].state.score_ema, reason(static_cast<int>(outcomes[i].verdict.reason)));
        }
        std::printf("\n");
    }

    static const char *reason(int value) {
        switch (value) {
        case NONE:
            return "none";
        case HOLD:
            return "hold";
        case DECODE_FAILED:
            return "decode";
        case NO_SKIN:
            return "noskin";
        case NO_BLOB:
            return "noblob";
        case SHAPE:
            return "shape";
        case LOW_CONTRAST:
            return "flat";
        case NO_RIDGES:
            return "noridge";
        case BELOW_THRESHOLD:
            return "below";
        default:
            return "?";
        }
    }
};

// Runs a sequence of encoded frames through one Trigger and one state, exactly
// as the watch loop does.
Run sequence(const std::vector<std::vector<std::uint8_t>> &frames) {
    Trigger trigger;
    TriggerState state = initial_trigger_state();
    Run run;
    for (const std::vector<std::uint8_t> &encoded : frames) {
        TriggerOutcome outcome = trigger.evaluate(encoded.data(), encoded.size(), state);
        state = outcome.state;
        run.outcomes.push_back(outcome);
    }
    return run;
}

std::vector<std::vector<std::uint8_t>> repeat(const std::vector<std::uint8_t> &encoded, int times) {
    return std::vector<std::vector<std::uint8_t>>(static_cast<std::size_t>(times), encoded);
}

bool equalIndices(const std::vector<int> &got, const std::vector<int> &want) {
    if (got != want) {
        std::fprintf(stderr, "  fired at");
        for (int index : got) {
            std::fprintf(stderr, " %d", index);
        }
        std::fprintf(stderr, ", expected");
        for (int index : want) {
            std::fprintf(stderr, " %d", index);
        }
        std::fprintf(stderr, "\n");
        return false;
    }
    return true;
}

} // namespace

void triggerTests() {
    const std::vector<std::uint8_t> background = frame("frame-background.png");
    const std::vector<std::uint8_t> hand = frame("frame-hand.png");
    const std::vector<std::uint8_t> finger = frame("frame-finger.png");
    const std::vector<std::uint8_t> twoFingers = frame("frame-two-fingers.png");

    SECTION("the trigger's working size is fixed, whatever the camera's");
    {
        Trigger trigger;
        CHECK(trigger.width() == kTriggerWidth);
        CHECK(trigger.height() == kTriggerHeight);
        const TriggerState start = initial_trigger_state();
        CHECK(start.frame_index == 0 && start.score_ema == 0.0f && start.hold == 0);
    }

    SECTION("an empty stream never fires, and says why");
    {
        const Run run = sequence(repeat(background, 8));
        run.print("background");
        CHECK(equalIndices(run.fired(), {}));
        CHECK(run.outcomes.front().verdict.reason == NO_SKIN);
        CHECK(run.outcomes.back().state.frame_index == 8);
        CHECK(run.outcomes.back().state.below == 8);
        CHECK(run.outcomes.back().state.above == 0);
    }

    SECTION("a hand is skin but not a finger: contrast fails, and the stream stays closed");
    {
        const Run run = sequence(repeat(hand, 8));
        run.print("hand");
        CHECK(equalIndices(run.fired(), {}));
        CHECK(run.outcomes.front().verdict.reason == LOW_CONTRAST);
        CHECK(run.outcomes.front().verdict.skin > 0.9f);
        CHECK(run.outcomes.front().verdict.contrast < 0.2f);
        // The score is in the band between the two thresholds: not firing, and
        // not counting as "gone" either.
        CHECK(run.outcomes.back().state.score_ema < kTriggerHigh);
        CHECK(run.outcomes.back().state.score_ema > kTriggerLow);
    }

    SECTION("a finger entering: the first frame above the threshold does not fire, the second does");
    {
        std::vector<std::vector<std::uint8_t>> frames = repeat(background, 3);
        for (int i = 0; i < 6; ++i) {
            frames.push_back(finger);
        }
        const Run run = sequence(frames);
        run.print("entering");
        // Frames 0..2 are the desk. Frame 3 is the first with a finger: its
        // smoothed score is 0.5 of the frame's, which is under the high
        // threshold; frame 4 carries it over, and frame 5 is the second
        // consecutive frame above it, which is what fires.
        CHECK(equalIndices(run.fired(), {5, 6, 7, 8}));
        CHECK(run.outcomes[3].verdict.score > kTriggerHigh);      // the frame itself is convincing
        CHECK(run.outcomes[3].state.score_ema < kTriggerHigh);    // the smoothed value is not
        CHECK(run.outcomes[3].verdict.fire == false);
        CHECK(run.outcomes[5].verdict.reason == NONE);
    }

    SECTION("a finger held still: fires from the second frame on");
    {
        const Run run = sequence(repeat(finger, 8));
        run.print("steady");
        CHECK(equalIndices(run.fired(), {2, 3, 4, 5, 6, 7}));
        CHECK(run.outcomes[2].verdict.reason == NONE);
        CHECK(run.outcomes[2].verdict.roi_count == 1);
    }

    SECTION("a finger leaving: the hold keeps the stream open for five more frames");
    {
        std::vector<std::vector<std::uint8_t>> frames = repeat(finger, 6);
        for (int i = 0; i < 6; ++i) {
            frames.push_back(background);
        }
        const Run run = sequence(frames);
        run.print("leaving");
        // 2..5 fire from the evidence; the desk arrives at 6, and the frames
        // 6..10 are the five frames of hold. Frame 11 is the first that does
        // not fire.
        CHECK(equalIndices(run.fired(), {2, 3, 4, 5, 6, 7, 8, 9, 10}));
        for (int index = 6; index <= 10; ++index) {
            CHECK(run.outcomes[static_cast<std::size_t>(index)].verdict.reason == HOLD);
            // A held frame reports where the finger was, not where it is not.
            CHECK(run.outcomes[static_cast<std::size_t>(index)].verdict.roi_count == 1);
        }
        CHECK(run.outcomes[11].verdict.fire == false);
    }

    SECTION("one frame of noise in an empty stream never fires");
    {
        std::vector<std::vector<std::uint8_t>> frames;
        frames.push_back(background);
        frames.push_back(background);
        frames.push_back(finger); // a single convincing frame
        for (int i = 0; i < 5; ++i) {
            frames.push_back(background);
        }
        const Run run = sequence(frames);
        run.print("single-frame");
        CHECK(equalIndices(run.fired(), {}));
        CHECK(run.outcomes[2].verdict.score > kTriggerHigh);
        CHECK(run.outcomes[2].state.above == 0);
    }

    SECTION("two fingers in one frame: one more region, and both are reported");
    {
        const Run run = sequence(repeat(twoFingers, 4));
        run.print("two-fingers");
        CHECK(equalIndices(run.fired(), {2, 3}));
        const TriggerVerdict &verdict = run.outcomes[2].verdict;
        CHECK(verdict.roi_count == 2);
        CHECK(verdict.roi_w[0] > 0 && verdict.roi_h[0] > 0);
        CHECK(verdict.roi_w[1] > 0 && verdict.roi_h[1] > 0);
        // The two boxes are the two fingers of the fixture, a third of a frame
        // apart.
        const int separation = verdict.roi_x[1] - verdict.roi_x[0];
        std::printf("   regions: (%d,%d %dx%d) and (%d,%d %dx%d), %.0f px apart\n", verdict.roi_x[0], verdict.roi_y[0],
                    verdict.roi_w[0], verdict.roi_h[0], verdict.roi_x[1], verdict.roi_y[1], verdict.roi_w[1],
                    verdict.roi_h[1], static_cast<double>(separation));
        CHECK(separation > 250 && separation < 350);
        CHECK(run.outcomes[2].state.roi_count == 2);
    }

    SECTION("the regions are the source frame's, not the working image's");
    {
        const Run run = sequence(repeat(finger, 3));
        const TriggerVerdict &verdict = run.outcomes[2].verdict;
        // The fixture's finger is at x 240..410 of a 640-wide frame; a working
        // coordinate would land under 160.
        CHECK(verdict.roi_x[0] > 160);
        CHECK(verdict.roi_x[0] + verdict.roi_w[0] < 640);
        CHECK(verdict.roi_h[0] > 300);
    }

    SECTION("a frame that is not an image is refused, and the hold still applies");
    {
        const std::vector<std::uint8_t> garbage{0, 1, 2, 3, 4, 5, 6, 7, 8, 9};
        Trigger trigger;
        TriggerState state = initial_trigger_state();
        const TriggerOutcome out = trigger.evaluate(garbage.data(), garbage.size(), state);
        CHECK(out.verdict.fire == false);
        CHECK(out.verdict.reason == DECODE_FAILED);
        CHECK(out.state.frame_index == 1);
        const TriggerOutcome empty = trigger.evaluate(nullptr, 0, state);
        CHECK(empty.verdict.reason == DECODE_FAILED);
    }

    SECTION("evaluate_yuyv: a camera's raw frame decides the same way as its encoded form");
    {
        // The frame the camera would hand over: the fixture, converted to the
        // YUYV packing V4L2 uses. The conversion is the fixture's here, so the
        // test is about the trigger and not about the camera.
        const cv::Mat bgr = cv::imread(harness::fixturePath("frame-finger.png"), cv::IMREAD_COLOR);
        CHECK(!bgr.empty());
        cv::Mat yuyv;
        cv::cvtColor(bgr, yuyv, cv::COLOR_BGR2YUV_YUYV);
        const std::vector<std::uint8_t> bytes(yuyv.data, yuyv.data + yuyv.total() * yuyv.elemSize());

        Trigger trigger;
        TriggerState state = initial_trigger_state();
        std::vector<int> fires;
        for (int i = 0; i < 6; ++i) {
            const TriggerOutcome out =
                trigger.evaluate_yuyv(bytes.data(), bytes.size(), FrameSize{bgr.cols, bgr.rows}, state);
            state = out.state;
            if (out.verdict.fire) {
                fires.push_back(i);
            }
        }
        std::printf("   yuyv: fires at");
        for (int index : fires) {
            std::printf(" %d", index);
        }
        std::printf(", score %.2f\n", state.score_ema);
        // The YUYV round trip loses a little of the ridge contrast, so the
        // smoothed score crosses the high threshold one frame later than the
        // encoded frame's does. Where it fires is the same rule, one frame
        // further along.
        CHECK(equalIndices(fires, {3, 4, 5}));
        // A truncated frame is refused, and a fresh state names the decode as
        // the reason (an established stream would spend its hold instead).
        const TriggerState fresh = initial_trigger_state();
        CHECK(trigger.evaluate_yuyv(bytes.data(), 8, FrameSize{bgr.cols, bgr.rows}, fresh).verdict.reason ==
              DECODE_FAILED);
    }
}
