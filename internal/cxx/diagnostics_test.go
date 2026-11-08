package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseSource parses a scratch C++ file and returns its translation unit.
func parseSource(t *testing.T, body string) *TU {
	t.Helper()
	library := skipWithoutLibclang(t)
	path := filepath.Join(t.TempDir(), "scratch.cpp")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	tu, err := library.ParseTranslationUnit(path, []string{"-std=c++17", "-x", "c++"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tu.Close() })
	return tu
}

// TestDiagnosticsReportsErrors checks that a malformed translation unit yields
// an error diagnostic with its file and line.
func TestDiagnosticsReportsErrors(t *testing.T) {
	tu := parseSource(t, "int broken( {\n")
	diagnostics := tu.Diagnostics()
	require.NotEmpty(t, diagnostics)

	var found *Diagnostic
	for i := range diagnostics {
		if diagnostics[i].Severity == DiagnosticError || diagnostics[i].Severity == DiagnosticFatal {
			found = &diagnostics[i]
			break
		}
	}
	require.NotNil(t, found, "a syntax error must be reported as an error")
	assert.NotEmpty(t, found.Message)
	assert.Equal(t, filepath.Clean(tu.File), filepath.Clean(found.File))
	assert.Greater(t, found.Line, 0)
}

// TestDiagnosticsClean checks that a valid translation unit has no errors.
func TestDiagnosticsClean(t *testing.T) {
	tu := parseSource(t, "namespace ok { int add(int a, int b) { return a + b; } }\n")
	for _, diagnostic := range tu.Diagnostics() {
		assert.Less(t, diagnostic.Severity, DiagnosticError, diagnostic.Message)
	}
}

// TestDiagnosticsAfterClose checks the disposed handle is not walked.
func TestDiagnosticsAfterClose(t *testing.T) {
	library := skipWithoutLibclang(t)
	path := filepath.Join(t.TempDir(), "scratch.cpp")
	require.NoError(t, os.WriteFile(path, []byte("int fine() { return 0; }\n"), 0o644))
	tu, err := library.ParseTranslationUnit(path, []string{"-std=c++17", "-x", "c++"})
	require.NoError(t, err)
	require.NoError(t, tu.Close())
	assert.Nil(t, tu.Diagnostics())
}

// TestDiagnosticSeverityMapping checks the libclang values and their names.
func TestDiagnosticSeverityMapping(t *testing.T) {
	cases := []struct {
		kind int32
		want DiagnosticSeverity
		name string
	}{
		{0, DiagnosticIgnored, "ignored"},
		{1, DiagnosticNote, "note"},
		{2, DiagnosticWarning, "warning"},
		{3, DiagnosticError, "error"},
		{4, DiagnosticFatal, "fatal error"},
		{42, DiagnosticIgnored, "ignored"},
	}
	for _, tc := range cases {
		got := diagnosticSeverityFromKind(tc.kind)
		assert.Equal(t, tc.want, got, tc.kind)
		assert.Equal(t, tc.name, got.String(), tc.kind)
	}
	assert.Equal(t, "unknown diagnostic", DiagnosticSeverity(99).String())
}
