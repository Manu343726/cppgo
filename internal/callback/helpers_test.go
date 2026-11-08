package callback_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo/internal/rtl"
)

var (
	once    bool
	libRef  *rtl.Library
	libPath string
	symbols func(string) unsafe.Pointer
	libErr  error
)

// fixture builds testdata/callback_fixture.cc once and returns a resolver for
// its exported symbols. The fixture is the user's own C++ build: cppgo
// generates nothing into it, and it is compiled with default visibility, which
// is what puts its template instantiations in .dynsym where the Go side can
// call them.
func fixture(t *testing.T) func(string) unsafe.Pointer {
	t.Helper()

	if !once {
		once = true
		libPath, symbols, libErr = buildFixture()
	}
	if libErr != nil {
		t.Skipf("cannot build the C++ fixture: %v", libErr)
	}
	return symbols
}

func buildFixture() (string, func(string) unsafe.Pointer, error) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp("", "cppgo-callback-")
	if err != nil {
		return "", nil, err
	}

	out := filepath.Join(dir, "libcppgo_callback_fixture.so")
	cmd := exec.Command(gxx, "-O1", "-fPIC", "-shared", "-std=c++17",
		"-o", out, "testdata/callback_fixture.cc")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("build fixture: %w\n%s", err, b)
	}

	lib, err := rtl.Open(out)
	if err != nil {
		return "", nil, err
	}
	libRef = lib

	return out, func(name string) unsafe.Pointer {
		addr, err := lib.Symbol(name)
		if err != nil {
			panic(err)
		}
		return rtl.Pointer(addr)
	}, nil
}

// libSymbol resolves a symbol name from the fixture library, keeping the error
// rather than panicking: a missing template instantiation is a measurement, not
// a crash.
func libSymbol(name string) (uintptr, error) {
	return libRef.Symbol(name)
}

// fixtureB is fixture for benchmarks, which have no *testing.T.
func fixtureB(b *testing.B) func(string) unsafe.Pointer {
	b.Helper()
	if !once {
		once = true
		libPath, symbols, libErr = buildFixture()
	}
	if libErr != nil {
		b.Skipf("cannot build the C++ fixture: %v", libErr)
	}
	return symbols
}

func TestMain(m *testing.M) {
	code := m.Run()
	if libPath != "" {
		_ = os.RemoveAll(filepath.Dir(libPath))
	}
	os.Exit(code)
}

// goid reports the goroutine id from the runtime's own traceback header, the
// only stable way to name the goroutine a callback runs on.
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	var id uint64
	_, _ = fmt.Sscanf(string(buf[:n]), "goroutine %d ", &id)
	return id
}
