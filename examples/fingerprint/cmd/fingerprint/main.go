// Command fingerprint is the Go program of the application example: the CLI
// over a C++ library that does all of the image work.
//
//	fingerprint enroll <photo> [-out template.ansi]   one frame, one template
//	fingerprint dump <template.ansi>                   the template, as text
//	fingerprint watch -source dir:<path> [-out <dir>]  a stream, gated
//	fingerprint watch -source camera:<index>           a camera, gated
//	fingerprint version                                what is loaded
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

// openBinding loads the shim and the library.
func openBinding(verbose bool) error {
	fingerprint.CppgoLibraryPath = libraryPath()
	if err := fingerprint.Open(shimPath()); err != nil {
		return fmt.Errorf("opening the generated binding: %w", err)
	}
	return nil
}

// withLogs loads the binding, registers the library's sink for the duration of
// the command, and hands the caller the counter the summary reads. The sink is
// cleared and the registration released when the command returns, in that
// order: the C++ side holds a raw pointer and must forget it first.
func withLogs(verbose bool, out io.Writer) (*bindingLog, func(), error) {
	logs := newBindingLog(out, verbose)
	if err := openBinding(verbose); err != nil {
		return nil, nil, err
	}
	stop, err := logs.install()
	if err != nil {
		return nil, nil, err
	}
	return logs, func() {
		if err := stop(); err != nil {
			fmt.Fprintf(out, "fingerprint: releasing the C++ log sink: %v\n", err)
		}
	}, nil
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
	switch args[0] {
	case "enroll":
		return runEnroll(args[1:], stdout, stderr)
	case "dump":
		return runDump(args[1:], stdout, stderr)
	case "watch":
		return runWatch(args[1:], stdout, stderr)
	case "version":
		return runVersion(args[1:], stdout, stderr)
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

func runEnroll(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("enroll", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "template.ansi", "where to write the template")
	verbose := flags.Bool("v", false, "print the C++ library's records at every level")
	if err := flags.Parse(flagsFirst(args, map[string]bool{"-v": true, "--v": true})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("enroll takes exactly one photo")
	}
	logs, stop, err := withLogs(*verbose, stderr)
	if err != nil {
		return err
	}
	defer stop()
	photo, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}

	enrollment, err := fingerprint.Enroll(photo)
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

func runDump(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("dump", flag.ContinueOnError)
	flags.SetOutput(stderr)
	verbose := flags.Bool("v", false, "print the C++ library's records at every level")
	if err := flags.Parse(flagsFirst(args, map[string]bool{"-v": true, "--v": true})); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("dump takes exactly one template")
	}
	logs, stop, err := withLogs(*verbose, stderr)
	if err != nil {
		return err
	}
	defer stop()
	bytes, err := os.ReadFile(flags.Arg(0))
	if err != nil {
		return err
	}
	text, err := fingerprint.DumpAnsi378(bytes)
	if err != nil {
		return fmt.Errorf("reading %s: %w", flags.Arg(0), err)
	}
	// The listing is the command's output and stays parseable: what the library
	// wrote goes to the error stream, and only when the caller asked for it.
	if *verbose {
		fmt.Fprintf(stderr, "C++ log records: %s\n", logs.summary())
	}
	fmt.Fprint(stdout, text)
	return nil
}

func runVersion(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("version", flag.ContinueOnError)
	flags.SetOutput(stderr)
	verbose := flags.Bool("v", false, "print the C++ library's records at every level")
	if err := flags.Parse(flagsFirst(args, map[string]bool{"-v": true, "--v": true})); err != nil {
		return err
	}
	logs, stop, err := withLogs(*verbose, stderr)
	if err != nil {
		return err
	}
	defer stop()
	version, err := fingerprint.Version()
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
	// Neither call writes a record; the summary appears only if something did,
	// so a version report is not padded with "no records".
	if summary := logs.summary(); summary != "no records" {
		fmt.Fprintf(stdout, "C++ log records: %s\n", summary)
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
