// Package cxx introspects C++ declarations through libclang and answers the two
// questions the code generator cannot answer on its own:
//
//   - what is the mangled symbol of this declaration, and
//   - does that symbol actually exist in the compiled library?
//
// The second question is the important one. A member function that is defined
// inside its class definition (an inline accessor, a ctor written in the header)
// is implicitly inline and emits **no dynamic symbol**. Binding it produces a
// trampoline that fails at load time. The generator must refuse those, with an
// actionable message. This package has to tell the difference between:
//
//	Vector::norm()          declared in the header, defined in the .cpp  -> symbol
//	Vector::x() const       defined inside the class                    -> no symbol
//
// See Symbol.HasOutOfLineBody and Project for how that is determined.
//
// # No cgo
//
// libclang is loaded at run time with purego's dlopen/dlsym wrappers. There is
// no import "C" anywhere in the package, and it builds with CGO_ENABLED=0.
// libclang's C API passes several C structs by value and returns others through
// a hidden pointer (see the inventory on libclangFuncs for every call and its
// class). This layer therefore needs an FFI that understands the SysV AMD64
// aggregate rules. The struct fields are uintptr for a reason documented on
// CXString, because libclang tags some cursors with small integers.
//
// Symbols are cross-checked against the library itself. DefinedDynamicSymbols
// reads the ELF dynamic symbol table, and the tests assert that what the model
// calls "has an out-of-line body" is exactly what `nm -D --defined-only` shows.
//
// # Compilation flags
//
// A translation unit is only meaningfully parsed with the flags it was
// compiled with. Without them a header fails to find its own includes.
// Discover implements clangd's resolution order. See the documentation of
// Discover for the exact steps and for which source won.
//
// When libclang is absent every entry point returns an error wrapping
// ErrLibclangNotFound so the caller can degrade to the shim mode.
package cxx
