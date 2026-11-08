// The prototype of zz_cppgo_shim.cpp: the file cppgo generates and compiles
// itself into its own shared object, linking the user's library. It is written
// here by hand because this workstream measures mechanisms, not the emitter;
// what a generator would emit is exactly this shape.
//
// Three rules shape it:
//
//   - every exported function is extern "C" and noexcept: an exception never
//     crosses into Go, it is caught here and reported through the thread-local
//     error string the Go side reads;
//   - everything that cannot be expressed by a direct register call is done in
//     C++, where the layout, the vtable and the template instantiations exist:
//     virtual dispatch through a base pointer, a cast that adjusts this,
//     std::string and std::vector marshalling, by-value non-POD copies,
//     template instantiation, and constructing a std::function from a plain
//     function pointer;
//   - each result that is not a scalar is written through an out-pointer the
//     Go side owns, so no allocation crosses the boundary in either direction.
#include "user.hpp"

#include <cstdint>
#include <cstring>
#include <functional>
#include <new>
#include <string>
#include <vector>

namespace {

thread_local char g_last_error[256];

void set_error(const char *msg) {
    std::strncpy(g_last_error, msg, sizeof(g_last_error) - 1);
    g_last_error[sizeof(g_last_error) - 1] = '\0';
}

} // namespace

#define TRY_BEGIN try {
#define TRY_END                                                                                    \
    }                                                                                              \
    catch (const std::exception &e) {                                                              \
        set_error(e.what());                                                                       \
        return -1;                                                                                 \
    }                                                                                              \
    catch (...) {                                                                                  \
        set_error("unknown exception");                                                            \
        return -1;                                                                                 \
    }

