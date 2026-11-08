// The prototype of the cppgo-built shim for the error-mapping rows.
//
// One extern "C" thunk per bound function, all noexcept, each reporting failure
// through the same block — a status, and a struct the Go side owns holding the
// code, the category and the message. Bundling them in one pointer is not
// cosmetic: the System V register convention carries six INTEGER arguments, and
// a thunk that took a value, an out-parameter, a code, a category, a buffer and
// a capacity would already be over the line.
//
//	int32_t shim_x(<in parameters>, <out parameters>, cppgo_error *err)
//
//	status 0    success; the out parameters hold the value
//	status !=0  failure; err holds the code (when the convention has one), the
//	            category's name and the message
//
// The generator emits this file from the header plus the package's error config;
// it is written by hand here because this workstream measures mechanisms.
#include "errlib.hpp"

#include <cstdint>
#include <cstring>
#include <expected>
#include <string>
#include <system_error>

extern "C" {

struct cppgo_error {
    std::int32_t code;
    const char *category;
    char message[256];
};

} // extern "C"

namespace {

// Records a failure in the caller's block. A zeroed block means success, so a
// thunk that succeeds writes nothing.
void fail(cppgo_error *err, const std::error_code &ec) {
    if (err == nullptr) {
        return;
    }
    err->code = ec.value();
    err->category = ec.category().name();
    std::strncpy(err->message, ec.message().c_str(), sizeof(err->message) - 1);
    err->message[sizeof(err->message) - 1] = '\0';
}

// A failure whose code comes from a std::error_code but whose message is the
// library's own text: a std::system_error's what() is what a caller reads.
void fail(cppgo_error *err, const std::error_code &ec, const char *text) {
    if (err == nullptr) {
        return;
    }
    err->code = ec.value();
    err->category = ec.category().name();
    std::strncpy(err->message, text, sizeof(err->message) - 1);
    err->message[sizeof(err->message) - 1] = '\0';
}

void fail(cppgo_error *err, const std::exception &e) {
    if (err == nullptr) {
        return;
    }
    err->category = "";
    std::strncpy(err->message, e.what(), sizeof(err->message) - 1);
    err->message[sizeof(err->message) - 1] = '\0';
}

} // namespace

extern "C" {

// std::error_code return.
__attribute__((visibility("default"))) std::int32_t shim_open_file(const char *path,
                                                                  std::int64_t path_len,
                                                                  std::int32_t *out_fd,
                                                                  cppgo_error *err) {
    try {
        std::string p(path, static_cast<std::size_t>(path_len));
        std::error_code ec = ns::open_file(p, out_fd);
        if (ec) {
            fail(err, ec);
            return 1;
        }
        return 0;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

// std::expected<T, std::error_code> (C++23).
__attribute__((visibility("default"))) std::int32_t shim_parse_int(const char *text,
                                                                  std::int64_t text_len,
                                                                  std::int32_t *out,
                                                                  cppgo_error *err) {
    try {
        std::string t(text, static_cast<std::size_t>(text_len));
        std::expected<std::int32_t, std::error_code> result = ns::parse_int(t);
        if (!result.has_value()) {
            fail(err, result.error());
            return 1;
        }
        *out = *result;
        return 0;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

// A thrown exception: no code and no category, only the message.
__attribute__((visibility("default"))) std::int32_t shim_divide(std::int32_t a, std::int32_t b,
                                                               std::int32_t *out,
                                                               cppgo_error *err) {
    try {
        *out = ns::divide(a, b);
        return 0;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

// A thrown std::system_error: the exception carries a code and a category, so
// the Go side gets the same error shape an error_code return produces.
__attribute__((visibility("default"))) std::int32_t shim_connect(const char *host,
                                                                std::int64_t host_len,
                                                                std::int32_t *out,
                                                                cppgo_error *err) {
    try {
        std::string h(host, static_cast<std::size_t>(host_len));
        *out = ns::connect(h);
        return 0;
    } catch (const std::system_error &e) {
        fail(err, e.code(), e.what());
        return 1;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

// bool + out parameters: the shim knows which parameter is the value and which
// is the failure because the config told it.
__attribute__((visibility("default"))) std::int32_t shim_parse_point(const char *text,
                                                                    std::int64_t text_len,
                                                                    std::int32_t *out_x,
                                                                    std::int32_t *out_y,
                                                                    std::int32_t *status,
                                                                    cppgo_error *err) {
    try {
        std::string t(text, static_cast<std::size_t>(text_len));
        ns::Point p{};
        std::int32_t st = 0;
        if (!ns::parse_point(t.c_str(), &p, &st)) {
            if (err != nullptr) {
                err->code = st;
                err->category = "status";
                std::strncpy(err->message, "invalid point", sizeof(err->message) - 1);
                err->message[sizeof(err->message) - 1] = '\0';
            }
            return 1;
        }
        *out_x = p.x;
        *out_y = p.y;
        return 0;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

// A project Result<T>: has_value/value/error, the shape the generator infers
// without a config entry.
__attribute__((visibility("default"))) std::int32_t shim_load(std::int32_t key,
                                                             std::int32_t *out,
                                                             cppgo_error *err) {
    try {
        ns::Result<std::int32_t> r = ns::load(key);
        if (!r.has_value()) {
            fail(err, r.error());
            return 1;
        }
        *out = r.value();
        return 0;
    } catch (const std::exception &e) {
        fail(err, e);
        return 1;
    }
}

} // extern "C"
