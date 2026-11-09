package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeHeader writes a header into a temporary directory and returns its path.
func writeHeader(t *testing.T, name, text string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(text), 0o644))
	return path
}

// TestBorrowedRecordResultIsAView pins the ownership promise of a method that
// returns a reference or a pointer to its own type: the Go value is a view of
// the receiver's object, one reference more, and the address C++ returned is
// what this value hands to C++, so a base-subobject adjustment survives.
func TestBorrowedRecordResultIsAView(t *testing.T) {
	header := writeHeader(t, "borrow.hpp", `
namespace borrow {
class Chain {
public:
    explicit Chain(int value) noexcept;
    /// A reference to itself.
    Chain &self() noexcept;
    /// A pointer to itself.
    Chain *pointer() noexcept;
    int value() const noexcept;
private:
    int value_;
};
} // namespace borrow
`)
	m := renderMap(t, Config{Headers: []string{header}, Package: "borrow", Library: "libborrow", Out: "."})
	source := m[GenFileName]

	assert.Contains(t, source, "\taddr uintptr\n")
	assert.Contains(t, source, "if c.addr != 0 {\n\t\treturn c.addr\n\t}")
	assert.Contains(t, source, "// A borrowed record result is a view: the receiver's object, one more reference.")
	assert.Contains(t, source, "c.obj.Retain()")
	assert.Contains(t, source, "return &Chain{obj: c.obj, addr: uintptr(cppgoResult)}")
}

// TestAFreeFunctionBorrowingARecordIsDropped is the other half: a raw pointer
// out of a free function is an address nobody owns, and Go cannot hold it. The
// declaration is named and omitted; the class it returned is not refused.
func TestAFreeFunctionBorrowingARecordIsDropped(t *testing.T) {
	header := writeHeader(t, "borrow.hpp", `
namespace borrow {
class Node {
public:
    Node() noexcept;
    int value() const noexcept;
private:
    int value_;
};
/// The address nobody owns.
Node *global_node();
} // namespace borrow
`)
	rendered, err := RenderResult(Config{Headers: []string{header}, Package: "borrow", Library: "libborrow", Out: "."})
	require.NoError(t, err)
	assert.Contains(t, joinWarnings(rendered.Warnings), "pointer to an opaque record")
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}
	assert.NotContains(t, generated, "func GlobalNode", "a pointer nobody owns is never handed to Go")
	assert.Contains(t, generated, "type Node struct {", "the record itself still binds")
}
