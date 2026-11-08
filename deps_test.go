package cppgo_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRuntimeHasNoGeneratorDependency enforces the layer split in DESIGN.md:
// the runtime package owns the ABI and the loader, and libclang is a
// generation-time tool. If internal/cxx ever appears in the root package's
// dependency graph, a user's program would need libclang to run, which is the
// one thing the split exists to prevent.
//
// `go list -deps` is the honest measurement: it follows the real import graph,
// including transitive imports, instead of reading the source.
func TestRuntimeHasNoGeneratorDependency(t *testing.T) {
	listed, err := exec.Command("go", "list", "-deps", ".").Output()
	require.NoErrorf(t, err, "go list -deps .: %s", listed)

	for _, imported := range strings.Split(strings.TrimSpace(string(listed)), "\n") {
		forbidden := imported == "github.com/Manu343726/cppgo/internal/cxx" ||
			imported == "github.com/Manu343726/cppgo/cmd/cppgo" ||
			strings.HasPrefix(imported, "github.com/Manu343726/cppgo/internal/gen")
		require.Falsef(t, forbidden, "%s is in the runtime's dependency graph", imported)
	}
}
