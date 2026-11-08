#pragma once

#include <cstddef>
#include <cstdint>

// The type-map fixture: every scalar width, bool, pointers, the slice shape,
// the record shapes the emitter has to tell apart (mirrored POD, opaque with
// and without a destructor, implicitly constructible, abstract), and the
// special members that are deliberately not exposed.
namespace types {

struct Pod {
    int32_t a;
    uint32_t b;
    float c;
    double d;
    bool e;

    int32_t sum() const noexcept;
};

struct Empty {};

class Tiny {
public:
    ~Tiny();

private:
    int value_;
};

class Composed {
public:
    ~Composed();
    int32_t value() const;

private:
    Tiny inner_;
};

class Abstract {
public:
    virtual ~Abstract();
    virtual int32_t area() const = 0;
    int32_t id() const;
};

class Opaque {
public:
    Opaque();
    ~Opaque();
    int32_t get() const;
    void set(int32_t value);
    void gone() = delete;

private:
    void secret();
    int32_t value_;
};

class NoDtor {
public:
    explicit NoDtor(int32_t value) noexcept;
    int32_t value() const noexcept;

private:
    int32_t value_;
};

class Implicit {
public:
    ~Implicit();
    int32_t value() const;

private:
    int32_t value_;
};

class Mover {
public:
    Mover(int32_t value);
    Mover(Mover &&other) noexcept;
    ~Mover();
    int32_t value() const;

private:
    int32_t value_;
};

class CopyNoDtor {
public:
    CopyNoDtor(int32_t value);
    int32_t value() const;

private:
    int32_t value_;
};

class DeletedCtor {
public:
    DeletedCtor() = delete;
    ~DeletedCtor();

private:
    int32_t value_;
};

class DeletedDtor {
public:
    DeletedDtor(int32_t value);
    ~DeletedDtor() = delete;

private:
    int32_t value_;
};

class NoThrowCopy {
public:
    NoThrowCopy(int32_t value);
    NoThrowCopy(const NoThrowCopy &other) noexcept;
    ~NoThrowCopy();
    int32_t value() const;

private:
    int32_t value_;
};

// Inline bodies: no exported symbol, so they are carried by thunks even though
// they are noexcept. One returns a value, one returns nothing and takes no
// arguments (the shim's `(void)` prototype).
inline int32_t inline_value() noexcept { return 7; }

inline void ping() noexcept {}

inline bool always_true() noexcept { return true; }

inline int32_t *null_pointer() noexcept { return nullptr; }

bool negate(bool value) noexcept;
bool is_positive(int32_t value);
int8_t add_i8(int8_t a, int8_t b) noexcept;
uint8_t add_u8(uint8_t a, uint8_t b) noexcept;
int16_t add_i16(int16_t a, int16_t b) noexcept;
uint16_t add_u16(uint16_t a, uint16_t b) noexcept;
int32_t add_i32(int32_t a, int32_t b) noexcept;
uint32_t add_u32(uint32_t a, uint32_t b) noexcept;
int64_t add_i64(int64_t a, int64_t b) noexcept;
uint64_t add_u64(uint64_t a, uint64_t b) noexcept;
float add_f32(float a, float b) noexcept;
double add_f64(double a, double b) noexcept;
int32_t *pointer_identity(int32_t *value) noexcept;
int32_t *maybe(int32_t *value);
uint32_t count(const int32_t *values, std::size_t n) noexcept;
void add_into(const int32_t *a, int32_t *b, std::size_t n) noexcept;
void bump(double &value);
void take_matrix(int32_t **rows, std::size_t n) noexcept;

// Reference parameters: an opaque record crosses as the object's address, a
// mirrored one as a pointer to the Go value.
int32_t read(const Opaque &value) noexcept;
double sum_pod(const Pod &pod) noexcept;

} // namespace types
