#pragma once
namespace err {
struct Op {
    int value;
    int operator+(const Op &other) const;
};
}
