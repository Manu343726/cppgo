#pragma once

// The library's logger, internal to the C++ side. It is spdlog with one sink:
// the callback sink in log.cpp, which formats each record, buffers it for
// drain_log(), and tells the Go side that a record arrived.
//
// This header is not part of the bound surface and is never included by
// fingerprint.hpp: a caller of the library gets the records through the three
// functions that header declares, not through spdlog.

#include <cstdint>
#include <memory>
#include <string>

#include <spdlog/spdlog.h>

namespace fingerprint {
namespace detail {

// The one logger the library writes through, created on first use and kept for
// the process's lifetime. Never null.
std::shared_ptr<spdlog::logger> logger();

// The records buffered since the last call, one per line, as a joined string.
std::string take_buffered_log();

// RecordScope installs the callback the sink delivers to for as long as the
// object lives — one call, on the thread that made it. A null callback means
// "buffer only", which is what a call whose signature takes no callback gets.
class RecordScope {
public:
    using Callback = void (*)(std::int32_t level, std::int32_t pending);

    explicit RecordScope(Callback callback) noexcept;
    ~RecordScope();

    RecordScope(const RecordScope &) = delete;
    RecordScope &operator=(const RecordScope &) = delete;

private:
    Callback previous_;
};

} // namespace detail
} // namespace fingerprint
