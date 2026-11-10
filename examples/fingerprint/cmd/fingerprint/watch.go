// The watch loop: a frame source, the trigger on every frame, and one worker
// running the full pipeline on the frames it selects.
//
// The shape is the point of the example. The trigger runs on the calling
// goroutine at the source's pace; the pipeline runs on exactly one worker
// goroutine, behind a one-slot hand-off. A frame that fires while the worker is
// busy replaces the pending one — the newest frame is the one worth segmenting,
// and the replaced frame is counted as dropped rather than queued, because a
// queue that grows is a queue that runs minutes behind the camera.

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	fingerprint "github.com/Manu343726/cppgo/examples/fingerprint/gen"
)

// syncWriter serializes what two goroutines write. The watch loop prints its
// verdicts and the worker prints what it did with the frames it processed, and
// they share one output: a writer that interleaved them, or that raced on a
// bytes.Buffer under the race detector, would be a bug in the report.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// Frame is one frame, in the form the source produced it: encoded bytes as a
// camera or a file hands them over, or a raw YUYV buffer when the device has no
// MJPEG to give.
//
// The frame is data plus what it is; nothing here knows how to read a pixel.
type Frame struct {
	Index  int
	Name   string
	Data   []byte
	YUYV   bool
	Width  int
	Height int
}

// FrameSource is where the watch loop's frames come from. The trigger, the
// pipeline and the loop do not know which implementation is in use: a directory
// of files for a test or a replay, a V4L2 device for a camera.
type FrameSource interface {
	// Name is the source as the user named it, for the report.
	Name() string
	// Next returns the next frame, or io.EOF when the stream ended.
	Next() (Frame, error)
	Close() error
}

// openSource resolves a -source value: dir:<path>, camera:<index>, or a bare
// path, which is a directory.
func openSource(spec string) (FrameSource, error) {
	switch {
	case strings.HasPrefix(spec, "dir:"):
		return newDirSource(strings.TrimPrefix(spec, "dir:"))
	case strings.HasPrefix(spec, "camera:"):
		var index int
		if _, err := fmt.Sscanf(strings.TrimPrefix(spec, "camera:"), "%d", &index); err != nil {
			return nil, fmt.Errorf("source %q: a camera is camera:<index>", spec)
		}
		return newCameraSource(index)
	case spec == "":
		return nil, errors.New("no source: pass -source dir:<path> or -source camera:<index>")
	default:
		return newDirSource(spec)
	}
}

// --- a directory of encoded frames -----------------------------------------

// dirSource replays a directory's images in name order. It is the source the
// tests and CI use: the frames are committed files, so a run is reproducible.
type dirSource struct {
	dir   string
	names []string
	next  int
}

// imageExtensions are the formats the decoder is asked to read. The list is the
// source's, not the library's: the source only decides what is a frame.
var imageExtensions = map[string]bool{".png": true, ".jpg": true, ".jpeg": true}

func newDirSource(dir string) (*dirSource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("source dir:%s: %w", dir, err)
	}
	source := &dirSource{dir: dir}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if imageExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			source.names = append(source.names, entry.Name())
		}
	}
	sort.Strings(source.names)
	return source, nil
}

func (s *dirSource) Name() string { return "dir:" + s.dir }

func (s *dirSource) Next() (Frame, error) {
	if s.next >= len(s.names) {
		return Frame{}, io.EOF
	}
	name := s.names[s.next]
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		return Frame{}, err
	}
	frame := Frame{Index: s.next, Name: name, Data: data}
	s.next++
	return frame, nil
}

func (s *dirSource) Close() error { return nil }

// --- the hand-off to the single segmentation worker ------------------------

// segSlot is the one-slot hand-off between the trigger and the worker.
type segSlot struct {
	mu      sync.Mutex
	frame   Frame
	has     bool
	closed  bool
	wake    chan struct{}
	stats   *watchStats
	outDir  string
	verbose bool
	out     io.Writer
	logs    *sink
}

func newSegSlot(stats *watchStats, outDir string, verbose bool, out io.Writer, logs *sink) *segSlot {
	return &segSlot{wake: make(chan struct{}, 1), stats: stats, outDir: outDir, verbose: verbose, out: out, logs: logs}
}

