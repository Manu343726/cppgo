// Package testlib builds the small C++ shared object the cppgo tests call into.
//
// The fixture is compiled on demand with the system g++ into a temporary
// directory, so the tests need no checked-in binaries and no cgo. The library is
// plain C at the boundary, like a generated cppgo shim: it carries the two integer
// banks, the stack burner the probe is measured against, the pointer that calls
// back into Go, and the cppgo_last_error convention, and Full and NoLastError
// select whether the last of those is present.
//
// It is internal to cppgo and intended for tests and benchmarks only.
package testlib

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// Variant selects which fixture is built.
type Variant int

const (
	// Full is the complete fixture: it exports cppgo_last_error and friends.
	Full Variant = iota
	// NoLastError is the same fixture built without the cppgo_last_error
	// symbol, so the "error function is absent" path has something to test.
	NoLastError
)

// String implements fmt.Stringer.
func (v Variant) String() string {
	switch v {
	case Full:
		return "full"
	case NoLastError:
		return "no-last-error"
	default:
		return fmt.Sprintf("Variant(%d)", int(v))
	}
}

// fixtureSource is the C++ source of the fixture. It is deliberately plain C
// at the boundary: no C++ type ever crosses it, exactly like generated cppgo
// shims.
const fixtureSource = `
#include <cstddef>
#include <cstdint>
#include <cstring>

#define CPPGO_EXPORT extern "C" __attribute__((visibility("default")))

namespace {

// Thread-local, like the real cppgo_last_error contract.
thread_local char g_last_error[256];

void set_error(const char *msg) {
    if (msg == nullptr) {
        g_last_error[0] = '\0';
        return;
    }
    std::strncpy(g_last_error, msg, sizeof(g_last_error) - 1);
    g_last_error[sizeof(g_last_error) - 1] = '\0';
}

} // namespace

#ifndef CPPGO_FIXTURE_NO_LAST_ERROR

CPPGO_EXPORT const char *cppgo_last_error(void) { return g_last_error; }

CPPGO_EXPORT void cppgo_fixture_set_error(const char *msg) { set_error(msg); }

CPPGO_EXPORT void cppgo_fixture_clear_error(void) { set_error(nullptr); }

#endif // CPPGO_FIXTURE_NO_LAST_ERROR

// A data symbol, so the tests can resolve something that is not code.
CPPGO_EXPORT const char *cppgo_fixture_exported_data = "cppgo fixture data";

// Two integer arguments: the smallest possible call.
CPPGO_EXPORT std::int64_t cppgo_fixture_add(std::int64_t a, std::int64_t b) {
    return a + b;
}

// Six integer arguments: exercises every integer argument register
// (RDI, RSI, RDX, RCX, R8, R9). The weights make the register assignment
// observable: a swapped argument produces a different result.
CPPGO_EXPORT std::int64_t cppgo_fixture_add6(std::int64_t a, std::int64_t b,
                                             std::int64_t c, std::int64_t d,
                                             std::int64_t e, std::int64_t f) {
    return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f;
}

// Eight double arguments: exercises every float argument register (X0..X7).
CPPGO_EXPORT double cppgo_fixture_weighted8(double a, double b, double c,
                                            double d, double e, double f,
                                            double g, double h) {
    return a + 2 * b + 3 * c + 4 * d + 5 * e + 6 * f + 7 * g + 8 * h;
}

// Mixed integer and floating point arguments: the two register banks are
// assigned independently, so an off-by-one in either bank is visible.
CPPGO_EXPORT double cppgo_fixture_mix(std::int64_t a, double x, std::int64_t b,
                                      double y) {
    return static_cast<double>(a) + 10.0 * static_cast<double>(b) + 100.0 * x +
           1000.0 * y;
}

// (pointer, length) pair, like a []float64 crossing the boundary.
CPPGO_EXPORT double cppgo_fixture_dot(const double *a, const double *b,
                                      std::int64_t n) {
    double sum = 0.0;
    for (std::int64_t i = 0; i < n; ++i) {
        sum += a[i] * b[i];
    }
    return sum;
}

// Reads a NUL-terminated string the way a shim would.
CPPGO_EXPORT std::int64_t cppgo_fixture_strlen(const char *s) {
    return s == nullptr ? -1 : static_cast<std::int64_t>(std::strlen(s));
}

// Round-trips a pointer so the Go side can prove the exact value arrived.
CPPGO_EXPORT std::uintptr_t cppgo_fixture_identity_ptr(const void *p) {
    return reinterpret_cast<std::uintptr_t>(p);
}

// Burns up to 28 KiB of stack below the caller's stack pointer, touching one
// byte per cache line. Only survivable if cppgo.ProbeStack reserved room: the
// reserve is 32 KiB.
CPPGO_EXPORT std::int64_t cppgo_fixture_deep_stack(std::int64_t bytes) {
    constexpr std::size_t kMax = 28 * 1024;
    volatile unsigned char buf[kMax];
    std::size_t n = bytes <= 0 ? kMax : static_cast<std::size_t>(bytes);
    if (n > kMax) {
        n = kMax;
    }
    std::int64_t sum = 0;
    for (std::size_t i = 0; i < n; i += 64) {
        buf[i] = static_cast<unsigned char>(i & 0x7f) + 1;
        sum += buf[i];
    }
    buf[n - 1] = 7;
    sum += buf[n - 1];
    return sum;
}

// Calls a function pointer the Go side produced: the fixture of the reentrant
// carrier. It is only safe when the caller entered C through runtime.cgocall,
// which is what cppgo.CallReentrant does.
typedef std::int64_t (*cppgo_fixture_binary)(std::int64_t, std::int64_t);

CPPGO_EXPORT std::int64_t cppgo_fixture_apply(cppgo_fixture_binary fn, std::int64_t a,
                                              std::int64_t b) {
    return fn(a, b);
}

// The cppgo error convention: non-zero return means "call cppgo_last_error".
CPPGO_EXPORT std::int64_t cppgo_fixture_fail(const char *msg) {
#ifndef CPPGO_FIXTURE_NO_LAST_ERROR
    set_error(msg);
#endif
    (void)msg;
    return 1;
}
`

