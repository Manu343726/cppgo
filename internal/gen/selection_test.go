package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestManifestNarrowsLowering is the property that makes a large header
// bindable: a declaration the manifest does not select is never lowered, so a
// signature this build cannot represent outside the selection cannot even be
// reported. Without -bind the same header binds, and the declaration it cannot
// carry is a warning that names itself.
func TestManifestNarrowsLowering(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "narrow.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <cstdint>

namespace narrow {

int add(int a, int b) noexcept;

// No Go value type, so this declaration cannot be lowered.
long double widen(std::int32_t v) noexcept;

struct Point { std::int32_t x; std::int32_t y; };

} // namespace narrow
`), 0o644))

	// Selecting one declaration lowers only that one, and the record the
	// signature names is still part of the API.
	outs, err := Render(Config{
		Headers: []string{header},
		Package: "narrow",
		Library: "libnarrow",
		Out:     ".",
		Bind:    []string{"narrow::add"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, outs)

	// The whole header is a root: the unrepresentable declaration names itself
	// with the fix, and everything else binds.
	rendered, err := RenderResult(Config{
		Headers: []string{header},
		Package: "narrow",
		Library: "libnarrow",
		Out:     ".",
	})
	require.NoError(t, err)
	joined := joinWarnings(rendered.Warnings)
	assert.Contains(t, joined, "narrow::widen")
	assert.Contains(t, joined, "no Go value type")
}

// TestManifestExcludeNarrowsLowering is the same property for -exclude.
func TestManifestExcludeNarrowsLowering(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "narrow.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <cstdint>

namespace narrow {

int add(int a, int b) noexcept;
long double widen(std::int32_t v) noexcept;

} // namespace narrow
`), 0o644))

	_, err := Render(Config{
		Headers: []string{header},
		Package: "narrow",
		Library: "libnarrow",
		Out:     ".",
		Exclude: []string{"narrow::widen"},
	})
	require.NoError(t, err)
}

// TestManifestEntryThatSelectsNothingIsStillAnError guards the other half: the
// narrowing must not turn a typo into a silently empty package.
func TestManifestEntryThatSelectsNothingIsStillAnError(t *testing.T) {
	dir := t.TempDir()
	header := filepath.Join(dir, "narrow.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
namespace narrow { int add(int a, int b) noexcept; }
`), 0o644))

	_, err := Render(Config{
		Headers: []string{header},
		Package: "narrow",
		Library: "libnarrow",
		Out:     ".",
		Bind:    []string{"narrow::misspelled"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names nothing")
}

// selectionConfig is the declaration-selection fixture's canonical run.
func selectionConfig() Config {
	return fixtureConfig("selection", "selection.hpp")
}

// TestUncarryableDeclarationsDoNotRefuseTheRecord is the fixture the contract
// names: a class with one carryable constructor and one whose parameter is a
// void *, and an overload set with one supported and one unsupported signature.
// The carryable declarations bind, the others are warnings, and the class
// exists.
func TestUncarryableDeclarationsDoNotRefuseTheRecord(t *testing.T) {
	rendered, err := RenderResult(selectionConfig())
	require.NoError(t, err)
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}
	assert.Contains(t, generated, "type Value struct {", "the class exists")
	assert.Contains(t, generated, "func NewValue(value int32) (*Value, error) {", "the carryable constructor binds")
	assert.Contains(t, generated, "func (v *Value) Get()", "the carryable method binds")
	assert.Contains(t, generated, "func Plain(value int32)", "the carryable overload binds")

	joined := joinWarnings(rendered.Warnings)
	assert.Contains(t, joined, "sel::Value::Value(void *)", "the uncarryable constructor is named")
	assert.Contains(t, joined, "void pointer", "the reason is the same one the declaration-level error used")
	assert.Contains(t, joined, "sel::plain(void *)", "the uncarryable overload is named")
	assert.NotContains(t, generated, "Opaque", "nothing is emitted for the uncarryable declarations")
}

// TestExcludeNamesOneOverload proves -exclude removes one declaration of a set,
// not the set: the signature is accepted and the rest of the class still binds.
func TestExcludeNamesOneOverload(t *testing.T) {
	cfg := selectionConfig()
	cfg.Exclude = []string{"sel::Value::Value(void *)", "sel::plain(void *) noexcept"}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	joined := joinWarnings(rendered.Warnings)
	assert.NotContains(t, joined, "void pointer", "an excluded declaration is not lowered, so it is not reported")
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}
	assert.Contains(t, generated, "func NewValue(value int32)")
	assert.Contains(t, generated, "func Plain(value int32)")
}

// TestBindNamesOneOverload proves -bind selects one declaration of a set by
// signature, and does not drag the class in with it.
func TestBindNamesOneOverload(t *testing.T) {
	cfg := selectionConfig()
	cfg.Bind = []string{"sel::plain(int)"}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	generated := ""
	for _, output := range rendered.Outputs {
		if output.Name == GenFileName {
			generated = string(output.Data)
		}
	}
	assert.Contains(t, generated, "func Plain(value int32)")
	assert.NotContains(t, generated, "type Value")
	assert.NotContains(t, joinWarnings(rendered.Warnings), "void pointer", "the overloads the manifest did not name are not lowered")
}
