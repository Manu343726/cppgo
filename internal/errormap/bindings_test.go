package errormap_test

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/errormap"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// The bindings below are what the generator emits from a rule: one Go function
// with the value first and the error last, the thunk's status turned into an
// error by the projection the rule named. They are written out here because this
// package measures the runtime half of the mapping, not the emitter.

// OpenFile binds ns::open_file, which returns std::error_code.
func OpenFile(t *testing.T, path string) (int32, error) {
	t.Helper()
	fn := build(t)("shim_open_file")

	var fd int32
	err := newErrorBuf(t)
	var a asm.Args
	a.I[0] = uintptr(unsafe.Pointer(unsafe.StringData(path)))
	a.I[1] = uintptr(len(path))
	a.I[2] = uintptr(unsafe.Pointer(&fd))
	a.I[3] = uintptr(unsafe.Pointer(err))

	if status := call(t, fn, &a); errormap.Failed(status) {
		return 0, codeError("ns::open_file", err)
	}
	return fd, nil
}

// ParseInt binds ns::parse_int, which returns std::expected<int, std::error_code>.
func ParseInt(t *testing.T, text string) (int32, error) {
	t.Helper()
	fn := build(t)("shim_parse_int")

	var out int32
	err := newErrorBuf(t)
	var a asm.Args
	a.I[0] = uintptr(unsafe.Pointer(unsafe.StringData(text)))
	a.I[1] = uintptr(len(text))
	a.I[2] = uintptr(unsafe.Pointer(&out))
	a.I[3] = uintptr(unsafe.Pointer(err))

	if status := call(t, fn, &a); errormap.Failed(status) {
		return 0, codeError("ns::parse_int", err)
	}
	return out, nil
}

// Divide binds ns::divide, which throws std::runtime_error: the message only.
func Divide(t *testing.T, a, b int32) (int32, error) {
	t.Helper()
	fn := build(t)("shim_divide")

	var out int32
	err := newErrorBuf(t)
	var args asm.Args
	args.I[0] = uintptr(a)
	args.I[1] = uintptr(b)
	args.I[2] = uintptr(unsafe.Pointer(&out))
	args.I[3] = uintptr(unsafe.Pointer(err))

	if status := call(t, fn, &args); errormap.Failed(status) {
		return 0, textError("ns::divide", err)
	}
	return out, nil
}

// Connect binds ns::connect, which throws std::system_error: a code, a category
// and a message, projected exactly like an error_code return.
func Connect(t *testing.T, host string) (int32, error) {
	t.Helper()
	fn := build(t)("shim_connect")

	var out int32
	err := newErrorBuf(t)
	var a asm.Args
	a.I[0] = uintptr(unsafe.Pointer(unsafe.StringData(host)))
	a.I[1] = uintptr(len(host))
	a.I[2] = uintptr(unsafe.Pointer(&out))
	a.I[3] = uintptr(unsafe.Pointer(err))

	if status := call(t, fn, &a); errormap.Failed(status) {
		return 0, codeError("ns::connect", err)
	}
	return out, nil
}

// ParsePoint is the configured case: ns::parse_point returns bool and writes its
// value and its failure through parameters 1 and 2. The config is what says so,
// and the generated signature is (Point, error) — the bool is not exposed.
func ParsePoint(t *testing.T, text string, rule errormap.Rule) (Point, error) {
	t.Helper()

	// The rule is what the generator would use to locate the channels. This
	// binding is hand-written, so it asserts the rule agrees with it rather
	// than reading it at run time: a mismatch would be a generation bug.
	if rule.Value != 1 || rule.Error != 2 {
		t.Fatalf("rule for %s has channels value=%d error=%d, want 1 and 2", rule.Symbol, rule.Value, rule.Error)
	}

	fn := build(t)("shim_parse_point")

	var x, y, status int32
	err := newErrorBuf(t)
	var a asm.Args
	a.I[0] = uintptr(unsafe.Pointer(unsafe.StringData(text)))
	a.I[1] = uintptr(len(text))
	a.I[2] = uintptr(unsafe.Pointer(&x))
	a.I[3] = uintptr(unsafe.Pointer(&y))
	a.I[4] = uintptr(unsafe.Pointer(&status))
	a.I[5] = uintptr(unsafe.Pointer(err))

	if errormap.Failed(call(t, fn, &a)) {
		return Point{}, codeError("ns::parse_point", err)
	}
	return Point{X: x, Y: y}, nil
}

