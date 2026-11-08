package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// validateShim parses the generated shim through libclang with the project's
// flags before anything is written. An Error or Fatal diagnostic is a
// generation error naming the binding whose thunk it belongs to and the
// compiler line, which is what makes the contract's "a thunk that would not
// compile is a generation error" true.
//
// The files are written to a scratch directory because libclang parses files,
// not buffers; the content is byte-identical to what the package writes.
func (m *packageModel) validateShim(header, source []byte, spans []thunkSpan) error {
	if len(m.flags) == 0 {
		// A model built without headers (the emitter tests) has no flags and
		// nothing to compile against.
		return nil
	}
	dir, err := os.MkdirTemp("", "cppgo-shim-")
	if err != nil {
		return fmt.Errorf("cppgo: creating the shim self-check directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	headerPath := filepath.Join(dir, ShimHeaderFileName)
	if err := os.WriteFile(headerPath, header, 0o644); err != nil {
		return fmt.Errorf("cppgo: writing %s for the self-check: %w", ShimHeaderFileName, err)
	}

	diagnostics, err := m.parseShim(dir, "full", source)
	if err != nil {
		return err
	}
	first := firstError(diagnostics)
	if first == nil {
		return nil
	}

	// A diagnostic inside an inline body is attributed to the header, not to
	// the thunk that instantiated it. Isolating one thunk per translation unit
	// is what names the binding, and it only runs on the failure path.
	for _, binding := range m.bindings {
		if binding.Carrier != thunk {
			continue
		}
		_, single, _, err := m.renderShim(binding)
		if err != nil {
			return err
		}
		singleDiagnostics, err := m.parseShim(dir, binding.ID, single)
		if err != nil {
			return err
		}
		if diagnostic := firstError(singleDiagnostics); diagnostic != nil {
			return shimDiagnostic(dir, binding.sym.QualifiedName, diagnostic)
		}
	}
	return shimDiagnostic(dir, shimSymbol(spans, first.Line), first)
}

// parseShim writes source to a scratch translation unit and parses it with the
// project's flags. The shim header is already in dir.
func (m *packageModel) parseShim(dir, name string, source []byte) ([]cxx.Diagnostic, error) {
	sourcePath := filepath.Join(dir, name+".cpp")
	if err := os.WriteFile(sourcePath, source, 0o644); err != nil {
		return nil, fmt.Errorf("cppgo: writing %s for the self-check: %w", ShimSourceFileName, err)
	}
	library, err := cxx.Load()
	if err != nil {
		return nil, fmt.Errorf("cppgo: loading libclang for the shim self-check: %w", err)
	}
	defer func() { _ = library.Close() }()
	args := m.shimFlags(dir)
	if resourceDir := library.ResourceDir(); resourceDir != "" {
		// libclang loaded as a shared object does not add clang's resource
		// directory by itself, so a builtin header in the bound header (or in
		// the shim) would be "not found" without this.
		args = append([]string{"-resource-dir", resourceDir}, args...)
	}
	tu, err := library.ParseTranslationUnit(sourcePath, args)
	if err != nil {
		return nil, fmt.Errorf("cppgo: parsing %s for the self-check: %w", ShimSourceFileName, err)
	}
	defer func() { _ = tu.Close() }()
	return tu.Diagnostics(), nil
}

// firstError returns the first Error or Fatal diagnostic, or nil.
func firstError(diagnostics []cxx.Diagnostic) *cxx.Diagnostic {
	for i := range diagnostics {
		if diagnostics[i].Severity >= cxx.DiagnosticError {
			return &diagnostics[i]
		}
	}
	return nil
}

// shimDiagnostic renders one compiler diagnostic as the contract's one-line
// error. A location inside the scratch directory is reported as the generated
// file the user will actually compile.
func shimDiagnostic(dir, symbol string, diagnostic *cxx.Diagnostic) error {
	file := diagnostic.File
	if file != "" && filepath.Dir(file) == dir {
		file = filepath.Base(file)
	}
	if file == "" {
		file = ShimSourceFileName
	}
	return declError(file, diagnostic.Line, symbol, pending(
		"the generated shim does not compile",
		diagnostic.Message))
}

// shimFlags is the project's flag list made fit for parsing the shim: the
// language selection is replaced with C++ (the headers were discovered as
// C++ headers), and the scratch directory, the working directory and each
// header's directory are on the include path so the shim's own includes
// resolve exactly as they will in the user's build.
func (m *packageModel) shimFlags(dir string) []string {
	out := make([]string, 0, len(m.flags)+2*len(m.headers)+6)
	for i := 0; i < len(m.flags); i++ {
		arg := m.flags[i]
		switch {
		case arg == "-x":
			i++
		case strings.HasPrefix(arg, "-x"):
			continue
		default:
			out = append(out, arg)
		}
	}
	out = append(out, "-x", "c++", "-I", dir)
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		out = append(out, "-I", cwd)
	}
	for _, header := range m.headers {
		if abs, err := filepath.Abs(header); err == nil {
			out = append(out, "-I", filepath.Dir(abs))
		}
	}
	return out
}

// shimSymbol maps a diagnostic line back to the binding whose thunk it is in.
func shimSymbol(spans []thunkSpan, line int) string {
	for _, span := range spans {
		if line >= span.start && line <= span.end {
			return span.symbol
		}
	}
	return ShimSourceFileName
}
