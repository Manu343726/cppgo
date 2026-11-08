#pragma once
namespace err {
struct Op { int value; };
bool operator==(const Op &a, const Op &b);
}
