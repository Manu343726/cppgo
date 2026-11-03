package cxx

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FlagSource says which rule produced a set of compilation flags. The
// generator prints it so a surprising parse can be traced back to its cause.
type FlagSource uint8

const (
	// SourceArgs means the caller supplied the flags; nothing was discovered.
	SourceArgs FlagSource = iota
	// SourceCompileDatabase means a compile_commands.json won: either the one
	// passed with -compile-commands, the one named by .clangd's
	// CompileFlags.CompilationDatabase, or the nearest one found walking up
	// from the file.
	SourceCompileDatabase
	// SourceClangd means there was no database but a .clangd adjusted the
	// default flags.
	SourceClangd
	// SourceFallback means nothing was found and the flags are an assumption.
	SourceFallback
)

// String implements fmt.Stringer.
func (s FlagSource) String() string {
	switch s {
	case SourceArgs:
		return "arguments"
	case SourceCompileDatabase:
		return "compile_commands.json"
	case SourceClangd:
		return ".clangd"
	case SourceFallback:
		return "fallback"
	default:
		return "unknown"
	}
}

// Flags is the outcome of flag discovery.
type Flags struct {
	// Args is the complete argument list to hand to libclang, excluding the
	// file being parsed. It already includes the language selection (-x).
	Args []string
	// Source records which rule won.
	Source FlagSource
	// Detail is a one-line human-readable explanation, e.g.
	// "compile_commands.json /src/build/compile_commands.json (nearest ancestor)".
	// The generator prints it verbatim next to the assumption warning.
	Detail string
	// CompileDatabase is the compile_commands.json that was used, empty if none.
	CompileDatabase string
	// ClangdFiles lists the .clangd files that were merged, outermost first.
	ClangdFiles []string
	// Compiler is the compiler named by .clangd's CompileFlags.Compiler, or the
	// one from the database entry, when it influenced the language mode.
	Compiler string
}

// Assumed reports whether these flags are a guess rather than something the
// project declared. The generator must say so on stderr when it is true.
func (f *Flags) Assumed() bool { return f.Source == SourceFallback }

// DiscoverOptions controls flag discovery.
type DiscoverOptions struct {
	// File is the C++ file or header to be parsed. Required.
	File string
	// CompileCommands is the -compile-commands value: either a
	// compile_commands.json or a directory containing one. When set it takes
	// precedence over everything found on disk.
	CompileCommands string
	// Args, when non-empty, replaces discovery entirely.
	Args []string
}

// Discover finds the compilation flags for a C++ file, following clangd's
// resolution order instead of inventing one:
//
//  1. An explicit -compile-commands path, else the CompileFlags.
//     CompilationDatabase named by a .clangd, else the nearest
//     compile_commands.json found by walking up from the file's directory.
//     The database entry for the file is used; when the file is a header with
//     no entry of its own, the entry of a source that includes it is used.
//  2. .clangd files in the file's directory and in its ancestors, merged
//     outermost first as clangd does through CompileFlags.Compiler,
//     CompileFlags.Remove and CompileFlags.Add (in that order).
//  3. Otherwise -std=c++17 -I<dir of the file>, reported as an assumption.
//
// A few arguments that would confuse a parse-only run are dropped on the way
// through: the input file itself, -c, and -o with its value. The language is
// always made explicit (-x c++-header for headers), because libclang would
// otherwise treat a .h header as C and produce unmangled names.
func Discover(opts DiscoverOptions) (*Flags, error) {
	if opts.File == "" {
		return nil, errors.New("cxx: no file to discover flags for")
	}
	file := opts.File
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	file = filepath.Clean(file)
	if _, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("cxx: discovering flags: %w", err)
	}

	if len(opts.Args) > 0 {
		args := sanitizeArgs(opts.Args, file)
		return &Flags{
			Args:   withLanguage(args, file, ""),
			Source: SourceArgs,
			Detail: "flags supplied by the caller",
		}, nil
	}

	clangd, err := loadClangd(filepath.Dir(file))
	if err != nil {
		return nil, err
	}
	flags := &Flags{ClangdFiles: clangd.files, Compiler: clangd.compiler}

	database, databaseDetail, err := clangd.findDatabase(opts.CompileCommands, filepath.Dir(file))
	if err != nil {
		return nil, err
	}

	var base []string
	// databaseNote explains a database that could not supply flags, so the
	// fallback message can say why it fell back.
	var databaseNote string
	if database != "" {
		entry, err := databaseEntry(database, file)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			argv, err := entry.argv()
			if err != nil {
				return nil, err
			}
			base = sanitizeArgs(argv, file)
			flags.CompileDatabase = database
			flags.Source = SourceCompileDatabase
			flags.Detail = databaseDetail
			if len(base) > 0 && !strings.HasPrefix(base[0], "-") {
				// argv[0] is the compiler of the entry. Report it, then drop it:
				// we always drive libclang, and a foreign driver name would be a
				// fiction (only its language mode would survive anyway).
				if flags.Compiler == "" {
					flags.Compiler = base[0]
				}
				base = base[1:]
			}
		} else {
			flags.CompileDatabase = database
			databaseNote = fmt.Sprintf("%s has no entry for %s or for a source that includes it", database, filepath.Base(file))
		}
	}

	if len(base) == 0 {
		base = []string{"-std=c++17", "-I" + filepath.Dir(file)}
		fallback := fmt.Sprintf("assumed: -std=c++17 -I%s", filepath.Dir(file))
		if databaseNote != "" {
			fallback = databaseNote + "; " + fallback
		}
		switch {
		case clangd.touched():
			flags.Source = SourceClangd
			flags.Detail = "no flags from a compilation database: base from " +
				strings.Join(clangd.files, ", ") + " (" + fallback + ")"
		default:
			flags.Source = SourceFallback
			flags.Detail = fallback
		}
	}

	args := clangd.apply(base)
	flags.Args = withLanguage(args, file, clangd.compiler)
	return flags, nil
}

