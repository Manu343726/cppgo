// Command fingerprint is the Go program of the application example: the CLI
// over a C++ library that does all of the image work.
//
//	 fingerprint enroll <photo> [-out template.ansi]   one frame, one template
//	 fingerprint dump <template.ansi>                   the template, as text
//	 fingerprint watch -source dir:<path> [-out <dir>]  a stream, gated
//	 fingerprint watch -source camera:<index>           a camera, gated
//	 fingerprint version                                what is loaded
//
// The Go side is the orchestration and nothing else: it reads frames, moves
// them across the boundary, runs the trigger on every one and the pipeline on
// the ones the trigger selects, writes files and prints numbers. It never
// decodes an image, never touches a pixel, never reads a minutia and never
// parses a template — every one of those lives on the C++ side, and the
// boundary is one call per frame.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	fingerprint "github.com/Manu343726/cppgo/examples/fingerprint/gen"
)

// The two objects a generated binding loads. The defaults are the places the
// Makefile puts them, relative to the example root; the environment overrides
// them for an installation somewhere else.
const (
	defaultShim    = "gen/libcppgo_fingerprint.so"
	defaultLibrary = "build/libfingerprint.so"
)

func shimPath() string {
	if path := os.Getenv("CPPGO_FINGERPRINT_SHIM"); path != "" {
		return path
	}
	return defaultShim
}

func libraryPath() string {
	if path := os.Getenv("CPPGO_LIBRARY_PATH"); path != "" {
		return path
	}
	return defaultLibrary
}

// sink is the Go end of the library's spdlog sink.
//
// The C++ sink calls the function once per record, with the record's level and
// the number of records its buffer now holds. The function is the argument of
// the call that produced the records — not a registration kept across calls —
// because a callback only exists for the duration of the call that passes it
// and only a reentrant call can deliver one (see the README).
//
// The text itself is not an argument: this build's callback carrier carries
// scalars, so a C string cannot cross as one. The records are read with
// DrainLog when the call returns, which is also the cheaper shape — one
// crossing for a burst of records instead of one per record.
type sink struct {
	mu      sync.Mutex
	records int64
	levels  [6]int64
	pumped  int64
}

func (l *sink) record(level int32, pending int32) {
	l.mu.Lock()
	if level >= 0 && level < int32(len(l.levels)) {
		l.levels[level]++
	}
	l.records++
	l.mu.Unlock()
}

// pump reads the buffered records and writes them when asked to. It is called
// after every call that can log, so the buffer never grows across a stream.
func (l *sink) pump(out io.Writer, verbose bool) {
	l.mu.Lock()
	total := l.records
	changed := total != l.pumped
	l.pumped = total
	l.mu.Unlock()
	if !changed {
		return
	}
	text, err := fingerprint.DrainLog()
	if err != nil {
		fmt.Fprintf(out, "fingerprint: reading the C++ log: %v\n", err)
		return
	}
	if verbose && text != "" {
		fmt.Fprint(out, text)
		if text[len(text)-1] != '\n' {
			fmt.Fprintln(out)
		}
	}
}

// summary is the per-level count of the records delivered through the callback.
func (l *sink) summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := []string{"trace", "debug", "info", "warn", "error", "critical"}
	out := ""
	for i, name := range names {
		if l.levels[i] == 0 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%s %d", name, l.levels[i])
	}
	if out == "" {
		return "no records"
	}
	return out
}

// openBinding loads the shim and the library.
func openBinding() error {
	fingerprint.CppgoLibraryPath = libraryPath()
	if err := fingerprint.Open(shimPath()); err != nil {
		return fmt.Errorf("opening the generated binding: %w", err)
	}
	return nil
}

// flagsFirst reorders a command line so the flag package sees every flag before
// the first positional argument. The package stops parsing at the first
// non-flag, and "enroll photo.png -out t.ansi" is the order the usage line
// shows, so a command that only accepted the flags first would be lying about
// its own help.
func flagsFirst(args []string, booleanFlags map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) == 0 || arg[0] != '-' || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		// A flag that is not boolean takes the next word as its value, so the
		// word is not a positional even when the caller wrote it after one.
		if !booleanFlags[arg] && !strings.Contains(arg, "=") && i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positional...)
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "fingerprint:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return flag.ErrHelp
	}
	// One record counter per invocation: the C++ side buffers, and this is what
	// reads what it wrote. It lives on the command's stack, not the package's:
	// two runs in one process (which is what a test is) do not share it.
	logs := &sink{}
	switch args[0] {
	case "enroll":
		return runEnroll(args[1:], logs, stdout, stderr)
	case "dump":
		return runDump(args[1:], logs, stdout, stderr)
	case "watch":
		return runWatch(args[1:], logs, stdout, stderr)
	case "version":
		return runVersion(args[1:], logs, stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `fingerprint — a fingerprint pipeline in C++, driven from Go.

  fingerprint enroll <photo> [-out template.ansi] [-v]
      one encoded frame through the whole pipeline: segment, enhance, binarise,
      thin, extract and serialise.

  fingerprint dump <template.ansi>
      the template's fields, printed from the C++ side.

  fingerprint watch -source <dir:<path>|camera:<index>> [-out <dir>] [-limit N] [-v]
      a stream: the trigger decides per frame, the full pipeline runs only on
      the frames it selects.

  fingerprint version [-v]
      the library, the OpenCV it links and the trigger's working size.
`)
}

