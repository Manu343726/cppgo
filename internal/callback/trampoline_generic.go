//go:build !amd64

package callback

import "errors"

// ErrUnsupported is what Register returns on an architecture where the callback
// trampoline has not been written. No callback can be registered there.
var ErrUnsupported = errors.New("cppgo: callbacks are not implemented on this architecture")

// Register is unimplemented on this architecture: there is no trampoline to aim
// a slot at, so no callback can be registered.
func Register(fn func(*Args)) (uintptr, error) { return 0, ErrUnsupported }

// Release is unimplemented on this architecture and reports ErrUnknown, because
// Register never handed out an address.
func Release(code uintptr) error { return ErrUnknown }

func slotAddr(i int) uintptr { return 0 }

func slotIndex(code uintptr) (int, bool) { return 0, false }
