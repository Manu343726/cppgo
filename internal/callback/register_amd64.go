//go:build amd64

package callback

import "runtime"

// Register makes fn callable from C and returns its address.
//
// The address is a code address in this process, valid until Release is called
// for it. C may call it any number of times, from any thread, as long as the
// call that reaches it was made through asm.CallReentrant. Registering the same
// function twice gives two independent slots, because a caller may release one
// while another is still held by C++.
//
// Returns ErrNilFunc for a nil fn and ErrFull when the table has no free slot.
func Register(fn func(*Args)) (uintptr, error) {
	if fn == nil {
		return 0, ErrNilFunc
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()

	old := registry.current.Load()
	next := new([Slots]*slot)
	*next = *old

	index := -1
	for i, s := range next {
		if s == nil {
			index = i
			break
		}
	}
	if index < 0 {
		return 0, ErrFull
	}

	next[index] = &slot{fn: fn}
	registry.current.Store(next)

	return slotAddr(index), nil
}

// Release removes a callback registered by Register, and waits for the calls
// already inside fn to return. A caller that is done with a callback can
// therefore release it knowing no Go frame of it is still running.
//
// It makes no promise about C. A C++ object that still holds the address and
// calls it after Release reaches the "released callback" panic described at
// dispatchFunc, which is a programmer error on the C++ side and never a
// use-after-free. Releasing an address twice, or one that was never registered,
// returns ErrUnknown.
func Release(code uintptr) error {
	index, ok := slotIndex(code)
	if !ok {
		return ErrUnknown
	}

	registry.mu.Lock()
	old := registry.current.Load()
	s := old[index]
	if s == nil {
		registry.mu.Unlock()
		return ErrUnknown
	}

	next := new([Slots]*slot)
	*next = *old
	next[index] = nil
	registry.current.Store(next)
	registry.mu.Unlock()

	// The slot is out of the snapshot, so no new call can find it; a call
	// already past the snapshot is counted in active.
	for s.active.Load() != 0 {
		runtime.Gosched()
	}

	return nil
}