// flagsTakingValue are the flags whose *next* argument is a value rather than
// an input file. "-include mathx.hpp" is common in real databases and the
// header is not the source being compiled, so it must survive.
var flagsTakingValue = map[string]bool{
	"-include": true, "-imacros": true, "-isystem": true, "-iquote": true,
	"-idirafter": true, "-I": true, "-D": true, "-U": true, "-x": true,
	"-MF": true, "-MT": true, "-MQ": true, "-o": true,
}

// sanitizeArgs drops the arguments that make sense for a compiler invocation
// but not for a parse: the input file, -c, and "-o value". Dependency and
// provenance flags (-MD, -MF, ...) are left alone; libclang ignores them.
func sanitizeArgs(args []string, file string) []string {
	out := make([]string, 0, len(args))
	value := false // the previous argument was a flag that takes a value
	for i := 0; i < len(args); i++ {
		a := args[i]
		isValue := value
		value = flagsTakingValue[a]
		switch {
		case a == "-c":
			continue
		case a == "-o":
			i++
			continue
		case strings.HasPrefix(a, "-o") && len(a) > 2:
			continue
		case isValue:
			// A flag value, even when it looks like a file name.
		case a == file || filepath.Clean(a) == file:
			continue
		case !strings.HasPrefix(a, "-") && isSourceOrHeader(a):
			// The entry's own source file (or another one it names).
			continue
		}
		out = append(out, a)
	}
	return out
}

// withLanguage appends the language selection when the arguments do not carry
// one. libclang guesses from the extension, and it guesses "C" for a .h file,
// which would silently produce unmangled names.
func withLanguage(args []string, file, compiler string) []string {
	for _, a := range args {
		if a == "-x" || strings.HasPrefix(a, "-x") {
			return args
		}
	}
	out := append([]string(nil), args...)
	if isCxx(compiler) || isHeaderFile(file) {
		if isHeaderFile(file) {
			return append(out, "-x", "c++-header")
		}
		return append(out, "-x", "c++")
	}
	return out
}

// isCxx reports whether a compiler name selects C++ mode.
func isCxx(compiler string) bool {
	if compiler == "" {
		return false
	}
	base := filepath.Base(compiler)
	return strings.HasSuffix(base, "++") || base == "cxx"
}

// isHeaderFile reports whether the extension is one of the C++ header ones.
func isHeaderFile(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".h", ".hh", ".hxx", ".hpp", ".h++", ".inl", ".ipp", ".tcc", ".tpp":
		return true
	default:
		return false
	}
}

// isSourceFile reports whether the extension is one of the C++ source ones.
func isSourceFile(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".cc", ".cpp", ".cxx", ".c++", ".cp":
		return true
	default:
		return false
	}
}

func isSourceOrHeader(file string) bool { return isSourceFile(file) || isHeaderFile(file) }

// ---------------------------------------------------------------------------
// compile_commands.json
// ---------------------------------------------------------------------------

