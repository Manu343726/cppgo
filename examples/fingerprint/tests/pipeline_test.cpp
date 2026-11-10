// The pipeline's own assertions: every stage is checked against a value, and
// the minutiae against the exact positions of a hand-drawn skeleton.

#include "harness.hpp"

#include "fingerprint/pipeline.hpp"

#include <opencv2/imgproc.hpp>

#include <cmath>
#include <vector>

namespace {

using namespace fingerprint;

bool onlyValues(const cv::Mat &image, const std::vector<int> &allowed) {
    for (int y = 0; y < image.rows; ++y) {
        for (int x = 0; x < image.cols; ++x) {
            const int value = image.at<unsigned char>(y, x);
            bool found = false;
            for (int candidate : allowed) {
                found = found || value == candidate;
            }
            if (!found) {
                std::fprintf(stderr, "  value %d at (%d,%d) is not one of the expected ones\n", value, x, y);
                return false;
            }
        }
    }
    return true;
}

// The fraction of skeleton pixels with at most two of their neighbours set: a
// one-pixel-wide ridge has two, a junction three, and anything thicker more.
double thinFraction(const cv::Mat &skeleton) {
    int foreground = 0;
    int thin = 0;
    for (int y = 1; y + 1 < skeleton.rows; ++y) {
        for (int x = 1; x + 1 < skeleton.cols; ++x) {
            if (!skeleton.at<unsigned char>(y, x)) {
                continue;
            }
            ++foreground;
            int neighbours = 0;
            for (int dy = -1; dy <= 1; ++dy) {
                for (int dx = -1; dx <= 1; ++dx) {
                    if (dx == 0 && dy == 0) {
                        continue;
                    }
                    neighbours += skeleton.at<unsigned char>(y + dy, x + dx) ? 1 : 0;
                }
            }
            if (neighbours <= 2) {
                ++thin;
            }
        }
    }
    return foreground == 0 ? 0.0 : static_cast<double>(thin) / foreground;
}

Minutia expectAt(const std::vector<Minutia> &minutiae, int x, int y) {
    for (const Minutia &minutia : minutiae) {
        if (minutia.x == x && minutia.y == y) {
            return minutia;
        }
    }
    std::fprintf(stderr, "  no minutia at (%d,%d); found:", x, y);
    for (const Minutia &minutia : minutiae) {
        std::fprintf(stderr, " (%d,%d)%s", minutia.x, minutia.y, minutia.type == MINUTIA_BIFURCATION ? "b" : "e");
    }
    std::fprintf(stderr, "\n");
    return Minutia{-1, -1, 0.0, -1, 0, 0};
}

} // namespace

