// Package shim is the test fixture for the second carrier: the translation unit
// cppgo compiles itself, which links the user's library and carries every
// declaration the direct ABI cannot call.
//
// The package holds no code, only tests. testdata/user.cpp is the user's library,
// built with no cppgo file in it; testdata/shim.cpp is written the way the
// generator writes one, compiled by the tests into an object of its own and linked
// against the user's library. What that arrangement measures is the contract's
// claim: a translation unit compiled by cppgo, rather than by the user's build, is
// what makes inline bodies, virtual dispatch, templates, standard-library types,
// non-POD values, multiple inheritance and the callback parameter reachable. One
// test in the same fixture calls an exported, out-of-line function by its mangled
// name with no shim involved, so the rows have a baseline to be compared against.
package shim
