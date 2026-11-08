package errormap_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/Manu343726/cppgo"
	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/errormap"
	"github.com/Manu343726/cppgo/internal/rtl"
)

// errorBuf mirrors testdata/shim.cpp's cppgo_error: the code, the category and
// the message, in one block the Go side owns. A zeroed block means success.
type errorBuf struct {
	Code     int32
	_        int32
	Category uintptr
	Message  [256]byte
}

var (
	buildOnce sync.Once
	libDir    string
	libErr    error
	symbols   func(string) unsafe.Pointer
)

// build compiles the user's library and the cppgo-built shim as two objects —
// the v4 artifact model — with -std=c++23, which std::expected needs.
func build(t *testing.T) func(string) unsafe.Pointer {
	t.Helper()

	buildOnce.Do(func() {
		gxx, err := exec.LookPath("g++")
		if err != nil {
			libErr = err
			return
		}
		libDir, err = os.MkdirTemp("", "cppgo-errormap-")
		if err != nil {
			libErr = err
			return
		}

		user := filepath.Join(libDir, "liberruser.so")
		shim := filepath.Join(libDir, "libcppgo_shim.so")

		if out, err := run(gxx, "-O1", "-fPIC", "-shared", "-std=c++23", "-I", "testdata",
			"-o", user, "testdata/errlib.cpp"); err != nil {
			libErr = fmt.Errorf("build user library: %w\n%s", err, out)
			return
		}
		if out, err := run(gxx, "-O1", "-fPIC", "-shared", "-std=c++23", "-I", "testdata",
			"-o", shim, "testdata/shim.cpp", user, "-Wl,-rpath,"+libDir); err != nil {
			libErr = fmt.Errorf("build shim: %w\n%s", err, out)
			return
		}

		lib, err := rtl.Open(shim)
		if err != nil {
			libErr = err
			return
		}
		symbols = func(name string) unsafe.Pointer {
			addr, err := lib.Symbol(name)
			if err != nil {
				panic(err)
			}
			return rtl.Pointer(addr)
		}
	})

	if libErr != nil {
		t.Skipf("cannot build the fixture: %v", libErr)
	}
	return symbols
}

func run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// TestMain removes the built objects.
func TestMain(m *testing.M) {
	code := m.Run()
	if libDir != "" {
		_ = os.RemoveAll(libDir)
	}
	os.Exit(code)
}

// newErrorBuf allocates the block a thunk writes its failure into.
//
// It is heap storage, pinned for the call, and that is a rule rather than a
// detail: a stack local's address is a uintptr the moment it is handed to the
// foreign call, and a Go frame moves — so the message the projection reads
// through that address would be a view of memory the frame no longer owns. The
// contract states the same thing for every out-block a thunk writes.
func newErrorBuf(t *testing.T) *errorBuf {
	t.Helper()
	b := new(errorBuf)
	var pinner runtime.Pinner
	pinner.Pin(b)
	t.Cleanup(pinner.Unpin)
	return b
}

// call invokes a thunk through the direct carrier. The probe is the caller's and
// runs before any pointer into the caller's frame is taken, because it grows the
// stack and moves that frame.
func call(t *testing.T, fn unsafe.Pointer, a *asm.Args) int32 {
	t.Helper()
	got, _ := asm.Call(fn, a)
	return int32(got)
}

// cstr copies a NUL-terminated string out of the error block. The copy is
// deliberate: the returned error outlives the block, and a view into pinned
// storage would keep that storage alive for as long as the error is held.
func cstr(p uintptr) string { return strings.Clone(rtl.GoString(p)) }

// codeError is the projection a generated binding performs for a thunk that
// reported a code and a category: *cppgo.CallError wrapping *errormap.ErrorCode.
func codeError(symbol string, b *errorBuf) error {
	return &cppgo.CallError{Symbol: symbol, Err: &errormap.ErrorCode{
		Code:     int(b.Code),
		Category: cstr(b.Category),
		Message:  cstr(uintptr(unsafe.Pointer(&b.Message[0]))),
	}}
}

// textError is the projection for a thunk with no code: *cppgo.CallError
// wrapping *errormap.Message.
func textError(symbol string, b *errorBuf) error {
	return &cppgo.CallError{Symbol: symbol, Err: &errormap.Message{
		Text: cstr(uintptr(unsafe.Pointer(&b.Message[0]))),
	}}
}

// The generic_category values are errno values on Linux; they are named here so
// the assertions read as the C++ error they are.
const (
	enoent        = 2   // std::errc::no_such_file_or_directory
	einval        = 22  // std::errc::invalid_argument
	ehostunreach  = 113 // std::errc::host_unreachable
	parseBadPoint = 1   // ns::parse_point's own status numbering
)
