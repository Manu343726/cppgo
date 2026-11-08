package cxx

// DiagnosticSeverity is libclang's CXDiagnosticSeverity.
type DiagnosticSeverity uint8

const (
	// DiagnosticIgnored is a message deliberately ignored by the compiler.
	DiagnosticIgnored DiagnosticSeverity = iota
	// DiagnosticNote annotates another diagnostic.
	DiagnosticNote
	// DiagnosticWarning is a warning.
	DiagnosticWarning
	// DiagnosticError is an error: the translation unit is not valid.
	DiagnosticError
	// DiagnosticFatal is a fatal error, after which parsing stopped.
	DiagnosticFatal
)

// String implements fmt.Stringer.
func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticIgnored:
		return "ignored"
	case DiagnosticNote:
		return "note"
	case DiagnosticWarning:
		return "warning"
	case DiagnosticError:
		return "error"
	case DiagnosticFatal:
		return "fatal error"
	default:
		return "unknown diagnostic"
	}
}

// diagnosticSeverityFromKind maps clang_getDiagnosticSeverity onto the model.
func diagnosticSeverityFromKind(kind int32) DiagnosticSeverity {
	switch kind {
	case 1:
		return DiagnosticNote
	case 2:
		return DiagnosticWarning
	case 3:
		return DiagnosticError
	case 4:
		return DiagnosticFatal
	default:
		return DiagnosticIgnored
	}
}

// Diagnostic is one message libclang produced while parsing a translation unit.
type Diagnostic struct {
	// Severity is how serious the message is.
	Severity DiagnosticSeverity
	// Message is the text without the source location prefix.
	Message string
	// File and Line locate the diagnostic, empty and zero when unknown.
	File string
	Line int
}

// Diagnostics returns the messages of the translation unit, in the order the
// compiler produced them. It is what lets the generator prove that a shim it is
// about to write compiles. An Error or Fatal diagnostic is a generation error.
func (tu *TU) Diagnostics() []Diagnostic {
	if tu.isClosed() {
		return nil
	}
	count := tu.l.f.getNumDiagnostics(tu.tu)
	out := make([]Diagnostic, 0, count)
	for i := uint32(0); i < count; i++ {
		diagnostic := tu.l.f.getDiagnostic(tu.tu, i)
		severity := diagnosticSeverityFromKind(tu.l.f.getDiagnosticSeverity(diagnostic))
		message := tu.l.str(tu.l.f.getDiagnosticSpelling(diagnostic))
		file, line := tu.l.locationValues(tu.l.f.getDiagnosticLocation(diagnostic))
		tu.l.f.disposeDiagnostic(diagnostic)
		out = append(out, Diagnostic{Severity: severity, Message: message, File: file, Line: line})
	}
	return out
}
