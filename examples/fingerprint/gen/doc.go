// Package fingerprint is the Go API of the fingerprint C++ library. cppgo
// generates it from include/fingerprint/fingerprint.hpp, and the generated
// files beside this one are the whole package: nothing here is hand-written
// except this comment.
//
// The surface is two calls and the types they carry. Trigger is opaque — a
// constructor, a Close, and the working size its prefilter runs at — and its
// Evaluate returns a TriggerOutcome: the verdict for one frame, the state to
// pass in on the next, and where the time went inside C++. Enroll runs the
// whole pipeline on one frame and returns an Enrollment whose bytes are the
// ANSI/INCITS 378 subset the CLI writes to disk.
//
// Nothing here exposes a pixel, a matrix, a minutia or a field of the template.
// The Go side hands over an encoded frame and gets a decision or a template
// back; the image work is the C++ library's, in a call each.
package fingerprint