extern "C" {

__attribute__((visibility("default"))) const char *cppgo_last_error(void) { return g_last_error; }

// --- virtual dispatch through a base pointer --------------------------------

// The object is held at its static type (Shape*), and the call is virtual: the
// vtable picks Circle's override. No Go-side vtable arithmetic is involved.
__attribute__((visibility("default"))) std::int32_t shim_shape_area(void *shape, double *out) {
    TRY_BEGIN
    *out = static_cast<user::Shape *>(shape)->area();
    return 0;
    TRY_END
}

__attribute__((visibility("default"))) std::int32_t shim_shape_name(void *shape, char *buf,
                                                                   std::int64_t cap) {
    TRY_BEGIN
    const char *name = static_cast<user::Shape *>(shape)->name();
    std::strncpy(buf, name, static_cast<std::size_t>(cap) - 1);
    buf[cap - 1] = '\0';
    return 0;
    TRY_END
}

// --- this adjustment for multiple inheritance --------------------------------

// Disk inherits Serializable first and Circle second, so the Circle subobject
// is at a non-zero offset and both casts are real adjustments the Go side never
// computes. Each address is handed back to Go so the offsets can be measured.
__attribute__((visibility("default"))) std::int32_t shim_disk_serial(void *disk,
                                                                     std::uintptr_t *circle_at,
                                                                     std::int32_t *serial) {
    TRY_BEGIN
    auto *d = static_cast<user::Disk *>(disk);
    auto *c = static_cast<user::Circle *>(d);
    auto *s = static_cast<user::Serializable *>(d);
    *circle_at = reinterpret_cast<std::uintptr_t>(c);
    *serial = s->serial();
    return 0;
    TRY_END
}

// --- inline, header-only body ------------------------------------------------

// user::twice is inline in the header and the library never calls it, so the
// library exports no symbol for it. Including the header is what makes the call
// possible: the body is compiled into the shim.
__attribute__((visibility("default"))) double shim_twice(double v) { return user::twice(v); }

// --- std::string and std::vector --------------------------------------------

// In: two Go strings, copied into std::string. Out: the result copied into the
// caller's buffer, so no C++ object outlives the call.
__attribute__((visibility("default"))) std::int32_t shim_greet(const char *prefix,
                                                               std::int64_t prefix_len,
                                                               const char *name,
                                                               std::int64_t name_len, char *buf,
                                                               std::int64_t cap) {
    TRY_BEGIN
    std::string p(prefix, static_cast<std::size_t>(prefix_len));
    std::string n(name, static_cast<std::size_t>(name_len));
    std::string out = user::greet(p, n);
    if (static_cast<std::int64_t>(out.size()) >= cap) {
        set_error("result buffer too small");
        return -1;
    }
    std::memcpy(buf, out.data(), out.size());
    return static_cast<std::int32_t>(out.size());
    TRY_END
}

// A std::vector in and out: the Go slice is copied into a vector, the result is
// copied into the caller's slice. This is the copy-through-buffer shape that
// needs no C++ object to survive the call.
__attribute__((visibility("default"))) std::int32_t shim_doubled(const double *values,
                                                                 std::int64_t n, double *out,
                                                                 std::int64_t cap) {
    TRY_BEGIN
    std::vector<double> in(values, values + n);
    std::vector<double> result = user::doubled(in);
    if (static_cast<std::int64_t>(result.size()) > cap) {
        set_error("result buffer too small");
        return -1;
    }
    std::memcpy(out, result.data(), result.size() * sizeof(double));
    return static_cast<std::int32_t>(result.size());
    TRY_END
}

__attribute__((visibility("default"))) std::int32_t shim_sum(const double *values, std::int64_t n,
                                                             double *out) {
    TRY_BEGIN
    std::vector<double> in(values, values + n);
    *out = user::sum(in);
    return 0;
    TRY_END
}

// --- non-POD by value --------------------------------------------------------

// The shim constructs the value and passes it by value: the copy constructor
// runs inside C++, where the layout is known. Nothing about the value crosses
// the boundary.
__attribute__((visibility("default"))) std::int32_t shim_tiny_value(std::int32_t value,
                                                                   std::int32_t *out) {
    TRY_BEGIN
    user::Tiny t(value);
    user::Tiny copy(t); // an explicit copy, the shape a by-value parameter takes
    *out = copy.value();
    return 0;
    TRY_END
}

// --- exceptions --------------------------------------------------------------

// The only place an exception can be stopped: a C++ frame. The shim catches it
// and reports its message; the Go side reads it and returns an error.
__attribute__((visibility("default"))) std::int32_t shim_checked_div(std::int32_t a,
                                                                    std::int32_t b,
                                                                    std::int32_t *out) {
    TRY_BEGIN
    *out = user::checked_div(a, b);
    return 0;
    TRY_END
}

// --- object lifetime ---------------------------------------------------------

// Construction, a call through a base pointer, and destruction through a
// virtual destructor: the three steps a Go object of a polymorphic C++ class
// needs. Go holds the pointer; C++ owns the layout.
__attribute__((visibility("default"))) void shim_circle_new(double r, void **out) {
    *out = new user::Circle(r);
}

__attribute__((visibility("default"))) void shim_circle_delete(void *p) {
    delete static_cast<user::Shape *>(p);
}

__attribute__((visibility("default"))) double shim_circle_radius(void *p) {
    return static_cast<user::Circle *>(p)->radius();
}

__attribute__((visibility("default"))) void shim_disk_new(std::int32_t id, double r, void **out) {
    *out = new user::Disk(id, r);
}

__attribute__((visibility("default"))) void shim_disk_delete(void *p) {
    delete static_cast<user::Shape *>(p);
}

__attribute__((visibility("default"))) std::int32_t shim_disk_destroys(void) {
    return user::disk_destroys();
}

// --- callbacks ---------------------------------------------------------------

// The std::function is constructed here, from a plain function pointer the Go
// side produced. Go never sees the std::function's layout, and the shim needs
// no exported template instantiation of its own: it is C++ and compiles one.
__attribute__((visibility("default"))) std::int32_t shim_apply(
    std::int32_t (*fn)(std::int32_t, std::int32_t), std::int32_t a, std::int32_t b,
    std::int32_t *out) {
    TRY_BEGIN
    std::function<std::int32_t(std::int32_t, std::int32_t)> f = fn;
    *out = user::apply(f, a, b);
    return 0;
    TRY_END
}

// --- templates ---------------------------------------------------------------

// Instantiations the Go side asked for. In a generated shim these lines are
// derived from the Go package's type arguments; here they are written out.
template double user::maxof<double>(double, double);
template std::int32_t user::maxof<std::int32_t>(std::int32_t, std::int32_t);

__attribute__((visibility("default"))) double shim_maxof_double(double a, double b) {
    return user::maxof<double>(a, b);
}

__attribute__((visibility("default"))) std::int32_t shim_maxof_int(std::int32_t a,
                                                                   std::int32_t b) {
    return user::maxof<std::int32_t>(a, b);
}

} // extern "C"
