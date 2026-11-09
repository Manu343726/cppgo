#pragma once

// The reference-parameter fixture. `WidgetRef` is what libclang reports for
// OpenCV's `InputArray`: the parameter is a `const Widget &`, but the spelling
// the extractor sees is the alias, so a call site that decides "is this a
// reference" from the spelling alone writes a pointer the callee cannot take.
namespace refs {

class Widget {
public:
    Widget(int value) noexcept;
    int value() const noexcept;

private:
    int value_;
};

using WidgetRef = const Widget &;

int measure(const Widget &widget) noexcept;
int measure_alias(WidgetRef widget) noexcept;

} // namespace refs
