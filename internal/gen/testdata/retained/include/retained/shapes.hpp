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

} // namespace retained
