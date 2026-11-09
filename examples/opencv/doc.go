// Package opencv binds a slice of OpenCV's core header to Go, and is the worked
// example of binding a large, installed C++ library.
//
// It is generated from opencv2/core.hpp of the OpenCV installation: the Go types
// and functions here are the C++ API as a caller sees it, with Go values and Go
// errors. It is generator output, not hand-written code.
//
// The C++ side is OpenCV itself. There is no library to build in this directory:
// the shim cppgo compiles links against the system's libopencv_core.so, and a
// caller needs nothing from it but the generated package.
//
// The bound declarations exercise what a large header asks of a binding: an enum
// whose values are documented in the header, a small record with its
// constructors, a field accessor per public member, an object that holds state
// across calls, and free functions that report the library's own configuration.
package opencv
