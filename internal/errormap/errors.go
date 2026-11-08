package errormap

import "fmt"

// ErrorCode is a C++ failure that carries a numeric code, a category and a
// message projected into Go. Those fields are what std::error_code holds, what
// std::system_error wraps, and what a status int or an errno becomes when the
// config names a category for it.
//
// The generated binding returns it wrapped in the runtime's *cppgo.CallError,
// so errors.As finds both. ErrorCode implements Is, so errors.Is works against a
// target built with the same code and category. That is the strongest test the
// mapping supports, because a C++ error category is a singleton in the library
// and its identity is what a caller compares.
type ErrorCode struct {
	// Code is the numeric value: std::error_code::value(), the status int, or
	// the errno value.
	Code int
	// Category is the error category's name, e.g. "generic" or
	// "system:2", or the category the config named for a status channel.
	Category string
	// Message is the human-readable message the shim read from the category.
	Message string
}

// Error implements error as "cppgo: <category>: <message> (code <n>)". The
// shape is deliberately parseable. The fields are exported too, for callers that
// read the failure another way.
func (e *ErrorCode) Error() string {
	msg := e.Category
	if msg == "" {
		msg = "error"
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return fmt.Sprintf("cppgo: %s (code %d)", msg, e.Code)
}

// Is reports whether target is an *ErrorCode with the same code and category.
// The message is outside the identity. Two occurrences of errc::file_exists
// are the same failure even when their paths differ.
func (e *ErrorCode) Is(target error) bool {
	other, ok := target.(*ErrorCode)
	if !ok {
		return false
	}
	return e.Code == other.Code && e.Category == other.Category
}

// Message is a C++ failure that carries only text. It is a thrown
// std::runtime_error, or a std::expected whose error alternative is a string.
//
// It implements Is by message equality. A caller that knows the exact text a
// library produces can compare it, and errors.As is available for the ones that
// want the string without the comparison.
type Message struct {
	// Text is the message exactly as the library produced it.
	Text string
}

// Error implements error.
func (e *Message) Error() string {
	if e.Text == "" {
		return "cppgo: error"
	}
	return "cppgo: " + e.Text
}

// Is reports whether target is a *Message with the same text.
func (e *Message) Is(target error) bool {
	other, ok := target.(*Message)
	return ok && e.Text == other.Text
}

// Failed reports whether the status the shim returned means failure. Every
// thunk the generator emits uses zero for success and non-zero for a caught or
// reported failure. This is the one place that convention is written down.
func Failed(status int32) bool { return status != 0 }
