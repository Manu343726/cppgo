#pragma once

#include <vector>

// The proxy fixture: a declaration whose parameter is a stack object the
// library's own API uses to pass arrays in and out, and which has no Go shape of
// its own. The stand-in has the shape OpenCV's _InputArray/_OutputArray have:
// a constructor from the container, and only the constructor is templated.
namespace prx {

/// A mirrored record, the element of the container the proxy carries.
struct Point {
    int x;
    int y;
};

/// Stands in for _OutputArray: it keeps a pointer to the vector it writes.
class Sink {
public:
    template <typename T>
    Sink(std::vector<std::vector<T>> &values) noexcept;
};

/// Stands in for _InputArray: it reads the container it was built from.
class Source {
public:
    template <typename T>
    Source(const std::vector<std::vector<T>> &values) noexcept;
};

/// Writes its result into the container the proxy was built around.
void trace(const Sink &sink) noexcept;

/// Reads the container the proxy was built around.
int total(const Source &source) noexcept;

/// A declaration whose second parameter is an ordinary scalar: adapting it with
/// -proxy is a manifest mistake the generator names.
void traceWithFlags(const Sink &sink, int flags) noexcept;

} // namespace prx
