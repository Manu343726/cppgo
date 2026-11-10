package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetainedCallbackIsDeliveredByLaterCalls is the end-to-end proof of a
// callback the callee stores: the registering call returns, the Go function
// stays registered, and a call of its own delivers records through it until the
// caller closes the registration.
//
// The measured sequence is the one the lifetime rules promise: the registration
// survives a collection and a stack growth between registering and delivering,
// a call that does not log does not lose it, closing it makes a later delivery
// the released-slot panic rather than a jump through freed memory, and a nil
// function clears the callee's sink without registering anything.
func TestRetainedCallbackIsDeliveredByLaterCalls(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	require.NoError(t, copyTree("testdata/retained", project))
	makeBin, err := exec.LookPath("make")
	require.NoError(t, err)
	runIn(t, project, makeBin, "library")
	library := filepath.Join(project, "build", "libretained.so")
	require.FileExists(t, library)

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "retained")
	var report string
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(project, "include", "retained", "retained.hpp")},
		Package: "retained",
		Library: library,
		Out:     out,
		Retain:  []string{"retained::set_log_callback"},
		Report:  writerFunc(func(p []byte) (int, error) { report += string(p); return len(p), nil }),
	}))

	// Every call of the package is callback-capable: any of them may deliver a
	// record the callee stored, and the header cannot say which.
	carrierOf(t, report, "retained::set_log_callback(LogCallback) noexcept", "reentrant")
	carrierOf(t, report, "retained::log(std::int32_t) noexcept", "reentrant")
	carrierOf(t, report, "retained::touch() noexcept", "reentrant")
	assert.Contains(t, report, "a retained callback is registered for this package")

	// The default deployment: the user's library beside the shim, which is what
	// the generated Open resolves without an override.
	deployBeside(t, library, out)

	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "retained_test.go"), []byte(retainedTestSource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./retained")
	ran := runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + filepath.Join(out, ShimLibraryName("retained"))},
		"test", "-count=1", "-v", "./retained")
	assert.Contains(t, ran, "--- PASS: TestRetained")
	ran = runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + filepath.Join(out, ShimLibraryName("retained"))},
		"test", "-race", "-count=1", "-v", "./retained")
	assert.Contains(t, ran, "--- PASS: TestRetained")
}

// writerFunc adapts a closure to io.Writer, so the carrier report can be read
// as a string.
type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// retainedTestSource is the program the generated package runs. Every assertion
// is a value the C++ produced or a rule the lifetime states.
const retainedTestSource = `package retained

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/Manu343726/cppgo"
)

// growStack pushes the goroutine's stack deep enough to be copied. A callback
// may grow it at any time, which is what the pinned copies and the argument
// frame have to survive.
//
//go:noinline
func growStack(depth int) byte {
	var block [4096]byte
	block[0] = byte(depth)
	if depth <= 0 {
		return block[0]
	}
	return growStack(depth-1) + block[0]
}

func TestRetained(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Register, and return from the registering call: the sink lives on.
	var records []string
	reg, err := SetLogCallback(func(level int32, message string) {
		records = append(records, fmt.Sprintf("%d:%s", level, message))
	})
	if err != nil {
		t.Fatalf("SetLogCallback: %v", err)
	}
	if reg == nil {
		t.Fatal("a registration with a function must return a registration")
	}

	// A collection and a stack growth between registering and delivering lose
	// nothing: the function is held by the table and by the registration, and
	// the C++ side holds only the slot.
	runtime.GC()
	growStack(64)

	// A later call of the library delivers through the stored callback. Its
	// records are the library's own text, copied into Go strings for the
	// callback only.
	Log(3)
	if want := []string{"0:even", "1:odd", "2:even"}; fmt.Sprint(records) != fmt.Sprint(want) {
		t.Fatalf("records = %q, want %q", records, want)
	}

	// A call that does not log leaves the registration alone.
	if got := Touch(); got != 7 {
		t.Fatalf("Touch = %d, want the library's own 7", got)
	}
	if len(records) != 3 {
		t.Fatalf("a call that does not log delivered %q", records[3:])
	}

	// Close releases the slot, once.
	if err := reg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := reg.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("a second Close = %v, want cppgo.ErrClosed", err)
	}

	// The library still holds the released address: a delivery is the released
	// slot's panic, raised in Go when the call returns, and never a jump
	// through freed memory.
	func() {
		defer func() {
			raised := recover()
			if raised == nil {
				t.Fatal("a delivery after Close must panic")
			}
			if message := fmt.Sprint(raised); !strings.Contains(message, "was released while C called it") {
				t.Fatalf("the panic names %q, not a released callback slot", message)
			}
		}()
		Log(1)
	}()

	// A nil function is the way to tell the library to forget the callback: it
	// registers nothing, returns no registration, and the call is safe.
	cleared, err := SetLogCallback(nil)
	if err != nil {
		t.Fatalf("SetLogCallback(nil): %v", err)
	}
	if cleared != nil {
		t.Fatalf("SetLogCallback(nil) returned %v, want no registration", cleared)
	}
	Log(4)

	// A worker goroutine that calls C++ receives its own callback: the
	// registration is per goroutine's use, and the delivery happens on the
	// goroutine that made the call.
	var fromWorker []string
	done := make(chan error, 1)
	go func() {
		worker, err := SetLogCallback(func(level int32, message string) {
			fromWorker = append(fromWorker, message)
		})
		if err != nil {
			done <- err
			return
		}
		defer func() { _ = worker.Close() }()
		growStack(32)
		Log(2)
		done <- nil
	}()
	if err := <-done; err != nil {
		t.Fatalf("the worker's call: %v", err)
	}
	if want := []string{"even", "odd"}; fmt.Sprint(fromWorker) != fmt.Sprint(want) {
		t.Fatalf("the worker received %q, want %q", fromWorker, want)
	}
	if len(records) != 3 {
		t.Fatalf("the worker's records reached the first registration: %q", records)
	}

	// A full table reports the failure instead of registering: the table has 64
	// slots and a registration per call is what fills it.
	var held = make([]*CallbackRegistration, 0, 64)
	for i := 0; i < 64; i++ {
		one, err := SetLogCallback(func(int32, string) {})
		if err != nil {
			break
		}
		held = append(held, one)
	}
	if len(held) != 64 {
		t.Fatalf("the table took %d registrations, want the 64 slots", len(held))
	}
	if _, err := SetLogCallback(func(int32, string) {}); err == nil {
		t.Fatal("a full table must report the failure")
	}
	for _, one := range held {
		if err := one.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	// A nil registration is safe to close.
	var none *CallbackRegistration
	if err := none.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("closing a nil registration = %v, want cppgo.ErrClosed", err)
	}
}
`
