// The C++ side of the stack-allocation experiment. It is ordinary C++17 built
// with the system compiler into a shared object, with no cppgo-generated code
// in it, and it mirrors the shapes the generator emits: an extern "C" thunk
// per operation, a placement-new constructor, an explicit destructor call, and
// a result written through an out-pointer.
//
// It exists so docs/dev/stack-allocation.md can quote a measurement instead of
// an argument. Nothing here is shipped.

#include <cstdint>
#include <cstring>
#include <new>
#include <string>
#include <type_traits>

#define CPPGO_EXPORT extern "C" __attribute__((visibility("default")))

namespace {

// The address the most recent cppgo_cell_construct ran at. A test compares it
// with the address the Go value ended up at: a constructor that ran somewhere
// else is what "the object was relocated" means.
std::uintptr_t g_construct_this = 0;

} // namespace

// cppgo_cell is mathx::Vector's shape: a 16-byte class with a user-provided
// copy constructor and destructor, both declared out of line. That is what
// makes it not trivially copyable, and it is the property the analysis turns
// on. (A user-provided *ordinary* constructor alone does not: copy, assignment
// and destruction are what std::is_trivially_copyable looks at.)
struct cppgo_cell {
    double x;
    double y;

    cppgo_cell(double ax, double ay);
    cppgo_cell(const cppgo_cell &other);
    ~cppgo_cell();

    double norm() const { return x * x + y * y; }
};

cppgo_cell::cppgo_cell(double ax, double ay) : x(ax), y(ay) {}
cppgo_cell::cppgo_cell(const cppgo_cell &other) : x(other.x), y(other.y) {}
cppgo_cell::~cppgo_cell() = default;

// cppgo_point is the type cppgo already mirrors as a Go struct: trivially
// copyable, no constructor, no destructor.
struct cppgo_point {
    double x;
    double y;
};

// cppgo_selfref is trivially copyable and stores its own address. Byte-moving
// it is a well-defined copy of its object representation, and it still breaks:
// the trait describes the copy, not the invariant.
struct cppgo_selfref {
    cppgo_selfref *me;
};

// cppgo_text owns a heap allocation. Moving its bytes is not a move.
struct cppgo_text {
    std::string value;

    explicit cppgo_text(const char *v) : value(v) {}
};

// A polymorphic type, for the trait table: trivially copyable is false for it,
// because a class with virtual functions has no trivial copy constructor.
struct cppgo_poly {
    virtual ~cppgo_poly() = default;
    virtual int value() const { return 1; }
};

// ---------------------------------------------------------------------------
// cppgo_cell
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::uintptr_t cppgo_construct_this(void) { return g_construct_this; }