// offer hands a frame to the worker. If a frame is already pending it is
// replaced: single-flight, newest wins, and the replaced frame is counted.
func (s *segSlot) offer(frame Frame) {
	s.mu.Lock()
	if s.has {
		atomic.AddInt64(&s.stats.Dropped, 1)
	}
	s.frame = frame
	s.has = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// take returns the pending frame, if any.
func (s *segSlot) take() (Frame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has {
		return Frame{}, false
	}
	frame := s.frame
	s.has = false
	return frame, true
}

func (s *segSlot) finish() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *segSlot) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed && !s.has
}

// run processes frames until the producer finishes and nothing is pending.
func (s *segSlot) run() {
	for {
		<-s.wake
		for {
			frame, ok := s.take()
			if !ok {
				break
			}
			s.process(frame)
		}
		if s.isClosed() {
			return
		}
	}
}

// process runs the full pipeline on one frame. The in-flight count is what the
// single-flight test asserts: it is never more than one.
func (s *segSlot) process(frame Frame) {
	inFlight := atomic.AddInt64(&s.stats.InFlight, 1)
	defer atomic.AddInt64(&s.stats.InFlight, -1)
	for {
		peak := atomic.LoadInt64(&s.stats.MaxInFlight)
		if inFlight <= peak || atomic.CompareAndSwapInt64(&s.stats.MaxInFlight, peak, inFlight) {
			break
		}
	}

	started := time.Now()
	enrollment, err := fingerprint.Enroll(frame.Data)
	// The call buffered its records; this one delivers them, and it is the call
	// that takes the callback because only a reentrant call may deliver one.
	if _, deliverErr := fingerprint.DeliverLog(s.logs.record); deliverErr != nil && err == nil {
		err = deliverErr
	}
	elapsed := time.Since(started)
	s.logs.pump(s.out, s.verbose)
	if err != nil {
		// The pipeline found no finger in a frame the trigger selected. The
		// hold window is why: it keeps a stream alive across a few frames after
		// the finger goes, and the scene may already have moved on. Counted
		// apart from a failure of the call itself.
		atomic.AddInt64(&s.stats.Empty, 1)
		if s.verbose {
			fmt.Fprintf(s.out, "  frame %04d %-24s no finger in the selected frame: %v\n", frame.Index, frame.Name, err)
		}
		return
	}
	defer enrollment.Close()

	templateBytes, err := enrollment.TemplateBytes()
	if err != nil {
		atomic.AddInt64(&s.stats.Failed, 1)
		return
	}
	if s.outDir != "" {
		path := filepath.Join(s.outDir, templateName(frame.Index))
		if err := os.WriteFile(path, []byte(templateBytes), 0o644); err != nil {
			atomic.AddInt64(&s.stats.Failed, 1)
			fmt.Fprintf(s.out, "frame %04d write %s: %v\n", frame.Index, path, err)
			return
		}
	}
	atomic.AddInt64(&s.stats.Processed, 1)

	cppMicros := enrollment.TotalMicros()
	goMicros := float64(elapsed.Microseconds()) - cppMicros
	s.stats.mu.Lock()
	s.stats.SegmentMicros += cppMicros
	s.stats.SegmentGoMicros += goMicros
	s.stats.Minutiae += int64(enrollment.Minutiae())
	s.stats.mu.Unlock()
	if s.verbose {
		fmt.Fprintf(s.out, "  frame %04d %-24s segmented: %d minutiae, %.1f ms in C++, %.1f µs in Go\n",
			frame.Index, frame.Name, enrollment.Minutiae(), cppMicros/1000, goMicros)
	}
}

// --- the statistics the run reports ----------------------------------------

// watchStats is what the run measured. The times come from two places and stay
// apart: the microseconds the C++ side reported for its own stages, and the
// microseconds the call took from Go, whose difference is what the boundary and
// the orchestration cost.
type watchStats struct {
	mu sync.Mutex

	Frames    int64
	Fired     int64
	Processed int64
	Gated     int64
	Dropped int64
	// Empty counts the frames the trigger selected but the pipeline found no
	// finger in: a held frame is one the trigger kept for continuity, and the
	// scene may already have moved on. It is not a failure.
	Empty  int64
	Failed int64

	InFlight    int64
	MaxInFlight int64

	TriggerMicros   float64 // C++: decode + prefilter
	TriggerGoMicros float64 // Go: the rest of the call, and the loop
	SegmentMicros   float64 // C++: the whole pipeline
	SegmentGoMicros float64
	Minutiae        int64
	WallMicros      float64
}

