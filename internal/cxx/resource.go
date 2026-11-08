package cxx

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ResourceDirEnv names the environment variable that overrides the clang
// resource directory. It is the escape hatch for an installation the search
// below cannot guess.
const ResourceDirEnv = "CPPGO_CLANG_RESOURCE_DIR"

// resourcePatterns is the ordered list of places a clang resource directory
// can sit, given the directory holding libclang. It is a variable so a test can
// drive the search to a miss.
var resourcePatterns = func(dir string) []string {
	return []string{
		// /usr/lib/clang/22 next to /usr/lib/libclang.so
		filepath.Join(dir, "clang", "*"),
		// /usr/lib/llvm-22/lib/clang/22 next to /usr/lib/llvm-22/lib/libclang.so
		filepath.Join(dir, "..", "lib", "clang", "*"),
		"/usr/lib/clang/*",
		"/usr/lib/llvm-*/lib/clang/*",
		"/usr/local/Cellar/llvm/*/lib/clang/*",
		"/usr/local/opt/llvm/lib/clang/*",
	}
}

// resourceDir locates clang's resource directory: the one that holds the
// builtin headers (stddef.h, stdint.h) and whose presence the compiler expects.
//
// libclang loaded as a shared object does not know the installation it came
// from, so without this a translation unit that includes any builtin header
// fails with "'stddef.h' file not found". The headers' own includes still
// resolve through the system paths, which is why the failure is easy to miss.
func resourceDir(libPath string) string {
	if env := os.Getenv(ResourceDirEnv); env != "" {
		return env
	}
	var candidates []string
	for _, pattern := range resourcePatterns(filepath.Dir(libPath)) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		candidates = append(candidates, matches...)
	}
	// Newest version first, which is what the library search does too.
	sort.Sort(sort.Reverse(sort.StringSlice(candidates)))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if _, err := os.Stat(filepath.Join(candidate, "include", "stddef.h")); err == nil {
			return candidate
		}
	}
	return ""
}

// ResourceDir is the clang resource directory this handle's installation uses,
// or "" when it cannot be found. A caller that has to resolve builtin headers
// (the generated shim's self-check, and any translation unit that includes
// them) prepends "-resource-dir <dir>" to its parse arguments: unlike a
// clang driver invocation, libclang loaded as a shared object does not add it
// by itself.
func (l *Libclang) ResourceDir() string { return resourceDir(l.path) }

// withResourceDir adds -resource-dir when the arguments do not already name one
// and a resource directory is discoverable. Project parsing uses it so the
// standard headers resolve; a caller of ParseTranslationUnit controls its own
// flag list.
func (l *Libclang) withResourceDir(args []string) []string {
	for _, arg := range args {
		if arg == "-resource-dir" || arg == "--resource-dir" || strings.HasPrefix(arg, "-resource-dir=") {
			return args
		}
	}
	dir := resourceDir(l.path)
	if dir == "" {
		return args
	}
	out := make([]string, 0, len(args)+2)
	out = append(out, "-resource-dir", dir)
	out = append(out, args...)
	return out
}