const fixtureFile = "cppgo_fixture.cc"

var (
	mu    sync.Mutex
	dir   string
	built = map[Variant]string{}
)

// Build compiles the fixture and returns the absolute path of the shared
// object. The result is cached for the lifetime of the test process. Call
// Cleanup to remove it.
//
// The test is skipped when no g++ is available. The library under test needs
// no C++ compiler, and only this fixture does.
func Build(t testing.TB, v Variant) string {
	t.Helper()

	mu.Lock()
	defer mu.Unlock()

	if path, ok := built[v]; ok {
		return path
	}

	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skipf("testlib: no g++ available to build the C++ fixture: %v", err)
	}

	if dir == "" {
		dir, err = os.MkdirTemp("", "cppgo-testlib-")
		if err != nil {
			t.Fatalf("testlib: create temp dir: %v", err)
		}
	}

	src := filepath.Join(dir, fixtureFile)
	if _, err := os.Stat(src); err != nil {
		if err := os.WriteFile(src, []byte(fixtureSource), 0o644); err != nil {
			t.Fatalf("testlib: write fixture source: %v", err)
		}
	}

	out := filepath.Join(dir, "libcppgo_fixture_"+v.String()+".so")

	args := []string{"-O2", "-fPIC", "-shared", "-fvisibility=hidden", "-std=c++17"}
	if v == NoLastError {
		args = append(args, "-DCPPGO_FIXTURE_NO_LAST_ERROR")
	}
	args = append(args, "-o", out, src)

	cmd := exec.Command(gxx, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("testlib: build fixture: %v\n%s", err, output)
	}

	built[v] = out
	return out
}

// Cleanup removes the temporary directory holding the built fixtures. It is a
// no-op when no fixture was built.
func Cleanup() {
	mu.Lock()
	defer mu.Unlock()

	if dir != "" {
		_ = os.RemoveAll(dir)
		dir = ""
	}
	built = map[Variant]string{}
}
