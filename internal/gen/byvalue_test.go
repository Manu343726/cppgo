package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestByValueRecordShape pins the API and the thunk shape of a mirrored POD
// passed and returned by value. The Go value is the C layout, so the parameter
// is the struct itself, the thunk takes its address and copies it, and a result
// is written into Go storage whatever its size.
func TestByValueRecordShape(t *testing.T) {
	for _, tc := range []struct {
		header   string
		goFunc   string
		shimCall string
		readme   string
	}{
		{
			header:   "byvalue.hpp",
			goFunc:   "func Take(value Pod) (int32, error)",
			shimCall: "::err::take(*static_cast<const ::err::Pod *>(value))",
			readme:   "a 4-byte POD by value, through the out-pointer its status needs",
		},
		{
			header:   "bigresult.hpp",
			goFunc:   "func Make() Big",
			shimCall: "*static_cast<::err::Big *>(cppgo_result) = ::err::make()",
			readme:   "a 24-byte POD by value: a shim result with no error channel",
		},
	} {
		tc := tc
		t.Run(tc.header, func(t *testing.T) {
			files := renderMap(t, Config{
				Headers: []string{filepath.Join("testdata", "errors", tc.header)},
				Package: "err",
				Library: "liberr",
				Out:     ".",
			})
			assert.Contains(t, files[GenFileName], tc.goFunc, tc.readme)
			assert.Contains(t, files[ShimSourceFileName], tc.shimCall, tc.readme)
			// The record's C++ layout never appears in the generated C header:
			// the thunk casts the pointer where the layout exists.
			assert.NotContains(t, files[ShimHeaderFileName], "::err::")
		})
	}
}

// TestByValueRecordShimCompiles is the gate the shim self-check used to be: a
// real C++ compiler accepts the generated translation unit, for the two shapes
// whose argument and result cross as memory.
func TestByValueRecordShimCompiles(t *testing.T) {
	gxx, err := exec.LookPath("g++")
	if err != nil {
		t.Skip("g++ is not in PATH")
	}
	for _, header := range []string{"byvalue.hpp", "bigresult.hpp"} {
		header := header
		t.Run(header, func(t *testing.T) {
			dir := t.TempDir()
			cfg := Config{
				Headers: []string{filepath.Join("testdata", "errors", header)},
				Package: "err",
				Library: "liberr",
				Out:     dir,
			}
			require.NoError(t, Generate(cfg))
			// The fixture headers are relative to this package's directory,
			// which is how a user names them on the command line.
			output, err := exec.Command(gxx, "-std=c++17", "-fsyntax-only", "-I.",
				ShimPath(dir, ShimSourceFileName)).CombinedOutput()
			require.NoErrorf(t, err, "g++ rejected the shim:\n%s", output)
			_, err = os.Stat(filepath.Join(dir, GenFileName))
			require.NoError(t, err)
		})
	}
}
