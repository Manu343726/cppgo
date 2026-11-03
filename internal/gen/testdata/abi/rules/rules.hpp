#pragma once

// rules.hpp is the fixture for the direct ABI mode's validation rules. Every
// declaration in it is here because the generator has to REJECT it (or, for
// the few marked as fine, accept it and check a layout detail): a by-value
// struct, a std:: type, a virtual member, an inline member, internal linkage,
// register exhaustion, and the widths that make a status a status.
//
// Nothing in this file is bound at run time: the tests assert on the
// diagnostics, so the shapes only have to be declarations the compiler accepts.

#include <cstddef>
#include <string>

namespace rules {

// Pod is a plain aggregate, which the direct ABI mode still refuses to pass by
// value: it would be memory-class on the stack.
struct Pod {
    int a;
    int b;
};

class Widget {
public:
    // Out of line in rules.cpp: a symbol, and a 4-byte object with 4-byte
    // alignment (the shape the constructor allocation has to reproduce).
    Widget(int n);
    ~Widget();

    int size() const;

    // Defined inside the class: no exported symbol.
    int inline_size() const { return n_; }

    // Virtual: reachable only through the vtable.
    virtual int area() const;

    // Six explicit integer arguments plus `this` is one more than the SysV
    // budget of six.
    int six(int a, int b, int c, int d, int e, int f) const;

    // A static member: no `this`, so it is bound with //cppgo:function.
    static int dims();

private:
    int n_;
};

// A by-value struct parameter.
int sum_pod(Pod p);

// A standard-library type by value.
std::size_t text_length(std::string s);

// A struct returned by value: the sret convention, which v1 does not emit.
Pod make_pod(int a, int b);

// A pointer-to-member parameter: neither an integer nor a pointer at the ABI
// level.
int pick(Widget w, int Widget::*member);
int pick_one(int Widget::*member);

// An 8-byte integer status.
long count();

// A void function: a binding with a trailing error has nothing to read the
// status from.
void notify();

// A C string, which a Go string crosses as (pointer, length).
int text_bytes(const char *text, std::size_t length);

// Two overloads that differ only by arity: -force has to pick the one whose
// arity matches when the types do not.
int over(int a);
int over(int a, int b);

// Two overloads that differ by parameter width: the Go signature has to pick
// one, and getting the width wrong has to be an error.
int add(int a, int b);
double add(double a, double b);

// Floats, to check the register class and the width separately.
double ratio(double a, float b);

// Internal linkage: a definition out of line, but no exported symbol.
static int internal_counter();

// An anonymous namespace has internal linkage too, and is invisible to the
// generator: binding it must report "not declared".
namespace {
void hidden_helper();
}

} // namespace rules
