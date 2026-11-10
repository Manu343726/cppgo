package main

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
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
		return c.generate(args[1:], modeGen)
	case "check":
		return c.generate(args[1:], modeCheck)
	case "build":
		return c.generate(args[1:], modeBuild)
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
	case "build":
		c.printBuildUsage(c.stdout)
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

// mode is which generation command is running. The three share one flag set;
// they differ in what they do with the result.
type mode int

const (
	modeGen mode = iota
	modeCheck
	modeBuild
)

// verb is the subcommand's name.
func (m mode) verb() string {
	switch m {
	case modeCheck:
		return "check"
	case modeBuild:
		return "build"
	default:
		return "gen"
	}
}

// usage prints the command's own help.
func (c *cli) usage(m mode, w io.Writer) {
	switch m {
	case modeCheck:
		c.printCheckUsage(w)
	case modeBuild:
		c.printBuildUsage(w)
	default:
		c.printGenUsage(w)
	}
}

// generate implements "cppgo gen", "cppgo check" and "cppgo build". The first
// two are the same command with a different default for -check; build adds the
// second phase's -scan, which collects the template instantiations a Go
// program uses before the shim is emitted and compiled.
func (c *cli) generate(args []string, m mode) int {
	verb := m.verb()
	if wantsHelp(args) {
		c.usage(m, c.stdout)
		return exitOK
	}

	fs, v := newFlagSet(m)
	fs.SetOutput(c.stderr)
	fs.Usage = func() { c.usage(m, c.stderr) }
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		c.errorf("cppgo %s: unexpected argument %q", verb, fs.Arg(0))
		fs.Usage()
		return exitUsage
	}
	if len(v.headers) == 0 {
		c.errorf("cppgo %s: -header is required: name the C++ header to bind", verb)
		fs.Usage()
		return exitUsage
	}
	if v.out == "" {
		c.errorf("cppgo %s: -out must not be empty", verb)
		return exitUsage
	}

	cfg := gen.Config{
		Headers:         v.headers,
		Package:         v.packageName,
		Library:         v.library,
		CXX:             v.cxx,
		Out:             v.out,
		Check:           v.check,
		CompileCommands: v.compileCommands,
		Bind:            v.bind,
		Exclude:         v.exclude,
		Instantiate:     v.instantiate,
		Rename:          v.rename,
		Status:          v.status,
		Scan:            v.scan,
		ScanDir:         v.scanDir,
		Coverage:        v.coverage,
		Warnings:        c.stderr,
	}
	if m == modeGen {
		// Phase one knows no concrete type: -scan is phase two's input, and
		// build and check both accept it.
		cfg.Scan = nil
		cfg.ScanDir = ""
	}
	if !v.quiet && !v.check {
		// The per-binding carrier report is the answer to "what did this
		// bind, and how?", so it goes to stdout for "gen" and "build".
		cfg.Report = c.stdout
	}
	if err := gen.Generate(cfg); err != nil {
		c.errorf("cppgo %s: %v", verb, err)
		return exitError
	}
	if v.check && !v.quiet {
		c.printf("%s: generated files are up to date\n", v.out)
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

// flagValues holds the values of the flag set that gen, check and build share.
type flagValues struct {
	headers, bind, exclude, instantiate, rename, status, scan stringList
	packageName, library, cxx, out, compileCommands, scanDir  string
	coverage                                                  string
	check, quiet                                              bool
}

// newFlagSet creates the flag set "cppgo <verb>" parses with. Every flag the
// help documents is registered here, and every flag registered here is
// documented by flagDocs, so the two cannot drift apart silently.
func newFlagSet(m mode) (*flag.FlagSet, *flagValues) {
	fs := flag.NewFlagSet("cppgo "+m.verb(), flag.ContinueOnError)
	v := &flagValues{}
	fs.Var(&v.headers, "header", "C++ header to read; repeatable")
	fs.StringVar(&v.packageName, "package", "", "Go package name")
	fs.StringVar(&v.library, "library", "", "the user's shared object the shim links against")
	fs.StringVar(&v.cxx, "cxx", "", "C++ compiler for the shim (default g++, then clang++)")
	fs.StringVar(&v.out, "out", ".", "output directory")
	fs.StringVar(&v.compileCommands, "compile-commands", "", "compile_commands.json (or its directory) for the header's flags")
	fs.Var(&v.bind, "bind", "add a root; repeatable")
	fs.Var(&v.exclude, "exclude", "remove a root; repeatable")
	fs.Var(&v.instantiate, "instantiate", "concrete template specialization to emit; repeatable")
	fs.Var(&v.rename, "rename", "<qualified signature>=<GoName>; repeatable")
	fs.Var(&v.status, "status", "declaration whose int32 return is a status code; repeatable")
	fs.Var(&v.scan, "scan", "Go package whose template instantiations to collect; repeatable")
	fs.StringVar(&v.scanDir, "scan-dir", ".", "directory the -scan patterns resolve against")
	fs.StringVar(&v.coverage, "coverage", "", "write the per-declaration coverage report to this file")
	fs.BoolVar(&v.check, "check", m == modeCheck, "report whether the generated files are up to date instead of writing them")
	fs.BoolVar(&v.quiet, "q", false, "suppress the per-binding report and the confirmation message")
	return fs, v
}

// flagDoc is one flag's entry in a command's help: the value it takes, the
// prose under its name, and the default when the flag has a meaningful one.
type flagDoc struct {
	meta  string
	lines []string
	def   string
}

// flagOrder is the order the shared flags are documented in.
var flagOrder = []string{
	"header", "package", "library", "out", "compile-commands",
	"bind", "exclude", "instantiate", "rename", "status",
	"scan", "scan-dir", "coverage", "cxx", "check", "q",
}

// flagDocs returns the help for every flag of one command. The flag set is
// shared; only the flags whose role differs per command are worded per command.
func flagDocs(m mode) map[string]flagDoc {
	docs := map[string]flagDoc{
		"header": {
			meta: "path",
			lines: []string{
				"a C++ header to read; repeatable. Every public declaration in the",
				"header is bound unless -bind or -exclude narrows the set",
			},
		},
		"package": {
			meta: "name",
			lines: []string{
				"the Go package name. There is no default: a missing or invalid",
				"name is an error",
			},
		},
		"library": {
			meta: "file",
			lines: []string{
				"your shared object. The shim is linked against it, and when it is",
				"a readable file cppgo checks that every function it binds directly",
				"is listed in its dynamic symbol table. Omit it for a header-only",
				"library, whose shim is the whole binary",
			},
		},
		"out": {
			meta:  "dir",
			lines: []string{"the output directory"},
			def:   `"."`,
		},
		"compile-commands": {
			meta: "path",
			lines: []string{
				"compile_commands.json, or the directory that holds it, to take",
				"the header's flags from instead of discovering them",
			},
			def: "discovered",
		},
		"bind": {
			meta: "qualified",
			lines: []string{
				"add a root: the namespace, class, member or function to start",
				"selecting from, or one overload by its full signature,",
				"e.g. 'cv::Mat::Mat(int, int, int)'; repeatable. A root is one",
				"declaration and everything below it",
			},
			def: "everything in the header",
		},
		"exclude": {
			meta: "qualified",
			lines: []string{
				"remove a root: a qualified name, or one overload by its full",
				"signature, e.g. 'cv::Mat::Mat(int, int, int, void *)'; repeatable.",
				"An entry that matches nothing is an error",
			},
			def: "none",
		},
		"instantiate": {
			meta: "specialization",
			lines: []string{
				"emit a concrete template specialization: a template with its type",
				"argument supplied, e.g. metrics::Accumulator<double>; repeatable.",
				"The header must make the specialization visible, e.g.",
				"'template class ::metrics::Accumulator<double>;'",
			},
			def: "none",
		},
		"rename": {
			meta:  "signature=GoName",
			lines: []string{"resolve a Go name collision; repeatable"},
			def:   "none",
		},
		"status": {
			meta: "qualified",
			lines: []string{
				"a declaration whose 32-bit return is a status code: 0 is success",
				"and any other value a failure, so the Go signature returns only",
				"an error; repeatable",
			},
			def: "none",
		},
		"cxx": {
			meta:  "compiler",
			lines: []string{"the C++ compiler to build the shim with"},
			def:   "g++, then clang++",
		},
		"check": {
			meta: "",
			lines: []string{
				"report whether the generated files are up to date instead of",
				"writing them",
			},
			def: "off",
		},
		"q": {
			meta:  "",
			lines: []string{"suppress the per-binding report and the confirmation message"},
			def:   "off",
		},
		"coverage": {
			meta: "<file>",
			lines: []string{
				"write the machine-readable coverage report: the declarations parsed,",
				"bound, refused (each classified) and skipped, with the ratio per module;",
				"the file is a generated output, so check compares it",
			},
			def: "none",
		},
	}

	switch m {
	case modeGen:
		docs["scan"] = flagDoc{
			meta: "package",
			lines: []string{
				"accepted and ignored: -scan is build's input, and gen writes",
				"no concrete template code; repeatable",
			},
			def: "none",
		}
		docs["scan-dir"] = flagDoc{
			meta:  "dir",
			lines: []string{"accepted and ignored with -scan"},
			def:   `"."`,
		}
	case modeCheck:
		docs["scan"] = flagDoc{
			meta: "package",
			lines: []string{
				"a Go package pattern whose uses of the generated generic API are",
				"collected, so the shim is compared for the same instantiations;",
				"repeatable. The source is read, not run",
			},
			def: "none",
		}
		docs["scan-dir"] = flagDoc{meta: "dir", lines: []string{"the directory the -scan patterns resolve against"}, def: `"."`}
		docs["check"] = flagDoc{
			meta: "",
			lines: []string{
				"report whether the generated files are up to date instead of",
				"writing them; check is this mode already, so only -check=false",
				"writes",
			},
			def: "on",
		}
	case modeBuild:
		docs["scan"] = flagDoc{
			meta: "package",
			lines: []string{
				"a Go package pattern whose uses of the generated generic API are",
				"collected; repeatable. The source is read, not run. Without it,",
				"build writes the generic API and adds no specialization",
			},
			def: "none",
		}
		docs["scan-dir"] = flagDoc{meta: "dir", lines: []string{"the directory the -scan patterns resolve against"}, def: `"."`}
	}

	return docs
}

// printFlags renders the shared flags for one command. It reads flagOrder and
// flagDocs, so a flag added to the set without a doc entry is caught by the
// help test rather than silently missing from the help.
func (c *cli) printFlags(w io.Writer, m mode) {
	docs := flagDocs(m)
	fmt.Fprintln(w, "flags:")
	for _, name := range flagOrder {
		doc := docs[name]
		if doc.meta == "" {
			fmt.Fprintf(w, "  -%s\n", name)
		} else {
			fmt.Fprintf(w, "  -%s %s\n", name, doc.meta)
		}
		for _, line := range doc.lines {
			fmt.Fprintln(w, "        "+line)
		}
		if doc.def != "" {
			fmt.Fprintf(w, "        (default: %s)\n", doc.def)
		}
	}
}

// printUsage is the top-level help.
func (c *cli) printUsage(w io.Writer) {
	fmt.Fprintln(w, "cppgo generates a type-safe Go package from C++ headers and calls into")
	fmt.Fprintln(w, "them through one assembly trampoline per binding that does not pass a Go")
	fmt.Fprintln(w, "function, plus the free trampoline. A shim cppgo itself generates and")
	fmt.Fprintln(w, "compiles carries everything the direct ABI cannot express. The user's")
	fmt.Fprintln(w, "build is never touched and no cgo is involved.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  cppgo gen   -header <path> -package <name> [-library <file>] [-out <dir>]")
	fmt.Fprintln(w, "  cppgo build -header <path> -package <name> -scan <go-package> [-out <dir>]")
	fmt.Fprintln(w, "  cppgo check -header <path> -package <name> [-library <file>] [-out <dir>]")
	fmt.Fprintln(w, "  cppgo version")
	fmt.Fprintln(w, "  cppgo help [command]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "commands:")
	fmt.Fprintf(w, "  %-8s %s\n", "gen", "write the Go package, the trampolines and the C++ shim, and build the shim")
	fmt.Fprintf(w, "  %-8s %s\n", "build", "collect a program's template instantiations and emit their shim thunks")
	fmt.Fprintf(w, "  %-8s %s\n", "check", "verify that the generated files are up to date, without writing")
	fmt.Fprintf(w, "  %-8s %s\n", "version", "print the version of the generator")
	fmt.Fprintf(w, "  %-8s %s\n", "help", "print this message, or the help of one command")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "run \"cppgo help <command>\" for command-specific help.")
	fmt.Fprintln(w, "-h, -help and --help print the same help after a subcommand and exit 0.")
}

// printGenUsage documents "cppgo gen".
func (c *cli) printGenUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo gen -header <path> [-header <path>]... -package <name>")
	fmt.Fprintln(w, "                  [-library <file>] [-out <dir>] [-compile-commands <path>]")
	fmt.Fprintln(w, "                  [-bind <qualified>]... [-exclude <qualified>]...")
	fmt.Fprintln(w, "                  [-instantiate '<specialization>']...")
	fmt.Fprintln(w, "                  [-rename '<signature>=<GoName>']... [-status <qualified>]...")
	fmt.Fprintln(w, "                  [-cxx <compiler>]")
	fmt.Fprintln(w, "                  [-check] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "gen reads the C++ headers through libclang and writes:")
	fmt.Fprintln(w)
	const fileColumn = 28
	shimHeader := filepath.Join(gen.ShimDirName, gen.ShimHeaderFileName)
	shimSource := filepath.Join(gen.ShimDirName, gen.ShimSourceFileName)
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, gen.GenFileName, "the Go API, the generated Open and the trampoline")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, "", "declarations")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, gen.AsmFileName, "one NOSPLIT trampoline per binding that passes no Go")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, "", "function, plus cppgoFree")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, shimHeader, "the extern \"C\" thunk declarations")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, shimSource, "the thunks")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, gen.ShimLibraryName("<package>"), "the shim cppgo compiles and links against")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, "", "the library: Open loads it, and the library brings")
	fmt.Fprintf(w, "  %-*s %s\n", fileColumn, "", "the user's library in")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Templates are a two-phase flow: gen writes the generic API, and")
	fmt.Fprintln(w, "\"cppgo build -scan <go package>\" collects the program's instantiations")
	fmt.Fprintln(w, "and emits one thunk per member into the shim, where each call instantiates it.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The user's build is untouched: no generated file goes into it. Every")
	fmt.Fprintln(w, "binding gets exactly one carrier: the direct trampoline when it is")
	fmt.Fprintln(w, "noexcept, not virtual, register representable and its symbol is in the")
	fmt.Fprintln(w, "library; a thunk for everything else; the reentrant carrier when the")
	fmt.Fprintln(w, "signature passes a Go function to C++. The report names the carrier and")
	fmt.Fprintln(w, "why, and the shim build reports the compiler it used.")
	fmt.Fprintln(w)
	c.printFlags(w, modeGen)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo gen -header include/mathx/vector.hpp -package mathx \\")
	fmt.Fprintln(w, "            -library build/libmathx.so -out .")
}

