// shim.cpp — the hand-written cgo wrapper around the same libbench.so the
// cppgo arm calls.
//
// Every wrapper below mirrors the thunk cppgo generates for the same
// declaration: the same status/out-parameter shape, the same try/catch, the
// same copied result. That is the point of the comparison — the C++ work and
// the C ABI work are identical, and what differs is only how Go reaches the
// function.
#include "bench_c.h"

#include <bench/bench.hpp>

#include <cstdint>
#include <cstring>
#include <new>
#include <string>

namespace {

// One message per thread, like the cppgo shim's: the last failed call on this
// thread records it and the Go side reads it right after the status.
thread_local std::string bench_error_message;

// Returns the failure of the current call. Kept separate so the wrappers read
// like the generated thunks.
template <typename Call>
std::int32_t guarded(Call &&call) {
    bench_error_message.clear();
    try {
        call();
        return 0;
    } catch (const std::exception &error) {
        bench_error_message = error.what();
        return 1;
    } catch (...) {
        bench_error_message = "unknown C++ exception";
        return 1;
    }
}

} // namespace

// goFoldCallback is the Go function cgo exports (see export.go). Declaring it
// here is the whole callback mechanism: the shim hands its address to
// std::function exactly as cppgo's thunk does.
extern "C" int32_t goFoldCallback(int32_t a, int32_t b);

extern "C" {

const char *bench_last_error(void) { return bench_error_message.c_str(); }

void bench_free(void *block) { ::operator delete[](block); }

int64_t bench_add(int64_t a, int64_t b) { return ::bench::add(a, b); }

int32_t bench_add_thunk(int64_t a, int64_t b, int64_t *out) {
    return guarded([&] { *out = ::bench::add_thunk(a, b); });
}

int32_t bench_midpoint(struct bench_point a, struct bench_point b, struct bench_point *out) {
    return guarded([&] {
        const bench::Point result =
            ::bench::midpoint(bench::Point{a.x, a.y}, bench::Point{b.x, b.y});
        out->x = result.x;
        out->y = result.y;
    });
}

// blend is noexcept in C++, so the thunk cppgo generates for it carries no
// status either; the wrapper still exists to convert the layout.
void bench_blend(struct bench_sample a, struct bench_sample b, double t, struct bench_sample *out) {
    const bench::Sample result =
        ::bench::blend(bench::Sample{a.a, a.b, a.c}, bench::Sample{b.a, b.b, b.c}, t);
    out->a = result.a;
    out->b = result.b;
    out->c = result.c;
}

int32_t bench_describe(const char *prefix_data, size_t prefix_size, double value, void **out_data,
                       size_t *out_size) {
    return guarded([&] {
        const std::string result = ::bench::describe(std::string_view(prefix_data, prefix_size), value);
        const size_t size = result.size();
        char *copy = static_cast<char *>(::operator new[](size == 0 ? 1 : size));
        if (size != 0) {
            std::memcpy(copy, result.data(), size);
        }
        *out_data = copy;
        *out_size = size;
    });
}

int32_t bench_doubled(const double *values_data, size_t values_size, void **out_data,
                      size_t *out_size) {
    return guarded([&] {
        std::vector<double> values;
        if (values_size != 0) {
            values.assign(values_data, values_data + values_size);
        }
        const std::vector<double> result = ::bench::doubled(values);
        const size_t size = result.size();
        double *copy = static_cast<double *>(
            ::operator new[]((size == 0 ? 1 : size) * sizeof(double)));
        for (size_t i = 0; i < size; ++i) {
            copy[i] = result[i];
        }
        *out_data = copy;
        *out_size = size;
    });
}

int32_t bench_ratio(int32_t a, int32_t b, int32_t *out) {
    return guarded([&] { *out = ::bench::ratio(a, b); });
}

int32_t bench_parse(const char *text_data, size_t text_size) {
    // The status convention: the caller's integer is the failure, and the
    // message beside it is empty because the library did not throw.
    bench_error_message.clear();
    try {
        const std::string text(text_data, text_size);
        return ::bench::parse(text);
    } catch (const std::exception &error) {
        bench_error_message = error.what();
        return 1;
    } catch (...) {
        bench_error_message = "unknown C++ exception";
        return 1;
    }
}

int32_t bench_fold(int32_t a, int32_t b, int32_t *out) {
    return guarded([&] {
        const std::function<std::int32_t(std::int32_t, std::int32_t)> fn = goFoldCallback;
        *out = ::bench::fold(fn, a, b);
    });
}

void *bench_counter_new(int64_t start) {
    bench_error_message.clear();
    try {
        return new ::bench::Counter(start);
    } catch (const std::exception &error) {
        bench_error_message = error.what();
        return nullptr;
    } catch (...) {
        bench_error_message = "unknown C++ exception";
        return nullptr;
    }
}

void bench_counter_free(void *counter) { delete static_cast<::bench::Counter *>(counter); }

int64_t bench_counter_get(void *counter) {
    return static_cast<::bench::Counter *>(counter)->get();
}

void bench_counter_bump(void *counter, int64_t by) {
    static_cast<::bench::Counter *>(counter)->bump(by);
}

} // extern "C"
