package shim_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// fixture is the pair of artifacts the contract describes: the user's library,
// built by the user's own command, and the shim cppgo builds beside it. The
// test owns both paths so the artifact-model assertions can inspect them.
type fixture struct {
	dir  string
	user string
	shim string
	sym  func(string) unsafe.Pointer
}

// build compiles testdata/user.cpp into the user's library and
// testdata/shim.cpp into the cppgo-built shim, then loads the shim.
func build(t *testing.T) *fixture {
	t.Helper()

	buildOnce.Do(func() { buildDir, buildErr = buildFixture() })
	if buildErr != nil {
		t.Skipf("cannot build the fixture: %v", buildErr)
	}

	f := &fixture{
		dir:  buildDir,
		user: filepath.Join(buildDir, "libuser.so"),
		shim: filepath.Join(buildDir, "libcppgo_shim.so"),
	}

	lib, err := rtl.Open(f.shim)
	if err != nil {
		t.Fatalf("open shim: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })

	f.sym = func(name string) unsafe.Pointer {
		t.Helper()
		addr, err := lib.Symbol(name)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		return rtl.Pointer(addr)
	}

	return f
}

// runCommand is exec.Command with the output in the error, which is what a
// failing C++ build needs to be diagnosable.
func runCommand(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// lookPath is exec.LookPath, named so main_test.go needs no import of its own.
func lookPath(file string) (string, error) { return exec.LookPath(file) }

// exported reports whether the definition of name appears in the dynamic symbol
// table of path.
func exported(t *testing.T, path, name string) bool {
	t.Helper()

	nm, err := exec.LookPath("nm")
	if err != nil {
		t.Skipf("no nm available: %v", err)
	}

	out, err := exec.Command(nm, "-D", "--defined-only", path).Output()
	if err != nil {
		t.Fatalf("nm %s: %v", path, err)
	}

	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[2] == name {
			return true
		}
	}
	return false
}

// symbolsMatching reports the dynamic symbols of path whose name contains
// substr, for the assertions about what the user's library does and does not
// export.
func symbolsMatching(t *testing.T, path, substr string) []string {
	t.Helper()

	nm, err := exec.LookPath("nm")
	if err != nil {
		t.Skipf("no nm available: %v", err)
	}

	out, err := exec.Command(nm, "-D", path).Output()
	if err != nil {
		t.Fatalf("nm %s: %v", path, err)
	}

	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, substr) {
			found = append(found, strings.TrimSpace(line))
		}
	}
	return found
}

// cstring copies the shim's thread-local error message, if any.
func cstring(p unsafe.Pointer) string {
	if p == nil {
		return ""
	}
	return rtl.GoString(uintptr(p))
}

// lastError calls the shim's thread-local error accessor and copies the
// message out. The accessor is a foreign call like any other, so it is made
// after the stack probe and its C string is copied immediately.
func (f *fixture) lastError(t *testing.T) string {
	t.Helper()
	asm.Probe()
	addr, _ := asm.Call(f.sym("cppgo_last_error"), &asm.Args{})
	return cstring(rtl.Pointer(addr))
}

var _ = os.Getenv

// openUser loads the user's library on its own, for the tests that bind it
// directly instead of through the shim.
func openUser(t *testing.T, f *fixture) (*rtl.Library, error) {
	t.Helper()
	return rtl.Open(f.user)
}

// rtlPointer is the documented conversion from a resolved symbol address to the
// unsafe.Pointer the trampoline calls.
func rtlPointer(addr uintptr) unsafe.Pointer { return rtl.Pointer(addr) }

// unsafePointerFrom is the same conversion for an address read out of a vtable,
// which is a code pointer inside the loaded library.
func unsafePointerFrom(addr uintptr) unsafe.Pointer { return rtl.Pointer(addr) }

var _ = unsafePointerFrom

// symbol resolves one of the shim's exported names, returning the error rather
// than failing the test: the generics test asserts on a missing instantiation.
func (f *fixture) symbol(name string) (unsafe.Pointer, error) {
	lib, err := rtl.Open(f.shim)
	if err != nil {
		return nil, err
	}
	defer func() { _ = lib.Close() }()

	addr, err := lib.Symbol(name)
	if err != nil {
		return nil, err
	}
	return rtl.Pointer(addr), nil
}
