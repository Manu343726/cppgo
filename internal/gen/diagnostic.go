package gen

// thunkSpan is one thunk's line range in the generated translation unit. It is
// how a compiler diagnostic is turned back into the binding that produced it,
// which is the difference between "the shim does not compile" and "the thunk of
// user::Circle::area does not compile".
type thunkSpan struct {
	start  int
	end    int
	symbol string
}

// shimSymbol maps a diagnostic line back to the binding whose thunk it is in,
// or to the generated file when the line is outside every thunk.
func shimSymbol(spans []thunkSpan, line int) string {
	for _, span := range spans {
		if line >= span.start && line <= span.end {
			return span.symbol
		}
	}
	return ShimSourceFileName
}
