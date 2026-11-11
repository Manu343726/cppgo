package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	fingerprint "github.com/Manu343726/cppgo/examples/fingerprint/gen"
)

// testPath names one of the two objects relative to the example root. A test
// runs with its package's directory as the working directory, so the root is
// two levels up.
func testPath(rel string) string {
	path := filepath.Join("..", "..", rel)
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return path
}

func TestMain(m *testing.M) {
	fingerprint.CppgoLibraryPath = testPath(filepath.Join("build", "libfingerprint.so"))
	shim := os.Getenv("CPPGO_FINGERPRINT_SHIM")
	if shim == "" {
		shim = testPath(filepath.Join("gen", "libcppgo_fingerprint.so"))
	}
	if err := fingerprint.Open(shim); err != nil {
		fmt.Fprintf(os.Stderr, "opening the generated binding: %v\nrun \"make\" in the example root first\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// fixtureBytes is a committed test frame, by name.
func fixtureBytes(name string) ([]byte, error) {
	path := filepath.Join(testPath("testdata"), name)
	return os.ReadFile(path)
}

// fixture is fixtureBytes for a test that stops on a missing frame.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := fixtureBytes(name)
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	return data
}

// frameSequence copies fixtures into a directory in the order given, named so
// the source reads them in that order.
func frameSequence(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i, name := range names {
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s", i, name))
		if err := os.WriteFile(path, fixture(t, name), 0o644); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return dir
}

// verdicts parses the watch loop's per-frame lines out of its output.
type verdict struct {
	index  int
	fire   bool
	score  float64
	rois   int
	reason string
}

func parseVerdicts(t *testing.T, output string) []verdict {
	t.Helper()
	var got []verdict
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 10 || fields[0] != "frame" || fields[4] != "score" || fields[6] != "rois" {
			continue
		}
		var index int
		var score float64
		var rois int
		if _, err := fmt.Sscanf(fields[1], "%d", &index); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(fields[5], "%f", &score); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(fields[7], "%d", &rois); err != nil {
			continue
		}
		got = append(got, verdict{index: index, fire: fields[3] == "fire", score: score, rois: rois, reason: fields[9]})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].index < got[j].index })
	return got
}

func firedIndices(verdicts []verdict) []int {
	var indices []int
	for _, verdict := range verdicts {
		if verdict.fire {
			indices = append(indices, verdict.index)
		}
	}
	return indices
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"version"}, &stdout, &stderr); err != nil {
		t.Fatalf("version: %v", err)
	}
	text := stdout.String()
	if !strings.HasPrefix(text, "fingerprint 0.1.0 (OpenCV 4.") {
		t.Errorf("version line: %q", firstLine(text))
	}
	if !strings.Contains(text, "trigger: 160x120 working image") {
		t.Errorf("the trigger's working size is not reported: %q", text)
	}
}

// TestCLIRecordsReachSlog is the CLI's end of the sink: an enrolment's records
// arrive through the retained callback and are written by slog, and the run
// reports how many of each level it received.
func TestCLIRecordsReachSlog(t *testing.T) {
	out := filepath.Join(t.TempDir(), "template.ansi")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"enroll", "-v", "-out", out, filepath.Join(testPath("testdata"), "print.png")}, &stdout, &stderr); err != nil {
		t.Fatalf("enroll -v: %v\n%s", err, stderr.String())
	}
	for _, want := range []string{
		"msg=\"enroll: decoded 320x320 (3642 bytes)\"",
		"msg=\"enroll: roi 219x301 at (51, 10)\"",
		"msg=\"enroll: 320x320 frame, roi 219x301, 33 minutiae\"",
		"msg=\"enroll: decode ",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("the record %q did not reach slog:\n%s", want, stderr.String())
		}
	}
	if !strings.Contains(stdout.String(), "C++ log records: trace 1, debug 2, info 1") {
		t.Errorf("the per-level counts are not reported:\n%s", stdout.String())
	}
	// The registration is released when the command returns: a second run
	// registers again, which is what proves the first one was cleared.
	stderr.Reset()
	if err := run([]string{"dump", out}, &stdout, &stderr); err != nil {
		t.Fatalf("dump after enroll: %v", err)
	}
}

func firstLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		return text[:index]
	}
	return text
}

