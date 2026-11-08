// Package shim measures the second carrier of the v4 contract. That carrier is
// the shim cppgo generates and compiles itself, into an object of its own that
// links the user's library.
//
// The package exists so the mechanism has a test that fails when it breaks, and
// so every claim the contract makes about a construct has a command behind it.
// The only generated piece here is the fixture. The point being measured is
// that a C++ translation unit compiled by the *tool*, rather than by the user's
// build, is what makes the hard constructs reachable.
package shim