// compileCommand is one entry of a compile_commands.json database.
type compileCommand struct {
	Directory string   `json:"directory"`
	File      string   `json:"file"`
	Command   string   `json:"command"`
	Arguments []string `json:"arguments"`
	Output    string   `json:"output"`
}

// path is the absolute path of the entry's file.
func (c compileCommand) path() string {
	file := c.File
	if !filepath.IsAbs(file) && c.Directory != "" {
		file = filepath.Join(c.Directory, file)
	}
	return filepath.Clean(file)
}

// argv returns the command line as a slice, accepting both the "command" and
// the "arguments" spellings that the standard allows.
func (c compileCommand) argv() ([]string, error) {
	if len(c.Arguments) > 0 {
		return append([]string(nil), c.Arguments...), nil
	}
	if strings.TrimSpace(c.Command) == "" {
		return nil, fmt.Errorf("cxx: compile_commands.json entry for %s has neither command nor arguments", c.File)
	}
	return splitCommand(c.Command)
}

// readDatabase parses a compile_commands.json.
func readDatabase(path string) ([]compileCommand, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cxx: reading compilation database: %w", err)
	}
	var entries []compileCommand
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("cxx: parsing %s: %w", path, err)
	}
	return entries, nil
}

// databaseEntry finds the entry to take the flags from: the file's own entry,
// else the entry of a source that includes the file (the header case), else
// the single entry of a one-entry database.
func databaseEntry(database, file string) (*compileCommand, error) {
	entries, err := readDatabase(database)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].path() == file {
			return &entries[i], nil
		}
	}
	if len(entries) == 1 {
		return &entries[0], nil
	}
	// A header has no entry of its own in a normal database: borrow the flags
	// of a translation unit that includes it, the way clangd does.
	base := filepath.Base(file)
	for i := range entries {
		candidate := entries[i].path()
		if !isSourceFile(candidate) {
			continue
		}
		if includes(candidate, base) {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// resolveDatabasePath turns a -compile-commands or CompileFlags.
// CompilationDatabase value into a compile_commands.json path. Both may name
// the file itself or the directory that holds it.
func resolveDatabasePath(value, relTo string) (string, error) {
	path := value
	if !filepath.IsAbs(path) && relTo != "" {
		path = filepath.Join(relTo, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cxx: compile_commands.json: %w", err)
	}
	if info.IsDir() {
		path = filepath.Join(path, "compile_commands.json")
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("cxx: compilation database: %w", err)
		}
	}
	return path, nil
}

// nearestDatabase walks up from dir looking for a compile_commands.json.
func nearestDatabase(dir string) string {
	for {
		candidate := filepath.Join(dir, "compile_commands.json")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// includes reports whether a source file mentions the header by name.
func includes(source, headerBase string) bool {
	info, err := os.Stat(source)
	if err != nil || info.Size() > 4<<20 {
		return false
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#include") {
			continue
		}
		if strings.Contains(line, `"`+headerBase+`"`) || strings.Contains(line, "<"+headerBase+">") {
			return true
		}
	}
	return false
}

// splitCommand splits a shell command line into words, honouring single and
// double quotes and backslash escapes. A compile_commands.json "command" is a
// shell string, so this is the only correct way to read it.
func splitCommand(command string) ([]string, error) {
	var (
		words   []string
		current strings.Builder
		inWord  bool
		quote   rune
	)
	flush := func() {
		if inWord {
			words = append(words, current.String())
			current.Reset()
			inWord = false
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			if r == '\\' && quote == '"' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\') {
				i++
				current.WriteRune(runes[i])
				continue
			}
			current.WriteRune(r)
		case r == '\\':
			if i+1 < len(runes) {
				i++
				current.WriteRune(runes[i])
				inWord = true
			}
		case r == '\'' || r == '"':
			quote = r
			inWord = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			current.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("cxx: unterminated %q in command line %q", string(quote), command)
	}
	flush()
	return words, nil
}

// ---------------------------------------------------------------------------
// .clangd
// ---------------------------------------------------------------------------

// clangdConfig is the merged result of every .clangd from the file's directory
// up to the filesystem root, applied outermost first as clangd does.
type clangdConfig struct {
	files               []string
	compiler            string
	compilationDatabase string
	add                 []string
	remove              []string
	// databaseRelativeTo is the directory of the .clangd that set
	// CompilationDatabase, which is what a relative value is relative to.
	databaseRelativeTo string
}

// touched reports whether any .clangd contributed anything.
func (c *clangdConfig) touched() bool {
	return len(c.add) > 0 || len(c.remove) > 0 || c.compiler != "" || c.compilationDatabase != ""
}

// apply layers the config over a base argument list: the compiler replaces
// argv[0], Remove drops matching arguments, and Add is appended last so that
// an added -std= wins over the database's.
func (c *clangdConfig) apply(args []string) []string {
	out := make([]string, 0, len(args)+len(c.add))
	for _, a := range args {
		if c.removes(a) {
			continue
		}
		out = append(out, a)
	}
	return append(out, c.add...)
}

// removes reports whether an argument matches one of the Remove entries. An
// entry ending in '*' is a prefix match, as in clangd's "-W*"; any other entry
// matches the whole argument.
func (c *clangdConfig) removes(arg string) bool {
	for _, pattern := range c.remove {
		if strings.HasSuffix(pattern, "*") {
			if strings.HasPrefix(arg, strings.TrimSuffix(pattern, "*")) {
				return true
			}
			continue
		}
		if arg == pattern {
			return true
		}
	}
	return false
}

// clangdFile is the on-disk shape of a .clangd file.
type clangdFile struct {
	CompileFlags struct {
		CompilationDatabase string         `yaml:"CompilationDatabase"`
		Compiler            string         `yaml:"Compiler"`
		Add                 yamlStringList `yaml:"Add"`
		Remove              yamlStringList `yaml:"Remove"`
	} `yaml:"CompileFlags"`
}

// yamlStringList accepts both spellings clangd allows: a single scalar
// ("Add: -std=c++20") and a sequence.
type yamlStringList []string

// UnmarshalYAML implements yaml.Unmarshaler.
func (l *yamlStringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		// A scalar node's value is the string; there is nothing to decode.
		*l = yamlStringList{node.Value}
		return nil
	case yaml.SequenceNode:
		var many []string
		if err := node.Decode(&many); err != nil {
			return err
		}
		*l = yamlStringList(many)
		return nil
	default:
		return fmt.Errorf("cxx: .clangd CompileFlags list must be a string or a list, got YAML node kind %d", node.Kind)
	}
}

// loadClangd reads and merges every .clangd from dir up to the root, outermost
// first, so that nearer files win.
func loadClangd(dir string) (*clangdConfig, error) {
	config := &clangdConfig{}
	var paths []string
	for d := dir; ; {
		candidate := filepath.Join(d, ".clangd")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			paths = append(paths, candidate)
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	// Ancestors are string prefixes of each other, so ascending order is
	// outermost first: the nearer file is applied last and wins.
	sort.Strings(paths)
	for _, path := range paths {
		file, err := readClangd(path)
		if err != nil {
			return nil, err
		}
		config.files = append(config.files, path)
		config.add = append(config.add, file.CompileFlags.Add...)
		config.remove = append(config.remove, file.CompileFlags.Remove...)
		if file.CompileFlags.Compiler != "" {
			config.compiler = file.CompileFlags.Compiler
		}
		if file.CompileFlags.CompilationDatabase != "" {
			config.compilationDatabase = file.CompileFlags.CompilationDatabase
			config.databaseRelativeTo = filepath.Dir(path)
		}
	}
	return config, nil
}

// readClangd parses one .clangd file.
func readClangd(path string) (*clangdFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cxx: reading .clangd: %w", err)
	}
	var file clangdFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("cxx: parsing %s: %w", path, err)
	}
	return &file, nil
}

// findDatabase decides which compilation database to use and describes the
// decision: explicit path, then .clangd, then the nearest one above dir.
func (c *clangdConfig) findDatabase(explicit, dir string) (string, string, error) {
	if explicit != "" {
		path, err := resolveDatabasePath(explicit, dir)
		if err != nil {
			return "", "", err
		}
		return path, "compile_commands.json " + path + " (from -compile-commands)", nil
	}
	if c.compilationDatabase != "" {
		path, err := resolveDatabasePath(c.compilationDatabase, c.databaseRelativeTo)
		if err != nil {
			return "", "", err
		}
		return path, "compile_commands.json " + path + " (from CompileFlags.CompilationDatabase in " + c.databaseRelativeTo + "/.clangd)", nil
	}
	if path := nearestDatabase(dir); path != "" {
		detail := "compile_commands.json " + path
		if filepath.Dir(path) != dir {
			detail += " (nearest ancestor)"
		}
		return path, detail, nil
	}
	return "", "", nil
}