// Point is ns::Point, mirrored because it is a POD.
type Point struct {
	X int32
	Y int32
}

// Load binds ns::load, whose ns::Result<int> is the inferred shape: no config
// entry, the has_value/value/error members are enough.
func Load(t *testing.T, key int32) (int32, error) {
	t.Helper()
	fn := build(t)("shim_load")

	var out int32
	err := newErrorBuf(t)
	var a asm.Args
	a.I[0] = uintptr(key)
	a.I[1] = uintptr(unsafe.Pointer(&out))
	a.I[2] = uintptr(unsafe.Pointer(err))

	if status := call(t, fn, &a); errormap.Failed(status) {
		return 0, codeError("ns::load", err)
	}
	return out, nil
}

func TestErrorCodeReturn(t *testing.T) {
	build(t)
	asm.Probe()

	t.Run("success", func(t *testing.T) {
		asm.Probe()
		fd, err := OpenFile(t, "/tmp/file.txt")
		require.NoError(t, err)
		assert.EqualValues(t, 42, fd)
	})

	t.Run("failure carries code, category and message", func(t *testing.T) {
		asm.Probe()
		fd, err := OpenFile(t, "missing.txt")
		require.Error(t, err)
		assert.Zero(t, fd, "a failed call returns the zero value")

		var ec *errormap.ErrorCode
		require.ErrorAs(t, err, &ec)
		assert.Equal(t, enoent, ec.Code)
		assert.Equal(t, "generic", ec.Category)
		assert.Equal(t, "No such file or directory", ec.Message)
		assert.Equal(t, "cppgo: generic: No such file or directory (code 2)", ec.Error())

		// The chain names the C++ declaration: errors.As finds the runtime's
		// CallError and the projection under it.
		var ce *cppgo.CallError
		require.ErrorAs(t, err, &ce)
		assert.Equal(t, "ns::open_file", ce.Symbol)

		// errors.Is works on the error's identity, which for a C++ category
		// is its code and its category.
		assert.True(t, errors.Is(err, &errormap.ErrorCode{Code: enoent, Category: "generic"}))
		assert.False(t, errors.Is(err, &errormap.ErrorCode{Code: einval, Category: "generic"}))
		assert.False(t, errors.Is(err, &errormap.ErrorCode{Code: enoent, Category: "system"}))
	})
}

func TestExpectedReturn(t *testing.T) {
	build(t)
	asm.Probe()

	t.Run("value", func(t *testing.T) {
		asm.Probe()
		got, err := ParseInt(t, "42")
		require.NoError(t, err)
		assert.EqualValues(t, 42, got)
	})

	t.Run("unexpected carries the error alternative", func(t *testing.T) {
		asm.Probe()
		got, err := ParseInt(t, "4x")
		require.Error(t, err)
		assert.Zero(t, got)

		var ec *errormap.ErrorCode
		require.ErrorAs(t, err, &ec)
		assert.Equal(t, einval, ec.Code)
		assert.Equal(t, "generic", ec.Category)
		assert.True(t, errors.Is(err, &errormap.ErrorCode{Code: einval, Category: "generic"}))
	})
}

func TestExceptionReturn(t *testing.T) {
	build(t)
	asm.Probe()

	t.Run("no throw", func(t *testing.T) {
		asm.Probe()
		got, err := Divide(t, 6, 2)
		require.NoError(t, err)
		assert.EqualValues(t, 3, got)
	})

	t.Run("std::runtime_error becomes the message", func(t *testing.T) {
		asm.Probe()
		_, err := Divide(t, 1, 0)
		require.Error(t, err)

		var msg *errormap.Message
		require.ErrorAs(t, err, &msg)
		assert.Equal(t, "division by zero", msg.Text)
		assert.Equal(t, "cppgo: division by zero", msg.Error())
		assert.True(t, errors.Is(err, &errormap.Message{Text: "division by zero"}))
	})

	t.Run("std::system_error becomes code, category and message", func(t *testing.T) {
		asm.Probe()
		_, err := Connect(t, "elsewhere")
		require.Error(t, err)

		var ec *errormap.ErrorCode
		require.ErrorAs(t, err, &ec)
		assert.Equal(t, ehostunreach, ec.Code)
		assert.Equal(t, "generic", ec.Category)
		assert.Contains(t, ec.Message, "elsewhere")
		assert.True(t, errors.Is(err, &errormap.ErrorCode{Code: ehostunreach, Category: "generic"}))
	})
}

