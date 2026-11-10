// The image pipeline: segmentation, normalisation, orientation, Gabor
// enhancement, binarisation, thinning, minutiae and ridge frequency.
//
// Every step is a separate function over cv::Mat so the C++ tests can assert
// each one on its own, and the whole sequence is what `enroll()` walks in
// session.cpp. The constants the tests assert against are in pipeline.hpp.

#include "fingerprint/pipeline.hpp"

#include <algorithm>
#include <cmath>
#include <cstdlib>
#include <stdexcept>
#include <vector>

#include <opencv2/imgproc.hpp>

namespace fingerprint {
namespace {

// The Chan/Ngan skin box in YCrCb, in the channel order OpenCV's YCrCb uses
// (Y, Cr, Cb).
constexpr double kSkinCrMin = 133.0;
constexpr double kSkinCrMax = 173.0;
constexpr double kSkinCbMin = 77.0;
constexpr double kSkinCbMax = 127.0;

// The kernel of the two morphology passes segment() runs on the mask: a close
// to bridge the gaps between ridges, an open to drop single-pixel speckle.
constexpr int kCloseKernel = 9;
constexpr int kOpenKernel = 5;

} // namespace

cv::Mat segment(const cv::Mat &photo) {
    if (photo.empty()) {
        throw std::invalid_argument("fingerprint::segment: the photo is empty");
    }
    cv::Mat ycc;
    cv::cvtColor(photo, ycc, cv::COLOR_BGR2YCrCb);

    cv::Mat mask;
    cv::inRange(ycc, cv::Scalar(0.0, kSkinCrMin, kSkinCbMin), cv::Scalar(255.0, kSkinCrMax, kSkinCbMax), mask);

    const cv::Mat close = cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(kCloseKernel, kCloseKernel));
    const cv::Mat open = cv::getStructuringElement(cv::MORPH_ELLIPSE, cv::Size(kOpenKernel, kOpenKernel));
    cv::morphologyEx(mask, mask, cv::MORPH_CLOSE, close);
    cv::morphologyEx(mask, mask, cv::MORPH_OPEN, open);

    // The finger is the largest connected skin region: a hand at the edge of
    // the frame is skin too, and it must not become the ROI.
    cv::Mat labels, stats, centroids;
    const int count = cv::connectedComponentsWithStats(mask, labels, stats, centroids, 8, CV_32S);
    int best = -1;
    int bestArea = 0;
    for (int label = 1; label < count; ++label) {
        const int area = stats.at<int>(label, cv::CC_STAT_AREA);
        if (area > bestArea) {
            bestArea = area;
            best = label;
        }
    }
    if (best < 0) {
        return cv::Mat::zeros(photo.size(), CV_8U);
    }
    cv::Mat selected = (labels == best);
    return selected; // CV_8UC1, 255 where the finger is
}

Region largestRegion(const cv::Mat &mask) {
    Region region{0, 0, 0, 0};
    if (mask.empty()) {
        return region;
    }
    const cv::Rect box = cv::boundingRect(mask);
    region.x = box.x;
    region.y = box.y;
    region.width = box.width;
    region.height = box.height;
    return region;
}

cv::Mat normalize(const cv::Mat &gray, double mean, double sigma) {
    cv::Mat gray32;
    gray.convertTo(gray32, CV_32F);
    cv::Scalar gotMean, gotSigma;
    cv::meanStdDev(gray32, gotMean, gotSigma);
    if (gotSigma[0] < 1e-6) {
        // A flat image has no variance to stretch; the target mean is the
        // honest answer, and dividing by zero would not be.
        return cv::Mat(gray.size(), CV_32F, cv::Scalar(mean));
    }
    // The result stays in floating point on purpose: the normalisation is the
    // one of Hong, Wan and Jain, "Fingerprint image enhancement: algorithm and
    // performance evaluation" (IEEE TPAMI 20(8), 1998), whose Gabor stage reads
    // it as real values. Rounding it to eight bits here would clip the tails
    // and hand the Gabor bank an image whose mean and deviation are no longer
    // the ones asked for.
    return (gray32 - gotMean[0]) * (sigma / gotSigma[0]) + mean;
}

