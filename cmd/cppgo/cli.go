package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Manu343726/cppgo/internal/gen"
)

// Exit codes, as documented in usage().
const (
	exitOK    = 0 // the command succeeded
	exitError = 1 // generation (or verification) failed
	exitUsage = 2 // the command line itself was wrong
)

// version is printed by "cppgo version". Set it at build time with
//
//	go build -ldflags "-X main.version=v1.2.3" ./cmd/cppgo
var version = "dev"

// cli holds the two streams every command writes to, so tests can capture
// them instead of relying on the process's own stdout and stderr.
type cli struct {
	stdout io.Writer
	stderr io.Writer
}

func (c *cli) printf(format string, args ...any) { fmt.Fprintf(c.stdout, format, args...) }

func (c *cli) errorf(format string, args ...any) { fmt.Fprintf(c.stderr, format+"\n", args...) }

// run is the whole CLI: it dispatches the subcommand and returns the process
// exit code. main is the only place that calls os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
	c := &cli{stdout: stdout, stderr: stderr}
	if len(args) == 0 {
		c.errorf("cppgo: missing command")
		c.printUsage(c.stderr)
		return exitUsage
	}
	switch args[0] {
	case "gen":
		return c.generate(args[1:], false)
	case "check":
		return c.generate(args[1:], true)
	case "version", "-v", "--version":
		return c.printVersion()
	case "help", "-h", "--help":
		return c.help(args[1:])
	default:
		c.errorf("cppgo: unknown command %q", args[0])
		c.printUsage(c.stderr)
		return exitUsage
	}
}

// help implements "cppgo help [command]".
func (c *cli) help(topic []string) int {
	if len(topic) == 0 {
		c.printUsage(c.stdout)
		return exitOK
	}
	switch topic[0] {
	case "gen":
		c.printGenUsage(c.stdout)
		return exitOK
	case "check":
		c.printCheckUsage(c.stdout)
		return exitOK
	case "version":
		c.printVersionUsage(c.stdout)
		return exitOK
	case "help":
		c.printUsage(c.stdout)
		return exitOK
	default:
		c.errorf("cppgo: unknown command %q", topic[0])
		c.printUsage(c.stderr)
		return exitUsage
	}
}

// printVersion implements "cppgo version".
func (c *cli) printVersion() int {
	c.printf("cppgo %s\n", version)
	return exitOK
}

// generate implements "cppgo gen" and "cppgo check". The two are the same
// command with a different default for -check: gen writes, check compares.
func (c *cli) generate(args []string, checkMode bool) int {
	verb := "gen"
	if checkMode {
		verb = "check"
	}
	if wantsHelp(args) {
		if checkMode {
			c.printCheckUsage(c.stdout)
		} else {
			c.printGenUsage(c.stdout)
		}
		return exitOK
	}

	fs := flag.NewFlagSet("cppgo "+verb, flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	fs.Usage = func() {
		if checkMode {
			c.printCheckUsage(c.stderr)
			return
		}
		c.printGenUsage(c.stderr)
	}
	dir := fs.String("dir", ".", "package directory holding the //cppgo: directives")
	out := fs.String("out", "", "output directory (defaults to -dir, rewriting the sources in place)")
	check := fs.Bool("check", checkMode, "report whether the generated files are up to date instead of writing them")
	quiet := fs.Bool("q", false, "suppress the confirmation message")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		c.errorf("cppgo %s: unexpected argument %q", verb, fs.Arg(0))
		fs.Usage()
		return exitUsage
	}
	if *dir == "" {
		c.errorf("cppgo %s: -dir must not be empty", verb)
		return exitUsage
	}
	info, err := os.Stat(*dir)
	if err != nil {
		c.errorf("cppgo %s: %v", verb, err)
		return exitError
	}
	if !info.IsDir() {
		c.errorf("cppgo %s: %s is not a directory", verb, *dir)
		return exitUsage
	}
	cfg := gen.Config{Dir: *dir, Out: *out, Check: *check}
	if err := gen.Generate(cfg); err != nil {
		c.errorf("cppgo %s: %v", verb, err)
		return exitError
	}
	if *check && !*quiet {
		c.printf("%s: generated files are up to date\n", *dir)
	}
	return exitOK
}

