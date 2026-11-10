package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpanResultIsRefused pins the one shape the container mapping deliberately
// does not carry: a span *result*. A span is a view, so the Go value would name
// C++ storage whose owner Go does not hold; the declaration is reported and the
// rest of the header binds.
func TestSpanResultIsRefused(t *testing.T) {
	rendered, err := RenderResult(fixtureConfig("spanret", "spanret.hpp"))
	require.NoError(t, err)

	joined := joinWarnings(rendered.Warnings)
	assert.Contains(t, joined, "spanret::viewOf(int *, int)")
	assert.Contains(t, joined, "is a span result")
	assert.Contains(t, joined, "take the span as a parameter, or return the container it views")

	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func Count()", "the other declaration still binds")
	assert.NotContains(t, goFile, "func ViewOf(", "the span result is not in the API")
}

// TestStatusResultWithACopyBack pins the thunk a status-returning callee with a
// standard-library out-parameter produces: the call is kept in a branch so the
// copy-back runs only after it succeeded, and the status decides the error.
func TestStatusResultWithACopyBack(t *testing.T) {
	cfg := fixtureConfig("statusvector", "statusvector.hpp")
	cfg.Status = []string{"statv::fillStatus"}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)

	goFile, shim := "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case GenFileName:
			goFile = string(out.Data)
		case ShimSourceFileName:
			shim = string(out.Data)
		}
	}
	// The callee's return value is the status, so the Go signature is the
	// out-parameter and an error, with no integer result.
	assert.Contains(t, goFile, "func FillStatus(values *[]int32) error")
	assert.Contains(t, goFile, "cppgoCallError(\"statv::fillStatus\")")
	// The call is kept in a branch: a failed call returns before the copy.
	assert.Contains(t, shim, "if (::statv::fillStatus(cppgo_values) != 0) { return 1; }")
	assert.Contains(t, shim, "*values_out_data = cppgo_copy;")
}