CPPGO_EXPORT std::int32_t cppgo_cell_construct(void *self, double x, double y) {
    g_construct_this = reinterpret_cast<std::uintptr_t>(self);
    new (self) cppgo_cell(x, y);
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_cell_destroy(void *self) {
    static_cast<cppgo_cell *>(self)->~cppgo_cell();
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_cell_norm(const void *self, double *result) {
    *result = static_cast<const cppgo_cell *>(self)->norm();
    return 0;
}

// The shape of a binding that passes a callback and keeps using the object
// afterwards: C++ holds `this` across a Go frame that may grow the stack.
typedef void (*cppgo_callback)(void);

// Runs a Go callback and then reports the `this` it still holds into two
// out-pointers. No dereference of self: the address may not be mapped any more,
// and the point of the measurement is the address itself.
//
// The two out-pointers are there to be compared: one the caller allocates on
// the heap, one in its own frame. The frame one is written after the callback
// grew the stack, so the write lands in the copy that is no longer live.
CPPGO_EXPORT std::int32_t cppgo_cell_after_callback_capture(void *self, cppgo_callback cb,
                                                            std::uintptr_t *heap_sink,
                                                            std::uintptr_t *frame_sink) {
    cb();
    const std::uintptr_t held = reinterpret_cast<std::uintptr_t>(self);
    *heap_sink = held;
    *frame_sink = held;
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_cell_after_callback_write(void *self, cppgo_callback cb) {
    cb();
    // Through the pointer C++ was given before the callback, which is the write
    // a use-after-free is made of.
    static_cast<cppgo_cell *>(self)->x = 999.0;
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_cell_x(const void *self, double *result) {
    *result = static_cast<const cppgo_cell *>(self)->x;
    return 0;
}

// ---------------------------------------------------------------------------
// cppgo_point: the by-value POD case, already mirrored as a Go struct
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::int32_t cppgo_point_midpoint(const cppgo_point *a, const cppgo_point *b,
                                               cppgo_point *result) {
    result->x = (a->x + b->x) / 2.0;
    result->y = (a->y + b->y) / 2.0;
    return 0;
}

// ---------------------------------------------------------------------------
// cppgo_selfref: a legal byte copy that still breaks the object
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::int32_t cppgo_selfref_construct(void *self) {
    cppgo_selfref *s = new (self) cppgo_selfref();
    s->me = s;
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_selfref_valid(const void *self, std::int32_t *result) {
    const cppgo_selfref *s = static_cast<const cppgo_selfref *>(self);
    *result = s->me == s ? 1 : 0;
    return 0;
}

// A byte-by-byte move, which is exactly what a Go stack copy does to a frame.
CPPGO_EXPORT void cppgo_bytes_move(void *dst, const void *src, std::uint64_t n) {
    std::memcpy(dst, src, static_cast<std::size_t>(n));
}

// ---------------------------------------------------------------------------
// cppgo_text: owns memory, so a byte move is not a move
// ---------------------------------------------------------------------------

CPPGO_EXPORT std::int32_t cppgo_text_construct(void *self, const char *value) {
    new (self) cppgo_text(value);
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_text_size(const void *self, std::uint64_t *result) {
    *result = static_cast<const cppgo_text *>(self)->value.size();
    return 0;
}

CPPGO_EXPORT std::int32_t cppgo_text_destroy(void *self) {
    static_cast<cppgo_text *>(self)->~cppgo_text();
    return 0;
}

// ---------------------------------------------------------------------------
// The trait table, read from the compiler rather than from a standard
// ---------------------------------------------------------------------------

// Type ids.
enum : std::int32_t {
    cppgo_type_cell = 0,
    cppgo_type_point = 1,
    cppgo_type_selfref = 2,
    cppgo_type_text = 3,
    cppgo_type_poly = 4,
    cppgo_type_count = 5,
};

// Bits: 1 trivially copyable, 2 polymorphic, 4 trivially destructible,
// 8 standard layout, 16 trivially relocatable (only when the library has the
// C++26 trait).
CPPGO_EXPORT std::uint32_t cppgo_traits(std::int32_t which) {
    std::uint32_t bits = 0;
    switch (which) {
    case cppgo_type_cell:
        bits = (std::is_trivially_copyable_v<cppgo_cell> ? 1u : 0u) |
               (std::is_polymorphic_v<cppgo_cell> ? 2u : 0u) |
               (std::is_trivially_destructible_v<cppgo_cell> ? 4u : 0u) |
               (std::is_standard_layout_v<cppgo_cell> ? 8u : 0u);
        break;
    case cppgo_type_point:
        bits = (std::is_trivially_copyable_v<cppgo_point> ? 1u : 0u) |
               (std::is_polymorphic_v<cppgo_point> ? 2u : 0u) |
               (std::is_trivially_destructible_v<cppgo_point> ? 4u : 0u) |
               (std::is_standard_layout_v<cppgo_point> ? 8u : 0u);
        break;
    case cppgo_type_selfref:
        bits = (std::is_trivially_copyable_v<cppgo_selfref> ? 1u : 0u) |
               (std::is_polymorphic_v<cppgo_selfref> ? 2u : 0u) |
               (std::is_trivially_destructible_v<cppgo_selfref> ? 4u : 0u) |
               (std::is_standard_layout_v<cppgo_selfref> ? 8u : 0u);
        break;
    case cppgo_type_text:
        bits = (std::is_trivially_copyable_v<cppgo_text> ? 1u : 0u) |
               (std::is_polymorphic_v<cppgo_text> ? 2u : 0u) |
               (std::is_trivially_destructible_v<cppgo_text> ? 4u : 0u) |
               (std::is_standard_layout_v<cppgo_text> ? 8u : 0u);
        break;
    case cppgo_type_poly:
        bits = (std::is_trivially_copyable_v<cppgo_poly> ? 1u : 0u) |
               (std::is_polymorphic_v<cppgo_poly> ? 2u : 0u) |
               (std::is_trivially_destructible_v<cppgo_poly> ? 4u : 0u) |
               (std::is_standard_layout_v<cppgo_poly> ? 8u : 0u);
        break;
    default:
        break;
    }
#ifdef __cpp_lib_trivially_relocatable
    if (which == cppgo_type_selfref && std::is_trivially_relocatable_v<cppgo_selfref>) {
        bits |= 16u;
    }
    if (which == cppgo_type_point && std::is_trivially_relocatable_v<cppgo_point>) {
        bits |= 16u;
    }
    if (which == cppgo_type_cell && std::is_trivially_relocatable_v<cppgo_cell>) {
        bits |= 16u;
    }
#endif
    return bits;
}

CPPGO_EXPORT std::uint64_t cppgo_sizeof(std::int32_t which) {
    switch (which) {
    case cppgo_type_cell:
        return sizeof(cppgo_cell);
    case cppgo_type_point:
        return sizeof(cppgo_point);
    case cppgo_type_selfref:
        return sizeof(cppgo_selfref);
    case cppgo_type_text:
        return sizeof(cppgo_text);
    case cppgo_type_poly:
        return sizeof(cppgo_poly);
    default:
        return 0;
    }
}

CPPGO_EXPORT std::uint64_t cppgo_alignof(std::int32_t which) {
    switch (which) {
    case cppgo_type_cell:
        return alignof(cppgo_cell);
    case cppgo_type_point:
        return alignof(cppgo_point);
    case cppgo_type_selfref:
        return alignof(cppgo_selfref);
    case cppgo_type_text:
        return alignof(cppgo_text);
    case cppgo_type_poly:
        return alignof(cppgo_poly);
    default:
        return 0;
    }
}

// The value of __cpp_trivial_relocatability, or -1 when the library does not
// define it. The experiment reports which compiler it ran on rather than
// assuming a standard.
CPPGO_EXPORT std::int64_t cppgo_trivial_relocatability_macro(void) {
#ifdef __cpp_trivial_relocatability
    return __cpp_trivial_relocatability;
#else
    return -1;
#endif
}
