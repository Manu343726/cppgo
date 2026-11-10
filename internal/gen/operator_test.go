package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestOperatorGoNames pins the contract's naming table against a header that
// declares the operators: the member takes the table's word, the free one is a
// package function with the same word, and a conversion operator is named after
// the Go type it converts to.
func TestOperatorGoNames(t *testing.T) {
	m := renderMap(t, fixtureConfig("operators", "operators.hpp"))
	source := m[GenFileName]

	cases := []struct {
		name string
		want string
	}{
		{"member +=", "func (n *Number) AddAssign(other *Number) (*Number, error) {"},
		{"member =", "func (n *Number) Assign(other *Number) (*Number, error) {"},
		{"member ==", "func (n *Number) Equal(other *Number) (bool, error) {"},
		{"member <", "func (n *Number) Less(other *Number) (bool, error) {"},
		{"member []", "func (n *Number) At(index int32) (float64, error) {"},
		{"member ()", "func (n *Number) Call(row int32, col int32) (float64, error) {"},
		{"conversion to double", "func (n *Number) Float64() (float64, error) {"},
		{"conversion to bool", "func (n *Number) Bool() (bool, error) {"},
		{"conversion to int", "func (n *Number) Int32() (int32, error) {"},
		{"conversion to std::string", "func (n *Number) String() (string, error) {"},
		{"free +", "func Add(a *Vec2, b *Vec2) (Vec2, error) {"},
		{"free -", "func Sub(a *Vec2, b *Vec2) (Vec2, error) {"},
		{"free ==", "func Equal(a *Vec2, b *Vec2) (bool, error) {"},
		{"free !=", "func NotEqual(a *Vec2, b *Vec2) (bool, error) {"},
		{"free *", "func Mul(number *Number, factor float64) (float64, error) {"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, strings.Contains(source, tc.want), "the generated API must contain %q", tc.want)
		})
	}

	// The evidence line shows the declaration a reader wrote, not libclang's
	// "operator basic_string" spelling of a conversion's name.
	assert.Contains(t, source, "// C++: ops::Number::operator std::string() const,")
}

// TestOperatorCollisionAsksForRename covers the row of the contract's table that
// an operator name can collide with a member function's: the error names both
// declarations and the -rename that separates them, and the rename works.
func TestOperatorCollisionAsksForRename(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "collide.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace collide {

class Value {
public:
    explicit Value(double v) noexcept;
    double value() const noexcept;

    /// A named method that wants the operator's Go name.
    double AddAssign(const Value &other) const noexcept;

    /// The operator that maps to Add.
    Value &operator+=(const Value &other);

private:
    double v_;
};

} // namespace collide
`), 0o644))

	cfg := Config{Headers: []string{header}, Package: "collide", Library: "libcollide", Out: "."}
	// The declared method keeps AddAssign and the operator takes the parameter
	// suffix: the automatic rule resolves the clash, and -rename still wins.
	result, err := RenderResult(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, result.Outputs)

	cfg.Rename = []string{"collide::Value::operator+=(const Value &)=Plus"}
	result, err = RenderResult(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, result.Outputs)
}

// TestOperatorWithUncarryableParametersIsAWarning covers the declaration rule
// in an operator overload set: the nothrow allocation overloads take a
// std::nothrow_t the API does not model, so they are omitted and named, and the
// class keeps the member that can be carried.
func TestOperatorWithUncarryableParametersIsAWarning(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "allocation.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <cstddef>
#include <new>
namespace allocation {
class Node {
public:
    static void *operator new(std::size_t size, const std::nothrow_t &) noexcept;
    static void operator delete(void *block, const std::nothrow_t &) noexcept;
    int value() const noexcept;
private:
    int v_;
};
}
`), 0o644))
	rendered, err := RenderResult(Config{Headers: []string{header}, Package: "allocation", Library: "liballocation", Out: "."})
	require.NoError(t, err)
	joined := joinWarnings(rendered.Warnings)
	assert.Contains(t, joined, "allocation::Node::operator new")
	assert.Contains(t, joined, "allocation::Node::operator delete")
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}
	assert.Contains(t, generated, "func (n *Node) Value()", "the class exists and keeps what it can carry")
}

// TestOperatorOutsideTheTableAsksForRename covers the other row: an operator
// with no name in the contract's table is refused with the rename that resolves
// it. It is checked on the naming function directly, because a declaration whose
// parameters cannot be lowered is dropped before it is ever named.
func TestOperatorOutsideTheTableAsksForRename(t *testing.T) {
	b := testBuilder()
	_, err := b.operatorGoName(&binding{sym: cxx.Symbol{
		QualifiedName: "ns::Owner::operator->*",
		IsOperator:    true,
		Operator:      "->*",
		Params:        []cxx.Param{{Kind: cxx.TypeInteger, Spelling: "int", SizeBytes: 4}},
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "naming table")
	assert.Contains(t, err.Error(), "-rename")
}
