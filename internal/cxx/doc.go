// Package cxx introspects C++ declarations through libclang: it produces the
// mangled symbol of a declaration, decides whether that symbol is in the compiled
// library, and reports the layout, access and template information the generator
// needs to emit a binding.
//
// It is the parsing half of the generator layer, with internal/gen and cmd/cppgo.
// The runtime never imports it: libclang is a generation-time tool.
//
// # No cgo
//
// libclang is loaded at run time with purego's dlopen/dlsym wrappers, so there is
// no import "C" in this package and it builds with CGO_ENABLED=0. libclang's C
// API passes several C structs by value and returns others through a hidden
// pointer, which is why this layer needs an FFI that understands the SysV AMD64
// aggregate rules; the inventory above libclangFuncs lists every call and its
// class. The pointer words of those structs are uintptr rather than
// unsafe.Pointer because libclang tags some cursors with small integers, and a Go
// pointer field holding 0x1 makes the runtime abort the process with "bad pointer
// in frame" (see CXString).
//
// # The symbol question
//
// A member defined inside its class definition is implicitly inline and a library
// emits no dynamic symbol for it, so a direct call fails at load time and the
// binding has to go through a generated thunk. Symbol carries that evidence in
// HasOutOfLineBody, which Project computes by parsing the header and the sources
// that include it. The answer is cross-checked against the library itself:
// DefinedDynamicSymbols reads the ELF dynamic symbol table, and the tests assert
// that "has an out-of-line body" is exactly what `nm -D --defined-only` shows.
//
// # Compilation flags
//
// A translation unit only parses meaningfully with the flags it was compiled
// with; without them a header cannot find its own includes. Discover follows
// clangd's resolution order and records which rule produced the flags, so a
// surprising parse can be traced back to its source.
//
// Load returns an error wrapping ErrLibclangNotFound when the library cannot be
// located, and a caller uses that to degrade to the shim-only mode.
package cxx
