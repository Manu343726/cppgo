package main

import (
	"flag"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// helpInvocations is every command that renders help text. The defect this
// suite pins down is that all of them described the pre-v4 generator and an
// internal fixture.
var helpInvocations = []struct {
	name string
	args []string
}{
	{"top-level", []string{"help"}},
	{"gen", []string{"help", "gen"}},
	{"check", []string{"help", "check"}},
	{"build", []string{"help", "build"}},
	{"version", []string{"help", "version"}},
	{"gen -h", []string{"gen", "-h"}},
	{"check -h", []string{"check", "-h"}},
	{"build -h", []string{"build", "-h"}},
}

// TestHelpNamesNoFixtureOrStaleNotation asserts that no help screen mentions an
// internal path, the testdata fixtures, or the pre-v4 mathx::Box example.
func TestHelpNamesNoFixtureOrStaleNotation(t *testing.T) {
	forbidden := []string{"internal/", "testdata", "Box<int>", "box.hpp"}
	for _, tc := range helpInvocations {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := exec(tc.args...)
			require.Equal(t, exitOK, code, "cppgo %v", tc.args)
			require.Empty(t, stderr)
			for _, text := range forbidden {
				assert.NotContains(t, stdout, text, "help still names %q", text)
			}
		})
	}
}

// exampleHeader matches a header path as the help prints it, relative to the
// examples directory.
var exampleHeader = regexp.MustCompile(`include/[A-Za-z0-9_/]+\.hpp`)

// TestHelpExamplesAreRealHeaders proves every header the help tells the reader
// to bind exists under examples/, which is what "a real header" means here.
func TestHelpExamplesAreRealHeaders(t *testing.T) {
	for _, topic := range []string{"gen", "check", "build"} {
		topic := topic
		t.Run(topic, func(t *testing.T) {
			_, stdout, _ := exec("help", topic)
			headers := exampleHeader.FindAllString(stdout, -1)
			require.NotEmpty(t, headers, "the help shows a command the reader can run")
			for _, header := range headers {
				matches, err := filepath.Glob(filepath.Join("..", "..", "examples", "*", header))
				require.NoError(t, err)
				assert.NotEmpty(t, matches,
					"cppgo help %s names %s, which is not under examples/", topic, header)
			}
		})
	}
}

// TestHelpFlagsAreTheParsedFlags drives the assertion from the real flag set:
// every flag the renderer prints is one the parser accepts, every flag the
// parser accepts is printed with help, and every help entry names a flag.
func TestHelpFlagsAreTheParsedFlags(t *testing.T) {
	for _, m := range []mode{modeGen, modeCheck, modeBuild} {
		m := m
		t.Run(m.verb(), func(t *testing.T) {
			fs, _ := newFlagSet(m)
			registered := map[string]bool{}
			fs.VisitAll(func(f *flag.Flag) { registered[f.Name] = true })

			code, stdout, stderr := exec("help", m.verb())
			require.Equal(t, exitOK, code)
			require.Empty(t, stderr)

			printed := printedFlags(t, stdout)
			for _, name := range printed {
				assert.True(t, registered[name],
					"help %s prints -%s, which the parser does not accept", m.verb(), name)
			}
			for name := range registered {
				assert.Contains(t, printed, name,
					"help %s omits -%s, which the parser accepts", m.verb(), name)
			}

			// The renderer reads flagOrder and flagDocs: both must cover the
			// flag set, or a flag could be accepted and documented nowhere.
			docs := flagDocs(m)
			for _, name := range flagOrder {
				doc, ok := docs[name]
				require.True(t, ok, "-%s is in flagOrder but has no help", name)
				assert.NotEmpty(t, doc.lines, "-%s has no help text", name)
				assert.True(t, registered[name], "flagOrder names -%s, which is not parsed", name)
			}
			assert.Len(t, docs, len(registered), "flagDocs and the flag set disagree")
		})
	}
}

// printedFlags extracts the names from the "  -name" lines the help renderer
// prints. Synopsis lines start with "usage:" or "  [", so they do not match.
func printedFlags(t *testing.T, help string) []string {
	t.Helper()
	var names []string
	for _, line := range strings.Split(help, "\n") {
		if !strings.HasPrefix(line, "  -") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "  -"))
		require.NotEmpty(t, fields, "a flag line has a name: %q", line)
		names = append(names, fields[0])
	}
	return names
}

// TestHelpDocumentsScanPerCommand pins the v4 semantics: gen ignores -scan,
// check and build read the program it names.
func TestHelpDocumentsScanPerCommand(t *testing.T) {
	_, gen, _ := exec("help", "gen")
	assert.Contains(t, gen, "accepted and ignored", "gen's help says -scan does nothing")

	for _, topic := range []string{"check", "build"} {
		_, stdout, _ := exec("help", topic)
		assert.Contains(t, stdout, "collected", "cppgo help %s describes the scan", topic)
	}

	_, build, _ := exec("help", "build")
	assert.Contains(t, build, "Without -scan", "build's help says what omitting -scan does")
}

// TestHelpInstantiateNamesARealSpecialization: the flag is still accepted by
// gen, check and build, and its example must be a real template specialization
// of the examples tree.
func TestHelpInstantiateNamesARealSpecialization(t *testing.T) {
	for _, topic := range []string{"gen", "check", "build"} {
		_, stdout, _ := exec("help", topic)
		assert.Contains(t, stdout, "-instantiate specialization",
			"cppgo help %s documents -instantiate", topic)
		assert.Contains(t, stdout, "metrics::Accumulator<double>",
			"cppgo help %s uses a real specialization", topic)
	}
}
