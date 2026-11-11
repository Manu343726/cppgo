// The retained callback's assertions: the C++ side stores the Go function, and
// delivers every record it writes from inside whatever call is running, on the
// goroutine that made it.
//
// These replace the tests the drain_log workaround had. The observable
// behaviour is the same one — a record arrives with its level and its text — and
// what changed is how it gets there: the registration survives the call that
// made it, and there is no buffer to pump.

package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	fingerprint "github.com/Manu343726/cppgo/examples/fingerprint/gen"
)

// record is one delivery, as the callback saw it.
type record struct {
	level   int32
	message string
}

// recorder is a registration the test holds: it appends what arrives and knows
// when it was closed.
type recorder struct {
	mu      sync.Mutex
	records []record
}

func (r *recorder) callback(level int32, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, record{level, message})
}

func (r *recorder) take() []record {
	r.mu.Lock()
	defer r.mu.Unlock()
	taken := r.records
	r.records = nil
	return taken
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// register installs a new sink for the duration of the test and returns the
// recorder and the release the test must call. The order matters and is the
// documented one: clear the C++ side first, then release the registration.
func register(t *testing.T) (*recorder, func()) {
	t.Helper()
	// A test that installs its own sink starts from a clean one: the previous
	// test's release has already cleared it, and this makes the failure a
	// message rather than a surprise.
	rec := &recorder{}
	registration, err := fingerprint.SetLogCallback(rec.callback)
	if err != nil {
		t.Fatalf("SetLogCallback: %v", err)
	}
	return rec, func() {
		if _, err := fingerprint.SetLogCallback(nil); err != nil {
			t.Errorf("clearing the sink: %v", err)
		}
		if err := registration.Close(); err != nil {
			t.Errorf("releasing the registration: %v", err)
		}
	}
}

// growStack pushes the goroutine's stack deep enough that it is copied. A call
// that may run a callback works on pinned copies of the Go values it hands to
// C++, and this is what proves the copies are pinned: the callback runs after
// the stack moved.
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

// TestLogRecordsArriveInGo is the base case: a registration made by one call
// delivers the records of a later call, with the level spdlog wrote and the
// message it formatted.
func TestLogRecordsArriveInGo(t *testing.T) {
	rec, release := register(t)
	defer release()

	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	defer enrollment.Close()

	got := rec.take()
	// debug (the decoded frame), debug (the region), info (the enrolment) and
	// trace (the per-stage times), in the order the pipeline reached them.
	want := []record{
		{1, "enroll: decoded 320x320 (3642 bytes)"},
		{1, "enroll: roi 219x301 at (51, 10)"},
		{2, fmt.Sprintf("enroll: 320x320 frame, roi 219x301, %d minutiae", enrollment.Minutiae())},
	}
	if len(got) != 4 {
		t.Fatalf("%d records, wanted 4: %v", len(got), got)
	}
	for index, expected := range want {
		if got[index] != expected {
			t.Errorf("record %d = %+v, wanted %+v", index, got[index], expected)
		}
	}
	if got[3].level != 0 || !strings.HasPrefix(got[3].message, "enroll: decode ") {
		t.Errorf("the trace record = %+v", got[3])
	}
}

// TestLogEveryPipelineLevel covers the levels the library writes through real
// calls: trace, debug and info from an enrolment, a warn for a frame it found
// no finger in, and an error for one it could not decode.
func TestLogEveryPipelineLevel(t *testing.T) {
	rec, release := register(t)
	defer release()

	levels := func() map[int32]string {
		seen := make(map[int32]string)
		for _, one := range rec.take() {
			seen[one.level] = one.message
		}
		return seen
	}

	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	enrollment.Close()
	seen := levels()
	for _, level := range []int32{0, 1, 2} {
		if _, ok := seen[level]; !ok {
			t.Errorf("an enrolment wrote no record at level %d: %v", level, seen)
		}
	}

	desk := fixture(t, "frame-background.png")
	if _, err := fingerprint.Enroll(desk); err == nil {
		t.Fatal("an enrolment of the desk frame succeeded")
	}
	seen = levels()
	if message, ok := seen[3]; !ok || message != "enroll: no finger was found in the 640x480 frame" {
		t.Errorf("the warning = %q, present %v", message, ok)
	}

	garbage := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if _, err := fingerprint.Enroll(garbage); err == nil {
		t.Fatal("an enrolment of eight bytes succeeded")
	}
	seen = levels()
	if message, ok := seen[4]; !ok || message != "enroll: the frame did not decode (8 bytes)" {
		t.Errorf("the error = %q, present %v", message, ok)
	}

	// The trigger writes its own warning, through the same sink.
	trigger, err := fingerprint.NewTrigger()
	if err != nil {
		t.Fatal(err)
	}
	defer trigger.Close()
	state := fingerprint.InitialTriggerState()
	if _, err := trigger.Evaluate(garbage, &state); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	seen = levels()
	if message, ok := seen[3]; !ok || !strings.Contains(message, "did not decode") {
		t.Errorf("the trigger's warning = %q, present %v", message, ok)
	}
}

// TestLogSurvivesGCAndStackGrowth registers, returns from the registering call,
// and only then delivers: a collection and a stack growth in between must lose
// nothing, because the registration is what keeps the function alive.
func TestLogSurvivesGCAndStackGrowth(t *testing.T) {
	rec, release := register(t)
	defer release()

	runtime.GC()
	growStack(64)
	runtime.GC()

	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll after a collection: %v", err)
	}
	defer enrollment.Close()
	if rec.count() != 4 {
		t.Fatalf("%d records after the stack moved: %v", rec.count(), rec.take())
	}
	// The template the call returned is still the right one, which is the other
	// half of what the pinned copies are for.
	templateBytes, err := enrollment.TemplateBytes()
	if err != nil {
		t.Fatal(err)
	}
	if len(templateBytes) < 20 || templateBytes[:4] != "FMR\x00" {
		t.Fatalf("the template did not survive the callback: %x", templateBytes[:4])
	}
}

