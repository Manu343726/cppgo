package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// goldenFixtures are the configurations whose full output is compared byte for
// byte. Regenerate them with CPPGO_UPDATE_GOLDEN=1 go test ./internal/gen/v3.
func goldenFixtures() map[string]Config {
	return map[string]Config{
		"mathx":     mathxConfig(),
		"types":     fixtureConfig("types", "types.hpp"),
		"overloads": fixtureConfig("overloads", "overloads.hpp"),
		"std":       fixtureConfig("std", "std.hpp"),
	}
}

// TestGolden compares every generated file with the checked-in golden copy.
func TestGolden(t *testing.T) {
	update := os.Getenv("CPPGO_UPDATE_GOLDEN") != ""
	fixtures := goldenFixtures()
	for name, cfg := range fixtures {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			outs, err := Render(cfg)
			require.NoError(t, err)
			dir := filepath.Join("testdata", "golden", name)
			if update {
				require.NoError(t, os.MkdirAll(dir, 0o755))
			}
			for _, out := range outs {
				path := filepath.Join(dir, out.Name)
				if update {
					require.NoError(t, os.WriteFile(path, out.Data, 0o644))
					continue
				}
				require.Equal(t, readFileString(t, path), string(out.Data), path)
			}
		})
	}
}
