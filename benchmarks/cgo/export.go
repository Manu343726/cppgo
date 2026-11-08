package benchcgo

/*
#include <stdint.h>
*/
import "C"

// foldFn is the Go function bench::fold calls back into. cgo cannot hand a Go
// closure to C — the callback is an exported function with a fixed address —
// so the bench_fold wrapper in shim.cpp calls this package's exported
// goFoldCallback, and this slot says which Go function that is.
//
// One slot is enough because the callback runs on the goroutine that made the
// call, inside bench_fold, after foldFn was written: write, then call on the
// same goroutine, then read. The benchmarks are single-goroutine, and Fold's
// doc comment says so.
var foldFn = func(a, b int32) int32 { return a + b }

// goFoldCallback is the C ABI entry point cgo exports for shim.cpp. Its name
// is the C symbol the wrapper declares.
//
//export goFoldCallback
func goFoldCallback(a, b C.int32_t) C.int32_t {
	return C.int32_t(foldFn(int32(a), int32(b)))
}