func TestEnrollAndDump(t *testing.T) {
	out := filepath.Join(t.TempDir(), "template.ansi")
	var stdout, stderr bytes.Buffer
	// The photo first and the flag after it: the order the usage line shows.
	if err := run([]string{"enroll", filepath.Join(testPath("testdata"), "print.png"), "-out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("enroll: %v\n%s", err, stderr.String())
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("the template was not written: %v", err)
	}
	if len(written) < 20 || string(written[:4]) != "FMR\x00" {
		t.Fatalf("the template does not start with the format identifier: %x", written[:4])
	}
	// The record length field is the file's own length, big-endian.
	length := int(written[8])<<24 | int(written[9])<<16 | int(written[10])<<8 | int(written[11])
	if length != len(written) {
		t.Errorf("record length %d, file %d bytes", length, len(written))
	}
	if !strings.Contains(stdout.String(), "minutiae") {
		t.Errorf("the enrolment's summary is missing: %q", stdout.String())
	}

	stdout.Reset()
	if err := run([]string{"dump", out}, &stdout, &stderr); err != nil {
		t.Fatalf("dump: %v\n%s", err, stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "image: 320x320") {
		t.Errorf("dump's image line is missing:\n%s", text)
	}
	if !strings.Contains(text, "format: FMR 0") {
		t.Errorf("dump's format line is missing:\n%s", text)
	}
	if !strings.Contains(text, "ridge frequency: 0.0") && !strings.Contains(text, "ridge frequency: 0.1") {
		t.Errorf("dump's ridge frequency line is missing:\n%s", text)
	}
}

func TestDumpRejectsWhatIsNotATemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-template.ansi")
	if err := os.WriteFile(path, []byte("hello, this is not a template"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"dump", path}, &stdout, &stderr); err == nil {
		t.Fatal("dump accepted bytes that are not a template")
	}
}

// TestWatchEntering is the loop's version of the C++ sequence test: three
// frames of desk, then three of a finger, and the frame the stream starts on.
func TestWatchEntering(t *testing.T) {
	dir := frameSequence(t, "frame-background.png", "frame-background.png", "frame-background.png",
		"frame-finger.png", "frame-finger.png", "frame-finger.png")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"watch", "-source", "dir:" + dir}, &stdout, &stderr); err != nil {
		t.Fatalf("watch: %v\n%s", err, stderr.String())
	}
	verdicts := parseVerdicts(t, stdout.String())
	if len(verdicts) != 6 {
		t.Fatalf("%d frame lines, wanted 6:\n%s", len(verdicts), stdout.String())
	}
	// The first two finger frames carry the smoothed score over the threshold;
	// the third is the second consecutive one, and that is what fires.
	if got := firedIndices(verdicts); !equalInts(got, []int{5}) {
		t.Errorf("fired at %v, wanted [5]", got)
	}
	if verdicts[3].fire {
		t.Errorf("the first frame with a finger fired on its own")
	}
	if verdicts[5].reason != "none" {
		t.Errorf("the firing frame's reason is %q", verdicts[5].reason)
	}
	if verdicts[0].reason != "no-skin" {
		t.Errorf("the desk frame's reason is %q", verdicts[0].reason)
	}
}

// TestWatchLeaving is the other half of the rule: the hold keeps the stream
// open after the finger goes.
func TestWatchLeaving(t *testing.T) {
	names := []string{"frame-finger.png", "frame-finger.png", "frame-finger.png", "frame-finger.png"}
	names = append(names, "frame-background.png", "frame-background.png", "frame-background.png",
		"frame-background.png", "frame-background.png", "frame-background.png")
	dir := frameSequence(t, names...)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"watch", "-source", "dir:" + dir}, &stdout, &stderr); err != nil {
		t.Fatalf("watch: %v\n%s", err, stderr.String())
	}
	verdicts := parseVerdicts(t, stdout.String())
	fired := firedIndices(verdicts)
	// Frames 0..3 hold the finger, so 2 and 3 fire from the evidence; the five
	// frames of hold are 4..8, and frame 9 is the first that does not fire.
	if !equalInts(fired, []int{2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("fired at %v, wanted [2 3 4 5 6 7 8]", fired)
	}
	for index := 4; index <= 8; index++ {
		if verdicts[index].reason != "hold" {
			t.Errorf("frame %d fired with reason %q, wanted the hold", index, verdicts[index].reason)
		}
	}
	if verdicts[9].fire {
		t.Errorf("frame 9 still fired; the hold should have expired")
	}
}

// TestWatchTemplatesFollowProcessedFrames is the accounting the single-flight
// hand-off promises: every fire is either processed or dropped, never both and
// never neither, and a template exists for exactly the processed ones.
func TestWatchTemplatesFollowProcessedFrames(t *testing.T) {
	names := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		names = append(names, "frame-finger.png")
	}
	dir := frameSequence(t, names...)
	out := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"watch", "-source", "dir:" + dir, "-out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("watch: %v\n%s", err, stderr.String())
	}
	summary := parseSummary(t, stdout.String())
	if summary["fired"] != summary["processed"]+summary["dropped"]+summary["empty"] {
		t.Errorf("fired %d but processed %d, dropped %d and empty %d", summary["fired"], summary["processed"],
			summary["dropped"], summary["empty"])
	}
	if summary["processed"] < 1 {
		t.Errorf("no frame reached the pipeline at all: %v", summary)
	}
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(files)) != summary["processed"] {
		t.Errorf("%d templates for %d processed frames", len(files), summary["processed"])
	}
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), "template-") || !strings.HasSuffix(file.Name(), ".ansi") {
			t.Errorf("unexpected file %q in the output directory", file.Name())
		}
	}
	if summary["max in flight"] != 1 {
		t.Errorf("the segmentation worker ran %d times at once", summary["max in flight"])
	}
	if summary["gated"] != summary["frames"]-summary["fired"] {
		t.Errorf("gated %d, frames %d and fired %d do not add up", summary["gated"], summary["frames"], summary["fired"])
	}
}

