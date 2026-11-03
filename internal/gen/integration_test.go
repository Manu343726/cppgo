package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedPackageBuildsAndRuns is the end-to-end proof that the emitted
// code is not just well-formed text:
//
//   - the generated package is compiled and vetted (go vet runs asmdecl, which
//     checks every FP offset and frame size against the Go declarations);
//   - the generated trampolines are then called for real against assembly
//     stand-ins for the C++ shim, which exercise the register assignment, the
//     stack switch and the result slots without needing cgo;
//   - the C++ header is fed to a C++ compiler when one is available.
//
// The module built here carries a stub of the cppgo runtime so the generator
// can be tested before the runtime package itself exists.
func TestGeneratedPackageBuildsAndRuns(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not in PATH")
	}

	tmp := t.TempDir()
	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, copyTree("testdata/e2e", moduleDir))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"),
		[]byte("module github.com/Manu343726/cppgo\n\ngo 1.25.0\n"), 0o644))

	// Generate into genpkg, leaving the DSL source outside the module.
	src := filepath.Join(tmp, "src")
	require.NoError(t, copyTree("testdata/e2e/src", src))
	require.NoError(t, Generate(Config{Dir: src, Out: filepath.Join(moduleDir, "genpkg")}))

	vetted := runGo(t, goBin, moduleDir, "vet", "./genpkg")
	require.NotNil(t, vetted)
	ran := runGo(t, goBin, moduleDir, "test", "-count=1", "./genpkg")
	assert.Contains(t, ran, "ok", "the generated package must run its own tests")

	// The C++ header must be valid C++, not just plausible text.
	if gxx, err := exec.LookPath("g++"); err == nil {
		cmd := exec.Command(gxx, "-std=c++17", "-fsyntax-only", "-x", "c++", "cppgo_shim.h")
		cmd.Dir = filepath.Join(moduleDir, "genpkg")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}
}

// runGo runs the go tool in dir and returns its combined output.
func runGo(t *testing.T, goBin, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(goBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOFLAGS=",
		"GOWORK=off",
		"GOPROXY=off",
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go %s failed:\n%s", strings.Join(args, " "), out)
	return string(out)
}

// copyTree copies a directory tree.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