// TestLogCloseStopsDelivery is the end of a registration's life, both ways: a
// delivery after Close without clearing is the released slot's panic, and a
// delivery after clearing is nothing at all.
func TestLogCloseStopsDelivery(t *testing.T) {
	rec, release := register(t)
	defer release()

	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	enrollment.Close()
	before := rec.count()
	if before == 0 {
		t.Fatal("nothing was delivered before Close")
	}

	// Closing the registration while the C++ side still holds the pointer is
	// the case the docs name: the delivery is a panic in Go, raised when the
	// call returns, and never a jump through freed memory.
	registration, err := fingerprint.SetLogCallback(rec.callback)
	if err != nil {
		t.Fatal(err)
	}
	if err := registration.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
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
		enrollment, err := fingerprint.Enroll(photo)
		if err == nil {
			enrollment.Close()
		}
	}()

	// Clearing is the documented way out, and then nothing arrives at all.
	if _, err := fingerprint.SetLogCallback(nil); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	delivered := rec.count()
	enrollment, err = fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll after clearing: %v", err)
	}
	enrollment.Close()
	if got := rec.count(); got != delivered {
		t.Fatalf("a delivery reached a cleared sink: %v", rec.take())
	}
}

// TestLogNilClearsSink is the setter's null case: no registration is returned,
// the call is safe, and the library stops delivering.
func TestLogNilClearsSink(t *testing.T) {
	rec, release := register(t)
	defer release()

	cleared, err := fingerprint.SetLogCallback(nil)
	if err != nil {
		t.Fatalf("SetLogCallback(nil): %v", err)
	}
	if cleared != nil {
		t.Fatalf("SetLogCallback(nil) returned %v, want no registration", cleared)
	}
	rec.take()
	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll with a cleared sink: %v", err)
	}
	enrollment.Close()
	if rec.count() != 0 {
		t.Fatalf("a cleared sink delivered %v", rec.take())
	}
	// A nil registration is safe to close: it reports the closed registration
	// the package names, and does not panic.
	var none *fingerprint.CallbackRegistration
	if err := none.Close(); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("closing a nil registration = %v, want the package's closed error", err)
	}
}

// TestLogFromWorkerGoroutine is the watch loop's shape: the worker's calls
// deliver to the process's registration, on the worker's own stack, while the
// loop is running its trigger on another.
func TestLogFromWorkerGoroutine(t *testing.T) {
	rec, release := register(t)
	defer release()

	photo := fixture(t, "print.png")
	done := make(chan error, 1)
	go func() {
		growStack(32)
		enrollment, err := fingerprint.Enroll(photo)
		if err != nil {
			done <- err
			return
		}
		enrollment.Close()
		done <- nil
	}()
	if err := <-done; err != nil {
		t.Fatalf("the worker's enrolment: %v", err)
	}
	if got := rec.count(); got != 4 {
		t.Fatalf("the worker delivered %d records, wanted 4: %v", got, rec.take())
	}
}

// TestLogRegistrationReplacesTheSink pins the one thing a process-wide sink
// means: the last registration wins, and the one before it receives nothing
// until it is registered again.
func TestLogRegistrationReplacesTheSink(t *testing.T) {
	first, releaseFirst := register(t)
	defer releaseFirst()

	var second recorder
	registration, err := fingerprint.SetLogCallback(second.callback)
	if err != nil {
		t.Fatal(err)
	}
	photo := fixture(t, "print.png")
	enrollment, err := fingerprint.Enroll(photo)
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	enrollment.Close()
	if first.count() != 0 {
		t.Errorf("the replaced registration received %v", first.take())
	}
	if second.count() != 4 {
		t.Errorf("the new registration received %d records, wanted 4", second.count())
	}
	// Put the test's own sink back before the deferred release runs, so the
	// release clears the right one.
	if err := registration.Close(); err != nil {
		t.Fatalf("releasing the second registration: %v", err)
	}
}
