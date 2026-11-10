#pragma once

#include <vector>

// The recursive-container fixture: every shape a std::vector can take across the
// boundary — a vector of vectors of a scalar, of a mirrored record, a vector of
// opaque records, and each of them as an input, an out-parameter and a result.
namespace containers {

/// A mirrored record: its Go value is the C++ layout, so a vector of it is a
/// flat Go slice.
struct Point {
    int x;
    int y;
};

/// An opaque record: Go owns each one, so a vector of it crosses as element
/// addresses in and a block of views out.
class Counter {
public:
    Counter(int start) noexcept;
    ~Counter();
    int value() const noexcept;

private:
    int value_;
};

/// The plain shapes: a vector of a scalar and a vector of a mirrored record.
int total(const std::vector<int> &values) noexcept;
void fill(std::vector<int> &values);
std::vector<int> sequence(int count) noexcept;
int totalPoints(const std::vector<Point> &points) noexcept;
std::vector<Point> points(int count) noexcept;

/// The nested shapes: a vector of vectors, which crosses as one element buffer
/// plus the bounds of every inner vector.
int totalNested(const std::vector<std::vector<int>> &values) noexcept;
void fillNested(std::vector<std::vector<int>> &values);
std::vector<std::vector<int>> nested(int count) noexcept;
int totalNestedPoints(const std::vector<std::vector<Point>> &points) noexcept;
std::vector<std::vector<Point>> nestedPoints(int count) noexcept;

/// The opaque shapes: a vector of records, which crosses as the element
/// addresses in and a block of objects out.
int totalCounters(const std::vector<Counter> &counters) noexcept;
void addCounter(std::vector<Counter> &counters);
std::vector<Counter> counters(int count);

} // namespace containers
