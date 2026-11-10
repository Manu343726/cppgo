package gen

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCallbackPayloads covers the shapes a callback parameter may carry beyond
// a scalar: the callee's text as a copied Go string (NUL-terminated or counted),
// its bytes as a slice view, and a record pointer as a mirrored Go pointer. The
// C++ side owns every one of these bytes, which is the rule that lets them be
// read while the callback runs and never afterwards.
func TestCallbackPayloads(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "payloads", "payloads.hpp")},
		Package: "payloads",
		Library: "libpayloads",
		Out:     ".",
	})
	require.NoError(t, err)
	for _, warning := range rendered.Warnings {
		assert.NotContains(t, warning, "not bound", "every payload shape binds")
	}

	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func SetLogCallback(fn func(int32, string)) error",
		"a NUL-terminated const char * is a Go string")
	assert.Contains(t, goFile, "cppgoGoString(cppgoWord(cppgoFrame.I[1]))",
		"the string is copied out of the callee's bytes during the callback")
	assert.Contains(t, goFile, "func SetText(fn func(string)) error",
		"a counted text buffer is one Go string, not two arguments")
	assert.Contains(t, goFile, "cppgoGoBytes(cppgoWord(cppgoFrame.I[0]), cppgoFrame.I[1])",
		"the count says how many bytes to read")
	assert.Contains(t, goFile, "func SetBytes(fn func([]uint8)) error",
		"a counted byte buffer is a slice view of the callee's memory")
	assert.Contains(t, goFile, "cppgoGoSlice[uint8](cppgoWord(cppgoFrame.I[0]), cppgoFrame.I[1])")
	assert.Contains(t, goFile, "func SetPoints(fn func([]Point)) error",
		"a counted record buffer is a slice of the mirrored struct")
	assert.Contains(t, goFile, "func SetPoint(fn func(*Point)) error",
		"a record pointer is the mirrored struct's address")
	assert.Contains(t, goFile, "func SetUntyped(fn func(unsafe.Pointer)) error",
		"a void pointer stays untyped")
	assert.Contains(t, goFile, "func SetLevel(fn func(int32, string)) (int32, error)",
		"a payload does not cost the callback its result")

	shimSource := outputNamed(t, rendered, ShimSourceFileName)
	assert.Contains(t, shimSource, "void (*)(int32_t, const char *)",
		"the shim casts the address to the declaration's own signature")
	assert.Contains(t, shimSource, "void (*)(const uint8_t *, uint64_t)",
		"a counted pair is two C parameters, and the cast keeps both")
}

// TestCallbackArgumentsTheBuildRefuses covers the callback-argument shapes with
// no Go value: a pointer to a pointer, a pointer to an opaque record (whose Go
// type is a wrapper of C++ storage nobody owns), and a counted buffer of one.
// Each is refused with the reason, and not guessed at.
func TestCallbackArgumentsTheBuildRefuses(t *testing.T) {
	m := &mapper{records: map[string]*recordModel{
		"payloads::Opaque": {GoName: "Opaque", CxxName: "payloads::Opaque"},
		"payloads::Point":  {GoName: "Point", CxxName: "payloads::Point", Mirrored: true},
	}}
	cases := []struct {
		spellings []string
		want      string
	}{
		{[]string{"char **"}, "is not a pointer this build carries"},
		{[]string{"const char *&"}, "is not a pointer this build carries"},
		{[]string{"Opaque *"}, "is a pointer to an opaque record"},
		{[]string{"std::string"}, "is not a scalar"},
		{[]string{"std::string *"}, "is not a pointer this build carries"},
		{[]string{"std::vector<int> *"}, "is not a pointer this build carries"},
	}
	for _, tc := range cases {
		_, err := m.callbackArg(tc.spellings)
		require.Error(t, err)
		assert.Contains(t, err.Error(), tc.want, "%v", tc.spellings)
	}
}

// TestCallbackResultRefusals covers the result side: one register, so one
// scalar, and a float32 is not carried.
func TestCallbackResultRefusals(t *testing.T) {
	m := &mapper{}
	_, err := m.callbackResult("float")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "return a double instead")

	_, err = m.callbackResult("payloads::Point")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a scalar")

	// A type the element table knows nothing about reports the table's own
	// reason, which is what names the fix.
	_, err = m.callbackResult("ns::Unknown")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a scalar or a known record")
}

// TestCallbackArgumentAcceptedShapes covers the boundary cases of the argument
// map: an empty pointer list, a volatile-qualified scalar, a bool (which travels
// in one byte of a register) and a character buffer whose count is spelled
// without the std:: prefix.
func TestCallbackArgumentAcceptedShapes(t *testing.T) {
	m := &mapper{records: map[string]*recordModel{
		"payloads::Point": {GoName: "Point", CxxName: "payloads::Point", Mirrored: true},
	}}
	cases := []struct {
		spellings []string
		goType    string
		words     int
	}{
		{[]string{"volatile int"}, "int32", 1},
		{[]string{"bool"}, "bool", 1},
		{[]string{"const char *", "size_t"}, "string", 2},
		{[]string{"const char *"}, "string", 1},
		{[]string{"const payloads::Point *", "size_t"}, "[]Point", 2},
		{[]string{"payloads::Point *"}, "*Point", 1},
		{[]string{"const double *", "size_t"}, "[]float64", 2},
	}
	for _, tc := range cases {
		arg, err := m.callbackArg(tc.spellings)
		require.NoError(t, err, "%v", tc.spellings)
		assert.Equal(t, tc.goType, arg.Type.Go, "%v", tc.spellings)
		assert.Equal(t, tc.words, arg.Words, "%v", tc.spellings)
		assert.Len(t, arg.C, tc.words, "one C spelling per argument word")
	}
}

// TestCallbackCallWithAStringResult covers the one standard-library result a
// callback-capable call carries: the thunk writes it through two words, and the
// binding pins the cells they point at, because the write happens after a
// callback may have grown the goroutine's stack.
func TestCallbackCallWithAStringResult(t *testing.T) {
	rendered, err := RenderResult(Config{
		Headers: []string{filepath.Join("testdata", "payloads", "describe.hpp")},
		Package: "payloads",
		Library: "libpayloads",
		Out:     ".",
	})
	require.NoError(t, err)
	goFile := outputNamed(t, rendered, GenFileName)
	assert.Contains(t, goFile, "func Describe(prefix string, fn func(int32)) (string, error)")
	assert.Contains(t, goFile, "cppgoDataCell := cppgoHeapCell[unsafe.Pointer]()")
	assert.Contains(t, goFile, "cppgoPin.Pin(cppgoDataCell)")
	assert.Contains(t, goFile, "cppgoData := *cppgoDataCell",
		"the result conversion reads the word out of the pinned cell")
	assert.Contains(t, goFile, "cppgoResult = string(unsafe.Slice((*byte)(cppgoData), int(cppgoSize)))",
		"the bytes are copied out before cppgo_free runs")
	assert.Contains(t, goFile, "cppgoData := *cppgoDataCell",
		"the word the thunk wrote is read out of the pinned cell")
	assert.Contains(t, goFile, "cppgo_prefix_cstr, cppgo_prefix_cstr_keep := cppgoCString(prefix)",
		"a char * parameter beside the result still gets its NUL-terminated copy")
}
