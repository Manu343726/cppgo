//go:build stackalloc

// Package zerocopy is the experiment behind the parameter-mapping section of
// docs/dev/stack-allocation.md (#40, feeding #43): for each C++ parameter shape
// the design table names, whether Go memory can be referenced directly, and
// what the copy costs when it cannot.
//
// It is behind the stackalloc build tag, so the production build, vet, test and
// coverage runs do not compile it. Reproduce with:
//
//	go test -tags stackalloc ./internal/zerocopy -v
//	go test -tags stackalloc ./internal/zerocopy -bench . -benchmem
package zerocopy
