// The C++ side of the zero-copy experiment: the same operation through each
// mapping the design table names, so docs/dev/stack-allocation.md can quote
// what the borrow costs and what the copy costs.
//
// Nothing here is shipped.

#include <cstddef>
#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

#if __cplusplus >= 202002L
#include <span>
#define CPPGO_HAS_SPAN 1
#else
#define CPPGO_HAS_SPAN 0
#endif

#define CPPGO_EXPORT extern "C" __attribute__((visibility("default")))

// ---------------------------------------------------------------------------
// []byte / string -> const char*, size_t: the borrow
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::uint64_t zc_bytes_borrowed(const std::uint8_t *data, std::uint64_t n) {
    std::uint64_t sum = 0;
    for (std::uint64_t i = 0; i < n; ++i) {
        sum += data[i];
    }
    return sum;
}

// The copy path for the same signature when the parameter is
// const std::vector<uint8_t>&: this is what a shim thunk does, one allocation
// and one copy before the user's function is reached.
CPPGO_EXPORT std::uint64_t zc_bytes_copied(const std::uint8_t *data, std::uint64_t n) {
    const std::vector<std::uint8_t> copied(data, data + n);
    std::uint64_t sum = 0;
    for (std::uint8_t byte : copied) {
        sum += byte;
    }
    return sum;
}

// ---------------------------------------------------------------------------
// []int -> const int*, size_t, and std::span<const int>
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::int64_t zc_ints_borrowed(const int *data, std::uint64_t n) {
    std::int64_t sum = 0;
    for (std::uint64_t i = 0; i < n; ++i) {
        sum += data[i];
    }
    return sum;
}

CPPGO_EXPORT std::int64_t zc_ints_copied(const int *data, std::uint64_t n) {
    const std::vector<int> copied(data, data + n);
    std::int64_t sum = 0;
    for (int value : copied) {
        sum += value;
    }
    return sum;
}

#if CPPGO_HAS_SPAN
// std::span<const int> is a pointer and a length: two registers in the System V
// classification, so a Go []int reaches it without a copy and without a thunk.
CPPGO_EXPORT std::int64_t zc_ints_span(std::span<const int> data) {
    std::int64_t sum = 0;
    for (int value : data) {
        sum += value;
    }
    return sum;
}

// The writable form: a []int the callee may write is std::span<int>, and the
// const-ness of the Go side is the whole safety argument.
CPPGO_EXPORT void zc_ints_span_scale(std::span<int> data, int factor) {
    for (int &value : data) {
        value *= factor;
    }
}

// A span built from the pointer and length inside the callee, for the negative
// control: the same work with the copy in between.
CPPGO_EXPORT std::int64_t zc_ints_span_from_vector(const int *data, std::uint64_t n) {
    const std::vector<int> copied(data, data + n);
    return zc_ints_span(std::span<const int>(copied));
}
#endif

// ---------------------------------------------------------------------------
// string -> std::string_view: the borrow, and std::string: the copy
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::uint64_t zc_string_view_length(const char *data, std::uint64_t n) {
#if CPPGO_HAS_SPAN
    const std::string_view view(data, n);
    return view.size();
#else
    (void)data;
    return n;
#endif
}

CPPGO_EXPORT std::uint64_t zc_string_length(const char *data, std::uint64_t n) {
    const std::string copied(data, n);
    return copied.size();
}

// ---------------------------------------------------------------------------
// std::array<T,N> <-> [N]T: the layout question, answered by the compiler
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::int64_t zc_array_sum(const int *data, std::uint64_t n) {
    std::int64_t sum = 0;
    for (std::uint64_t i = 0; i < n; ++i) {
        sum += data[i];
    }
    return sum;
}

CPPGO_EXPORT std::uint64_t zc_sizeof_array4(void) { return sizeof(std::array<int, 4>); }

CPPGO_EXPORT std::uint64_t zc_alignof_array4(void) { return alignof(std::array<int, 4>); }

// ---------------------------------------------------------------------------
// The mappings that cannot borrow: owned containers
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::uint64_t zc_vector_of_strings_total(const char *const *values, std::uint64_t n) {
    const std::vector<std::string> copied(values, values + n);
    std::uint64_t total = 0;
    for (const std::string &value : copied) {
        total += value.size();
    }
    return total;
}
