package asm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestReentrantWrapperIsDeclared pins the declaration asmdecl checks: the
// wrapper runtime.cgocall enters is written in reentrant_amd64.s, so its Go
// declaration is never called from Go and this reference is what keeps it from
// reading as dead code.
func TestReentrantWrapperIsDeclared(t *testing.T) {
	assert.NotNil(t, reentrantWrapper)
}
