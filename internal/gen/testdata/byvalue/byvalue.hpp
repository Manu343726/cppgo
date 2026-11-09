#pragma once

#include <cstdint>

// The by-value fixture: a record Go cannot mirror, crossed in both directions.
//
// Counter has a user-provided constructor and destructor and a private member,
// so it is not a POD and has no layout Go may copy: the shim makes every copy
// and Go holds the object it allocated. Two things make the copy and the
// destructor observable from Go:
//
//   - the copy constructor adds 1000 to the value it copies, so a Go caller can
//     tell a value that crossed by value from the one it held;
//   - the destructor counts itself, so Close's effect is a number Go reads.
namespace byval {

class Counter {
public:
    Counter();
    explicit Counter(std::int32_t value);
    Counter(const Counter &other);
    ~Counter();

    std::int32_t value() const noexcept;
    void set(std::int32_t value) noexcept;

private:
    std::int32_t value_;
};

/// read copies its argument into the call and returns what the copy holds.
std::int32_t read(Counter counter) noexcept;

/// make returns a value the Go side owns.
Counter make(std::int32_t value) noexcept;

/// passthrough copies in and returns the copy: +1000 on the way out.
Counter passthrough(Counter counter) noexcept;

/// NoDtor is not a POD either, because its constructor is user-provided, but it
/// declares no destructor: a value the Go side obtains by value releases the
/// storage alone, and Close has no destructor to run.
class NoDtor {
public:
    explicit NoDtor(std::int32_t value) noexcept;
    std::int32_t value() const noexcept;

private:
    std::int32_t value_;
};

/// make_no_dtor returns the record with no destructor by value.
NoDtor make_no_dtor(std::int32_t value) noexcept;

/// read_no_dtor takes the record with no destructor by value.
std::int32_t read_no_dtor(NoDtor value) noexcept;

/// destructions is how many destructors have run in this process, which is the
/// evidence Close destroys and a failed call does not.
std::int32_t destructions() noexcept;

} // namespace byval
