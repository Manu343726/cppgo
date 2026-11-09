package rtl_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// TestPreload loads the fixture with its symbols global, resolves one from the
// handle and unloads it again.
func TestPreload(t *testing.T) {
	path := testlib.Build(t, testlib.Full)

	lib, err := rtl.Preload(path)
	require.NoError(t, err)
	require.NotNil(t, lib)
	assert.Equal(t, path, lib.Path())

	addr, err := lib.Symbol("cppgo_fixture_add")
	require.NoError(t, err)
	assert.NotZero(t, addr)

	require.NoError(t, lib.Close())
}

func TestPreloadEmptyPath(t *testing.T) {
	lib, err := rtl.Preload("")
	require.Error(t, err)
	assert.Nil(t, lib)

	var loadErr *rtl.Error
	require.ErrorAs(t, err, &loadErr)
	assert.Equal(t, "dlopen", loadErr.Op)
}

func TestPreloadFailure(t *testing.T) {
	lib, err := rtl.Preload(testlib.Build(t, testlib.Full) + ".absent")
	require.Error(t, err)
	assert.Nil(t, lib)

	var loadErr *rtl.Error
	require.ErrorAs(t, err, &loadErr)
	assert.Equal(t, "dlopen", loadErr.Op)
	assert.Equal(t, testlib.Build(t, testlib.Full)+".absent", loadErr.Path)
}
