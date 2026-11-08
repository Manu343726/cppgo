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

// TestShimBodyStdResultError covers the copy-out failure of a result shape the
// emitter does not handle, and the same shape reached from stdResult directly.
// Both are one line in the shim, and both are the contract's "not emitted by
// this build yet" answer rather than a silently empty thunk.
func TestShimBodyStdResultError(t *testing.T) {
	model := &packageModel{}
	binding := &binding{
		sym:       cxx.Symbol{Kind: cxx.FreeFunc, QualifiedName: "ns::f"},
		Carrier:   thunk,
		Status:    true,
		StdResult: true,
		Result:    &goType{Std: stdUniquePtr, Go: "*V"},
	}
	_, err := model.shimBody(binding)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not emitted by this build yet")

	_, err = model.stdResult(binding, "call()")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not emitted by this build yet")
}

// TestShimArgUnknownRecord is the model that knows no record for a parameter a
// thunk wants to copy: the shim cannot name the type, so it is an error rather
// than a cast to nothing.
func TestShimArgUnknownRecord(t *testing.T) {
	model := &packageModel{mapper: &mapper{records: map[string]*recordModel{}}}
	_, err := model.shimArg(cxx.Param{Kind: cxx.TypeRecord, RecordName: "ns::Gone", Spelling: "ns::Gone"}, "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a record the model does not know")
}

// TestGenerateShimWriteErrors covers the writer's failure after a successful
// build: the shim's own path in the output directory is a directory, so the
// artifact cannot be placed.
func TestGenerateShimWriteErrors(t *testing.T) {
	if _, err := os.Stat("/usr/bin/g++"); err != nil {
		t.Skip("g++ is not in PATH")
	}
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ShimLibraryName("mathx")), 0o755))

	cfg := mathxConfig()
	cfg.Out = dir
	err := Generate(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), ShimLibraryName("mathx"))
}

// TestGenerateCheckModeBuildsNothing checks the split the contract states: check
// compares sources, and the compiled shim is a build artifact, not a generated
// file. A directory with no shim in it is up to date when the sources match.
func TestGenerateCheckModeBuildsNothing(t *testing.T) {
	dir := t.TempDir()
	cfg := mathxConfig()
	cfg.Out = dir
	require.NoError(t, Generate(cfg))
	require.NoError(t, os.Remove(filepath.Join(dir, ShimLibraryName("mathx"))))

	check := cfg
	check.Check = true
	require.NoError(t, Generate(check))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.NotEqual(t, ShimLibraryName("mathx"), entry.Name(), "check mode must not rebuild the shim")
		assert.False(t, strings.HasSuffix(entry.Name(), ".so"), entry.Name())
	}
}
