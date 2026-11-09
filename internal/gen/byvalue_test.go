package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestByValueRecordShape pins the API and the thunk shape of a mirrored POD
// passed and returned by value. The Go value is the C layout, so the parameter
// is the struct itself, the thunk takes its address and copies it, and a result
// is written into Go storage whatever its size.
func TestByValueRecordShape(t *testing.T) {
	for _, tc := range []struct {
		header   string
		goFunc   string
		shimCall string
		readme   string
	}{
		{
			header:   "byvalue.hpp",
			goFunc:   "func Take(value Pod) (int32, error)",
			shimCall: "::err::take(*static_cast<const ::err::Pod *>(value))",
			readme:   "a 4-byte POD by value, through the out-pointer its status needs",
		},
		{
			header:   "bigresult.hpp",
			goFunc:   "func Make() Big",
			shimCall: "*static_cast<::err::Big *>(cppgo_result) = ::err::make()",
			readme:   "a 24-byte POD by value: a shim result with no error channel",
		},
	} {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			files := renderMap(t, Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
				Out:     ".",
			})
			assert.Contains(t, files[GenFileName], tc.goFunc, tc.readme)
			assert.Contains(t, files[ShimSourceFileName], tc.shimCall, tc.readme)
			// The record's C++ layout never appears in the generated C header:
			// the thunk casts the pointer where the layout exists.
			assert.NotContains(t, files[ShimHeaderFileName], "::err::")
		})
	}
}

// TestByValueRecordShimCompiles is the gate the shim self-check used to be: a
// real C++ compiler accepts the generated translation unit, for the two shapes
// whose argument and result cross as memory.
func TestByValueRecordShimCompiles(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	for _, header := range []string{"byvalue.hpp", "bigresult.hpp"} {
		header := header
		t.Run(header, func(t *testing.T) {
			dir := t.TempDir()
			cfg := Config{
				Headers: []string{filepath.Join("testdata", "errors", header)},
				Package: "err",
				Library: "liberr",
				Out:     dir,
			}
			require.NoError(t, Generate(cfg))
			// The fixture headers are relative to this package's directory,
			// which is how a user names them on the command line.
			output, err := exec.Command(gxx, "-std=c++17", "-fsyntax-only", "-I.",
				ShimPath(dir, ShimSourceFileName)).CombinedOutput()
			require.NoErrorf(t, err, "g++ rejected the shim:\n%s", output)
			_, err = os.Stat(filepath.Join(dir, GenFileName))
			require.NoError(t, err)
		})
	}
}

// TestNonPODByValueRecordShape pins the crossing of a record Go cannot mirror.
// Both directions are one shape: the Go value is *T, an opaque value owning
// storage aligned for the C++ type; a parameter is copied into the call by the
// shim, a result is constructed into Go's storage by it, and the destructor the
// record declares runs at Close. It is the 4c rule, and the doc comment states
// it because the caller is who needs to know who frees the object.
func TestNonPODByValueRecordShape(t *testing.T) {
	m := renderMap(t, Config{
		Headers: []string{filepath.Join("testdata", "byvalue", "byvalue.hpp")},
		Package: "byval",
		Library: "libbyval",
		Out:     ".",
	})
	generated := m[GenFileName]
	shim := m[ShimSourceFileName]

	// The API: the record crosses as *Counter in both directions, with the
	// error channel a throwing copy needs.
	assert.Contains(t, generated, "func Read(counter *Counter) (int32, error)")
	assert.Contains(t, generated, "func Make(value int32) (*Counter, error)")
	assert.Contains(t, generated, "func Passthrough(counter *Counter) (*Counter, error)")

	// A nil argument is refused before the shim copies from address 0.
	assert.Contains(t, generated, "if counter == nil {")
	assert.Contains(t, generated, "has no C++ object to copy")

	// The ownership rule is the generated doc comment, and it follows the
	// record's declaration: Counter declares a destructor, NoDtor does not.
	assert.Contains(t, generated, "The returned Counter is Go-owned: the shim constructs it from the callee's result, Close runs the C++ destructor once")
	assert.Contains(t, generated, "The returned NoDtor is Go-owned: the shim constructs it from the callee's result, Close releases the storage and runs no C++ destructor")
	assert.NotContains(t, generated, "The returned NoDtor is Go-owned: the shim constructs it from the callee's result, Close runs the C++ destructor once")
	assert.Contains(t, generated, "The value of counter is copied into the call by the shim")

	// The type-level comment carries the same distinction: Close destroys a
	// Counter, and only releases the storage of a NoDtor.
	assert.Contains(t, generated, "Close destroys it, and a value obtained by value is a copy the shim constructed here.")
	assert.Contains(t, generated, "Close releases the storage and runs no C++ destructor, and a value obtained by value is a copy the shim constructed here.")

	// The shim copies in, and constructs the result into Go's storage: the
	// declaration's own copy constructor is what runs.
	assert.Contains(t, shim, "*static_cast<const ::byval::Counter *>(counter)")
	assert.Contains(t, shim, "new (cppgo_result) byval::Counter(::byval::make(value))")

	// The record's C++ layout never appears in the C header: the casts live in
	// the translation unit where the layout exists.
	assert.NotContains(t, m[ShimHeaderFileName], "::byval::")
}

