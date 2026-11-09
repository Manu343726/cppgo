// Package eigen binds a slice of Eigen's Dense header to Go, and is the worked
// example of binding a header-only C++ library.
//
// It is generated from Eigen/Dense of the Eigen installation: the Go types and
// functions here are the C++ API as a caller sees it. It is generator output,
// not hand-written code.
//
// Eigen is header-only, so there is no shared object to build and none to link:
// the shim cppgo compiles holds the bound functions itself, which makes it the
// only object a caller loads.
//
// The bound declarations are Eigen's configuration enums and the functions that
// report and set the thread count its parallel algorithms use. The matrix types
// are not bound yet; the module's README names the declaration that stops them.
package eigen
