package gen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// retainedFixtureConfig is the run over the retained-callback fixture, with the
// declaration the callee stores named with -retain.
func retainedFixtureConfig() Config {
	return Config{
		Headers: []string{filepath.Join("testdata", "retained", "include", "retained", "retained.hpp")},
		Package: "retained",
		Library: "libretained",
		Out:     ".",
		Retain:  []string{"retained::set_log_callback"},
	}
}

// TestRetainedCallbackKeepsTheRegistration covers the generated shape of a call
// whose callback the callee stores: it returns a registration and an error, it
// does not release the slot when it returns, and the package declares the
// registration type that Close is on.
func TestRetainedCallbackKeepsTheRegistration(t *testing.T) {
	rendered, err := RenderResult(retainedFixtureConfig())
	require.NoError(t, err)

	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func SetLogCallback(fn func(int32, string)) (*CallbackRegistration, error)",
		"a retained registration is the call's outcome")
	assert.NotContains(t, goFile, "defer cppgo.ReleaseCallback(cppgoCallback0)",
		"the slot lives past the registering call")
	assert.Contains(t, goFile, "return cppgoRetain(cppgoCallback0, fn), nil")
	assert.Contains(t, goFile, "type CallbackRegistration struct")
	assert.Contains(t, goFile, "func (r *CallbackRegistration) Close() error")
	assert.Contains(t, goFile, "return cppgo.ErrClosed", "a second Close reports the closed value")

	// A nil function clears the sink: the call is made with a null callback
	// address, and no registration is returned.
	assert.Contains(t, goFile, "if fn == nil {")
	assert.Contains(t, goFile, "return nil, nil")

	// Every call in the package enters C through the callback-capable carrier:
	// any of them may deliver a record the callee stored.
	for _, declaration := range []string{"retained::log", "retained::touch", "retained::set_log_callback"} {
		carried := false
		for _, line := range rendered.Report {
			if len(line) > len(declaration) && line[:len(declaration)] == declaration {
				assert.Contains(t, line, " reentrant ", "%s must be callback-capable", declaration)
				assert.Contains(t, line, "a retained callback is registered for this package")
				carried = true
			}
		}
		assert.True(t, carried, "%s is not in the report", declaration)
	}
}

// TestRetainRefusesWhatItCannotMean covers the three ways a -retain entry is
// wrong: it names nothing, it names a declaration with no callback, and it
// names one whose value the registration would have to stand in for.
func TestRetainRefusesWhatItCannotMean(t *testing.T) {
	cfg := retainedFixtureConfig()
	cfg.Retain = []string{"retained::not_a_declaration"}
	_, err := RenderResult(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "it names nothing this header declares")

	// touch() takes no callback, so there is nothing the callee could store.
	cfg = retainedFixtureConfig()
	cfg.Retain = []string{"retained::touch"}
	_, err = RenderResult(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "takes no callback parameter")

	// A declaration that returns a value cannot also hand back a registration.
	cfg = retainedFixtureConfig()
	cfg.Headers = []string{filepath.Join("testdata", "retained", "valued.hpp")}
	cfg.Retain = []string{"retained::set_valued_callback"}
	_, err = RenderResult(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returns a value")
}

// TestRetainedAccessorReadsAStringThroughTheShim covers the shape the free
// function fixture never reaches: an accessor that returns a std::string.
//
// The package's -retain puts every binding on the reentrant carrier while it is
// built, and the accessor is moved to the shim afterwards. The two carriers keep
// a result in different places — pinned heap cells whose names the reentrant
// call reads after it returns, against locals the shim is handed the address of
// — so the accessor's result has to be lowered again for the carrier it ends on.
// The names are the evidence: the shim's locals are declared and the reentrant
// carrier's cells are not.
func TestRetainedAccessorReadsAStringThroughTheShim(t *testing.T) {
	rendered, err := RenderResult(retainedFixtureConfig())
	require.NoError(t, err)
	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func (e *Enrollment) Label() (string, error)")
	assert.Contains(t, goFile, "var cppgoData unsafe.Pointer\n\tvar cppgoSize uint64")
	assert.Contains(t, goFile, "unsafe.Pointer(&cppgoData)")
	assert.NotContains(t, goFile, "cppgoDataCell",
		"the reentrant result's cells belong to a carrier this accessor does not use")
	assert.NotContains(t, goFile, "cppgoHeapCell",
		"the package pins nothing once its only pinning result is carried by the shim")

	carried := false
	for _, line := range rendered.Report {
		if !strings.HasPrefix(line, "retained::Enrollment::label() const noexcept") {
			continue
		}
		assert.Contains(t, line, " thunk ")
		assert.Contains(t, line, "a field accessor")
		carried = true
	}
	assert.True(t, carried, "the accessor is not in the report")
}

// TestRetainedCallbackAsAStatus covers the one result a retained declaration may
// have: a status code, which becomes the error beside the registration. Both the
// clearing call and the registering call read it, and a failed registering call
// releases the slot it just took.
func TestRetainedCallbackAsAStatus(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "retained", "status.hpp")},
		Package: "retained",
		Library: "libretained",
		Out:     ".",
		Retain:  []string{"retained::set_status_callback"},
		Status:  []string{"retained::set_status_callback"},
	})
	require.NoError(t, err)
	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func SetStatusCallback(fn func(int32)) (*CallbackRegistration, error)")
	assert.Contains(t, goFile, "cppgoFrame.I[0] = uintptr(0)",
		"the clearing call passes a null callback")
	assert.Contains(t, goFile, "if cppgoStatus := int32(cppgoFrame.Ret); cppgoStatus != 0 {",
		"the status is read whether or not a callback was registered")
	assert.Contains(t, goFile, "cppgo.ReleaseCallback(cppgoCallback0)\n\t\treturn nil, cppgoCallError",
		"a failed registering call releases the slot it took")
}

// TestRetainedCallbackNameCollision proves the generated registration type is
// part of the API's name space: a C++ record of the same name is reported
// instead of silently colliding with it.
func TestRetainedCallbackNameCollision(t *testing.T) {
	cfg := retainedFixtureConfig()
	cfg.Headers = []string{filepath.Join("testdata", "retained", "collision.hpp")}
	cfg.Retain = []string{"retained::set_log_callback"}
	_, err := RenderResult(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CallbackRegistration")
	assert.Contains(t, err.Error(), "-rename")
}
