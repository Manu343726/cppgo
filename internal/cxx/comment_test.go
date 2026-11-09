package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocCommentsAreReadFromTheHeader proves the model carries what the header
// wrote, verbatim: the block form, the line form, an enumerator's own comment,
// a record's, a field's and a static member's. Normalising the text for Go is
// the generator's job; the model does not paraphrase.
func TestDocCommentsAreReadFromTheHeader(t *testing.T) {
	skipWithoutLibclang(t)
	dir := t.TempDir()
	header := filepath.Join(dir, "doc.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace doc {

/** A counter.
 *
 * It counts things.
 */
class Counter {
public:
    /// Add one.
    void add(int by = 1);

    /** The current value. */
    int value() const;

    /// The backing field.
    int total;

    /// A shared instance count.
    static int instances;

    /// A kind of counter.
    enum class Kind {
        /// Fast.
        Fast = 1,
        /// Slow.
        Slow = 2,
    };
};

/** Free-standing. */
int free_function();

} // namespace doc
`), 0o644))

	project, err := Open(Options{Header: header, Args: []string{"-std=c++17", "-xc++-header"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })

	counter, ok := project.Record("doc::Counter")
	require.True(t, ok)
	assert.Contains(t, counter.Comment, "A counter.")
	assert.Contains(t, counter.Comment, "It counts things.")

	var add, value Symbol
	for _, sym := range project.Lookups("doc::Counter::add") {
		add = sym
	}
	require.NotEmpty(t, add.QualifiedName)
	assert.Contains(t, add.Comment, "Add one.")
	for _, sym := range project.Lookups("doc::Counter::value") {
		value = sym
	}
	assert.Contains(t, value.Comment, "The current value.")

	var total, instances bool
	for _, field := range counter.Fields {
		if field.Name == "total" {
			total = true
			assert.Contains(t, field.Comment, "The backing field.")
		}
	}
	assert.True(t, total)
	for _, field := range counter.StaticFields {
		if field.Name == "instances" {
			instances = true
			assert.Contains(t, field.Comment, "A shared instance count.")
		}
	}
	assert.True(t, instances)

	kind, ok := project.Enum("doc::Counter::Kind")
	require.True(t, ok)
	assert.Contains(t, kind.Comment, "A kind of counter.")
	require.Len(t, kind.Constants, 2)
	assert.Contains(t, kind.Constants[0].Comment, "Fast.")
	assert.Contains(t, kind.Constants[1].Comment, "Slow.")

	_, ok = project.Lookup("doc::free_function")
	assert.True(t, ok)
}

// TestAnUndocumentedDeclarationHasNoComment is the control: the model must not
// invent a doc comment, because the generator treats "" as "write the contract
// sentence".
func TestAnUndocumentedDeclarationHasNoComment(t *testing.T) {
	skipWithoutLibclang(t)
	dir := t.TempDir()
	header := filepath.Join(dir, "bare.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace bare { int f(); }
`), 0o644))
	project, err := Open(Options{Header: header, Args: []string{"-std=c++17", "-xc++-header"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = project.Close() })
	sym, ok := project.Lookup("bare::f")
	require.True(t, ok)
	assert.Empty(t, sym.Comment)
}