// TestConfiguredOutParamReturn is the non-typical row: a bool return with the
// value and the failure in parameters. The rule comes from the package's
// config, and the generated call has no bool in its signature.
func TestConfiguredOutParamReturn(t *testing.T) {
	build(t)
	asm.Probe()

	rules := resolve(t)
	rule := rules["ns::parse_point"]
	require.Equal(t, errormap.KindOutParam, rule.Kind)
	assert.True(t, rule.Configured)

	t.Run("success", func(t *testing.T) {
		asm.Probe()
		p, err := ParsePoint(t, "3,4", rule)
		require.NoError(t, err)
		assert.Equal(t, Point{X: 3, Y: 4}, p)
	})

	t.Run("failure projects the configured status channel", func(t *testing.T) {
		asm.Probe()
		_, err := ParsePoint(t, "nope", rule)
		require.Error(t, err)

		var ec *errormap.ErrorCode
		require.ErrorAs(t, err, &ec)
		assert.Equal(t, parseBadPoint, ec.Code)
		assert.Equal(t, "status", ec.Category)
		assert.Equal(t, "invalid point", ec.Message)
	})
}

// TestInferredCustomResult is the other direction of the same rule: no config
// entry, because ns::Result<int> has the shape the generator recognises.
func TestInferredCustomResult(t *testing.T) {
	build(t)
	asm.Probe()

	rule := resolve(t)["ns::load"]
	assert.Equal(t, errormap.KindCustom, rule.Kind)
	assert.False(t, rule.Configured, "a Result-shaped return needs no entry")

	asm.Probe()
	got, err := Load(t, 21)
	require.NoError(t, err)
	assert.EqualValues(t, 42, got)

	asm.Probe()
	_, err = Load(t, -1)
	require.Error(t, err)

	var ec *errormap.ErrorCode
	require.ErrorAs(t, err, &ec)
	assert.Equal(t, einval, ec.Code)
}

// TestNonFallibleCallHasNoErrorResult pins the rule that keeps every Go
// signature honest: a noexcept call that cannot fail gets no error result at
// all, rather than an error that is always nil.
func TestNonFallibleCallHasNoErrorResult(t *testing.T) {
	rule, err := (&errormap.Config{}).Resolve(
		errormap.Signature{Name: "ns::twice", Returns: "double"},
		errormap.Inference{})
	require.NoError(t, err)
	assert.Empty(t, rule.Kind)
	assert.Contains(t, rule.Report(), "no error channel")
}

var _ = rtl.GoString

// TestErrorRendering pins what the generated error types print and how Is
// behaves against a target of another type, which is what makes errors.Is safe
// to use with a mixed error chain.
func TestErrorRendering(t *testing.T) {
	assert.Equal(t, "cppgo: error (code 0)", (&errormap.ErrorCode{}).Error())
	assert.Equal(t, "cppgo: generic: bad thing (code 5)",
		(&errormap.ErrorCode{Code: 5, Category: "generic", Message: "bad thing"}).Error())
	assert.Equal(t, "cppgo: status (code 1)", (&errormap.ErrorCode{Code: 1, Category: "status"}).Error())

	assert.Equal(t, "cppgo: error", (&errormap.Message{}).Error())
	assert.Equal(t, "cppgo: boom", (&errormap.Message{Text: "boom"}).Error())

	assert.False(t, (&errormap.ErrorCode{Code: 1, Category: "generic"}).Is(&errormap.Message{Text: "x"}))
	assert.False(t, (&errormap.Message{Text: "x"}).Is(&errormap.ErrorCode{}))
	assert.True(t, (&errormap.Message{Text: "x"}).Is(&errormap.Message{Text: "x"}))

	assert.False(t, errormap.Failed(0))
	assert.True(t, errormap.Failed(1))
}
