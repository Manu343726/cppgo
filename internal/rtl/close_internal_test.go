package rtl

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/testlib"
)

// TestClosePropagatesLoaderError injects a failing dlclose: the real one only
// fails for a handle that is not a loaded library, which no test can produce
// without corrupting the process.
func TestClosePropagatesLoaderError(t *testing.T) {
	path := testlib.Build(t, testlib.Full)

	lib, err := Open(path)
	require.NoError(t, err)
	require.NotNil(t, lib)

	original := dlclose
	t.Cleanup(func() { dlclose = original })

	dlclose = func(uintptr) error { return errors.New("injected dlclose failure") }

	err = lib.Close()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected dlclose failure")

	var loaderErr *Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlclose", loaderErr.Op)
	assert.Equal(t, path, loaderErr.Path)
	assert.Empty(t, loaderErr.Name)

	// The handle was not really released by the injected call, so put the
	// real loader back and close for the sake of not leaking it in the test.
	dlclose = original
	require.NoError(t, lib.Close())
}
