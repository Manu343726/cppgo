// Package arrayfire binds a slice of ArrayFire's C++ header to Go, and is the
// worked example of binding a library whose arrays are opaque handles.
//
// It is generated from af/array.h of the ArrayFire installation: the Go types
// and functions here are the C++ API as a caller sees it, with Go values and Go
// errors. It is generator output, not hand-written code.
//
// The C++ side is ArrayFire itself. There is no library to build in this
// directory: the shim cppgo compiles links against the system's libaf.so, and a
// caller needs nothing from it but the generated package.
//
// The bound declarations are the ones that cross as values: the element type and
// the data-source enums, and the scalar functions that report the library's
// device and memory configuration. The array type itself is not bound yet; the
// module's README names the declaration that stops it.
package arrayfire
