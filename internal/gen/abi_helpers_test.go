package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// libclangAvailable reports whether the direct ABI mode can resolve anything
// at all on this machine. Every abi-mode test that parses a header needs it.
func libclangAvailable() bool {
	library, err := cxx.Load()
	if err != nil {
		return false
	}
	// Decrement the reference count taken by Load; the process-wide handle
	// inside internal/cxx stays alive for the other callers.
	_ = library.Close()
	return true
}

// requireLibclang skips the test when libclang is missing.
func requireLibclang(t *testing.T) {
	t.Helper()
	if !libclangAvailable() {
		t.Skip("libclang is not available: the direct ABI mode cannot resolve its bindings")
	}
}

// testdataPath is an absolute path inside internal/gen/testdata.
func testdataPath(t *testing.T, parts ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(append([]string{"testdata"}, parts...)...))
	require.NoError(t, err)
	return path
}

// cxxTestdataPath is an absolute path inside internal/cxx/testdata, which holds
// the real mathx class the end-to-end test binds.
func cxxTestdataPath(t *testing.T, parts ...string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join(append([]string{"..", "cxx", "testdata"}, parts...)...))
	require.NoError(t, err)
	return path
}

// rulesHeader is the header of the validation-rules fixture.
func rulesHeader(t *testing.T) string {
	t.Helper()
	return testdataPath(t, "abi", "rules", "rules.hpp")
}

// mathxHeader is the header of the real mathx class.
func mathxHeader(t *testing.T) string {
	t.Helper()
	return cxxTestdataPath(t, "mathx", "mathx.hpp")
}

// writeABIPkg writes a one-file abi-mode package that binds the given header,
// with body appended after the //cppgo:library and //cppgo:header directives.
func writeABIPkg(t *testing.T, header, body string) string {
	t.Helper()
	src := "package x\n\nimport \"github.com/Manu343726/cppgo\"\n\n//cppgo:library libx\n//cppgo:header " + header + "\n\n" + body + "\n"
	return writePkg(t, map[string]string{"x.go": src})
}

// renderABI renders the package in dir and returns its files by name,
// requiring success.
func renderABI(t *testing.T, cfg Config) map[string]string {
	t.Helper()
	res, err := RenderResult(cfg)
	require.NoError(t, err)
	files := make(map[string]string, len(res.Outputs))
	for _, o := range res.Outputs {
		files[o.Name] = string(o.Data)
	}
	return files
}

// renderABIError renders the package in dir and returns the error, requiring
// that generation failed.
func renderABIError(t *testing.T, dir string) string {
	t.Helper()
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err)
	return err.Error()
}

// renderReports renders a package and returns its report and warnings.
func renderReports(t *testing.T, cfg Config) (files map[string]string, report, warnings []string) {
	t.Helper()
	res, err := RenderResult(cfg)
	require.NoError(t, err)
	files = make(map[string]string, len(res.Outputs))
	for _, o := range res.Outputs {
		files[o.Name] = string(o.Data)
	}
	return files, res.Report, res.Warnings
}

// writeProbeFixture writes a package that binds the incomplete-class fixture,
// whose size probe has to fail.
func writeProbeFixture(t *testing.T) string {
	t.Helper()
	header := testdataPath(t, "abi", "incomplete", "incomplete.hpp")
	dir := writeABIPkg(t, header, `//cppgo:class incomplete::Node
type Node cppgo.Handle

//cppgo:ctor
func New() (Node, error)
`)
	return dir
}
