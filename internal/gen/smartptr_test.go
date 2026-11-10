package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smartptrConfig is the smart-pointer fixture's canonical run.
func smartptrConfig() Config {
	return fixtureConfig("smartptr", "smartptr.hpp")
}

// TestSmartPointerResultIsTheObject pins the shape: a factory's result is the
// object it owns, and the control block is a holder object the Go value releases
// exactly once.
func TestSmartPointerResultIsTheObject(t *testing.T) {
	rendered, err := RenderResult(smartptrConfig())
	require.NoError(t, err)
	goFile, shim, header, asm := "", "", "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case GenFileName:
			goFile = string(out.Data)
		case ShimSourceFileName:
			shim = string(out.Data)
		case ShimHeaderFileName:
			header = string(out.Data)
		case AsmFileName:
			asm = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func MakeShared(value int32) (*Widget, error)")
	assert.Contains(t, goFile, "func MakeUnique(value int32) (*Widget, error)")
	// The shim moves the pointer into a holder and hands back both addresses.
	assert.Contains(t, shim, "auto *cppgo_holder = new std::shared_ptr<::smart::Widget>(std::move(cppgo_value));")
	assert.Contains(t, shim, "auto *cppgo_holder = new std::unique_ptr<::smart::Widget>(std::move(cppgo_value));")
	assert.Contains(t, shim, "*cppgo_out_pointee = cppgo_holder->get();")
	assert.Contains(t, shim, "*cppgo_out_ref = cppgo_holder;")
	// One release thunk per result, and its prototype.
	assert.Contains(t, header, "void cppgo_free_ref_")
	assert.Contains(t, shim, "delete static_cast<std::shared_ptr<::smart::Widget> *>(ref);")
	assert.Contains(t, shim, "delete static_cast<std::unique_ptr<::smart::Widget> *>(ref);")
	// The Go value owns the holder through one object and views the pointee.
	assert.Contains(t, goFile, "cppgoObject := cppgo.NewObject(1, 1, func() {")
	assert.Contains(t, goFile, "return &Widget{obj: cppgoObject, addr: uintptr(cppgoPointee)}, nil")
	assert.Contains(t, goFile, "if cppgoPointee == nil {")
	// The release goes through the shared free trampoline with this binding's
	// own symbol, which is what makes the control block's type right.
	assert.Contains(t, goFile, "cppgoFree(cppgoRefSym_")
	assert.Contains(t, goFile, "func cppgoFree(fn uintptr, block unsafe.Pointer)", "the trampoline is the shared one")
	assert.NotEmpty(t, asm)
}

// TestSmartPointerEmptyResult covers the empty pointer: the holder exists and is
// released, and the Go result is nil.
func TestSmartPointerEmptyResult(t *testing.T) {
	rendered, err := RenderResult(smartptrConfig())
	require.NoError(t, err)
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "if cppgoRef != nil {")
	assert.Contains(t, goFile, "cppgoFree(cppgoRefSym_")
}
