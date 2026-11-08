package callback_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// The two code pointers a std::function cannot exist without. libstdc++'s
// std::function<Ret(Args...)> stores the callable in a 16-byte _Any_data, then
// the invoker that calls it, then the manager that copies and destroys it. For
// a std::function built from a plain function pointer, the callable IS the
// pointer, so the first eight bytes of the block are the Go callback's address
// and the other two fields are these symbols: weak template instantiations the
// library exports as soon as its own code builds one of these objects.
//
// The names are for int(int, int) with an int(*)(int, int) target; a generated
// binding derives them from the signature the same way libstdc++ mangles the
// _Function_handler specialisation.
const (
	mangledInvoke  = "_ZNSt17_Function_handlerIFiiiEPS0_E9_M_invokeERKSt9_Any_dataOiS6_"
	mangledManager = "_ZNSt17_Function_handlerIFiiiEPS0_E10_M_managerERSt9_Any_dataRKS3_St18_Manager_operation"
)

// TestStdFunctionFromGo measures the second half of the callback story: a Go
// function handed to C++ as a std::function, which is a value C++ has to
// construct and Go has to lay out.
//
// The layout is 32 bytes on libstdc++ and is asserted rather than assumed: the
// library reports its own sizeof, and the two code pointers are resolved by the
// mangled names its .dynsym exports.
func TestStdFunctionFromGo(t *testing.T) {
	sym := fixture(t)

	// sizeof(std::function<int(int,int)>), measured through the library
	// itself, so a layout change is a test failure and not a corrupt object.
	size, _ := asm.CallReentrant(sym("cb_sizeof_std_function"), &asm.Args{})
	require.EqualValues(t, 32, size, "std::function<int(int,int)> must be 32 bytes for this layout")

	invoke, err := libSymbol(mangledInvoke)
	require.NoError(t, err, "the library must export _Function_handler::_M_invoke")
	manager, err := libSymbol(mangledManager)
	require.NoError(t, err, "the library must export _Function_handler::_M_manager")

	cb, err := callback.Register(func(a *callback.Args) {
		a.Ret = uintptr(int32(a.I[0]) * int32(a.I[1]))
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = callback.Release(cb) })

	// The block is Go memory pinned for the call: the C++ side may copy the
	// std::function (through _M_manager) and the callback may grow the
	// goroutine stack, so the object must not move while the call is in
	// flight. A generated binding holds the same four words in a Go value.
	//
	// libstdc++'s layout, in order: the 16-byte _Any_data that holds the
	// callable, the manager (_Function_base's second member), and then the
	// invoker that the derived function<> adds. The manager is the field that
	// copies and destroys, so it comes before the invoker, not after.
	block := make([]uintptr, 4)
	var pinner runtime.Pinner
	pinner.Pin(&block[0])
	t.Cleanup(pinner.Unpin)

	block[0] = cb
	block[1] = 0
	block[2] = manager
	block[3] = invoke

	var args asm.Args
	args.I[0] = uintptr(unsafe.Pointer(&block[0]))
	args.I[1] = 6
	args.I[2] = 7
	got, _ := asm.CallReentrant(sym("cb_apply_std_function"), &args)

	assert.EqualValues(t, 42, got, "std::function the Go side built called the Go callback")
	assert.Nil(t, callback.TakePanic())
}
