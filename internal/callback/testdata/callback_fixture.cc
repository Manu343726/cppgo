// The C++ side of the callback prototype: ordinary C++17, built with the
// system compiler into a shared object, with no cppgo-generated code in it.
//
// Every entry point here is what a user's library looks like from cppgo's
// point of view: functions that take a function pointer, a std::function, or
// that throw. The Go side binds them by their exported (mangled) names.

#include <cstdint>
#include <cstring>
#include <functional>
#include <stdexcept>

#define CPPGO_EXPORT extern "C" __attribute__((visibility("default")))

// A plain function pointer: the C-ABI shape cppgo passes as an integer
// register.
typedef std::int32_t (*binop)(std::int32_t, std::int32_t);
typedef double (*dblop)(double, double);
typedef void (*sink)(void *user, std::int32_t value);

CPPGO_EXPORT std::int32_t cb_apply(binop fn, std::int32_t a, std::int32_t b) {
    return fn(a, b);
}

CPPGO_EXPORT std::int32_t cb_apply_twice(binop fn, std::int32_t a, std::int32_t b) {
    return fn(fn(a, b), b);
}

CPPGO_EXPORT double cb_apply_double(dblop fn, double a, double b) {
    return fn(a, b);
}

CPPGO_EXPORT void cb_call_sink(sink fn, void *user, std::int32_t value) {
    fn(user, value);
}

// A std::function the Go side has to construct: the callable is the callback
// address, and the type is the one the user's library declares.
typedef std::function<std::int32_t(std::int32_t, std::int32_t)> int_binop;

CPPGO_EXPORT std::int32_t cb_apply_std_function(const int_binop &fn, std::int32_t a,
                                                std::int32_t b) {
    return fn(a, b);
}

// Calling the callback and then throwing, with the callback already returned:
// the throw happens in the C++ frame this function owns.
CPPGO_EXPORT std::int32_t cb_call_then_throw(binop fn, std::int32_t a, std::int32_t b) {
    std::int32_t r = fn(a, b);
    if (r >= 0) {
        throw std::runtime_error("thrown after the callback returned");
    }
    return r;
}

// A function that always throws, with no callback anywhere near it.
CPPGO_EXPORT std::int32_t cb_throws() {
    throw std::runtime_error("cb_throws");
}

// Stack burn on whatever stack the caller gave us: 28 KiB, one byte per cache
// line. It only survives if the carrier's stack has the room.
CPPGO_EXPORT std::int64_t cb_deep_stack(std::int64_t bytes) {
    constexpr std::size_t kMax = 28 * 1024;
    volatile unsigned char buf[kMax];
    std::size_t n = bytes <= 0 ? kMax : static_cast<std::size_t>(bytes);
    if (n > kMax) {
        n = kMax;
    }
    std::int64_t sum = 0;
    for (std::size_t i = 0; i < n; i += 64) {
        buf[i] = static_cast<unsigned char>(i & 0x7f) + 1;
        sum += buf[i];
    }
    buf[n - 1] = 7;
    sum += buf[n - 1];
    return sum;
}

// A plain noexcept call, the direct carrier's shape, for the cost comparison.
CPPGO_EXPORT std::int32_t cb_add(std::int32_t a, std::int32_t b) noexcept { return a + b; }

// A std::function the library builds itself from a plain function pointer.
// This is what puts _Function_handler<...>::_M_invoke and _M_manager in the
// library's .dynsym: they are the two code pointers a std::function cannot be
// built without, and no Go-side assembly can mint them.
CPPGO_EXPORT std::int32_t cb_use_fn(int_binop *out, binop raw, std::int32_t a, std::int32_t b) {
    int_binop f = raw;
    if (out != nullptr) {
        *out = f;
    }
    return f(a, b);
}

// The same handler instantiation, named so the Go side can resolve the two
// symbols by the mangled names libstdc++ gives them.
CPPGO_EXPORT std::uint64_t cb_sizeof_std_function() { return sizeof(int_binop); }

// A function that throws and catches inside itself: the only shape in which an
// exception is survivable across a Go frame.
CPPGO_EXPORT std::int32_t cb_swallow() {
    try {
        throw std::runtime_error("swallowed");
    } catch (const std::exception &e) {
        return static_cast<std::int32_t>(std::strlen(e.what()));
    }
}
