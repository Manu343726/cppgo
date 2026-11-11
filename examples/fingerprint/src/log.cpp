// The spdlog sink that carries the library's records to the Go side.
//
// The sink is an spdlog sink like any other — it formats the record with its
// pattern and does something with it — and what it does is call the function
// pointer the caller stored with set_log_callback, with the record's level and
// the formatted line.
//
// The pointer is stored, and that is why `cppgo gen` is told `-retain`: a C++
// function pointer says nothing about how long the callee keeps it, so the
// generator cannot infer it and the manifest states it. Everything the retention
// rules promise follows from that one word —
//
//   * the Go function is kept alive after the registering call returns;
//   * every binding of the package enters C++ through the reentrant carrier,
//     because any call may now deliver a record;
//   * a call that may run a Go callback works on its own copy of the Go values
//     it hands to C++, so a stack growth inside the callback cannot move them.
//
// The callback crosses into Go, so it may only be called on a thread already
// inside a call made from Go. The library starts no thread and logs from none:
// every record in this example is written from inside `enroll()` or
// `Trigger::evaluate()`, which are calls Go made, on the goroutine that made
// them. A background thread would have to hand its records to the next call
// instead, because the runtime cannot accept a callback on a thread it has
// never seen.
//
// The callback is called while the sink's own mutex is held (base_sink takes it
// around sink_it_). The Go handler therefore must not log: a record emitted from
// inside the handler would re-enter this mutex and deadlock.

#include "log.hpp"

#include "fingerprint/fingerprint.hpp"

#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <utility>

#include <spdlog/details/log_msg.h>
#include <spdlog/sinks/base_sink.h>

namespace fingerprint {
namespace {

class CallbackSink final : public spdlog::sinks::base_sink<std::mutex> {
public:
    // Stores fn, replacing whatever was stored. It is a plain pointer write
    // under the callback's own mutex: the sink's mutex is held only for the
    // duration of a record, and a registration is not a record.
    void set_callback(LogCallback fn) noexcept {
        std::lock_guard<std::mutex> lock(callback_mutex_);
        callback_ = fn;
    }

protected:
    void sink_it_(const spdlog::details::log_msg &msg) override {
        LogCallback fn = nullptr;
        {
            std::lock_guard<std::mutex> lock(callback_mutex_);
            fn = callback_;
        }
        if (fn == nullptr) {
            return;
        }
        spdlog::memory_buf_t formatted;
        formatter_->format(msg, formatted);
        // The formatter ends the line with the newline its next consumer would
        // write; a record is one line, so it does not travel with one.
        std::size_t length = formatted.size();
        while (length > 0 && (formatted.data()[length - 1] == '\n' || formatted.data()[length - 1] == '\r')) {
            --length;
        }
        const std::string line(formatted.data(), length);
        fn(static_cast<std::int32_t>(msg.level), line.c_str());
    }

    void flush_() override {}

private:
    std::mutex callback_mutex_;
    LogCallback callback_ = nullptr;
};

std::shared_ptr<CallbackSink> sink() {
    static const std::shared_ptr<CallbackSink> instance = [] {
        auto created = std::make_shared<CallbackSink>();
        // The level travels as the callback's first argument, so the pattern is
        // the message alone: a "[debug] " prefix would say it twice.
        created->set_pattern("%v");
        return created;
    }();
    return instance;
}

} // namespace

namespace detail {

std::shared_ptr<spdlog::logger> logger() {
    static const std::shared_ptr<spdlog::logger> instance = [] {
        auto created = std::make_shared<spdlog::logger>("fingerprint", sink());
        // Every level reaches the sink: the example's whole point is that the
        // Go side sees which record is which.
        created->set_level(spdlog::level::trace);
        created->flush_on(spdlog::level::trace);
        return created;
    }();
    return instance;
}

void write_log_samples() {
    const auto log = logger();
    log->trace("trace sample");
    log->debug("debug sample");
    log->info("info sample");
    log->warn("warn sample");
    log->error("error sample");
    log->critical("critical sample");
    // One record with several arguments: the sink formats it, so a test can
    // assert the formatted text and not only the level.
    log->info("frame {}x{} at ({}, {}) with {} minutiae", 640, 480, 12, 34, 7);
}

} // namespace detail

void set_log_callback(LogCallback fn) noexcept { sink()->set_callback(fn); }

// The library's own log samples are a diagnostic of the sink, not part of the
// surface a caller crosses: the pipeline's own records are what the Go side
// sees. The C++ tests are what call this.

} // namespace fingerprint
