// The spdlog sink that carries the library's records to the Go side.
//
// The sink is an spdlog sink like any other — it formats the record with its
// pattern and does something with it — and what it does is:
//
//   1. append the formatted line to a buffer the drain_log() binding reads;
//   2. call the registered C function pointer with the record's level and the
//      number of records now pending.
//
// The callback crosses into Go, so it may only be called on a thread that is
// already inside a call made from Go. The library starts no thread and logs
// from none: every record in this example is written from inside `enroll()`,
// `Trigger::evaluate()` or `emit_log_samples()`, which are the calls Go made.
// A future background thread would have to forward its records through a queue
// this library drains on the next call, because the Go runtime cannot accept a
// callback on a thread it has never seen.
//
// The callback is called while the sink's own mutex is held (base_sink takes it
// around sink_it_). The Go handler therefore must not log: a record emitted
// from inside the handler would re-enter this mutex and deadlock. The handler
// this example installs only counts, which is what the delivery signal is for.

#include "log.hpp"

#include "fingerprint/fingerprint.hpp"

#include <cstdint>
#include <memory>
#include <mutex>
#include <string>
#include <utility>
#include <vector>

#include <spdlog/details/log_msg.h>
#include <spdlog/sinks/base_sink.h>

namespace fingerprint {
namespace {

using LogCallbackFunction = void (*)(std::int32_t level, std::int32_t pending);

class CallbackSink final : public spdlog::sinks::base_sink<std::mutex> {
public:
    void set_callback(LogCallbackFunction fn) noexcept {
        std::lock_guard<std::mutex> lock(callback_mutex_);
        callback_ = fn;
    }

protected:
    void sink_it_(const spdlog::details::log_msg &msg) override {
        spdlog::memory_buf_t formatted;
        formatter_->format(msg, formatted);
        std::string line(formatted.data(), formatted.size());
        const std::int32_t level = static_cast<std::int32_t>(msg.level);

        std::int32_t pending = 0;
        {
            std::lock_guard<std::mutex> lock(buffer_mutex_);
            lines_.push_back(std::move(line));
            pending = static_cast<std::int32_t>(lines_.size());
        }

        LogCallbackFunction fn = nullptr;
        {
            std::lock_guard<std::mutex> lock(callback_mutex_);
            fn = callback_;
        }
        if (fn != nullptr) {
            fn(level, pending);
        }
    }

    void flush_() override {}

public:
    std::string take() {
        std::lock_guard<std::mutex> lock(buffer_mutex_);
        std::string joined;
        for (std::size_t i = 0; i < lines_.size(); ++i) {
            if (i != 0) {
                joined.push_back('\n');
            }
            joined += lines_[i];
        }
        lines_.clear();
        return joined;
    }

private:
    std::mutex callback_mutex_;
    std::mutex buffer_mutex_;
    LogCallbackFunction callback_ = nullptr;
    std::vector<std::string> lines_;
};

std::shared_ptr<CallbackSink> sink() {
    static const std::shared_ptr<CallbackSink> instance = [] {
        auto created = std::make_shared<CallbackSink>();
        created->set_pattern("[%l] %v");
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

std::string take_buffered_log() { return sink()->take(); }

} // namespace detail

void set_log_callback(void (*fn)(std::int32_t level, std::int32_t pending)) noexcept {
    sink()->set_callback(fn);
}

void clear_log_callback() noexcept { sink()->set_callback(nullptr); }

std::string drain_log() { return detail::take_buffered_log(); }

void emit_log_samples() {
    const auto log = detail::logger();
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

} // namespace fingerprint
