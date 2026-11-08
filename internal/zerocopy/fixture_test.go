//go:build stackalloc

package zerocopy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
)

var (
	fixtureBuilt bool
	fixturePath  string
	fixtureLib   *rtl.Library
	fixtureErr   error
)

// fixture compiles testdata/zerocopy_fixture.cc with C++20 (std::span) and
// returns a resolver for its exported symbols.
func fixture(tb testing.TB) func(string) unsafe.Pointer {
	tb.Helper()

	if !fixtureBuilt {
		fixtureBuilt = true
		fixturePath, fixtureLib, fixtureErr = buildFixture()
	}
	if fixtureErr != nil {
		tb.Skipf("cannot build the C++ fixture: %v", fixtureErr)
	}

	return func(name string) unsafe.Pointer {
		tb.Helper()
		addr, err := fixtureLib.Symbol(name)
		if err != nil {
			tb.Fatalf("resolve %s: %v", name, err)
		}
		return rtl.Pointer(addr)
	}
}

func buildFixture() (string, *rtl.Library, error) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp("", "cppgo-zerocopy-")
	if err != nil {
		return "", nil, err
	}

	out := filepath.Join(dir, "libcppgo_zerocopy_fixture.so")
	cmd := exec.Command(gxx, "-O2", "-fPIC", "-shared", "-std=c++20",
		"-o", out, filepath.Join("testdata", "zerocopy_fixture.cc"))
	if output, err := cmd.CombinedOutput(); err != nil {
		return out, nil, fmt.Errorf("build fixture: %w\n%s", err, output)
	}

	lib, err := rtl.Open(out)
	if err != nil {
		return out, nil, err
	}
	return out, lib, nil
}

func TestMain(m *testing.M) {
	code := m.Run()
	if fixturePath != "" {
		_ = os.RemoveAll(filepath.Dir(fixturePath))
	}
	os.Exit(code)
}

// call invokes fn(data, n) and returns the integer result.
func call(fn unsafe.Pointer, data unsafe.Pointer, n int) uintptr {
	var args asm.Args
	args.I[0] = uintptr(data)
	args.I[1] = uintptr(n)
	got, _ := asm.Call(fn, &args)
	return got
}