// byvalConfig is the by-value fixture's configuration.
func byvalConfig(library, out string) Config {
	cfg := fixtureConfig("byvalue", "byvalue.hpp")
	cfg.Package = "byval"
	cfg.Library = library
	cfg.Out = out
	return cfg
}

// byvalE2ESource is the program the generated package runs. It measures the two
// promises the generator makes about an object it allocated: the copy is C++'s
// (the copy constructor's +1000 is readable), and Close runs the destructor
// exactly once (the fixture counts them).
const byvalE2ESource = `package byval

import (
	"os"
	"testing"
)

func TestByValue(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// A value the callee constructed, owned by Go.
	made, err := Make(7)
	if err != nil {
		t.Fatalf("Make: %v", err)
	}
	if got := made.Value(); got != 7 {
		t.Fatalf("Make(7).Value() = %d, want 7", got)
	}

	// A parameter is copied into the call by C++: the copy constructor's +1000
	// is what the callee reads, which the Go side could not produce itself.
	if got, err := Read(made); err != nil || got != 1007 {
		t.Fatalf("Read(made) = %d, %v; want 1007 (the C++ copy constructor's +1000), nil", got, err)
	}

	// Both directions at once: the argument is copied in (+1000) and the
	// callee's return statement copies its parameter out (+1000 again), so the
	// value Go holds is 2007 — two C++ copies Go did not make.
	passed, err := Passthrough(made)
	if err != nil {
		t.Fatalf("Passthrough: %v", err)
	}
	if got := passed.Value(); got != 2007 {
		t.Fatalf("Passthrough(made).Value() = %d, want 2007 (the copy in and the copy out)", got)
	}
	if err := passed.Close(); err != nil {
		t.Fatalf("Close(passed): %v", err)
	}

	// Close runs the destructor, once, and a second Close is an error rather
	// than a second destructor.
	before := Destructions()
	if err := made.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if after := Destructions(); after != before+1 {
		t.Fatalf("destructors after Close = %d, want %d", after, before+1)
	}
	if err := made.Close(); err == nil {
		t.Fatal("a second Close returned nil, want cppgo.ErrClosed")
	}
	if after := Destructions(); after != before+1 {
		t.Fatalf("destructors after a second Close = %d, want %d", after, before+1)
	}

	// A nil value has no C++ object to copy, and nothing is constructed.
	if _, err := Read(nil); err == nil {
		t.Fatal("Read(nil) returned nil, want the nil-argument error")
	}
	if after := Destructions(); after != before+1 {
		t.Fatalf("destructors after a refused call = %d, want %d", after, before+1)
	}

	// A record that declares no destructor crosses the same way, and Close
	// releases the storage it owns without running anything.
	plain, err := MakeNoDtor(9)
	if err != nil {
		t.Fatalf("MakeNoDtor: %v", err)
	}
	if got := plain.Value(); got != 9 {
		t.Fatalf("MakeNoDtor(9).Value() = %d, want 9", got)
	}
	read, err := ReadNoDtor(plain)
	if err != nil {
		t.Fatalf("ReadNoDtor: %v", err)
	}
	if read != 9 {
		t.Fatalf("ReadNoDtor = %d, want 9", read)
	}
	destructors := Destructions()
	if err := plain.Close(); err != nil {
		t.Fatalf("Close(plain): %v", err)
	}
	if after := Destructions(); after != destructors {
		t.Fatalf("destructors after Close(plain) = %d, want %d: the record declares none", after, destructors)
	}
}
`

// TestNonPODByValueRoundTrip runs the generated package against the fixture's
// library, with cgo off and with the race detector on: the same two passes every
// generated package is measured by.
func TestNonPODByValueRoundTrip(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs(filepath.Join("testdata", "byvalue"))
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libbyval.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "byvalue.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "byval")
	require.NoError(t, Generate(byvalConfig(library, out)))
	shim := filepath.Join(out, ShimLibraryName("byval"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "byval_test.go"), []byte(byvalE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./byval")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./byval"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./byval"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestByValue")
		})
	}
}