void pipelineTests() {
    const cv::Mat photo = harness::fixture("print.png");

    SECTION("segment: a mask the photo's size, 255 on the finger and 0 off it");
    const cv::Mat mask = segment(photo);
    CHECK(mask.size() == photo.size());
    CHECK(mask.type() == CV_8UC1);
    CHECK(onlyValues(mask, {0, 255}));
    const Region roi = largestRegion(mask);
    CHECK(roi.width >= 180);
    CHECK(roi.height >= 250);
    CHECK(roi.x > 0 && roi.y > 0);
    // Off the finger the mask is empty, so the desk frame has no region at all.
    const cv::Mat emptyMask = segment(harness::fixture("frame-background.png"));
    CHECK(cv::countNonZero(emptyMask) == 0);
    CHECK(largestRegion(emptyMask).width == 0);

    SECTION("normalize: the target mean and deviation, and a flat image stays flat");
    cv::Mat gray;
    cv::cvtColor(photo(cv::Rect(roi.x, roi.y, roi.width, roi.height)), gray, cv::COLOR_BGR2GRAY);
    const cv::Mat normalized = normalize(gray, kNormalizeMean, kNormalizeSigma);
    CHECK(normalized.type() == CV_32FC1);
    cv::Scalar mean;
    cv::Scalar deviation;
    cv::meanStdDev(normalized, mean, deviation);
    CHECK_NEAR(mean[0], kNormalizeMean, 1e-3);
    CHECK_NEAR(deviation[0], kNormalizeSigma, 1e-3);
    const cv::Mat flat(gray.size(), CV_8U, cv::Scalar(7));
    cv::meanStdDev(normalize(flat, kNormalizeMean, kNormalizeSigma), mean, deviation);
    CHECK_NEAR(mean[0], kNormalizeMean, 1e-3);
    CHECK_NEAR(deviation[0], 0.0, 1e-9);

    SECTION("orientationField: the print's ridges run along y, so the field is 0 mod pi");
    const cv::Mat orientation = orientationField(normalized, kOrientationBlock);
    CHECK(orientation.type() == CV_32FC1);
    CHECK(orientation.size() == normalized.size());
    // The circular mean of the central block, in half-turn units: a ridge and
    // its valley are both at 0, so the mean is taken on the doubled angle.
    double sumSine = 0.0;
    double sumCosine = 0.0;
    int samples = 0;
    for (int y = orientation.rows / 2 - 40; y < orientation.rows / 2 + 40; ++y) {
        for (int x = orientation.cols / 2 - 40; x < orientation.cols / 2 + 40; ++x) {
            const double angle = 2.0 * orientation.at<float>(y, x);
            sumSine += std::sin(angle);
            sumCosine += std::cos(angle);
            ++samples;
        }
    }
    const double meanAngle = 0.5 * std::atan2(sumSine, sumCosine);
    CHECK_NEAR(meanAngle, 0.0, 0.15);

    SECTION("ridgeFrequency: the print's period is 10 pixels, so 0.1 cycles per pixel");
    const double frequency = ridgeFrequency(normalized);
    CHECK_NEAR(frequency, 1.0 / kRidgePeriodPixels, 0.02);

    SECTION("gaborEnhance and binarize: a ridge image, then only 0 and 255");
    const cv::Mat enhanced = gaborEnhance(normalized, orientation, frequency);
    CHECK(enhanced.type() == CV_8UC1);
    CHECK(enhanced.size() == normalized.size());
    double low = 0.0;
    double high = 0.0;
    cv::minMaxLoc(enhanced, &low, &high);
    CHECK(high > 128.0);
    const cv::Mat binary = binarize(enhanced);
    CHECK(onlyValues(binary, {0, 255}));
    CHECK(cv::countNonZero(binary) > 0);
    CHECK(cv::countNonZero(binary) < binary.total());

    SECTION("thin: one value wide, and a one-pixel skeleton is left alone");
    const cv::Mat skeleton = thin(binary);
    CHECK(onlyValues(skeleton, {0, 1}));
    CHECK(cv::countNonZero(skeleton) > 0);
    const double fraction = thinFraction(skeleton);
    CHECK(fraction > 0.9);

    const cv::Mat drawn = cv::imread(harness::fixturePath("skeleton.png"), cv::IMREAD_GRAYSCALE);
    CHECK(!drawn.empty());
    const cv::Mat thinned = thin(drawn);
    // The drawn image is 0/255 and the thinned one 0/1: compare the shapes.
    int difference = 0;
    for (int y = 0; y < drawn.rows; ++y) {
        for (int x = 0; x < drawn.cols; ++x) {
            const int before = drawn.at<unsigned char>(y, x) ? 1 : 0;
            const int after = thinned.at<unsigned char>(y, x);
            if (before != after) {
                ++difference;
            }
        }
    }
    CHECK(difference == 0);

    SECTION("extractMinutiae: the four minutiae of the drawn skeleton, by position and type");
    const std::vector<Minutia> minutiae = extractMinutiae(drawn);
    CHECK(minutiae.size() == 4);
    const Minutia leftEnd = expectAt(minutiae, 60, 60);
    CHECK(leftEnd.type == MINUTIA_ENDING);
    const Minutia junction = expectAt(minutiae, 140, 60);
    CHECK(junction.type == MINUTIA_BIFURCATION);
    const Minutia rightEnd = expectAt(minutiae, 200, 60);
    CHECK(rightEnd.type == MINUTIA_ENDING);
    const Minutia bottomEnd = expectAt(minutiae, 140, 160);
    CHECK(bottomEnd.type == MINUTIA_ENDING);
    // The order the template relies on: by row, then by column.
    CHECK(minutiae.size() == 4 && minutiae[0].y == 60 && minutiae[0].x == 60);
    CHECK(minutiae.size() == 4 && minutiae[3].y == 160 && minutiae[3].x == 140);
    for (const Minutia &minutia : minutiae) {
        CHECK(minutia.angle >= 0.0 && minutia.angle < 2.0 * 3.141592653589793);
        CHECK(minutia.quality >= 0 && minutia.quality <= 100);
        CHECK(minutia.ridge_count >= 0 && minutia.ridge_count <= kRidgeCountReach);
    }

    SECTION("extractMinutiae: the print's own endings, and a clean ridge has none");
    const std::vector<Minutia> printMinutiae = extractMinutiae(enhanced);
    std::printf("   print: %zu minutiae, ridge frequency %.3f cycles/pixel\n", printMinutiae.size(), frequency);
    CHECK(printMinutiae.size() >= 2);
    // A perfect grating has no minutiae at all: the orientation is constant and
    // no ridge ends inside the image.
    cv::Mat grating(120, 120, CV_8U);
    for (int y = 0; y < grating.rows; ++y) {
        for (int x = 0; x < grating.cols; ++x) {
            grating.at<unsigned char>(y, x) =
                std::sin(2.0 * 3.141592653589793 * x / kRidgePeriodPixels) > 0.0 ? 255 : 0;
        }
    }
    const std::vector<Minutia> gratingMinutiae = extractMinutiae(grating);
    std::printf("   grating: %zu minutiae\n", gratingMinutiae.size());
    CHECK(gratingMinutiae.empty());
}
