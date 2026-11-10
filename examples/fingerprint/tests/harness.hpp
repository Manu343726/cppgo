#pragma once

// The tests' own harness: a few macros and a counter. The example has no test
// framework on purpose — a dependency that has to be fetched to run `make` is a
// dependency the reader has to install, and what these tests need is a line
// number and a value.

#include <cmath>
#include <cstdio>
#include <cstdlib>
#include <string>

#include <opencv2/core.hpp>
#include <opencv2/imgcodecs.hpp>

namespace harness {

inline int failures = 0;
inline int checks = 0;
inline std::string directory;

inline void check(bool condition, const char *expression, const char *file, int line) {
    ++checks;
    if (!condition) {
        ++failures;
        std::fprintf(stderr, "%s:%d: CHECK(%s) failed\n", file, line, expression);
    }
}

inline void checkNear(double got, double want, double tolerance, const char *expression, const char *file, int line) {
    ++checks;
    if (std::fabs(got - want) > tolerance) {
        ++failures;
        std::fprintf(stderr, "%s:%d: CHECK_NEAR(%s) got %.6f want %.6f (tolerance %.6f)\n", file, line, expression, got,
                     want, tolerance);
    }
}

inline void section(const char *name) { std::printf("-- %s\n", name); }

// A committed fixture, by file name.
inline cv::Mat fixture(const std::string &name) {
    const cv::Mat image = cv::imread(directory + "/" + name, cv::IMREAD_COLOR);
    if (image.empty()) {
        std::fprintf(stderr, "the fixture %s/%s could not be read; run \"make fixtures\"\n", directory.c_str(),
                     name.c_str());
        std::exit(1);
    }
    return image;
}

inline std::string fixturePath(const std::string &name) { return directory + "/" + name; }

} // namespace harness

#define CHECK(expression) ::harness::check((expression), #expression, __FILE__, __LINE__)
#define CHECK_NEAR(got, want, tolerance) \
    ::harness::checkNear((got), (want), (tolerance), #got " == " #want, __FILE__, __LINE__)
#define SECTION(name) ::harness::section(name)
#define CHECK_THROWS(expression, type)                                                                        \
    do {                                                                                                      \
        bool caught = false;                                                                                  \
        try {                                                                                                 \
            (void)(expression);                                                                               \
        } catch (const type &) {                                                                               \
            caught = true;                                                                                    \
        } catch (...) {                                                                                       \
        }                                                                                                     \
        ::harness::check(caught, #expression " throws " #type, __FILE__, __LINE__);                            \
    } while (false)