cv::Mat orientationField(const cv::Mat &gray, int block) {
    if (block < 1) {
        throw std::invalid_argument("fingerprint::orientationField: the block size is not positive");
    }
    cv::Mat gray32;
    gray.convertTo(gray32, CV_32F);
    cv::Mat gx, gy;
    cv::Sobel(gray32, gx, CV_32F, 1, 0, 3);
    cv::Sobel(gray32, gy, CV_32F, 0, 1, 3);

    // The doubled-angle trick: 2*gx*gy and gx^2-gy^2 are the components of a
    // field whose argument is twice the ridge orientation, so averaging them
    // over a block does not cancel opposite ridge directions. The first is the
    // sine component and the second the cosine one, which is the order
    // atan2() below reads: theta = 0.5 * atan2(2*gx*gy, gx^2-gy^2).
    cv::Mat sine = 2.0f * gx.mul(gy);
    cv::Mat cosine = gx.mul(gx) - gy.mul(gy);
    cv::boxFilter(sine, sine, -1, cv::Size(block, block));
    cv::boxFilter(cosine, cosine, -1, cv::Size(block, block));

    cv::Mat theta(gray.size(), CV_32F);
    for (int y = 0; y < theta.rows; ++y) {
        const float *rowSine = sine.ptr<float>(y);
        const float *rowCosine = cosine.ptr<float>(y);
        float *out = theta.ptr<float>(y);
        for (int x = 0; x < theta.cols; ++x) {
            double angle = 0.5 * std::atan2(static_cast<double>(rowSine[x]), static_cast<double>(rowCosine[x]));
            if (angle < 0.0) {
                angle += CV_PI;
            }
            out[x] = static_cast<float>(angle);
        }
    }
    return theta;
}


cv::Mat gaborEnhance(const cv::Mat &gray, const cv::Mat &orientation, double frequency) {
    if (frequency <= 0.0) {
        throw std::invalid_argument("fingerprint::gaborEnhance: the ridge frequency is not positive");
    }
    cv::Mat gray32;
    gray.convertTo(gray32, CV_32F);

    // The block's orientation quantised to the bank's directions: an index per
    // pixel, so each bank response is kept only where it is the right one.
    cv::Mat direction(gray.size(), CV_8U);
    for (int y = 0; y < orientation.rows; ++y) {
        const float *row = orientation.ptr<float>(y);
        unsigned char *out = direction.ptr<unsigned char>(y);
        for (int x = 0; x < orientation.cols; ++x) {
            int index = static_cast<int>(std::lround(row[x] * kGaborOrientations / CV_PI));
            out[x] = static_cast<unsigned char>(index % kGaborOrientations);
        }
    }

    // Exactly one bank direction is kept at each pixel, so the accumulated
    // response is that one kernel's value and not a maximum over eight of them:
    // a maximum would clip every valley to zero and leave a mask that is mostly
    // "ridge".
    cv::Mat response = cv::Mat::zeros(gray.size(), CV_32F);
    for (int k = 0; k < kGaborOrientations; ++k) {
        const double theta = k * CV_PI / kGaborOrientations;
        cv::Mat kernel = cv::getGaborKernel(cv::Size(kGaborKernel, kGaborKernel), kGaborSigma, theta,
                                            1.0 / frequency, kGaborGamma, 0.0, CV_32F);
        // A truncated Gabor kernel does not sum to zero, and the image it is
        // applied to has a mean of kNormalizeMean: without this the kernel's DC
        // gain multiplies that mean and the response is mostly positive, which
        // makes the Otsu threshold below meaningless. Removing the mean is what
        // makes the response a ridge/valley signal around zero.
        kernel -= cv::mean(kernel)[0];
        cv::Mat filtered;
        cv::filter2D(gray32, filtered, CV_32F, kernel, cv::Point(-1, -1), 0.0, cv::BORDER_REFLECT);

        cv::Mat chosen = cv::Mat::zeros(gray.size(), CV_32F);
        cv::Mat matches;
        cv::compare(direction, k, matches, cv::CMP_EQ);
        filtered.copyTo(chosen, matches);
        cv::add(response, chosen, response);
    }

    // The response is signed: a ridge is positive under its kernel and the
    // valley beside it negative. The scale maps both onto the 8-bit range, so
    // the Otsu threshold that follows falls between them.
    double low = 0.0;
    double high = 0.0;
    cv::minMaxLoc(response, &low, &high);
    if (high - low < 1e-6) {
        return cv::Mat::zeros(gray.size(), CV_8U);
    }
    const double scale = 255.0 / (high - low);
    cv::Mat out;
    response.convertTo(out, CV_8U, scale, -low * scale);
    return out;
}