// printCheckUsage documents "cppgo check".
func (c *cli) printCheckUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo check -header <path> [-header <path>]... -package <name>")
	fmt.Fprintln(w, "                    [-library <file>] [-out <dir>]")
	fmt.Fprintln(w, "                    [-scan <go-package>]... [-scan-dir <dir>] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "check regenerates the sources in memory and compares them with the files on")
	fmt.Fprintln(w, "disk. It writes nothing, builds nothing, exits 0 when everything is up to")
	fmt.Fprintln(w, "date and 1 when a file is missing or stale, which is the shape a CI gate")
	fmt.Fprintln(w, "wants.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "It accepts the same manifest flags as \"cppgo gen\", plus -scan/-scan-dir")
	fmt.Fprintln(w, "when the package has templates: the shim's source depends on the program's")
	fmt.Fprintln(w, "instantiations, so check compares the shim built for the same scan.")
	fmt.Fprintln(w)
	c.printFlags(w, modeCheck)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo check -header include/metrics/metrics.hpp -package metrics \\")
	fmt.Fprintln(w, "              -library build/libmetrics.so \\")
	fmt.Fprintln(w, "              -compile-commands build/compile_commands.json \\")
	fmt.Fprintln(w, "              -out ./gen -status metrics::validate_name \\")
	fmt.Fprintln(w, "              -scan ./cmd/... -scan-dir .")
}

