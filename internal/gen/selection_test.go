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
// signature this build cannot represent outside the selection cannot fail the
// run. Without -bind the same header is a generation error that names the
// declaration, which is the contract's "what remains a generation error" row.
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

	// The whole header is a root, and the unrepresentable declaration names
	// itself with the fix.
	_, err = Render(Config{
		Headers: []string{header},
		Package: "narrow",
		Library: "libnarrow",
		Out:     ".",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "narrow::widen")
	assert.Contains(t, err.Error(), "no Go value type")
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