cv::Mat binarize(const cv::Mat &enhanced) {
    cv::Mat source;
    if (enhanced.type() == CV_8U) {
        source = enhanced;
    } else {
        enhanced.convertTo(source, CV_8U);
    }
    cv::Mat binary;
    cv::threshold(source, binary, 0.0, 255.0, cv::THRESH_BINARY | cv::THRESH_OTSU);
    return binary;
}

cv::Mat thin(const cv::Mat &binary) {
    if (binary.empty()) {
        return cv::Mat();
    }
    // Work on 0/1 so the neighbour sums below are the pixel count.
    cv::Mat image;
    cv::compare(binary, 0, image, cv::CMP_GT); // 0 or 255
    image.convertTo(image, CV_8U, 1.0 / 255.0);

    bool changed = true;
    int iterations = 0;
    while (changed && iterations < kMaxThinningIterations) {
        changed = false;
        ++iterations;
        for (int step = 0; step < 2; ++step) {
            // Removing in place while scanning would let one removal change the
            // neighbours of the next pixel, so a pass collects first.
            std::vector<cv::Point> remove;
            for (int y = 1; y + 1 < image.rows; ++y) {
                const unsigned char *above = image.ptr<unsigned char>(y - 1);
                const unsigned char *row = image.ptr<unsigned char>(y);
                const unsigned char *below = image.ptr<unsigned char>(y + 1);
                for (int x = 1; x + 1 < image.cols; ++x) {
                    if (!row[x]) {
                        continue;
                    }
                    // P2..P9, clockwise from the north.
                    const int p2 = above[x];
                    const int p3 = above[x + 1];
                    const int p4 = row[x + 1];
                    const int p5 = below[x + 1];
                    const int p6 = below[x];
                    const int p7 = below[x - 1];
                    const int p8 = row[x - 1];
                    const int p9 = above[x - 1];

                    const int neighbours = p2 + p3 + p4 + p5 + p6 + p7 + p8 + p9;
                    if (neighbours < 2 || neighbours > 6) {
                        continue;
                    }
                    int transitions = 0;
                    if (!p2 && p3) ++transitions;
                    if (!p3 && p4) ++transitions;
                    if (!p4 && p5) ++transitions;
                    if (!p5 && p6) ++transitions;
                    if (!p6 && p7) ++transitions;
                    if (!p7 && p8) ++transitions;
                    if (!p8 && p9) ++transitions;
                    if (!p9 && p2) ++transitions;
                    if (transitions != 1) {
                        continue;
                    }
                    if (step == 0) {
                        if (p2 * p4 * p6 != 0 || p4 * p6 * p8 != 0) {
                            continue;
                        }
                    } else {
                        if (p2 * p4 * p8 != 0 || p2 * p6 * p8 != 0) {
                            continue;
                        }
                    }
                    remove.emplace_back(x, y);
                }
            }
            for (const cv::Point &p : remove) {
                image.at<unsigned char>(p) = 0;
            }
            if (!remove.empty()) {
                changed = true;
            }
        }
    }
    return image; // 0/1
}

