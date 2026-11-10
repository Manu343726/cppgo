// make_fixtures — writes the example's test frames and prints.
//
// The fixtures are deterministic: the same source produces the same bytes, so a
// committed frame can be regenerated and compared, and the trigger's tests can
// name the frame (and the frame index) their decision is about. `make fixtures`
// runs this; the output under testdata/ is committed, so a test run needs no
// generation step.
//
//   frame-background.png   a desk: mid grey, not skin, no texture
//   frame-hand.png         a skin-coloured hand-shaped blob, no ridges
//   frame-finger.png       one finger: skin, ridges of a 10-pixel period
//   frame-two-fingers.png  two fingers in the same frame
//   frame-finger.jpg       the same finger frame, JPEG-encoded — the format a
//                          camera actually hands out, and the one whose decode
//                          honours the reduced-scale flag
//   print.png              a full synthetic print with deliberate ridge breaks
//   skeleton.png           a hand-drawn one-pixel skeleton with four minutiae
//                          at known positions

#include <algorithm>
#include <cmath>
#include <cstdio>
#include <string>
#include <vector>

#include <opencv2/core.hpp>
#include <opencv2/imgcodecs.hpp>
#include <opencv2/imgproc.hpp>

namespace {

constexpr double kPi = 3.14159265358979323846;

// The skin tone the trigger's YCrCb test is built for: BGR(100, 140, 200) has
// Cr 161 and Cb 98, inside the [133,173] x [77,127] box. The ridge modulation
// is added to all three channels equally, so it changes the luminance and
// leaves the chroma where it was.
const cv::Scalar kSkin(100, 140, 200);

// A mid-grey desk: BGR(96, 92, 88) has Cr 126 and Cb 131, both outside the
// skin box.
const cv::Scalar kDesk(96, 92, 88);

// The ridge period, in pixels. pipeline.hpp names the same value, and the
// frequency tests assert 1/period.
constexpr double kRidgePeriod = 10.0;

void save(const std::string &dir, const std::string &name, const cv::Mat &image) {
    const std::string path = dir + "/" + name;
    std::vector<int> params;
    if (name.size() > 4 && name.substr(name.size() - 4) == ".jpg") {
        params = {cv::IMWRITE_JPEG_QUALITY, 90};
    } else {
        params = {cv::IMWRITE_PNG_COMPRESSION, 9};
    }
    if (!cv::imwrite(path, image, params)) {
        std::fprintf(stderr, "make_fixtures: cannot write %s\n", path.c_str());
        std::exit(1);
    }
    std::printf("wrote %s (%dx%d)\n", path.c_str(), image.cols, image.rows);
}

// The desk every frame starts from: a slight vertical gradient so a frame is
// not a constant, but far from any ridge.
cv::Mat desk(int width, int height) {
    cv::Mat image(height, width, CV_8UC3);
    for (int y = 0; y < height; ++y) {
        const double lift = 6.0 * y / height;
        image.row(y).setTo(kDesk + cv::Scalar(lift, lift, lift));
    }
    return image;
}

// A skin blob: the ellipse is filled, and `ridges` modulates the luminance with
// a phase that bends along y, so the ridge pattern is not a perfect grating.
void paintBlob(cv::Mat &image, cv::Point centre, cv::Size axes, bool ridges, double amplitude) {
    for (int y = 0; y < image.rows; ++y) {
        for (int x = 0; x < image.cols; ++x) {
            const double dx = (x - centre.x) / static_cast<double>(axes.width);
            const double dy = (y - centre.y) / static_cast<double>(axes.height);
            if (dx * dx + dy * dy > 1.0) {
                continue;
            }
            double lift = 0.0;
            if (ridges) {
                const double phase = 2.0 * kPi * (x - centre.x) / kRidgePeriod + 0.6 * std::sin((y - centre.y) / 40.0);
                lift = amplitude * std::sin(phase);
            }
            cv::Vec3b pixel;
            for (int c = 0; c < 3; ++c) {
                pixel[c] = cv::saturate_cast<unsigned char>(kSkin[c] + lift);
            }
            image.at<cv::Vec3b>(y, x) = pixel;
        }
    }
}

// The same blob, but drawn as a rounded rectangle: a finger's box, which is
// what the trigger's aspect test is about.
void paintFinger(cv::Mat &image, cv::Rect box, bool ridges, double amplitude) {
    const int radius = std::min(box.width, box.height) / 2;
    for (int y = box.y; y < box.y + box.height; ++y) {
        for (int x = box.x; x < box.x + box.width; ++x) {
            if (x < 0 || y < 0 || x >= image.cols || y >= image.rows) {
                continue;
            }
            const int cx = std::clamp(x, box.x + radius, box.x + box.width - radius);
            const int cy = std::clamp(y, box.y + radius, box.y + box.height - radius);
            const double dx = x - cx;
            const double dy = y - cy;
            if (dx * dx + dy * dy > static_cast<double>(radius) * radius) {
                continue;
            }
            double lift = 0.0;
            if (ridges) {
                const double phase = 2.0 * kPi * (y - box.y) / kRidgePeriod + 0.4 * std::sin((x - box.x) / 30.0);
                lift = amplitude * std::sin(phase);
            }
            cv::Vec3b pixel;
            for (int c = 0; c < 3; ++c) {
                pixel[c] = cv::saturate_cast<unsigned char>(kSkin[c] + lift);
            }
            image.at<cv::Vec3b>(y, x) = pixel;
        }
    }
}

// A synthetic print for the full pipeline: a finger-shaped skin region filled
// with ridges, plus two deliberate breaks in the ridge pattern. A break ends
// two ridges, so the extractor has real endings to find.
cv::Mat print() {
    cv::Mat image = desk(320, 320);
    paintBlob(image, cv::Point(160, 160), cv::Size(110, 150), true, 22.0);
    // Two breaks: a vertical slice of the ridge pattern replaced by the valley
    // value, which is what turns two ridges into four endings.
    for (const cv::Rect &gap : {cv::Rect(150, 120, 12, 26), cv::Rect(196, 196, 12, 26)}) {
        for (int y = gap.y; y < gap.y + gap.height; ++y) {
            for (int x = gap.x; x < gap.x + gap.width; ++x) {
                const double dx = (x - 160) / 110.0;
                const double dy = (y - 160) / 150.0;
                if (dx * dx + dy * dy > 1.0) {
                    continue;
                }
                image.at<cv::Vec3b>(y, x) = cv::Vec3b(100, 140, 200);
            }
        }
    }
    return image;
}

// The skeleton the minutiae test is written against: one horizontal ridge with
// a branch hanging down from it. The junction is a bifurcation and the three
// loose ends are endings, at positions the test names.
cv::Mat skeleton() {
    cv::Mat image = cv::Mat::zeros(240, 320, CV_8UC1);
    // The ridge: y = 60, x from 60 to 200.
    cv::line(image, cv::Point(60, 60), cv::Point(200, 60), cv::Scalar(255), 1, cv::LINE_8);
    // The branch: x = 140, y from 60 down to 160.
    cv::line(image, cv::Point(140, 60), cv::Point(140, 160), cv::Scalar(255), 1, cv::LINE_8);
    return image;
}

} // namespace

int main(int argc, char **argv) {
    const std::string dir = argc > 1 ? argv[1] : "testdata";

    save(dir, "frame-background.png", desk(640, 480));

    cv::Mat hand = desk(640, 480);
    paintBlob(hand, cv::Point(320, 250), cv::Size(260, 95), false, 0.0);
    save(dir, "frame-hand.png", hand);

    cv::Mat finger = desk(640, 480);
    paintFinger(finger, cv::Rect(240, 40, 170, 400), true, 20.0);
    save(dir, "frame-finger.png", finger);
    save(dir, "frame-finger.jpg", finger);

    cv::Mat two = desk(640, 480);
    paintFinger(two, cv::Rect(110, 90, 110, 300), true, 20.0);
    paintFinger(two, cv::Rect(420, 90, 110, 300), true, 20.0);
    save(dir, "frame-two-fingers.png", two);

    save(dir, "print.png", print());
    save(dir, "skeleton.png", skeleton());
    return 0;
}
