package callback_test

import (
	"testing"

	"github.com/Manu343726/cppgo/internal/asm"
	"github.com/Manu343726/cppgo/internal/callback"
)

// BenchmarkCallDirect and BenchmarkCallReentrant price the two carriers on the
// same trivial callee. The difference is what a callback-capable binding costs:
// one runtime.cgocall and the g0 stack switch that comes with it.
func BenchmarkCallDirect(b *testing.B) {
	sym := fixtureB(b)
	fn := sym("cb_add")
	args := &asm.Args{I: [6]uintptr{1, 2}}
	asm.Probe()

	b.ReportAllocs()
	for b.Loop() {
		_, _ = asm.Call(fn, args)
	}
}

func BenchmarkCallReentrant(b *testing.B) {
	sym := fixtureB(b)
	fn := sym("cb_add")
	args := &asm.Args{I: [6]uintptr{1, 2}}

	b.ReportAllocs()
	for b.Loop() {
		_, _ = asm.CallReentrant(fn, args)
	}
}

// BenchmarkCallbackThroughC prices a callback round trip: C++ calls the Go
// function through the trampoline and the runtime's cgo callback entry.
func BenchmarkCallbackThroughC(b *testing.B) {
	sym := fixtureB(b)
	fn := sym("cb_apply")

	cb, err := callback.Register(func(a *callback.Args) { a.Ret = a.I[0] + a.I[1] })
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = callback.Release(cb) }()

	args := &asm.Args{I: [6]uintptr{cb, 1, 2}}
	b.ReportAllocs()
	for b.Loop() {
		_, _ = asm.CallReentrant(fn, args)
	}
}
