// Package shim measures the second carrier of the v4 contract: the shim cppgo
// generates and compiles itself, into an object of its own that links the
// user's library.
//
// The package exists so the mechanism has a test that fails when it breaks, and
// so every claim the contract makes about a construct has a command behind it.
// Nothing here is generated at build time beyond the fixture: the point being
// measured is that a C++ translation unit the *tool* compiles — not the user's
// build — is what makes the hard constructs reachable.
package shim
