package cppgo_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
)

// TestCallErrorFormatsAsContract checks the exact message DESIGN.md states:
// "cppgo: mathx::describe: <message>".
func TestCallErrorFormatsAsContract(t *testing.T) {
	cases := []struct {
		name string
		err  *cppgo.CallError
		want string
	}{
		{
			name: "native message",
			err:  &cppgo.CallError{Symbol: "mathx::describe", Err: &cppgo.NativeError{Message: "bad input"}},
			want: "cppgo: mathx::describe: bad input",
		},
		{
			name: "nil error",
			err:  &cppgo.CallError{Symbol: "mathx::produce"},
			want: "cppgo: mathx::produce",
		},
		{
			name: "wrapped plain error",
			err:  &cppgo.CallError{Symbol: "mathx::add", Err: fmt.Errorf("status %d", 3)},
			want: "cppgo: mathx::add: status 3",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.err.Error())
		})
	}
}

// TestCallErrorExposesNativeError is the reason CallError exists: a caller that
// branches on the native message still finds it with errors.As.
func TestCallErrorExposesNativeError(t *testing.T) {
	native := &cppgo.NativeError{Message: "out of range"}
	err := &cppgo.CallError{Symbol: "mathx::Vector::dot", Err: native}

	var found *cppgo.NativeError
	require.ErrorAs(t, err, &found)
	assert.Same(t, native, found)
	require.ErrorIs(t, err, native)
	assert.False(t, errors.Is(err, cppgo.ErrClosed))
}

// TestCallErrorUnwrapNil covers the tolerated nil Err: Unwrap returns nil and
// errors.As finds nothing, without panicking.
func TestCallErrorUnwrapNil(t *testing.T) {
	err := &cppgo.CallError{Symbol: "mathx::produce"}
	assert.NoError(t, errors.Unwrap(err))

	var native *cppgo.NativeError
	assert.False(t, errors.As(err, &native))
}