// addTrigger records one frame's trigger time: what C++ reported for its own
// stages, and the rest of the call, which is the boundary and the loop.
func (s *watchStats) addTrigger(cppMicros, goMicros float64) {
	s.mu.Lock()
	s.TriggerMicros += cppMicros
	s.TriggerGoMicros += goMicros
	s.mu.Unlock()
}

// watchReport is the run's numbers once the stream has ended: the counters as
// they were and nothing to lock.
type watchReport struct {
	Frames      int64
	Fired       int64
	Processed   int64
	Gated       int64
	Dropped     int64
	Empty       int64
	Failed      int64
	MaxInFlight int64
	Trigger     float64
	TriggerGo   float64
	Segment     float64
	SegmentGo   float64
	Minutiae    int64
	Wall        float64
}

func (s *watchStats) snapshot() watchReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return watchReport{
		Frames:      atomic.LoadInt64(&s.Frames),
		Empty:       atomic.LoadInt64(&s.Empty),
		Fired:       atomic.LoadInt64(&s.Fired),
		Processed:   atomic.LoadInt64(&s.Processed),
		Gated:       atomic.LoadInt64(&s.Gated),
		Dropped:     atomic.LoadInt64(&s.Dropped),
		Failed:      atomic.LoadInt64(&s.Failed),
		MaxInFlight: atomic.LoadInt64(&s.MaxInFlight),
		Trigger:     s.TriggerMicros,
		TriggerGo:   s.TriggerGoMicros,
		Segment:     s.SegmentMicros,
		SegmentGo:   s.SegmentGoMicros,
		Minutiae:    s.Minutiae,
		Wall:        s.WallMicros,
	}
}

// report prints the run's summary: what the trigger saved, and where the time
// went.
func (s watchReport) report(out io.Writer) {
	frames := float64(s.Frames)
	processed := float64(s.Processed)
	segmentMicros := 0.0
	minutiae := s.Minutiae
	if s.Processed > 0 {
		segmentMicros = s.Segment / processed
	}
	triggerMicros := 0.0
	triggerGoMicros := 0.0
	if s.Frames > 0 {
		triggerMicros = s.Trigger / frames
		triggerGoMicros = s.TriggerGo / frames
	}
	segmentGoMicros := 0.0
	if s.Processed > 0 {
		segmentGoMicros = s.SegmentGo / processed
	}

	fmt.Fprintf(out, "\nframes     %d\n", s.Frames)
	fmt.Fprintf(out, "fired      %d\n", s.Fired)
	fmt.Fprintf(out, "processed  %d\n", s.Processed)
	fmt.Fprintf(out, "gated      %d  (the full pipeline never ran)\n", s.Gated)
	fmt.Fprintf(out, "dropped    %d  (fired while the worker was busy)\n", s.Dropped)
	if s.Empty > 0 {
		fmt.Fprintf(out, "empty      %d  (selected, and the pipeline found no finger)\n", s.Empty)
	}
	if s.Failed > 0 {
		fmt.Fprintf(out, "failed     %d\n", s.Failed)
	}
	fmt.Fprintf(out, "max in flight %d\n", s.MaxInFlight)
	fmt.Fprintf(out, "trigger    %.0f µs/frame in C++, %.0f µs/frame in Go\n", triggerMicros, triggerGoMicros)
	if s.Processed > 0 {
		fmt.Fprintf(out, "segment    %.2f ms/frame in C++, %.0f µs/frame in Go\n", segmentMicros/1000, segmentGoMicros)
		fmt.Fprintf(out, "minutiae   %.1f per processed frame\n", float64(minutiae)/processed)
	}
	if s.Frames > 0 {
		saved := 1 - processed/frames
		fmt.Fprintf(out, "saved      %.0f%% of the frames never reached the pipeline\n", saved*100)
	}
	fmt.Fprintf(out, "wall       %.2f ms for %d frames\n", s.Wall/1000, s.Frames)
}

// --- the command -----------------------------------------------------------

func runWatch(args []string, logs *sink, stdout, stderr io.Writer) error {
	return watchCommand(args, logs, stdout, stderr)
}

