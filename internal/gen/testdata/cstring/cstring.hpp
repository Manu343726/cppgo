#pragma once
namespace cs {

class Named {
public:
    Named(const char *name);
    const char *name() const;
};

const char *label();

} // namespace cs