namespace {

// The eight neighbours, clockwise from the east, which is the order the
// crossing number needs to be counted in.
constexpr int kNeighbourX[8] = {1, 1, 0, -1, -1, -1, 0, 1};
constexpr int kNeighbourY[8] = {0, -1, -1, -1, 0, 1, 1, 1};

bool inside(const cv::Mat &image, int x, int y) {
    return x >= 0 && y >= 0 && x < image.cols && y < image.rows;
}

// The Rutovitz crossing number of a skeleton pixel: half the number of
// foreground/background transitions around its eight neighbours. One is an
// ending, three a bifurcation, two an ordinary ridge pixel.
int crossingNumber(const cv::Mat &skeleton, int x, int y) {
    int value[8];
    for (int i = 0; i < 8; ++i) {
        const int nx = x + kNeighbourX[i];
        const int ny = y + kNeighbourY[i];
        value[i] = inside(skeleton, nx, ny) ? (skeleton.at<unsigned char>(ny, nx) ? 1 : 0) : 0;
    }
    int transitions = 0;
    for (int i = 0; i < 8; ++i) {
        transitions += std::abs(value[i] - value[(i + 1) % 8]);
    }
    return transitions / 2;
}

// The direction the ridge takes at a minutia: away from the neighbour(s) it is
// attached to, in radians in [0, 2*pi). An ending has one neighbour and the
// direction points past the end of the ridge; a bifurcation has three, and
// their mean points back down the trunk.
double minutiaAngle(const cv::Mat &skeleton, int x, int y) {
    double sumX = 0.0;
    double sumY = 0.0;
    for (int i = 0; i < 8; ++i) {
        const int nx = x + kNeighbourX[i];
        const int ny = y + kNeighbourY[i];
        if (inside(skeleton, nx, ny) && skeleton.at<unsigned char>(ny, nx)) {
            sumX += kNeighbourX[i];
            sumY += kNeighbourY[i];
        }
    }
    double angle = std::atan2(-sumY, -sumX);
    if (angle < 0.0) {
        angle += 2.0 * CV_PI;
    }
    return angle;
}

// How many ridges lie between a minutia and the end of kRidgeCountReach pixels
// across them: the skeleton is sampled along the direction perpendicular to the
// ridge and every background-to-ridge transition counts one.
int ridgeCount(const cv::Mat &skeleton, int x, int y, double angle) {
    const double acrossX = -std::sin(angle);
    const double acrossY = std::cos(angle);
    int count = 0;
    int previous = 1; // the minutia itself is on a ridge
    for (int step = 1; step <= kRidgeCountReach; ++step) {
        const int sx = static_cast<int>(std::lround(x + acrossX * step));
        const int sy = static_cast<int>(std::lround(y + acrossY * step));
        if (!inside(skeleton, sx, sy)) {
            break;
        }
        const int current = skeleton.at<unsigned char>(sy, sx) ? 1 : 0;
        if (!previous && current) {
            ++count;
        }
        previous = current;
    }
    return count;
}

// The ridge strength around a minutia, as the mean of the enhanced image in a
// five-pixel window, in the 0..100 range the template's quality field uses.
int minutiaQuality(const cv::Mat &enhanced, int x, int y) {
    int sum = 0;
    int count = 0;
    for (int dy = -2; dy <= 2; ++dy) {
        for (int dx = -2; dx <= 2; ++dx) {
            const int sx = x + dx;
            const int sy = y + dy;
            if (!inside(enhanced, sx, sy)) {
                continue;
            }
            sum += enhanced.at<unsigned char>(sy, sx);
            ++count;
        }
    }
    if (count == 0) {
        return 0;
    }
    return std::clamp(sum * 100 / (count * 255), 0, 100);
}

} // namespace