// parseSummary reads the watch report's "name value" lines.
func parseSummary(t *testing.T, output string) map[string]int64 {
	t.Helper()
	summary := make(map[string]int64)
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if fields[0] == "max" && len(fields) >= 4 && fields[1] == "in" && fields[2] == "flight" {
			var peak int64
			if _, err := fmt.Sscanf(fields[3], "%d", &peak); err == nil {
				summary["max in flight"] = peak
			}
			continue
		}
		var value int64
		if _, err := fmt.Sscanf(fields[1], "%d", &value); err != nil {
			continue
		}
		switch fields[0] {
		case "frames", "fired", "processed", "gated", "dropped", "empty", "failed":
			summary[fields[0]] = value
		}
	}
	return summary
}

func TestWatchSkipsAnAbsentCamera(t *testing.T) {
	// A device number no machine has; the command says there is nothing to
	// watch and stops without an error.
	var stdout, stderr bytes.Buffer
	if err := run([]string{"watch", "-source", "camera:99"}, &stdout, &stderr); err != nil {
		t.Fatalf("watch camera:99: %v", err)
	}
	if !strings.Contains(stderr.String(), "nothing to watch") {
		t.Errorf("the absence of the camera was not reported: %q", stderr.String())
	}
	if _, err := newCameraSource(99); err == nil {
		t.Error("camera 99 opened")
	}
}

// --- the cost contract -----------------------------------------------------

// measure returns the median duration of n runs of fn.
func measure(n int, fn func()) time.Duration {
	durations := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		started := time.Now()
		fn()
		durations = append(durations, time.Since(started))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	return durations[len(durations)/2]
}

// TestTriggerIsCheaperThanThePipeline asserts the contract this example exists
// for: the trigger is an order of magnitude cheaper than the full pipeline on
// the same frame, so it can run on every frame of a camera while the pipeline
// runs on the few it selects.
//
// The floor is generous on purpose — ten times, against a measured ratio in
// the report and in the benchmarks — because a wall-clock ratio is a
// measurement, not a constant, and a test that pins it would fail on a busy
// machine for a reason that has nothing to do with the code.
func TestTriggerIsCheaperThanThePipeline(t *testing.T) {
	photo := fixture(t, "frame-finger.jpg")

	trigger, err := fingerprint.NewTrigger()
	if err != nil {
		t.Fatal(err)
	}
	defer trigger.Close()
	state := fingerprint.InitialTriggerState()

	triggerTime := measure(11, func() {
		outcome, err := trigger.Evaluate(photo, &state)
		if err != nil {
			t.Fatalf("trigger: %v", err)
		}
		state = outcome.State
	})
	pipelineTime := measure(5, func() {
		enrollment, err := fingerprint.Enroll(photo)
		if err != nil {
			t.Fatalf("enroll: %v", err)
		}
		enrollment.Close()
	})

	one, err := trigger.Evaluate(photo, &state)
	if err != nil {
		t.Fatal(err)
	}
	state = one.State
	ratio := float64(pipelineTime) / float64(triggerTime)
	t.Logf("trigger %v/frame (decode %.0f µs, prefilter %.0f µs), pipeline %v/frame, ratio %.1fx",
		triggerTime, one.DecodeMicros, one.PrefilterMicros, pipelineTime, ratio)
	if float64(triggerTime)*10 > float64(pipelineTime) {
		t.Errorf("the trigger is %v/frame and the pipeline %v/frame (%.1fx): the trigger must be at least ten times cheaper",
			triggerTime, pipelineTime, ratio)
	}
	// The frame budget the report states: 30 frames per second is 33 ms, and
	// the trigger must have orders of magnitude to spare.
	if triggerTime > 33*time.Millisecond {
		t.Errorf("the trigger takes %v per frame, more than a 30 fps frame budget", triggerTime)
	}
}

func BenchmarkTrigger(b *testing.B) {
	photo, err0 := fixtureBytes("frame-finger.jpg")
	if err0 != nil {
		b.Fatal(err0)
	}
	trigger, err := fingerprint.NewTrigger()
	if err != nil {
		b.Fatal(err)
	}
	defer trigger.Close()
	state := fingerprint.InitialTriggerState()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		outcome, err := trigger.Evaluate(photo, &state)
		if err != nil {
			b.Fatal(err)
		}
		state = outcome.State
	}
}

func BenchmarkFullPipeline(b *testing.B) {
	photo, err0 := fixtureBytes("frame-finger.jpg")
	if err0 != nil {
		b.Fatal(err0)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enrollment, err := fingerprint.Enroll(photo)
		if err != nil {
			b.Fatal(err)
		}
		enrollment.Close()
	}
}
