package cppgo

// CallError reports a bound call that failed, naming the C++ declaration that
// was called and wrapping the native failure.
//
// A generated binding returns one when a thunk reports a non-zero status: Err
// is the *NativeError carrying the message the library stored in
// cppgo_last_error, so errors.As(err, &native) still finds it through the
// CallError. The formatted message is "cppgo: <symbol>: <message>".
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
