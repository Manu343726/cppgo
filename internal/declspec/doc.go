//go:build stackalloc

// Package declspec is the prototype behind the manual-declaration section of
// docs/dev/stack-allocation.md (#40, feeding #44): the rules by which a
// hand-written Go signature may override what the generator would emit, and the
// cases it must refuse.
//
// It is a *model*, not the emitter. It takes the facts libclang already gives
// the generator (kind, const, grouping, noexcept, throwing) as structs and
// answers accept-or-reject with a reason. Wiring it into internal/gen, reading
// the override file, and diffing it in cppgo check is #44.
//
// It is behind the stackalloc build tag, so the production build, vet, test and
// coverage runs do not compile it.
package declspec