// printBuildUsage documents "cppgo build", the second phase of the template
// flow.
func (c *cli) printBuildUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo build -header <path> [-header <path>]... -package <name>")
	fmt.Fprintln(w, "                    -scan <go-package> [-scan <go-package>]...")
	fmt.Fprintln(w, "                    [-scan-dir <dir>] [-library <file>] [-out <dir>]")
	fmt.Fprintln(w, "                    [-compile-commands <path>] [-instantiate '<specialization>']...")
	fmt.Fprintln(w, "                    [-rename <signature=GoName>]... [-status <qualified>]...")
	fmt.Fprintln(w, "                    [-cxx <compiler>] [-q]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "build is phase two of the two-phase generics flow:")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "  1. cppgo gen writes the generic API for every class and function template")
	fmt.Fprintln(w, "     in the headers. That package compiles without any concrete type;")
	fmt.Fprintln(w, "  2. cppgo build loads the Go program named by -scan with go/packages and")
	fmt.Fprintln(w, "     collects every instantiation of those generic types and functions;")
	fmt.Fprintln(w, "  3. for each collected specialization it writes the shim's")
	fmt.Fprintln(w, "     `template class ...;` line and one thunk per member, and compiles")
	fmt.Fprintln(w, "     the shim beside the generated package.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "The source is read, not run. Without -scan, build writes the generic API")
	fmt.Fprintln(w, "and adds no specialization.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A Go instantiation the shim has no counterpart for fails when it is used,")
	fmt.Fprintln(w, "naming the exact `template ...;` line to add; it never reaches a nil call.")
	fmt.Fprintln(w)
	c.printFlags(w, modeBuild)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "example:")
	fmt.Fprintln(w, "  cppgo gen   -header include/metrics/metrics.hpp -package metrics \\")
	fmt.Fprintln(w, "              -library build/libmetrics.so -out ./gen")
	fmt.Fprintln(w, "  cppgo build -header include/metrics/metrics.hpp -package metrics \\")
	fmt.Fprintln(w, "              -library build/libmetrics.so \\")
	fmt.Fprintln(w, "              -compile-commands build/compile_commands.json \\")
	fmt.Fprintln(w, "              -out ./gen -status metrics::validate_name \\")
	fmt.Fprintln(w, "              -scan ./cmd/... -scan-dir .")
}

// printVersionUsage documents "cppgo version".
func (c *cli) printVersionUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: cppgo version")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Prints the generator version, followed by a newline, on stdout.")
	fmt.Fprintln(w, "The version is set at build time with -ldflags \"-X main.version=...\"")
	fmt.Fprintln(w, "and is \"dev\" for an unmodified local build.")
}
