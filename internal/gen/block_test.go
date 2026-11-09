package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestArgumentBlockShape pins the crossing of a signature past the register
// file. The call is shim-carried, the shim's thunk takes one pointer to an
// argument block, and both compilers assert the block's size and every offset:
// the Go side with unsafe.Sizeof and unsafe.Offsetof, the C++ side with sizeof
// and offsetof, so a disagreement does not compile.
func TestArgumentBlockShape(t *testing.T) {
	m := renderMap(t, fixtureConfig("manyargs", "manyargs.hpp"))
	generated := m[GenFileName]
	shim := m[ShimSourceFileName]

	// The Go API is unchanged: the caller sees its parameters, not a block.
	assert.Contains(t, generated, "func Ints(a int64, b int64, c int64, d int64, e int64, f int64, g int64, h int64) int64")
	assert.Contains(t, generated, "func Floats(a float64, b float64, c float64, d float64, e float64, f float64, g float64, h float64, i float64) float64")

	// The block is a struct, filled argument by argument, and passed as one
	// pointer.
	assert.Contains(t, generated, "type cppgoArgs_")
	assert.Contains(t, generated, "var cppgoBlock cppgoArgs_")
	assert.Contains(t, generated, "cppgoBlock.a = int64(a)")
	assert.Contains(t, generated, "cppgoCall_")
	assert.Contains(t, generated, "unsafe.Pointer(&cppgoBlock)")

	// The C++ side declares the same fields at the same offsets.
	assert.Contains(t, shim, "struct cppgoArgs_")
	assert.Contains(t, shim, "static_assert(sizeof(cppgoArgs_")
	assert.Contains(t, shim, "static_assert(offsetof(cppgoArgs_")
	assert.Contains(t, shim, "static_cast<const cppgoArgs_")
	assert.Contains(t, shim, "static_cast<int64_t>(cppgo_args->a)")

	// A std::string parameter is built from the two words in the block, which is
	// the shape Eigen's IOFormat constructor has.
	assert.Contains(t, shim, "std::string cppgo_a(reinterpret_cast<const char *>(cppgo_args->a_data)")

	// The receiver is one more INTEGER word, so a method with six arguments
	// puts its this in the block too.
	assert.Contains(t, generated, "cppgoBlock.self = a.cppgoAddr()")
}

// TestArgumentBlockRoundTrip runs the generated package against the fixture: the
// positional encoding of every function is the measurement, because an argument
// read from the wrong block field cannot produce the number the Go side
// computes from the same arguments.
func TestArgumentBlockRoundTrip(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	fixture, err := filepath.Abs(filepath.Join("testdata", "manyargs"))
	require.NoError(t, err)

	tmp := t.TempDir()
	compiler, err := exec.LookPath("g++")
	require.NoError(t, err)
	library := filepath.Join(tmp, "libmanyargs.so")
	runIn(t, fixture, compiler, "-std=c++17", "-fPIC", "-shared", "-o", library, "manyargs.cpp")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "manyargs")
	cfg := fixtureConfig("manyargs", "manyargs.hpp")
	cfg.Library = library
	cfg.Out = out
	require.NoError(t, Generate(cfg))
	shim := filepath.Join(out, ShimLibraryName("manyargs"))
	require.FileExists(t, shim)
	deployBeside(t, library, out)
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "manyargs_test.go"), []byte(manyargsE2ESource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./manyargs")

	for _, pass := range []struct {
		name string
		env  []string
		args []string
	}{
		{
			name: "no cgo",
			env:  []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-count=1", "-v", "./manyargs"},
		},
		{
			name: "race detector",
			env:  []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
			args: []string{"test", "-race", "-count=1", "-v", "./manyargs"},
		},
	} {
		pass := pass
		t.Run(pass.name, func(t *testing.T) {
			ran := runGoEnv(t, goTool(t), moduleDir, pass.env, pass.args...)
			assert.Contains(t, ran, "--- PASS: TestArgumentBlock")
		})
	}
}

// manyargsE2ESource is the program the generated package runs. Every expected
// value is computed here from the same arguments with the positional weights the
// fixture uses, so a value read from the wrong block field shows as a mismatch.
const manyargsE2ESource = `package manyargs

import (
	"os"
	"testing"
)

func TestArgumentBlock(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Eight INTEGER words: two more than the register file carries.
	if got, want := Ints(1, 2, 3, 4, 5, 6, 7, 8), int64(1+2*10+3*100+4*1000+5*10000+6*100000+7*1000000+8*10000000); got != want {
		t.Fatalf("Ints = %d, want %d", got, want)
	}

	// Nine SSE words: one more than the register file carries.
	if got, want := Floats(1, 2, 3, 4, 5, 6, 7, 8, 9), float64(1+2*10+3*100+4*1000+5*10000+6*100000+7*1000000+8*10000000+9*100000000); got != want {
		t.Fatalf("Floats = %v, want %v", got, want)
	}

	// Both classes interleaved, starting with a four-byte word: the padding
	// after it is where a wrong block layout shows first.
	got := Mixed(1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14)
	want := 1.0 + 2*10.0 + 3*100.0 + 4*1000.0 + 5*10000.0 + 6*100000.0 + 7*1000000.0 +
		8*10000000.0 + 9*100000000.0 + 10*1000000000.0 + 11*10000000000.0 +
		12*100000000000.0 + 13*1000000000000.0 + 14*10000000000000.0
	if got != want {
		t.Fatalf("Mixed = %v, want %v", got, want)
	}

	// Fourteen INTEGER words, six of them the shim's std::string locals: the
	// shape Eigen's IOFormat constructor has.
	value, err := Strings(2, 3, "a", "bb", "ccc", "dddd", "eeeee", "ffffff")
	if err != nil {
		t.Fatalf("Strings: %v", err)
	}
	wantStrings := int64(2 + 3*10 + 1*100 + 2*1000 + 3*10000 + 4*100000 + 5*1000000 + 6*10000000)
	if value != wantStrings {
		t.Fatalf("Strings = %d, want %d", value, wantStrings)
	}

	// The receiver is a block field: six arguments plus this is seven words.
	acc, err := NewAccumulatorInt64(100)
	if err != nil {
		t.Fatalf("NewAccumulator: %v", err)
	}
	if got := acc.Add(1, 2, 3, 4, 5, 6); got != 100+1+20+300+4000+50000+600000 {
		t.Fatalf("Add = %d, want %d", got, 100+1+20+300+4000+50000+600000)
	}
	if got := acc.Add(1, 2, 3, 4, 5, 6); got != 2*(1+20+300+4000+50000+600000)+100 {
		t.Fatalf("the second Add = %d, want %d", got, 2*(1+20+300+4000+50000+600000)+100)
	}
	if got := acc.Value(); got != 2*(1+20+300+4000+50000+600000)+100 {
		t.Fatalf("Value = %d, want %d", got, 2*(1+20+300+4000+50000+600000)+100)
	}
}
`
