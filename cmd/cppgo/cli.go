package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

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

// stringList is a repeatable flag: -header a.hpp -header b.hpp appends.
type stringList []string

// String implements flag.Value.
func (l *stringList) String() string { return strings.Join(*l, ",") }

// Set implements flag.Value.
func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

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
	var headers, bind, exclude, rename stringList
	fs.Var(&headers, "header", "C++ header to bind; repeat for several")
	packageName := fs.String("package", "", "Go package name")
	library := fs.String("library", "", "shared object Open loads")
	out := fs.String("out", ".", "output directory")
	compileCommands := fs.String("compile-commands", "", "compile_commands.json (or its directory) for the header's flags")
	fs.Var(&bind, "bind", "add a root; repeatable")
	fs.Var(&exclude, "exclude", "remove a root; repeatable")
	fs.Var(&rename, "rename", "<qualified signature>=<GoName>; repeatable")
	check := fs.Bool("check", checkMode, "report whether the generated files are up to date instead of writing them")
	quiet := fs.Bool("q", false, "suppress the per-binding report and the confirmation message")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		c.errorf("cppgo %s: unexpected argument %q", verb, fs.Arg(0))
		fs.Usage()
		return exitUsage
	}
	if len(headers) == 0 {
		c.errorf("cppgo %s: -header is required: name the C++ header to bind", verb)
		fs.Usage()
		return exitUsage
	}
	if *out == "" {
		c.errorf("cppgo %s: -out must not be empty", verb)
		return exitUsage
	}

	cfg := gen.Config{
		Headers:         headers,
		Package:         *packageName,
		Library:         *library,
		Out:             *out,
		Check:           *check,
		CompileCommands: *compileCommands,
		Bind:            bind,
		Exclude:         exclude,
		Rename:          rename,
		Warnings:        c.stderr,
	}
	if !*quiet && !*check {
		// The per-binding carrier report is the answer to "what did this
		// bind, and how?", so it goes to stdout for "gen".
		cfg.Report = c.stdout
	}
	if err := gen.Generate(cfg); err != nil {
		c.errorf("cppgo %s: %v", verb, err)
		return exitError
	}
	if *check && !*quiet {
		c.printf("%s: generated files are up to date\n", *out)
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
	fmt.Fprintln(w, "cppgo generates a type-safe Go package from C++ headers and calls into")
	fmt.Fprintln(w, "them through one assembly trampoline per binding, plus a generated shim")
	fmt.Fprintln(w, "for everything the direct ABI cannot express. No cgo is involved.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  cppgo gen   -header <path> -package <name> -library <name> [-out <dir>]")
	fmt.Fprintln(w, "  cppgo check -header <path> -package <name> -library <name> [-out <dir>]")
	fmt.Fprintln(w, "  cppgo version")
	fmt.Fprintln(w, "  cppgo help [command]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintf(w, "  %-8s %s\n", "gen", "write the Go package, the trampolines and the C++ shim")
	fmt.Fprintf(w, "  %-8s %s\n", "check", "verify that the generated files are up to date, without writing")
	fmt.Fprintf(w, "  %-8s %s\n", "version", "print the version of the generator")
	fmt.Fprintf(w, "  %-8s %s\n", "help", "print this message, or the help of one command")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run \"cppgo help <command>\" for command-specific help.")
}

// printGenUsage documents "cppgo gen".
func (c *cli) printGenUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo gen -header <path> [-header <path>]... -package <name>")
	fmt.Fprintln(w, "                  -library <name-or-path> [-out <dir>] [-compile-commands <path>]")
	fmt.Fprintln(w, "                  [-bind <qualified>]... [-exclude <qualified>]...")
	fmt.Fprintln(w, "                  [-rename '<signature>=<GoName>']... [-check] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "gen reads the C++ headers through libclang and writes:")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-18s %s\n", gen.GenFileName, "the Go API, the generated Open and the trampoline")
	fmt.Fprintf(w, "  %-18s %s\n", "", "declarations")
	fmt.Fprintf(w, "  %-18s %s\n", gen.AsmFileName, "one NOSPLIT trampoline per binding")
	fmt.Fprintf(w, "  %-18s %s\n", gen.ShimHeaderFileName, "the extern \"C\" thunk declarations")
	fmt.Fprintf(w, "  %-18s %s\n", gen.ShimSourceFileName, "the thunks: compile this into the library")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Every binding gets exactly one carrier: the direct trampoline when it is")
	fmt.Fprintln(w, "noexcept, not virtual, defined out of line and register representable; a")
	fmt.Fprintln(w, "generated thunk everywhere else. The report names the carrier and why.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "flags:")
	fmt.Fprintln(w, "  -header string")
	fmt.Fprintln(w, "        C++ header to bind; repeatable")
	fmt.Fprintln(w, "  -package string")
	fmt.Fprintln(w, "        Go package name")
	fmt.Fprintln(w, "  -library string")
	fmt.Fprintln(w, "        shared object Open loads; when it is a readable file, its dynamic")
	fmt.Fprintln(w, "        symbol table cross-checks every direct binding")
	fmt.Fprintln(w, "  -out string")
	fmt.Fprintln(w, "        output directory (default \".\")")
	fmt.Fprintln(w, "  -compile-commands string")
	fmt.Fprintln(w, "        compile_commands.json (or its directory) to take the header's flags")
	fmt.Fprintln(w, "        from, instead of discovering them")
	fmt.Fprintln(w, "  -bind qualified")
	fmt.Fprintln(w, "        add a root (a namespace, class, member or function); repeatable")
	fmt.Fprintln(w, "  -exclude qualified")
	fmt.Fprintln(w, "        remove a root; repeatable")
	fmt.Fprintln(w, "  -rename 'signature=GoName'")
	fmt.Fprintln(w, "        resolve a Go name collision; repeatable")
	fmt.Fprintln(w, "  -check")
	fmt.Fprintln(w, "        report whether the generated files are up to date instead of writing")
	fmt.Fprintln(w, "  -q")
	fmt.Fprintln(w, "        suppress the per-binding report and the confirmation message")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo gen -header include/mathx/vector.hpp -package mathx \\")
	fmt.Fprintln(w, "            -library libmathx -out ./mathx")
}

// printCheckUsage documents "cppgo check".
func (c *cli) printCheckUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo check -header <path> [-header <path>]... -package <name>")
	fmt.Fprintln(w, "                    -library <name-or-path> [-out <dir>] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "check regenerates the package in memory and compares it with the files on")
	fmt.Fprintln(w, "disk. It writes nothing, exits 0 when everything is up to date and 1 when")
	fmt.Fprintln(w, "a file is missing or stale, which is the shape a CI gate wants.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "It accepts the same manifest flags as \"cppgo gen\".")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo check -header include/mathx/vector.hpp -package mathx \\")
	fmt.Fprintln(w, "              -library libmathx -out ./mathx || echo out of date")
}

// printVersionUsage documents "cppgo version".
func (c *cli) printVersionUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Prints the generator version, followed by a newline, on stdout.")
	fmt.Fprintln(w, "The version is set at build time with -ldflags \"-X main.version=...\"")
	fmt.Fprintln(w, "and is \"dev\" for an unmodified local build.")
}
