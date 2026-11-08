package callback_test

import "syscall"

// tid reports the operating system thread the caller is running on. It is how
// the callback tests tell "the same goroutine" from "the same thread": the
// runtime locks the goroutine to the M for the duration of a cgo callback.
func tid() uint64 {
	return uint64(syscall.Gettid())
}
