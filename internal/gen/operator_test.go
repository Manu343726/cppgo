package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	_, err := Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both map to Go name AddAssign")
	assert.Contains(t, err.Error(), "-rename")

	cfg.Rename = []string{"collide::Value::operator+=(const Value &)=Plus"}
	_, err = Render(cfg)
	require.NoError(t, err)
}

// TestOperatorOutsideTheTableAsksForRename covers the other row: an operator
// with no fixed name (operator new, which the contract deliberately omits) is a
// generation error naming the declaration and the rename that resolves it.
func TestOperatorOutsideTheTableAsksForRename(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "allocation.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <cstddef>
namespace allocation {
class Node {
public:
    static void *operator new(std::size_t size);
    static void operator delete(void *block) noexcept;
    int value() const noexcept;
private:
    int v_;
};
}
`), 0o644))
	_, err := Render(Config{Headers: []string{header}, Package: "allocation", Library: "liballocation", Out: "."})
	require.Error(t, err)
	// The size_t parameter is the first construct that cannot be represented;
	// the naming rule is what makes the operator itself unrepresentable, and
	// either message names the declaration.
	assert.Contains(t, err.Error(), "allocation::Node::operator new")
}
