package callback_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// The exception measurements run in a child process, because what happens is
// not an error a test can assert on: it is a call to std::terminate and a
// SIGABRT. Each scenario is selected by this environment variable, and the
// parent asserts on the exit status and the child's stderr.
const scenarioEnv = "CPPGO_CALLBACK_SCENARIO"

// runScenario runs this test binary again with one scenario selected and
// returns its exit code and output.
func runScenario(t *testing.T, scenario string) (int, string) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestScenarioChild$", "-test.v=false")
	cmd.Env = append(os.Environ(), scenarioEnv+"="+scenario)
	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if err != nil && !errorsAs(err, &exit) {
		t.Fatalf("run scenario %q: %v", scenario, err)
	}
	code := 0
	if exit != nil {
		code = exit.ExitCode()
	}
	return code, string(out)
}

func errorsAs(err error, target **exec.ExitError) bool {
	e, ok := err.(*exec.ExitError)
	if ok {
		*target = e
	}
	return ok
}

// TestScenarioChild is the child side. It does nothing unless the environment
// names a scenario, so the ordinary test run is unaffected.
func TestScenarioChild(t *testing.T) {
	scenario := os.Getenv(scenarioEnv)
	if scenario == "" {
		t.Skip("not a scenario child")
	}

	sym := fixture(t)

	switch scenario {
	case "throw-from-c":
		// A C++ function that throws, called from Go: there is no C++
		// frame anywhere on the stack, so the unwinder finds no handler.
		asm.CallReentrant(sym("cb_throws"), &asm.Args{})

	case "throw-after-callback":
		// The callback returns, then the C++ frame above it throws. The
		// exception has to unwind through the C frames and then into
		// the Go frames that called cgocall.
		cb, err := callback.Register(func(a *callback.Args) { a.Ret = 1 })
		require.NoError(t, err)
		var args asm.Args
		args.I[0] = cb
		asm.CallReentrant(sym("cb_call_then_throw"), &args)

	case "throw-inside-callback":
		// The Go callback calls C++ that throws. The unwinder meets the
		// Go frames of the callback with no exception table.
		cb, err := callback.Register(func(*callback.Args) {
			asm.CallReentrant(sym("cb_throws"), &asm.Args{})
		})
		require.NoError(t, err)
		var args asm.Args
		args.I[0] = cb
		asm.CallReentrant(sym("cb_apply"), &args)

	case "callback-on-direct-trampoline":
		// A callback invoked by a C call made through the direct
		// trampoline instead of runtime.cgocall: the runtime reaches
		// cgocallback on an M that is not in a foreign call.
		cb, err := callback.Register(func(a *callback.Args) { a.Ret = 1 })
		require.NoError(t, err)
		var args asm.Args
		args.I[0] = cb
		asm.Probe()
		asm.Call(sym("cb_apply"), &args)

	default:
		t.Fatalf("unknown scenario %q", scenario)
	}

	t.Log("scenario returned normally")
}

// TestExceptionFromCIsFatal measures the hard limit of the exception question:
// a C++ function that throws, called from a Go trampoline with no C++ frame
// anywhere, terminates the process. Nothing in the Go side can catch it.
func TestExceptionFromCIsFatal(t *testing.T) {
	code, out := runScenario(t, "throw-from-c")

	assert.NotZero(t, code, "the process died")
	assert.Contains(t, out, "terminate called after throwing an instance of 'std::runtime_error'")
	assert.Contains(t, out, "cb_throws")
}

// TestExceptionAfterACallbackIsFatal measures the same limit with a callback
// underneath: the throw is in the C++ frame that called the callback, and it
// still finds no handler above the Go frames.
func TestExceptionAfterACallbackIsFatal(t *testing.T) {
	code, out := runScenario(t, "throw-after-callback")

	assert.NotZero(t, code)
	assert.Contains(t, out, "terminate called after throwing an instance of 'std::runtime_error'")
}

// TestExceptionThroughTheGoCallbackIsFatal measures the worst case: the C++
// exception is raised by a call the Go callback made, so unwinding has to cross
// the callback's own Go frames.
func TestExceptionThroughTheGoCallbackIsFatal(t *testing.T) {
	code, out := runScenario(t, "throw-inside-callback")

	assert.NotZero(t, code)
	assert.Contains(t, out, "terminate called after throwing an instance of 'std::runtime_error'")
}

// TestCallbackOnTheDirectTrampolineIsFatal is the measured reason callbacks
// have their own carrier: the runtime's cgo callback entry requires the M to be
// inside a foreign call, and the direct trampoline does not put it there.
func TestCallbackOnTheDirectTrampolineIsFatal(t *testing.T) {
	code, out := runScenario(t, "callback-on-direct-trampoline")

	assert.NotZero(t, code)
	assert.True(t,
		strings.Contains(out, "morestack on g0") || strings.Contains(out, "fatal error"),
		"want a runtime fatal error, got:\n%s", out)
}

// TestExceptionThrownAndCaughtInCPPWorks is the other side of the limit: an
// exception the C++ code catches inside itself never reaches Go, so a
// `noexcept` wrapper is a real fix rather than a workaround.
func TestExceptionThrownAndCaughtInCPPWorks(t *testing.T) {
	sym := fixture(t)

	got, _ := asm.CallReentrant(sym("cb_swallow"), &asm.Args{})
	assert.EqualValues(t, len("swallowed"), int32(got))
}
