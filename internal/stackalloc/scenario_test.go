//go:build stackalloc

package stackalloc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// The stale-write scenario runs in a child process, because the outcome is not
// an error a test can assert on: the write goes to freed stack memory and the
// process either survives it or faults. The parent asserts on the exit status
// and the child's output.
const (
	scenarioEnv        = "CPPGO_STACKALLOC_SCENARIO"
	scenarioStaleWrite = "write-through-stale-this"
)

// runScenario runs this test binary again with one scenario selected and
// returns its exit code and output.
func runScenario(t *testing.T, scenario string) (int, string) {
	t.Helper()

	cmd := exec.Command(os.Args[0], "-test.run=^TestScenarioChild$", "-test.v=false")
	cmd.Env = append(os.Environ(), scenarioEnv+"="+scenario)
	out, err := cmd.CombinedOutput()

	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("run scenario %q: %v", scenario, err)
	}
	code := 0
	if exit != nil {
		code = exit.ExitCode()
	}
	return code, string(out)
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
	case scenarioStaleWrite:
		live := writeThroughStaleThis(
			sym("cppgo_cell_construct"),
			sym("cppgo_cell_after_callback_write"),
			sym("cppgo_cell_x"),
		)
		// The parent reads this line: 1 means the write through the address
		// C++ held never reached the live object.
		fmt.Printf("LIVE=%v\n", live)
	}
}

// writeThroughStaleThis constructs a cell in the calling frame, has C++ write
// through the `this` it captured before a Go callback grew the stack, and
// returns the live value read back through a recomputed address.
//
//go:noinline
func writeThroughStaleThis(construct, write, query unsafe.Pointer) float64 {
	var c Cell
	c.Construct(construct, 1, 2)

	cb, err := callback.Register(func(*callback.Args) { growStack(64) })
	if err != nil {
		panic(err)
	}
	defer func() { _ = callback.Release(cb) }()

	var args asm.Args
	args.I[0] = c.Addr()
	args.I[1] = cb
	asm.CallReentrant(write, &args)

	return c.X(query)
}
