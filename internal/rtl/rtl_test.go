package rtl_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/rtl"
	"github.com/Manu343726/cppgo/internal/testlib"
)

// TestMain removes the C++ fixture testlib compiles on demand.
func TestMain(m *testing.M) {
	code := m.Run()
	testlib.Cleanup()
	os.Exit(code)
}

func TestOpenPathSymbolClose(t *testing.T) {
	path := testlib.Build(t, testlib.Full)

	lib, err := rtl.Open(path)
	require.NoError(t, err)
	require.NotNil(t, lib)

	assert.Equal(t, path, lib.Path())

	addr, err := lib.Symbol("cppgo_fixture_add")
	require.NoError(t, err)
	assert.NotZero(t, addr)

	require.NoError(t, lib.Close())

	// The path survives Close: it is what a later error message needs.
	assert.Equal(t, path, lib.Path())

	// Closing again is an error wrapping ErrClosed, not a silent success.
	err = lib.Close()
	require.Error(t, err)
	assert.ErrorIs(t, err, rtl.ErrClosed)

	var loaderErr *rtl.Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlclose", loaderErr.Op)
	assert.Equal(t, path, loaderErr.Path)
}

func TestOpenEmptyPath(t *testing.T) {
	lib, err := rtl.Open("")
	require.Error(t, err)
	assert.Nil(t, lib)

	var loaderErr *rtl.Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlopen", loaderErr.Op)
	assert.Empty(t, loaderErr.Path)
	assert.ErrorIs(t, err, loaderErr.Err)
	assert.Contains(t, err.Error(), "empty library path")
}

func TestOpenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "libcppgo-missing.so")

	lib, err := rtl.Open(path)
	require.Error(t, err)
	assert.Nil(t, lib)

	var loaderErr *rtl.Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlopen", loaderErr.Op)
	assert.Equal(t, path, loaderErr.Path)
	assert.Empty(t, loaderErr.Name)

	// The loader's message is preserved verbatim underneath.
	assert.Contains(t, err.Error(), path)
	assert.NotNil(t, errors.Unwrap(err))
}

func TestSymbolMissing(t *testing.T) {
	lib, err := rtl.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	defer func() { require.NoError(t, lib.Close()) }()

	addr, err := lib.Symbol("cppgo_no_such_symbol")
	require.Error(t, err)
	assert.Zero(t, addr)

	var loaderErr *rtl.Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlsym", loaderErr.Op)
	assert.Equal(t, "cppgo_no_such_symbol", loaderErr.Name)
	assert.NotEmpty(t, loaderErr.Path)
	assert.Contains(t, err.Error(), "dlsym")
	assert.Contains(t, err.Error(), "cppgo_no_such_symbol")
}

func TestSymbolEmptyName(t *testing.T) {
	lib, err := rtl.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	defer func() { require.NoError(t, lib.Close()) }()

	addr, err := lib.Symbol("")
	require.Error(t, err)
	assert.Zero(t, addr)
	assert.Contains(t, err.Error(), "empty symbol name")
}

func TestSymbolAfterClose(t *testing.T) {
	lib, err := rtl.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	require.NoError(t, lib.Close())

	addr, err := lib.Symbol("cppgo_fixture_add")
	require.Error(t, err)
	assert.Zero(t, addr)
	assert.ErrorIs(t, err, rtl.ErrClosed)

	var loaderErr *rtl.Error
	require.ErrorAs(t, err, &loaderErr)
	assert.Equal(t, "dlsym", loaderErr.Op)
	assert.Equal(t, "cppgo_fixture_add", loaderErr.Name)
}

// TestErrorFormatting covers every shape of *rtl.Error: the message has to be
// readable whether or not the operation had a path, a symbol and a cause.
func TestErrorFormatting(t *testing.T) {
	cause := errors.New("boom")

	tests := []struct {
		name string
		err  *rtl.Error
		want string
	}{
		{
			name: "op only",
			err:  &rtl.Error{Op: "dlopen"},
			want: "rtl: dlopen",
		},
		{
			name: "symbol only",
			err:  &rtl.Error{Op: "dlsym", Name: "dot"},
			want: `rtl: dlsym "dot"`,
		},
		{
			name: "path only",
			err:  &rtl.Error{Op: "dlopen", Path: "/tmp/libfoo.so"},
			want: `rtl: dlopen in "/tmp/libfoo.so"`,
		},
		{
			name: "symbol and path",
			err:  &rtl.Error{Op: "dlsym", Path: "/tmp/libfoo.so", Name: "dot"},
			want: `rtl: dlsym "dot" in "/tmp/libfoo.so"`,
		},
		{
			name: "with cause",
			err:  &rtl.Error{Op: "dlsym", Path: "/tmp/libfoo.so", Name: "dot", Err: cause},
			want: `rtl: dlsym "dot" in "/tmp/libfoo.so": boom`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.err.Error())

			if tc.err.Err != nil {
				assert.ErrorIs(t, tc.err, cause)
				assert.Equal(t, cause, tc.err.Unwrap())
			} else {
				assert.Nil(t, tc.err.Unwrap())
			}
		})
	}
}

// TestPointerAndGoString reads the fixture's exported data symbol the way a
// generated wrapper reads a C string: resolve the symbol, reinterpret its
// address, follow the pointer it holds, copy the C string out.
func TestPointerAndGoString(t *testing.T) {
	lib, err := rtl.Open(testlib.Build(t, testlib.Full))
	require.NoError(t, err)
	defer func() { require.NoError(t, lib.Close()) }()

	addr, err := lib.Symbol("cppgo_fixture_exported_data")
	require.NoError(t, err)

	// The symbol is a `const char *`: its address points at a pointer.
	require.NotNil(t, rtl.Pointer(addr))
	ptr := *(*uintptr)(rtl.Pointer(addr))

	assert.Equal(t, "cppgo fixture data", rtl.GoString(ptr))
	assert.NotNil(t, rtl.Pointer(ptr))

	// A zero address is the empty string, never a dereference of NULL.
	assert.Equal(t, "", rtl.GoString(0))
	assert.Nil(t, rtl.Pointer(0))
}
