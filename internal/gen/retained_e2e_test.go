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
	assert.Contains(t, ran, "--- PASS: TestRetainedShapes")
	ran = runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + filepath.Join(out, ShimLibraryName("retained"))},
		"test", "-race", "-count=1", "-v", "./retained")
	assert.Contains(t, ran, "--- PASS: TestRetained")
	assert.Contains(t, ran, "--- PASS: TestRetainedShapes")
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

// TestRetainedShapes is the package's record proof: a C++ record Go cannot
// mirror, returned by value, with a std::string member reached through the
// synthesized accessor the shim carries. Every step runs on the callback-capable
// carrier, so nothing here may depend on a Go stack address staying put.
func TestRetainedShapes(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	enrolment, err := Enroll("template-7", 42)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if enrolment == nil {
		t.Fatal("Enroll returned no record")
	}

	// The Go value owns C++ storage: a collection and a stack growth between
	// the call and the read may move everything Go holds, and the record's
	// bytes are not among it.
	runtime.GC()
	growStack(64)

	label, err := enrolment.Label()
	if err != nil {
		t.Fatalf("Label: %v", err)
	}
	if label != "template-7" {
		t.Fatalf("Label = %q, want the bytes the C++ copy was built from", label)
	}
	if got := enrolment.Count(); got != 42 {
		t.Fatalf("Count = %d, want 42", got)
	}

	// The label is a copy C++ owns, so reading it twice returns the same bytes.
	if again, err := enrolment.Label(); err != nil || again != label {
		t.Fatalf("the second Label = %q, %v", again, err)
	}

	if err := enrolment.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := enrolment.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("a second Close = %v, want cppgo.ErrClosed", err)
	}
	if _, err := enrolment.Label(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("Label on a closed record = %v, want cppgo.ErrClosed", err)
	}

	// A class with a destructor: Close has to reach the C++ one through the
	// package's callback-capable carrier, and the library's own count of live
	// objects is the witness that it ran — once.
	meter, err := NewMeter()
	if err != nil {
		t.Fatalf("NewMeter: %v", err)
	}
	if got := LiveMeters(); got != 1 {
		t.Fatalf("LiveMeters = %d, want the one object NewMeter constructed", got)
	}
	meter.Bump()
	meter.Bump()
	if got := meter.Value(); got != 2 {
		t.Fatalf("Value = %d, want 2", got)
	}
	runtime.GC()
	growStack(32)

	// The copy constructor is a special member like the others: under the
	// callback-capable carrier it too is a call through runtime.cgocall, with
	// its own storage and its own destructor.
	clone, err := meter.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if got := LiveMeters(); got != 2 {
		t.Fatalf("LiveMeters after Clone = %d, want 2", got)
	}
	if got := clone.Value(); got != 2 {
		t.Fatalf("the clone's Value = %d, want the value it copied", got)
	}
	if err := clone.Close(); err != nil {
		t.Fatalf("the clone's Close: %v", err)
	}
	if got := LiveMeters(); got != 1 {
		t.Fatalf("LiveMeters after the clone's Close = %d, want 1", got)
	}

	if err := meter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := LiveMeters(); got != 0 {
		t.Fatalf("LiveMeters after Close = %d, want 0: the destructor did not run", got)
	}
	if err := meter.Close(); !errors.Is(err, cppgo.ErrClosed) {
		t.Fatalf("a second Close = %v, want cppgo.ErrClosed", err)
	}

	// A non-POD record returned by value: the shim constructs the record into
	// Go's storage, and Close destroys it through the record's own destructor.
	made, err := MakeMeter(5)
	if err != nil {
		t.Fatalf("MakeMeter: %v", err)
	}
	if got := made.Value(); got != 5 {
		t.Fatalf("MakeMeter(5).Value() = %d, want 5", got)
	}
	if got := LiveMeters(); got != 1 {
		t.Fatalf("LiveMeters = %d, want the one copy the shim constructed in Go's storage", got)
	}
	if err := made.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := LiveMeters(); got != 0 {
		t.Fatalf("LiveMeters after Close = %d, want 0", got)
	}

	// A borrowed C string member: the getter's result is copied out through an
	// out-pointer of the accessor's own frame, and a zeroed record's null
	// pointer is the empty string.
	tagged, err := NewTagged()
	if err != nil {
		t.Fatalf("NewTagged: %v", err)
	}
	if got := tagged.Text(); got != "" {
		t.Fatalf("Text on a zeroed record = %q, want the empty string", got)
	}
	if got := tagged.Count(); got != 0 {
		t.Fatalf("Count = %d, want 0", got)
	}
	if err := tagged.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A defaulted prefix: the call is written in C++, so it goes through the
	// shim's own carrier, which hands C++ the pointer expression the caller
	// gave instead of the copy the callback-capable carrier pins. The default
	// scale is the callee's, applied by the C++ compiler.
	value := int32(21)
	PokeInt32Ptr(&value)
	if got := LastPoke(); got != 42 {
		t.Fatalf("LastPoke = %d, want 21 * the callee's default scale of 2", got)
	}
	Poke(&value, 3)
	if got := LastPoke(); got != 63 {
		t.Fatalf("LastPoke = %d, want 63", got)
	}

	// The whole package is callback-capable, so every step above ran with the
	// carrier a callback can be delivered in. A retained callback survives a
	// collection and a stack growth and is delivered by a later call.
	var delivered []string
	registration, err := SetLogCallback(func(level int32, message string) {
		delivered = append(delivered, fmt.Sprintf("%d:%s", level, message))
	})
	if err != nil {
		t.Fatalf("SetLogCallback: %v", err)
	}
	runtime.GC()
	growStack(64)
	Log(2)
	if want := []string{"0:even", "1:odd"}; fmt.Sprint(delivered) != fmt.Sprint(want) {
		t.Fatalf("delivered = %q, want %q", delivered, want)
	}
	if _, err := SetLogCallback(nil); err != nil {
		t.Fatalf("SetLogCallback(nil): %v", err)
	}
	if err := registration.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
`
