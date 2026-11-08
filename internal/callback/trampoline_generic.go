//go:build !amd64

package callback

import "errors"

// ErrUnsupported is returned by Register where the callback trampoline has not
// been written. cppgo still builds there, so a dependency graph naming it is
// not poisoned, but no callback can be registered.
var ErrUnsupported = errors.New("cppgo: callbacks are not implemented on this architecture")

func Register(fn func(*Args)) (uintptr, error) { return 0, ErrUnsupported }

func Release(code uintptr) error { return ErrUnknown }

func slotAddr(i int) uintptr { return 0 }

func slotIndex(code uintptr) (int, bool) { return 0, false }
