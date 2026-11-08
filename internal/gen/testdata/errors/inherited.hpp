#pragma once
namespace err {
struct Base { int value() const; };
struct Derived : Base { int other() const; };
}