std::vector<Minutia> extractMinutiae(const cv::Mat &enhanced) {
    cv::Mat binary = binarize(enhanced);
    cv::Mat skeleton = thin(binary);

    std::vector<Minutia> found;
    for (int y = kMinutiaBorder; y + kMinutiaBorder < skeleton.rows; ++y) {
        for (int x = kMinutiaBorder; x + kMinutiaBorder < skeleton.cols; ++x) {
            if (!skeleton.at<unsigned char>(y, x)) {
                continue;
            }
            const int crossings = crossingNumber(skeleton, x, y);
            if (crossings != 1 && crossings != 3) {
                continue;
            }
            Minutia minutia{};
            minutia.x = x;
            minutia.y = y;
            minutia.angle = minutiaAngle(skeleton, x, y);
            minutia.type = crossings == 1 ? MINUTIA_ENDING : MINUTIA_BIFURCATION;
            minutia.quality = minutiaQuality(enhanced, x, y);
            minutia.ridge_count = ridgeCount(skeleton, x, y, minutia.angle);
            found.push_back(minutia);
        }
    }

    // Thinning leaves clusters of the same minutia; keep one per cluster, the
    // strongest, and sort what survives so the serialisation is reproducible.
    std::sort(found.begin(), found.end(), [](const Minutia &a, const Minutia &b) {
        return a.quality > b.quality || (a.quality == b.quality && a.y < b.y);
    });
    std::vector<Minutia> kept;
    for (const Minutia &candidate : found) {
        bool merged = false;
        for (const Minutia &accepted : kept) {
            const double dx = candidate.x - accepted.x;
            const double dy = candidate.y - accepted.y;
            if (std::sqrt(dx * dx + dy * dy) < kMinutiaMerge) {
                merged = true;
                break;
            }
        }
        if (!merged) {
            kept.push_back(candidate);
        }
    }
    std::sort(kept.begin(), kept.end(), [](const Minutia &a, const Minutia &b) {
        return a.y < b.y || (a.y == b.y && a.x < b.x);
    });
    return kept;
}

double ridgeFrequency(const cv::Mat &enhanced) {
    const cv::Mat binary = binarize(enhanced);
    if (binary.empty()) {
        return 0.0;
    }
    const int top = binary.rows / 4;
    const int bottom = binary.rows - top;
    const int left = binary.cols / 4;
    const int right = binary.cols - left;
    const double rowLength = static_cast<double>(right - left);
    const double columnLength = static_cast<double>(bottom - top);
    if (rowLength <= 0.0 || columnLength <= 0.0) {
        return 0.0;
    }

    // Cycles per pixel along each axis: the ridge starts on a scan line,
    // divided by the length of the scan line. Both axes are measured and
    // averaged, so an anisotropic ridge pattern is not read as a different
    // frequency depending on which way it happens to run.
    double rows = 0.0;
    int rowSamples = 0;
    for (int y = top; y < bottom; ++y) {
        const unsigned char *row = binary.ptr<unsigned char>(y);
        int transitions = 0;
        for (int x = left + 1; x < right; ++x) {
            if (!row[x - 1] && row[x]) {
                ++transitions;
            }
        }
        rows += static_cast<double>(transitions) / rowLength;
        ++rowSamples;
    }
    double columns = 0.0;
    int columnSamples = 0;
    for (int x = left; x < right; ++x) {
        int transitions = 0;
        for (int y = top + 1; y < bottom; ++y) {
            if (!binary.at<unsigned char>(y - 1, x) && binary.at<unsigned char>(y, x)) {
                ++transitions;
            }
        }
        columns += static_cast<double>(transitions) / columnLength;
        ++columnSamples;
    }
    const double rowFrequency = rowSamples > 0 ? rows / rowSamples : 0.0;
    const double columnFrequency = columnSamples > 0 ? columns / columnSamples : 0.0;
    // The axis that crosses the ridges is the one that sees them; the other one
    // runs along them and sees almost nothing. Taking the larger of the two is
    // what makes the measurement independent of the ridge direction, and it is
    // why a perfect grating reads as its own period and not half of it.
    return std::max(rowFrequency, columnFrequency);
}

} // namespace fingerprint