func watchCommand(args []string, logs *sink, stdout, stderr io.Writer) error {
	stdout = &syncWriter{w: stdout}
	stderr = &syncWriter{w: stderr}
	flags := newWatchFlags(stderr)
	if err := flags.flagSet.Parse(args); err != nil {
		return err
	}
	if flags.source == "" {
		flags.flagSet.Usage()
		return errors.New("watch needs -source")
	}

	source, err := openSource(flags.source)
	if err != nil {
		// A camera that is not there is not a failure of the program: there is
		// simply nothing to watch, and saying so is the whole behaviour.
		if errors.Is(err, errSourceAbsent) {
			fmt.Fprintf(stderr, "fingerprint: %v — nothing to watch\n", err)
			return nil
		}
		return err
	}
	defer source.Close()

	if err := openBinding(); err != nil {
		return err
	}
	if flags.outDir != "" {
		if err := os.MkdirAll(flags.outDir, 0o755); err != nil {
			return err
		}
	}

	var stats watchStats
	started := time.Now()
	slot := newSegSlot(&stats, flags.outDir, flags.verbose, stdout, logs)
	worker := make(chan struct{})
	go func() {
		defer close(worker)
		slot.run()
	}()

	trigger, err := fingerprint.NewTrigger()
	if err != nil {
		return err
	}
	defer trigger.Close()
	state := fingerprint.InitialTriggerState()

	for {
		if flags.limit > 0 && stats.Frames >= int64(flags.limit) {
			break
		}
		frame, err := source.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			slot.finish()
			<-worker
			return err
		}

		callStarted := time.Now()
		var outcome fingerprint.TriggerOutcome
		if frame.YUYV {
			outcome, err = trigger.EvaluateYuyv(frame.Data, int32(frame.Width), int32(frame.Height), &state)
		} else {
			outcome, err = trigger.Evaluate(frame.Data, &state)
		}
		elapsed := time.Since(callStarted)
		logs.pump(stderr, flags.verbose)
		if err != nil {
			slot.finish()
			<-worker
			return fmt.Errorf("frame %d: %w", frame.Index, err)
		}
		state = outcome.State

		atomic.AddInt64(&stats.Frames, 1)
		cppMicros := outcome.DecodeMicros + outcome.PrefilterMicros
		stats.addTrigger(cppMicros, float64(elapsed.Microseconds())-cppMicros)

		verdict := outcome.Verdict
		if verdict.Fire {
			atomic.AddInt64(&stats.Fired, 1)
			slot.offer(frame)
		} else {
			atomic.AddInt64(&stats.Gated, 1)
		}
		fmt.Fprintf(stdout, "frame %04d %-24s %-4s score %.2f rois %d reason %s\n", frame.Index, frame.Name,
			fireLabel(verdict.Fire), verdict.Score, verdict.RoiCount, reasonName(verdict.Reason))
	}

	slot.finish()
	<-worker
	stats.WallMicros = float64(time.Since(started).Microseconds())
	snapshot := stats.snapshot()
	snapshot.report(stdout)
	if flags.verbose {
		fmt.Fprintf(stdout, "C++ log records: %s\n", logs.summary())
	}
	return nil
}

// fireLabel is the verdict's decision as one word. Both words are four
// characters, so the per-frame line is the same shape whether it fired or not
// and a test can read it by position.
func fireLabel(fire bool) string {
	if fire {
		return "fire"
	}
	return "gate"
}

// reasonName is the verdict's reason as the C++ enum names it. The values are
// the generated constants; the strings are this program's business.
func reasonName(reason fingerprint.TriggerReason) string {
	switch reason {
	case fingerprint.TriggerReasonNONE:
		return "none"
	case fingerprint.TriggerReasonHOLD:
		return "hold"
	case fingerprint.TriggerReasonDECODEFAILED:
		return "decode-failed"
	case fingerprint.TriggerReasonNOSKIN:
		return "no-skin"
	case fingerprint.TriggerReasonNOBLOB:
		return "no-blob"
	case fingerprint.TriggerReasonSHAPE:
		return "shape"
	case fingerprint.TriggerReasonLOWCONTRAST:
		return "low-contrast"
	case fingerprint.TriggerReasonNORIDGES:
		return "no-ridges"
	case fingerprint.TriggerReasonBELOWTHRESHOLD:
		return "below-threshold"
	default:
		return "unknown"
	}
}
