#pragma once

// The shapes a retained package has to carry beyond free functions and scalars.
//
// A `-retain` package is callback-capable throughout: every binding enters C
// through runtime.cgocall, so every call may deliver the callback the callee
// stored. The shapes below are the ones that carrier has to get right and the
// free-function fixture never reaches — a record Go cannot mirror, a class whose
// destructor must run, and results returned by value. examples/fingerprint needs
// all three: its trigger owns the prefilter buffers, and its enrolment carries
// the template's bytes and metadata.

#include <cstdint>
#include <string>

namespace retained {

// A record Go cannot mirror: a std::string member is not a Go struct layout, so
// the Go value is an owner of C++ storage and the members are reached through
// synthesized accessors. The getter is the accessor that reads a std::string
// out, which is the one shape whose result is lowered for the carrier the
// accessor actually ends on.
struct Enrollment {
    std::string label;
    std::int32_t count;
};

// Returned by value: the Go side owns the storage, the shim constructs the
// record into it, and Close releases it. Enrollment declares no destructor, so
// the release is the storage alone and the record's own copy semantics decide
// what the Go value shares with the callee's temporary.
Enrollment enroll(const char *label, std::int32_t count) noexcept;

// A class with storage Go never sees and a destructor that must run at Close.
// It takes no callback and reads no standard-library value: the destructor is
// bound because the class declares it, and under -retain the whole package is
// callback-capable, so Close reaches it through runtime.cgocall.
class Meter {
public:
    Meter() noexcept;
    explicit Meter(std::int32_t start) noexcept;
    // Declared so that every construction that creates an object runs the
    // counter: an implicitly-defined copy constructor would not, and the
    // destructor's witness would then count a copy it never saw made.
    Meter(const Meter &other) noexcept;
    ~Meter();
    void bump() noexcept;
    std::int32_t value() const noexcept;

private:
    std::int32_t value_;
};

// Returned by value: a non-POD record result. The shim constructs it into Go's
// storage, and the destructor runs at Close through the record's own binding.
Meter make_meter(std::int32_t start) noexcept;

// The number of Meter objects alive: the destructor's only observable effect,
// and the witness that Close ran it.
std::int32_t live_meters() noexcept;

// A signature with a trailing default, and a pointer parameter: the prefix the
// caller may omit is written in C++, so that binding is moved to the shim after
// the package lowered it for the callback-capable carrier — and the pointer it
// hands C++ is a copy the shim's carrier does not pin.
void poke(const std::int32_t *value, std::int32_t scale = 2) noexcept;

// The value poke() last computed: the witness that the prefix carried the
// callee's own default for the argument it omitted.
std::int32_t last_poke() noexcept;

// A record with a borrowed C string member. The getter's result is a copy of
// the library's bytes, so the shim writes it through an out-pointer; the
// getter is an accessor, which is moved to the shim after the package lowered
// its result for the callback-capable carrier.
struct Tagged {
    const char *text;
    std::int32_t count;
};

} // namespace retained
