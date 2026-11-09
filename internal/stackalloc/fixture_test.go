//go:build stackalloc

package stackalloc

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// The fixture is compiled once per test run and removed by TestMain, the same
// arrangement internal/callback uses.
var (
	fixtureBuilt bool
	fixturePath  string
	fixtureLib   *rtl.Library
	fixtureErr   error
)

// fixture returns a resolver for the fixture's exported symbols. The test or
// benchmark is skipped when no C++ compiler is available, because the library
// under test needs none.
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

// buildFixture compiles testdata/stack_fixture.cc into a shared object with
// default visibility, so every CPPGO_EXPORT symbol lands in .dynsym.
func buildFixture() (string, *rtl.Library, error) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		return "", nil, err
	}

	dir, err := os.MkdirTemp("", "cppgo-stackalloc-")
	if err != nil {
		return "", nil, err
	}

	out := filepath.Join(dir, "libcppgo_stack_fixture.so")
	cmd := exec.Command(gxx, "-O2", "-fPIC", "-shared", "-std=c++17",
		"-o", out, filepath.Join("testdata", "stack_fixture.cc"))
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

// callNoArgs invokes a fixture function that takes no arguments and returns its
// integer result in RAX.
func callNoArgs(fn unsafe.Pointer) uintptr {
	var args asm.Args
	got, _ := asm.Call(fn, &args)
	return got
}

// callWith invokes a fixture function with the six integer argument registers
// set from regs and returns the integer result.
func callWith(fn unsafe.Pointer, regs [6]uintptr) uintptr {
	var args asm.Args
	args.I = regs
	got, _ := asm.Call(fn, &args)
	return got
}

// callInt32Out invokes fn(i0, &out) and returns the status and the value the
// callee wrote through the out-pointer.
func callInt32Out(fn unsafe.Pointer, i0 uintptr) (status int32, out int32) {
	var args asm.Args
	args.I[0] = i0
	args.I[1] = uintptr(unsafe.Pointer(&out))
	got, _ := asm.Call(fn, &args)
	runtime.KeepAlive(&out)
	return int32(got), out
}

// callVoid invokes fn with three integer arguments and ignores the result.
func callVoid(fn unsafe.Pointer, a, b, c uintptr) {
	var args asm.Args
	args.I[0], args.I[1], args.I[2] = a, b, c
	asm.Call(fn, &args)
}
