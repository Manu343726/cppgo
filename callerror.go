package cppgo

// CallError reports a bound call that failed, naming the C++ declaration that was
// called and wrapping the native failure.
//
// It is the error a generated binding returns when the call fails. Err carries the
// failure the C++ side reported, so errors.As still finds it through the
// CallError, and the message is "cppgo: <symbol>: <message>".
type CallError struct {
	// Symbol is the C++ declaration the binding called, e.g.
	// "mathx::Vector::norm".
	Symbol string
	// Err is the native failure, normally a *NativeError. A nil Err is
	// tolerated and formats as just the symbol.
	Err error
}

// Error implements the error interface.
func (e *CallError) Error() string {
	if e.Err == nil {
		return "cppgo: " + e.Symbol
	}
	return "cppgo: " + e.Symbol + ": " + e.Err.Error()
}

// Unwrap exposes the native failure to errors.Is and errors.As.
func (e *CallError) Unwrap() error { return e.Err }
