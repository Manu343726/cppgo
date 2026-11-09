package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCStringShape pins the two directions of a C string: a `const char *`
// parameter is a Go string copied into a NUL-terminated buffer for the call, and
// a `const char *` result is copied out of the library's storage into a Go
// string. A non-const `char *` parameter is the declaration that cannot carry.
func TestCStringShape(t *testing.T) {
	rendered, err := RenderResult(fixtureConfig("cstring", "cstring.hpp"))
	require.NoError(t, err)
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}

	assert.Contains(t, generated, "func Version() string", "a C string result is a Go string")
	assert.Contains(t, generated, "func Length(text string) int32", "a C string parameter is a Go string")
	assert.Contains(t, generated, ":= cppgoCString(text)", "the parameter is copied into a NUL-terminated buffer")
	assert.Contains(t, generated, "cppgoGoString(cppgoResult)", "the result is copied out of C storage")
	assert.Contains(t, generated, "func cppgoCString(s string) (unsafe.Pointer, []byte)", "the helpers are written only when needed")

	assert.Contains(t, joinWarnings(rendered.Warnings), "cstr::fill(char *)")
	assert.Contains(t, joinWarnings(rendered.Warnings), "non-const char pointer")
	assert.NotContains(t, generated, "func Fill", "a callee that writes into the string is not bound")
}

// cstringE2ESource is the program the generated package runs: both directions
// of the crossing, including the empty string (a non-null pointer to a zero
// byte) and bytes above ASCII.
const cstringE2ESource = `package cstr

import (
	"os"
	"testing"
)

func TestCString(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got := Version(); got != "1.2.3" {
		t.Fatalf("Version() = %q, want %q", got, "1.2.3")
	}
	if got := Length("hello"); got != 5 {
		t.Fatalf("Length(hello) = %d, want 5", got)
	}
	// The empty Go string crosses as a pointer to a zero byte, so the callee
	// sees "" and not null: the fixture reports -1 for null.
	if got := Length(""); got != 0 {
		t.Fatalf("Length(empty) = %d, want 0", got)
	}
	// strlen counts bytes: é is two bytes in UTF-8.
	if got := Length("héllo"); got != 6 {
		t.Fatalf("Length(héllo) = %d, want 6", got)
	}
	if got := NameOf(0); got != "zero" {
		t.Fatalf("NameOf(0) = %q, want zero", got)
	}
	if got := NameOf(7); got != "other" {
		t.Fatalf("NameOf(7) = %q, want other", got)
	}
}
`

// TestCStringRoundTrip runs the generated package against the fixture library,
// with cgo off and with the race detector on.
func TestCStringRoundTrip(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs(filepath.Join("testdata", "cstring"))
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libcstring.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "cstring.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "cstr")
	cfg := fixtureConfig("cstring", "cstring.hpp")
	cfg.Package = "cstr"
	cfg.Library = library
	cfg.Out = out
	require.NoError(t, Generate(cfg))
	shim := filepath.Join(out, ShimLibraryName("cstr"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "cstring_test.go"), []byte(cstringE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./cstr")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./cstr"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./cstr"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestCString")
		})
	}
}
