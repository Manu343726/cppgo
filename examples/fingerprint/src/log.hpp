#pragma once

// The library's logger, internal to the C++ side. It is spdlog with one sink:
// the callback sink in log.cpp, which formats each record and hands it to the
// function pointer the caller stored with set_log_callback.
//
// This header is not part of the bound surface and is never included by
// fingerprint.hpp: a caller of the library gets the records through the one
// function that header declares, not through spdlog.

#include <cstdint>
#include <memory>
#include <string>

#include <spdlog/spdlog.h>

namespace fingerprint {
namespace detail {

// The one logger the library writes through, created on first use and kept for
// the process's lifetime. Never null.
std::shared_ptr<spdlog::logger> logger();

// One record per spdlog level plus one record with several arguments, written
// through the same sink the pipeline uses. It is not bound: the records the Go
// side sees are the pipeline's own, and this is how the C++ tests prove that
// the sink carries every level, including the one no pipeline path deserves.
void write_log_samples();

} // namespace detail

// set_log_callback is declared in fingerprint.hpp, where a caller finds it.

} // namespace fingerprint
