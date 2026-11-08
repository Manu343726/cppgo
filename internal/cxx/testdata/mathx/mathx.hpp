#pragma once

#include <cstddef>

namespace mathx {

enum class Axis : unsigned char { X = 0, Y = 1 };

// Vector is the shape cppgo's direct ABI mode supports: a constructor and every
// method defined OUT OF LINE in mathx.cpp, so each one emits a dynamic symbol.
class Vector {
public:
    Vector(double x, double y);
    ~Vector();

    double norm() const;
    double dot(const double *ys, std::size_t n) const;
    Vector scaled(double k) const;

    // Defined inside the class definition, therefore implicitly inline and
    // invisible to dlsym: the generator must refuse to bind these.
    double x() const { return x_; }
    double y() const { return y_; }

    // Virtual dispatch goes through the vtable: needs the shim mode.
    virtual double scale() const;

    static int dims();
    static const char *name();

    // A pointer to member is neither an integer nor a pointer at the ABI level:
    // libclang reports MemberPointer and this package classifies it "other".
    static int pick(int Vector::*member);

private:
    double x_;
    double y_;
};

// A class whose constructor is inline too: the other shape the generator has to
// refuse.
class Point {
public:
    explicit Point(int value) : value_(value) {}

    int value() const { return value_; }

private:
    int value_;
};

// A polymorphic class: virtual destructor, a pure virtual with no body at all,
// and one ordinary out-of-line member.
class Shape {
public:
    virtual ~Shape();
    virtual double area() const = 0;
    int id() const;

protected:
    Shape(int id);

private:
    int id_;
};

// Tiny is small enough to ride in a register, but it declares a destructor, so
// the Itanium ABI returns it indirectly anyway: the caller must pass a hidden
// destination pointer. libclang has no "trivial for the purposes of calls"
// query, so this is the case that proves the structural rule in
// indirectResult.
struct Tiny {
    int value;
    ~Tiny();
};

// Pair, by contrast, is a plain aggregate: 16 bytes, trivially copyable, and it
// comes back in XMM0:XMM1 with no hidden pointer.
struct Pair {
    double a;
    double b;
};

// A deep hierarchy: five bases before the member that makes it non-trivial.
struct Level6 {
    int value;
    ~Level6();
};
struct Level5 : Level6 {};
struct Level4 : Level5 {};
struct Level3 : Level4 {};
struct Level2 : Level3 {};
struct Level1 : Level2 {};

// Small records that the ABI returns through a hidden pointer for reasons that
// have nothing to do with their size: each one is non-trivial for the purposes
// of calls in a different way. All of them are two eightbytes or less.
struct SmallVirtual {
    int value;
    virtual void touch();
};
struct SmallCopy {
    int value;
    SmallCopy();
    SmallCopy(const SmallCopy &other);
};
struct SmallMove {
    int value;
    SmallMove();
    SmallMove(SmallMove &&other);
};
struct VirtualBase {
    int value;
};
struct SmallVirtualBase : virtual VirtualBase {};
struct SmallMember {
    Tiny inner;
};

// Records below exist for the layout model: a mirrorable aggregate, a nested
// one, a bitfield, a packed layout Go cannot reproduce, and an incomplete type
// behind a pointer.

struct Point2D {
    double x;
    double y;
};

struct Segment {
    Point2D from;
    Point2D to;
    int id;
};

struct Flags {
    unsigned low : 3;
    unsigned high : 5;
    int ordinary;
};

#pragma pack(push, 1)
struct Packed {
    char tag;
    double value;
};
#pragma pack(pop)

struct Mixed {
public:
    int visible;

protected:
    int inherited;

private:
    int hidden;
};

class Forward;
struct UsesForward {
    Forward *pointer;
};

// Composition of trivially constructible fields, for the constructibility
// predicate: a nested trivial record and an array of them.
struct HasPair {
    Pair pair;
    Pair pairs[2];
};

union Value {
    int i;
    float f;
};

template <typename T>
struct Box {
    T value;
    T get() const { return value; }
    template <typename U>
    U convert(U input) const { return input; }
    operator T() const { return value; }
    static T shared;
    struct Inner { T inner; };
    enum class Kind : int { A, B = 3 };
};
// The holder before the explicit instantiation makes the specialisation
// reachable twice: once through the field, once through the instantiation.
struct BoxIntHolder { Box<int> value; };
template struct Box<int>;

struct BoxHolder {
    Box<double> box;
};

template <typename T, int N>
struct Fixed {
    T values[N];
};
struct FixedHolder {
    Fixed<double, 4> data;
};

template <typename T>
struct Pair2 {
    T first;
    T second;
    Pair2(T a, T b) : first(a), second(b) {}
    ~Pair2() {}
    T sum() const { return first + second; }
};
template struct Pair2<int>;

template <typename T>
struct NoCopy {
    T value;
    NoCopy() = default;
    NoCopy(const NoCopy &) = delete;
};
template struct NoCopy<int>;

template <typename... Ts>
struct Pack {
    int n;
};
template struct Pack<int, double>;

// Nested enum initializers of a specialisation: literal, negative, hex, a
// non-type parameter, and one the model cannot read.
template <typename T, int N>
struct EnumBox {
    enum class Kind : int { A, B = 3, C = -2, D = 0x10, E = N, F = 1 + 2 };
};
struct EnumBoxHolder { EnumBox<int, 7> value; };
template struct EnumBox<int, 7>;

template <typename T>
struct BigEnum {
    enum class Big : unsigned long long { Max = 0xFFFFFFFFFFFFFFFF };
};
template struct BigEnum<int>;

// Nested declarations the model has to skip or name: an unnamed struct and
// enum, a forward-declared struct, and an opaque enum declaration.
template <typename T>
struct NestedOdd {
    struct { int x; } anonymous;
    struct Fwd;
    Fwd *pointer;
    enum { Anonymous = 1 };
    enum class FwdEnum : int;
};
template struct NestedOdd<int>;

// The member kinds the contract names: operators, a conversion operator,
// static data (mutable and const), and a nested type.
struct Operators {
    int value;
    int operator+(const Operators &other) const;
    bool operator==(const Operators &other) const;
    int &operator[](int index);
    operator int() const;
    static int counter;
    static const int limit = 3;
    struct Entry { int index; };
    enum class Level : unsigned { Low, High = 5 };
};

// A derived specialization: the base name is substituted, and the thunk has to
// adjust this for the base subobject.
template <typename T>
struct DerivedBox : Box<T> {
    T extra;
};
template struct DerivedBox<int>;

// A polymorphic specialization: synthesis has to carry the vtable facts.
template <typename T>
struct VirtualBox {
    virtual ~VirtualBox() {}
    virtual T get() const { return T{}; }
    T value;
};
template struct VirtualBox<int>;

// A field whose type is itself a specialization of a parameter: the
// RecordName has to be substituted, not only the scalar spellings.
template <typename T>
struct Wrapper {
    Box<T> inner;
};
template struct Wrapper<int>;

template <typename T>
T max_of(T a, T b) { return a < b ? b : a; }
template double max_of<double>(double, double);
struct WithMemberTemplate {
    template <typename T>
    T identity(T value) const { return value; }
    int member;
};

// Exception specifications, for carrier routing.
int throw_none_add(int a, int b) throw();
int computed_add(int a, int b) noexcept(sizeof(int) == 4);

// Multiple inheritance and a virtual base: the thunk performs the casts.
struct Left {
    virtual int left() const;
};
struct Right {
    int tag;
    int right() const;
};
struct Both : Left, Right {
    int both() const;
};
struct VBase {
    int value;
};
struct VDerived : virtual VBase {
    int derived() const;
};

enum Mode { ModeOff, ModeOn = 5, ModeMax = 10 };

// Unnamed records and enums have no name to bind under. The model drops them
// instead of inventing one.
struct {
    int value;
} anonymous_record;

enum { AnonymousValue = 1 };

// A type in an anonymous namespace has no qualified name, so a base like this
// is recorded by its spelling and never generated.
namespace {
struct Hidden {
    int value;
};
} // namespace

struct DerivedFromHidden : Hidden {
    int tag;
};

// A field whose type is an unnamed struct: the type mapper falls back to the
// type spelling.
struct WithAnonymous {
    struct {
        int inner;
    } anon;
};

Vector make(double x, double y);
Tiny make_tiny(int value);
Level1 make_deep();
SmallVirtual make_small_virtual(int value);
SmallCopy make_small_copy(int value);
SmallMove make_small_move(int value);
SmallVirtualBase make_small_virtual_base(int value);
SmallMember make_small_member(int value);
Pair make_pair(double a, double b);
bool is_axis(Axis axis, bool strict);
int add(int a, int b);
double add(double a, double b);
int noexcept_add(int a, int b) noexcept;
int variadic_sum(int count, ...);
void take_ref(const Pair &pair);
void take_nullptr(std::nullptr_t value);

// Internal linkage: a definition out of line, but no exported symbol.
static int internal_counter();

namespace {
void hidden_helper();
}

} // namespace mathx
