package gen

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ShimLibraryName is the shared object cppgo compiles the generated shim into.
// It is written beside the generated Go package and linked against the user's
// library, which it names in its DT_NEEDED entry. The generated Open loads the
// user's library first and the shim second. A shim symbol is therefore always
// resolvable, and a missing symbol is a build error the message names.
func ShimLibraryName(pkg string) string { return "libcppgo_" + pkg + ".so" }

// findCompiler resolves the C++ compiler the shim is built with: -cxx when it
// is given, then g++, then clang++. A -cxx that is not executable is an error
// naming it, never a silent fallback. The shim and the library have to share a
// compiler family and a standard library, and a fallback could mix the two.
func findCompiler(configured string) (string, error) {
	if configured != "" {
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("cppgo: -cxx %q: %w", configured, err)
		}
		return path, nil
	}
	for _, candidate := range []string{"g++", "clang++"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("cppgo: no C++ compiler found: the shim is C++ and cppgo compiles it itself; install g++ or clang++, or name one with -cxx")
}

// shimBuild is one compile-and-link of the generated translation unit.
type shimBuild struct {
	// Compiler is the resolved compiler path.
	Compiler string
	// Path is the shared object written.
	Path string
	// Dir is the directory the translation unit was compiled in, which is where
	// the isolated per-thunk probes of the failure path are written.
	Dir string
	// Command is the exact argv, for the report and for an error message.
	Command []string
}

// buildShim compiles zz_cppgo_shim.cpp into libcppgo_<pkg>.so, linked against
// the user's library. It is the v4 gate. The shim is C++ cppgo owns, and a shim
// that fails to compile is a generation error carrying the compiler's
// diagnostic and the binding whose thunk it is in, never a symbol that fails at
// the first call.
func (m *packageModel) buildShim(dir string) (*shimBuild, error) {
	compiler, err := findCompiler(m.cfg.CXX)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, ShimLibraryName(m.name))
	args := append([]string{compiler}, m.compileFlags(filepath.Join(dir, ShimDirName))...)
	args = append(args, "-fPIC", "-shared")
	if !hasOptimisation(args) {
		args = append(args, "-O2")
	}
	args = append(args, "-o", out, ShimPath(dir, ShimSourceFileName))
	// -x c++ above applies to every input after it; a library is a linker
	// input, not a translation unit.
	args = append(args, "-x", "none")
	if dir, base, ok := linkableLibrary(m.cfg.Library); ok {
		// The library is named, never pathed: the linker records its soname (or
		// its file name) as the DT_NEEDED entry, so a build directory never
		// reaches the shim and $ORIGIN finds the library beside it.
		args = append(args, "-L"+dir, "-l:"+base)
	}
	args = append(args, "-Wl,-rpath,$ORIGIN")

	build := &shimBuild{Compiler: compiler, Path: out, Dir: dir, Command: args}
	output, err := runCommand(args)
	if err == nil {
		return build, nil
	}
	return nil, m.compileError(build, output)
}

// compileFlags is the project's flag list made fit for compiling the shim: the
// language selection is replaced with C++, the flags that belong to a
// compile-only invocation are dropped, and the directories the shim's own
// includes resolve against are on the search path.
func (m *packageModel) compileFlags(dir string) []string {
	out := make([]string, 0, len(m.flags)+2*len(m.headers)+8)
	for i := 0; i < len(m.flags); i++ {
		arg := m.flags[i]
		switch {
		case arg == "-x":
			i++
		case strings.HasPrefix(arg, "-x"):
		case arg == "-c", arg == "-S", arg == "-E", arg == "-fsyntax-only", arg == "-MD", arg == "-MMD", arg == "-M":
		case arg == "-o", arg == "-MF", arg == "-MT", arg == "-MQ":
			i++
		case strings.HasPrefix(arg, "-o") && len(arg) > 2:
		default:
			out = append(out, arg)
		}
	}
	out = append(out, "-x", "c++", "-I", dir)
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		out = append(out, "-I", cwd)
	}
	for _, header := range m.headers {
		if abs, err := filepath.Abs(header); err == nil {
			out = append(out, "-I", filepath.Dir(abs))
		}
	}
	if !hasLanguageStandard(out) {
		out = append(out, "-std=c++17")
	}
	return out
}

