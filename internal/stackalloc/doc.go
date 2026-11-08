//go:build stackalloc

// Package stackalloc is the experiment behind docs/dev/stack-allocation.md: a
// Go value type that holds a C++ object in its own frame, the measurements that
// compare it with the heap Object the generator emits, and the demonstrations
// of why the value form is not safe in general.
//
// It is behind the stackalloc build tag, so the production build, vet, test and
// coverage runs do not compile it. Reproduce the numbers in the document with:
//
//	go test -tags stackalloc ./internal/stackalloc -v
//	go test -tags stackalloc ./internal/stackalloc -bench . -benchmem
//	go build -tags stackalloc -gcflags=-m ./internal/stackalloc
//	go vet -tags stackalloc ./internal/stackalloc
//
// The fixture under testdata/ is compiled on demand with g++, and every test
// and benchmark skips when no compiler is available, exactly like
// internal/testlib.
package stackalloc