// wantsHelp reports whether the arguments ask for help. It is checked before
// parsing so that help always goes to stdout and exits successfully, while
// malformed commands go to stderr and exit with exitUsage.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "-help", "--help":
			return true
		}
	}
	return false
}

// printUsage is the top-level help.
func (c *cli) printUsage(w io.Writer) {
	fmt.Fprintln(w, "cppgo generates Go bindings for C++ that call into it directly,")
	fmt.Fprintln(w, "through one assembly trampoline per function, with no cgo involved.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  cppgo gen   -dir <pkgdir> [-out <outdir>] [-check]")
	fmt.Fprintln(w, "  cppgo check -dir <pkgdir> [-out <outdir>]")
	fmt.Fprintln(w, "  cppgo version")
	fmt.Fprintln(w, "  cppgo help [command]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintf(w, "  %-8s %s\n", "gen", "write the wrappers, the trampolines, the C++ shim header and the stub file")
	fmt.Fprintf(w, "  %-8s %s\n", "check", "verify that the generated files are up to date, without writing")
	fmt.Fprintf(w, "  %-8s %s\n", "version", "print the version of the generator")
	fmt.Fprintf(w, "  %-8s %s\n", "help", "print this message, or the help of one command")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run \"cppgo help <command>\" for command-specific help.")
}

// printGenUsage documents "cppgo gen".
func (c *cli) printGenUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo gen -dir <pkgdir> [-out <outdir>] [-check] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "gen reads the Go package in -dir and writes four files:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-18s %s\n", gen.GenFileName, "idiomatic wrappers, lazy library binding and the")
	fmt.Fprintf(w, "  %-18s %s\n", "", "declarations of the assembly trampolines")
	fmt.Fprintf(w, "  %-18s %s\n", gen.AsmFileName, "one NOSPLIT trampoline per bound function")
	fmt.Fprintf(w, "  %-18s %s\n", gen.ShimFileName, "the extern \"C\" prototypes the C++ side implements")
	fmt.Fprintf(w, "  %-18s %s\n", gen.SourceFileName, "the input file with every bound function body")
	fmt.Fprintf(w, "  %-18s %s\n", "", "replaced by a delegation to its wrapper")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The input package declares the library once with //cppgo:library <name>")
	fmt.Fprintln(w, "and every bound function with //cppgo:external <symbol> on its doc comment.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "flags:")
	fmt.Fprintln(w, "  -dir string")
	fmt.Fprintln(w, "        package directory holding the //cppgo: directives (default \".\")")
	fmt.Fprintln(w, "  -out string")
	fmt.Fprintln(w, "        output directory; defaults to -dir, which rewrites the input")
	fmt.Fprintln(w, "        sources in place instead of writing a copy")
	fmt.Fprintln(w, "  -check")
	fmt.Fprintln(w, "        report whether the generated files are up to date instead of writing")
	fmt.Fprintln(w, "  -q")
	fmt.Fprintln(w, "        suppress the confirmation message")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo gen -dir ./bindings/mathx -out ./bindings/mathx")
}

// printCheckUsage documents "cppgo check".
func (c *cli) printCheckUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo check -dir <pkgdir> [-out <outdir>] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "check regenerates the bindings in memory and compares them with the")
	fmt.Fprintln(w, "files on disk. It writes nothing, exits 0 when everything is up to")
	fmt.Fprintln(w, "date and 1 when a file is missing or stale, which is the shape a CI")
	fmt.Fprintln(w, "gate wants.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "It accepts the same -dir, -out and -q flags as \"cppgo gen\".")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo check -dir ./bindings/mathx || echo out of date")
}

// printVersionUsage documents "cppgo version".
func (c *cli) printVersionUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Prints the generator version, followed by a newline, on stdout.")
	fmt.Fprintln(w, "The version is set at build time with -ldflags \"-X main.version=...\"")
	fmt.Fprintln(w, "and is \"dev\" for an unmodified local build.")
}
