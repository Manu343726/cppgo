// The by-value fixture's implementation. The counters are the measurement: the
// copy constructor's +1000 proves a C++ copy made the value Go reads, and the
// destructor's count proves Close ran the destructor exactly when it should.
#include "byvalue.hpp"

namespace byval {
namespace {

std::int32_t cppgo_destructions = 0;

} // namespace

Counter::Counter() : value_(0) {}

Counter::Counter(std::int32_t value) : value_(value) {}

Counter::Counter(const Counter &other) : value_(other.value_ + 1000) {}

Counter::~Counter() { ++cppgo_destructions; }

std::int32_t Counter::value() const noexcept { return value_; }

void Counter::set(std::int32_t value) noexcept { value_ = value; }

std::int32_t read(Counter counter) noexcept { return counter.value(); }

NoDtor::NoDtor(std::int32_t value) noexcept : value_(value) {}

std::int32_t NoDtor::value() const noexcept { return value_; }

NoDtor make_no_dtor(std::int32_t value) noexcept { return NoDtor(value); }

std::int32_t read_no_dtor(NoDtor value) noexcept { return value.value(); }

Counter make(std::int32_t value) noexcept { return Counter(value); }

Counter passthrough(Counter counter) noexcept { return counter; }

std::int32_t destructions() noexcept { return cppgo_destructions; }

} // namespace byval
