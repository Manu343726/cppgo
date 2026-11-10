#pragma once

// pipeline — the image pipeline of the fingerprint example, over cv::Mat.
//
// This header is the *C++* half of the library: it is what the C++ tests call
// and what `src/*.cpp` implements. It is deliberately *not* the header `cppgo
// gen` reads (that is `fingerprint.hpp`): every declaration here takes or
// returns a `cv::Mat`, and a matrix is the one thing the Go side of this
// example never touches. The Go program hands the library the bytes of an
// encoded frame and gets bytes, verdicts and timings back; the pixels stay on
// this side of the boundary.
//
// The pipeline is the classic one of a minutiae-based fingerprint matcher:
//
//     photo ──segment──▶ mask ──(ROI)──▶ gray
//     gray ──normalize──▶ normalized ──orientationField──▶ orientation
//     normalized, orientation ──gaborEnhance──▶ enhanced
//     enhanced ──binarize──▶ binary ──thin──▶ skeleton
//     enhanced ──extractMinutiae──▶ minutiae + ridge frequency
//
// Every constant is named, because the tests assert against them.

#include <cstdint>
#include <string>
#include <vector>

#include <opencv2/core.hpp>

namespace fingerprint {

// A rectangle in source pixel coordinates. `width` and `height` are counts, so
// the last column a region covers is x + width - 1.
struct Region {
    int x;
    int y;
    int width;
    int height;
};

// --- 1. segmentation --------------------------------------------------------

// The skin/ridge mask of a finger: 255 where the photo holds finger-bearing
// skin, 0 elsewhere. The mask has the photo's size and type CV_8UC1, and holds
// the single largest connected skin region — a hand in the background is not
// the finger.
//
// The skin test is the YCrCb box of Chai & Ngan, "Face segmentation using
// skin-color map in videophone applications", IEEE TCSVT 9(4), 1999:
//
//     Cr in [133, 173], Cb in [77, 127]
//
// The constants are exact integers and are asserted by the C++ tests.
cv::Mat segment(const cv::Mat &photo);

// The bounding box of the mask's non-zero pixels. An empty region (all fields
// 0) when the mask is empty.
Region largestRegion(const cv::Mat &mask);

// --- 2. normalisation -------------------------------------------------------

// Mean/variance normalisation of a grey image: CV_32FC1 with exactly the given
// mean and standard deviation, whatever the input had. A zero-variance input
// (a flat image) yields a flat image of the target mean, not a division by
// zero.
//
// The result is floating point because the Gabor stage reads it as a real
// signal: rounding to eight bits here would clip the tails of the distribution
// and hand the bank an image whose statistics are no longer the ones asked for.
cv::Mat normalize(const cv::Mat &gray, double mean, double sigma);

// The defaults this example uses, named so the tests and the report agree.
inline constexpr double kNormalizeMean = 100.0;
inline constexpr double kNormalizeSigma = 100.0;

// --- 3. orientation ---------------------------------------------------------

// The local ridge orientation of an 8-bit grey image, smoothed over `block`
// pixels: CV_32FC1, radians in [0, pi). A ridge and the valley beside it have
// the same orientation, so the field is a half-turn quantity.
cv::Mat orientationField(const cv::Mat &gray, int block);

inline constexpr int kOrientationBlock = 16;

// --- 4. enhancement ---------------------------------------------------------

// Gabor enhancement along an orientation field: an even-symmetric Gabor bank of
// kGaborOrientations directions is applied to the normalised image and the
// strongest response at each pixel is kept. The result is CV_8UC1, scaled so
// the ridge structure fills the 8-bit range.
//
// `frequency` is cycles per pixel, from ridgeFrequency().
cv::Mat gaborEnhance(const cv::Mat &gray, const cv::Mat &orientation, double frequency);

inline constexpr int kGaborOrientations = 8;
inline constexpr int kGaborKernel = 9;
inline constexpr double kGaborSigma = 2.0;
inline constexpr double kGaborGamma = 0.5;

// --- 5. binarisation --------------------------------------------------------

// Otsu threshold of the enhanced image: CV_8UC1 holding only 0 and 255. The
// test asserts exactly that, because a downstream thinning step reads the
// image as a boolean.
cv::Mat binarize(const cv::Mat &enhanced);

// --- 6. thinning ------------------------------------------------------------

// Zhang-Suen thinning of a binarised image: CV_8UC1 holding only 0 and 1, every
// ridge one pixel wide. An input that is already a one-pixel skeleton comes out
// unchanged, which is what lets the minutiae test feed it a hand-drawn one.
cv::Mat thin(const cv::Mat &binary);

inline constexpr int kMaxThinningIterations = 100;

// --- 7. minutiae ------------------------------------------------------------

// The two minutia types of ANSI/INCITS 378. The values are the standard's
// `MINUTIAE_TYPE` field: 0 = ridge ending, 1 = ridge bifurcation.
enum MinutiaType : std::uint8_t {
    MINUTIA_ENDING = 0,
    MINUTIA_BIFURCATION = 1,
};

// One minutia, in source pixel coordinates of the image extractMinutiae() was
// called on.
struct Minutia {
    int x;              // column
    int y;              // row
    double angle;       // ridge direction at the minutia, radians in [0, 2*pi)
    int type;           // MinutiaType
    int quality;        // 0..100
    int ridge_count;    // ridges crossed from this minutia to the next one
};

// Minutiae of an enhanced image, by the Rutovitz crossing number on its
// thinned skeleton: a ridge pixel whose eight neighbours are traversed in order
// and yield a crossing number of 1 is a ridge ending, and one that yields 3 is
// a bifurcation. Pixels within kMinutiaBorder of the image edge are dropped
// (the skeleton is unreliable there) and minutiae closer than kMinutiaMerge to
// each other are merged.
//
// The result is sorted by (y, x), which is what makes the serialisation
// byte-for-byte reproducible.
std::vector<Minutia> extractMinutiae(const cv::Mat &enhanced);

inline constexpr int kMinutiaBorder = 10;
inline constexpr double kMinutiaMerge = 4.0;
inline constexpr int kRidgeCountReach = 12;

// The ridge frequency of an enhanced image in cycles per pixel, measured as the
// mean number of ridge crossings per pixel along the image's row and column
// axes and averaged. A synthetic print whose period is kRidgePeriodPixels comes
// out as 1/kRidgePeriodPixels.
double ridgeFrequency(const cv::Mat &enhanced);

inline constexpr int kRidgePeriodPixels = 10;

// --- 8. the template --------------------------------------------------------

// Everything a template record carries.
struct TemplateRecord {
    int width;                  // source image width in pixels
    int height;                 // source image height in pixels
    double ridge_frequency;     // cycles per pixel
    std::vector<Minutia> minutiae;
};

// The ANSI/INCITS 378 subset this example defines: see ansi378.cpp for the
// layout, and for which fields of the standard the subset carries and which it
// omits.
std::vector<std::uint8_t> toAnsi378(const TemplateRecord &record);

// The inverse. Throws std::invalid_argument when the bytes are not this
// subset's record.
TemplateRecord fromAnsi378(const std::vector<std::uint8_t> &bytes);

// A textual listing of a template, one line per field and per minutia.
std::string describeAnsi378(const std::vector<std::uint8_t> &bytes);

} // namespace fingerprint
