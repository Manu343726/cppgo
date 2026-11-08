#pragma once
namespace err {
struct Inner { int x; };
struct Outer { Inner inner; };
}