func runEnroll(args []string, logs *sink, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("enroll", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "template.ansi", "where to write the template")
	verbose := flags.Bool("v", false, "print the C++ library's log records")
	if err := flags.Parse(flagsFirst(args, map[string]bool{"-v": true, "--v": true})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("enroll takes exactly one photo")
	}
	if err := openBinding(); err != nil {
		return err
	}
	photo, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}

	enrollment, err := fingerprint.Enroll(photo)
	// Delivering is the second step: the call above buffered its records, and
	// this one hands them to the callback. Both happen before the text is read.
	if _, deliverErr := fingerprint.DeliverLog(logs.record); deliverErr != nil {
		return deliverErr
	}
	logs.pump(stderr, *verbose)
	if err != nil {
		return fmt.Errorf("enrolling %s: %w", flags.Arg(0), err)
	}
	defer enrollment.Close()

	bytes, err := enrollment.TemplateBytes()
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(bytes), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "enrolled %s: %d minutiae, ridge frequency %.2f cycles/pixel\n",
		flags.Arg(0), enrollment.Minutiae(), enrollment.RidgeFrequency())
	fmt.Fprintf(stdout, "  region %dx%d at (%d,%d), image %dx%d\n",
		enrollment.RoiWidth(), enrollment.RoiHeight(), enrollment.RoiX(), enrollment.RoiY(),
		enrollment.ImageWidth(), enrollment.ImageHeight())
	fmt.Fprintf(stdout, "  %s in C++: decode %.2f ms, segment %.2f ms, enhance %.2f ms, extract %.2f ms, encode %.2f ms\n",
		timeString(enrollment.TotalMicros()), enrollment.DecodeMicros()/1000, enrollment.SegmentMicros()/1000,
		enrollment.EnhanceMicros()/1000, enrollment.ExtractMicros()/1000, enrollment.EncodeMicros()/1000)
	fmt.Fprintf(stdout, "  wrote %s (%d bytes)\n", *out, len(bytes))
	fmt.Fprintf(stdout, "  C++ log records: %s\n", logs.summary())
	return nil
}

func runDump(args []string, logs *sink, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("dump", flag.ContinueOnError)
	flags.SetOutput(stderr)
	verbose := flags.Bool("v", false, "print the C++ library's log records")
	if err := flags.Parse(flagsFirst(args, map[string]bool{"-v": true, "--v": true})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("dump takes exactly one template")
	}
	if err := openBinding(); err != nil {
		return err
	}
	bytes, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	text, err := fingerprint.DumpAnsi378(bytes)
	logs.pump(stderr, *verbose)
	if err != nil {
		return fmt.Errorf("reading %s: %w", flags.Arg(0), err)
	}
	fmt.Fprint(stdout, text)
	return nil
}

func runVersion(args []string, logs *sink, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	verbose := flags.Bool("v", false, "emit one log record per level and print them")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := openBinding(); err != nil {
		return err
	}
	version, err := fingerprint.Version()
	logs.pump(stderr, *verbose)
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, version)

	trigger, err := fingerprint.NewTrigger()
	if err != nil {
		return err
	}
	defer trigger.Close()
	fmt.Fprintf(stdout, "trigger: %dx%d working image\n", trigger.Width(), trigger.Height())

	if *verbose {
		if err := fingerprint.EmitLogSamples(logs.record); err != nil {
			return err
		}
		logs.pump(stdout, true)
		fmt.Fprintf(stdout, "C++ log records: %s\n", logs.summary())
	}
	return nil
}

func timeString(micros float64) string {
	if micros >= 1000 {
		return fmt.Sprintf("%.1f ms", micros/1000)
	}
	return fmt.Sprintf("%.0f µs", micros)
}

// templateName is the file one processed frame of a watch run is written to.
func templateName(index int) string {
	return fmt.Sprintf("template-%04d.ansi", index)
}
