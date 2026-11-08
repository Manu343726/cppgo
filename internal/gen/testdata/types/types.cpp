#include "types.hpp"

namespace types {

int32_t Pod::sum() const noexcept { return a + static_cast<int32_t>(b) + static_cast<int32_t>(c) + static_cast<int32_t>(d) + (e ? 1 : 0); }

Tiny::~Tiny() {}

Composed::~Composed() {}

int32_t Composed::value() const { return 0; }

Abstract::~Abstract() {}

int32_t Abstract::id() const { return 0; }

Opaque::Opaque() : value_(0) {}

Opaque::~Opaque() {}

int32_t Opaque::get() const { return value_; }

void Opaque::set(int32_t value) { value_ = value; }

NoDtor::NoDtor(int32_t value) noexcept : value_(value) {}

int32_t NoDtor::value() const noexcept { return value_; }

Implicit::~Implicit() {}

int32_t Implicit::value() const { return value_; }

Mover::Mover(int32_t value) : value_(value) {}

Mover::Mover(Mover &&other) noexcept : value_(other.value_) { other.value_ = 0; }

Mover::~Mover() {}

int32_t Mover::value() const { return value_; }

CopyNoDtor::CopyNoDtor(int32_t value) : value_(value) {}

int32_t CopyNoDtor::value() const { return value_; }

DeletedCtor::~DeletedCtor() {}

DeletedDtor::DeletedDtor(int32_t value) : value_(value) {}

NoThrowCopy::NoThrowCopy(int32_t value) : value_(value) {}

NoThrowCopy::NoThrowCopy(const NoThrowCopy &other) noexcept : value_(other.value_) {}

NoThrowCopy::~NoThrowCopy() {}

int32_t NoThrowCopy::value() const { return value_; }

bool negate(bool value) noexcept { return !value; }

bool is_positive(int32_t value) { return value > 0; }

int8_t add_i8(int8_t a, int8_t b) noexcept { return static_cast<int8_t>(a + b); }

uint8_t add_u8(uint8_t a, uint8_t b) noexcept { return static_cast<uint8_t>(a + b); }

int16_t add_i16(int16_t a, int16_t b) noexcept { return static_cast<int16_t>(a + b); }

uint16_t add_u16(uint16_t a, uint16_t b) noexcept { return static_cast<uint16_t>(a + b); }

int32_t add_i32(int32_t a, int32_t b) noexcept { return a + b; }

uint32_t add_u32(uint32_t a, uint32_t b) noexcept { return a + b; }

int64_t add_i64(int64_t a, int64_t b) noexcept { return a + b; }

uint64_t add_u64(uint64_t a, uint64_t b) noexcept { return a + b; }

float add_f32(float a, float b) noexcept { return a + b; }

double add_f64(double a, double b) noexcept { return a + b; }

int32_t *pointer_identity(int32_t *value) noexcept { return value; }

int32_t *maybe(int32_t *value) { return value; }

uint32_t count(const int32_t *values, std::size_t n) noexcept {
    uint32_t total = 0;
    for (std::size_t i = 0; i < n; ++i) {
        total += static_cast<uint32_t>(values[i]);
    }
    return total;
}

void add_into(const int32_t *a, int32_t *b, std::size_t n) noexcept {
    for (std::size_t i = 0; i < n; ++i) {
        b[i] += a[i];
    }
}

void bump(double &value) { value += 1.0; }

void take_matrix(int32_t **rows, std::size_t n) noexcept {
    for (std::size_t i = 0; i < n; ++i) {
        rows[i] = nullptr;
    }
}

int32_t read(const Opaque &value) noexcept { return value.get() + 1; }

double sum_pod(const Pod &pod) noexcept {
    return static_cast<double>(pod.a) + static_cast<double>(pod.b) + static_cast<double>(pod.c) + pod.d;
}

} // namespace types
