package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mathxConfig is the canonical fixture run: the header is named relative to the
// package directory so the generated invocation comment and the shim include
// are stable.
func mathxConfig() Config {
	return Config{
		Headers: []string{"testdata/mathx/vector.hpp"},
		Package: "mathx",
		Library: "libmathx",
		Out:     ".",
	}
}

// fixtureConfig is a run over another fixture directory.
func fixtureConfig(dir, header string) Config {
	return Config{
		Headers: []string{filepath.Join("testdata", dir, header)},
		Package: dir,
		Library: "lib" + dir,
		Out:     ".",
	}
}

// renderMap renders a configuration and returns its files by name.
func renderMap(t *testing.T, cfg Config) map[string]string {
	t.Helper()
	outs, err := Render(cfg)
	require.NoError(t, err)
	m := make(map[string]string, len(outs))
	for _, o := range outs {
		m[o.Name] = string(o.Data)
	}
	return m
}

// findBinding returns the binding with the given Go name.
func findBinding(t *testing.T, m *packageModel, name string) *binding {
	t.Helper()
	for _, binding := range m.bindings {
		if binding.GoName == name {
			return binding
		}
	}
	require.FailNowf(t, "binding not found", "%s is not in the model", name)
	return nil
}

// findRecord returns the record with the given Go name.
func findRecord(t *testing.T, m *packageModel, name string) *recordModel {
	t.Helper()
	for _, record := range m.records {
		if record.GoName == name {
			return record
		}
	}
	require.FailNowf(t, "record not found", "%s is not in the model", name)
	return nil
}

// readFileString reads a golden file.
func readFileString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// outputNamed returns one rendered output's text.
func outputNamed(t *testing.T, result *Result, name string) string {
	t.Helper()
	for _, out := range result.Outputs {
		if out.Name == name {
			return string(out.Data)
		}
	}
	require.FailNowf(t, "output not found", "%s is not one of the run's outputs", name)
	return ""
}
