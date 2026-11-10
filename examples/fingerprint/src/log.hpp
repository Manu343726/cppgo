#pragma once

// The library's logger, internal to the C++ side. It is spdlog with one sink:
// the callback sink in log.cpp, which formats each record, buffers it for
// drain_log(), and tells the Go side that a record arrived.
//
// This header is not part of the bound surface and is never included by
// fingerprint.hpp: a caller of the library gets the records through the three
// functions that header declares, not through spdlog.

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

} // namespace detail
} // namespace fingerprint
