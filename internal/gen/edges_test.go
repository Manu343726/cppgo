package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateReportsAnUnwritableOutput covers the I/O edge of the generator:
// an output path that is a file, not a directory, fails before anything is
// written and names the directory it could not create.
func TestGenerateReportsAnUnwritableOutput(t *testing.T) {
	header := writeHeader(t, "io.hpp", "namespace io { int f() noexcept; }\n")
	dir := t.TempDir()
	blocked := filepath.Join(dir, "not-a-directory")
	require.NoError(t, os.WriteFile(blocked, []byte("x"), 0o644))
	err := Generate(Config{Headers: []string{header}, Package: "io", Library: "libio", Out: blocked})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-directory")
}

// TestCompileFlagsDropsCompileOnlyArguments covers the flag rewrite: the language
// selection is replaced, a compile-only switch goes with its value, and an
// optimized build keeps its level.
func TestCompileFlagsDropsCompileOnlyArguments(t *testing.T) {
	m := &packageModel{
		flags: []string{"-std=c++17", "-I/inc", "-c", "-o", "out.o", "-O2", "-MD", "-x", "c++", "-DNAME=1"},
	}
	flags := m.compileFlags("/shim")
	joined := strings.Join(flags, " ")
	assert.Contains(t, joined, "-I/inc")
	assert.Contains(t, joined, "-DNAME=1")
	assert.Contains(t, joined, "-O2")
	assert.NotContains(t, joined, "-out.o")
	assert.Contains(t, joined, "-std=c++17")

	// A flag list with a joined -x and a joined -o is rewritten the same way.
	m.flags = []string{"-xc++", "-oout.o", "-std=c++11"}
	flags = m.compileFlags("/shim")
	joined = strings.Join(flags, " ")
	assert.NotContains(t, joined, "-oout.o")
	assert.Contains(t, joined, "-std=c++11")
	assert.True(t, hasOptimisation([]string{"-O3"}))
	assert.False(t, hasOptimisation([]string{"-g"}))
	assert.True(t, hasLanguageStandard([]string{"-std=c++20"}))
	assert.False(t, hasLanguageStandard([]string{"-g"}))
}

// TestEmitEnumWithoutConstants covers the enum a header declares with no
// enumerators: the type is emitted and the const block is not.
func TestEmitEnumWithoutConstants(t *testing.T) {
	m := &packageModel{}
	var b strings.Builder
	m.emitEnum(&b, &enumModel{GoName: "Empty", CxxName: "ns::Empty", Go: "int32"})
	assert.Contains(t, b.String(), "type Empty int32")
	assert.NotContains(t, b.String(), "const (")
}

// TestStaticFieldSelectionEdgeCases covers the two manifest edges for a static
// member: a private one is not part of the API, and an entry that names another
// member drops it.
func TestStaticFieldSelectionEdgeCases(t *testing.T) {
	header := writeHeader(t, "static.hpp", `
namespace statics {
class Counter {
public:
    static int visible;
    /// A field with no Go value type: the getter is omitted and reported.
    static long double wide;
private:
    static int hidden;
};
}
`)
	result, err := RenderResult(Config{Headers: []string{header}, Package: "statics", Library: "libstatics", Out: "."})
	require.NoError(t, err)
	joined := ""
	for _, warning := range result.Warnings {
		joined += warning + "\n"
	}
	assert.Contains(t, joined, "statics::Counter::wide is not bound")
	assert.NotContains(t, joined, "statics::Counter::hidden")
	assert.NotEmpty(t, result.Report)

	// An entry that names something else keeps no static member.
	result, err = RenderResult(Config{
		Headers: []string{header}, Package: "statics", Library: "libstatics", Out: ".",
		Bind:    []string{"statics::Counter"},
		Exclude: []string{"statics::Counter::visible"},
	})
	require.NoError(t, err)
	for _, line := range result.Report {
		assert.NotContains(t, line, "statics::Counter::visible")
	}
}

// TestBuildTemplatesNarrowedByTheManifest covers the template guards: a header
// with a template, a free function template with no parameters, and a manifest
// that does not name them.
func TestBuildTemplatesNarrowedByTheManifest(t *testing.T) {
	header := writeHeader(t, "tmpl.hpp", `
namespace tmpl {
template <typename T> struct Box { T get() const; void set(T value); };
template struct Box<int>;
template <typename T> T identity(T value);
struct Plain { int value; };
}
`)
	// A manifest that names a record only leaves the templates out.
	result, err := RenderResult(Config{
		Headers: []string{header}, Package: "tmpl", Library: "libtmpl", Out: ".",
		Bind: []string{"tmpl::Plain"},
	})
	require.NoError(t, err)
	var generated string
	for _, out := range result.Outputs {
		if out.Name == GenFileName {
			generated = string(out.Data)
		}
	}
	assert.NotContains(t, generated, "type Box[")
	assert.Contains(t, generated, "type Plain struct {")

	// A manifest that names the template emits both the pattern and its
	// specialization.
	result, err = RenderResult(Config{
		Headers: []string{header}, Package: "tmpl", Library: "libtmpl", Out: ".",
		Bind:        []string{"tmpl::Box"},
		Instantiate: []string{"tmpl::Box<int>"},
	})
	require.NoError(t, err)
	for _, out := range result.Outputs {
		if out.Name == GenFileName {
			generated = string(out.Data)
		}
	}
	assert.Contains(t, generated, "type Box[")
}

// TestAPrefixGroupWithTwoFullDeclarationsSuffixesEveryName covers the naming
// branch where a group holds two declarations and their default-argument
// prefixes: no single one can keep the bare name, so all of them take the suffix.
func TestAPrefixGroupWithTwoFullDeclarationsSuffixesEveryName(t *testing.T) {
	b := &builder{}
	param := func(name string) []*goParam {
		return []*goParam{{Name: name, Type: &goType{Kind: goInteger, Go: "int32", C: "int32_t"}}}
	}
	full := &binding{GoName: "F", Params: param("a")}
	other := &binding{GoName: "F", Params: param("a")}
	prefix := &binding{GoName: "F", Params: param("a"), defaultPrefix: true}
	b.bindings = []*binding{full, other, prefix}
	b.applyOverloadSuffixes()
	assert.Equal(t, "FInt32", full.GoName)
	assert.Equal(t, "FInt32", other.GoName)
	assert.Equal(t, "FInt32", prefix.GoName)
}