// hasOptimisation reports whether the project already chose an optimization
// level, so the shim is built the way the library is.
func hasOptimisation(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-O") {
			return true
		}
	}
	return false
}

// hasLanguageStandard reports whether the project named the C++ standard.
func hasLanguageStandard(args []string) bool {
	for _, arg := range args {
		if strings.HasPrefix(arg, "-std=") {
			return true
		}
	}
	return false
}

// linkableLibrary reports whether -library names a file the linker can be given
// as an input, and returns its directory and file name.
//
// The directory goes on the linker's search path and the file name after -l:,
// so the DT_NEEDED entry is the object's soname rather than the path it was
// found at. A bare loader name (or a header-only library, which has no object
// at all) is not linkable: the shim is then built with the user's symbols
// undefined, which only resolves if the loader already has the library.
func linkableLibrary(library string) (dir, base string, ok bool) {
	if library == "" {
		return "", "", false
	}
	info, err := os.Stat(library)
	if err != nil || info.IsDir() {
		return "", "", false
	}
	absolute, err := filepath.Abs(library)
	if err != nil {
		absolute = library
	}
	return filepath.Dir(absolute), filepath.Base(absolute), true
}

// runCommand runs the shim's compile-and-link.
func runCommand(argv []string) ([]byte, error) {
	return exec.Command(argv[0], argv[1:]...).CombinedOutput()
}

// diagnosticLine is one compiler diagnostic as g++ and clang++ print it.
var diagnosticLine = regexp.MustCompile(`^(.+?):(\d+):(?:\d+:)?\s*(?:fatal error|error):\s*(.+)$`)

// compileError turns the compiler's output into the contract's generation
// error: the first diagnostic, the binding whose thunk it is in, and the
// command that failed.
func (m *packageModel) compileError(build *shimBuild, output []byte) error {
	command := strings.Join(build.Command, " ")
	match := firstDiagnostic(output)
	if match == nil {
		return fmt.Errorf("cppgo: the generated shim does not compile: %s (compiler: %s)",
			firstLine(strings.TrimSpace(string(output))), command)
	}
	file := filepath.Base(match.file)
	symbol := ShimSourceFileName
	switch {
	case file == ShimSourceFileName:
		symbol = shimSymbol(m.spans, match.line)
	case m.culprit(build) != "":
		// The diagnostic is in a header: an inline body the shim instantiated.
		// Compiling one thunk at a time names the declaration that did it.
		symbol = m.culprit(build)
	}
	return declError(file, match.line, symbol, pending(
		"the generated shim does not compile", match.message+" (compiler: "+command+")"))
}

// culprit compiles one thunk at a time and returns the first binding the
// compiler rejects. It runs on the failure path only, which is what makes
// naming the declaration affordable.
func (m *packageModel) culprit(build *shimBuild) string {
	for _, binding := range m.bindings {
		if !binding.Carrier.shimCarried() {
			continue
		}
		_, single, _, err := m.renderShim(binding)
		if err != nil {
			continue
		}
		path := ShimPath(build.Dir, "cppgo_probe.cpp")
		if err := os.WriteFile(path, single, 0o644); err != nil {
			continue
		}
		args := append([]string{build.Compiler}, m.compileFlags(filepath.Join(build.Dir, ShimDirName))...)
		args = append(args, "-fsyntax-only", path)
		if output, err := runCommand(args); err != nil && firstDiagnostic(output) != nil {
			return binding.sym.QualifiedName
		}
	}
	return ""
}

// diagnostic is one compiler error, split into the parts the error message
// needs.
type diagnostic struct {
	file    string
	line    int
	message string
}

// firstDiagnostic parses the first error line of a compiler's output. A link
// error has no location and yields nil, which the caller reports verbatim.
func firstDiagnostic(output []byte) *diagnostic {
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		match := diagnosticLine.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		return &diagnostic{file: match[1], line: number, message: match[3]}
	}
	return nil
}

// command is the compile command the last build ran, for the error message.
func (m *packageModel) command() []string {
	if m.build == nil {
		return nil
	}
	return m.build.Command
}

// firstLine is the most informative single line of an unparseable output.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "no output"
}
