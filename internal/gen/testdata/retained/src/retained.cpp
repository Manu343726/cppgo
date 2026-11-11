#include "retained/retained.hpp"

namespace retained {

namespace {

LogCallback g_callback = nullptr;

// Every Meter alive: incremented by the constructors and decremented by the
// destructor, so a test can see that Close ran it.
std::int32_t g_live_meters = 0;

// What poke() last computed, so a test can see both the value it read through
// the pointer and the default the callee applied.
std::int32_t g_last_poke = 0;

} // namespace

void set_log_callback(LogCallback fn) noexcept { g_callback = fn; }

void log(std::int32_t n) noexcept {
    if (g_callback == nullptr) {
        return;
    }
    for (std::int32_t level = 0; level < n; ++level) {
        g_callback(level, level % 2 == 0 ? "even" : "odd");
    }
}

std::int32_t touch() noexcept { return 7; }

Enrollment enroll(const char *label, std::int32_t count) noexcept {
    return Enrollment{label == nullptr ? std::string() : std::string(label), count};
}

Meter::Meter() noexcept : value_(0) { ++g_live_meters; }

Meter::Meter(std::int32_t start) noexcept : value_(start) { ++g_live_meters; }

Meter::Meter(const Meter &other) noexcept : value_(other.value_) { ++g_live_meters; }

Meter::~Meter() { --g_live_meters; }

void Meter::bump() noexcept { ++value_; }

std::int32_t Meter::value() const noexcept { return value_; }

Meter make_meter(std::int32_t start) noexcept { return Meter(start); }

std::int32_t live_meters() noexcept { return g_live_meters; }

void poke(const std::int32_t *value, std::int32_t scale) noexcept {
    if (value != nullptr) {
        g_last_poke = *value * scale;
    }
}

std::int32_t last_poke() noexcept { return g_last_poke; }

} // namespace retained
