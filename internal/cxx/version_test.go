package cxx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestClangVersionLooksSane pins the acceptance rule for the CXString smoke
// test. Distributions patch the banner, so a prefix match rejects a perfectly
// good libclang: that is exactly the bug that made every introspection test
// skip on a stock CI runner and dropped the coverage gate to 78%.
func TestClangVersionLooksSane(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"upstream", "clang version 22.1.8", true},
		{"debian", "Debian clang version 14.0.6", true},
		{"ubuntu", "Ubuntu clang version 18.1.3-1ubuntu1", true},
		{"apple", "Apple clang version 15.0.0 (clang-1500.3.9.4)", true},
		{"empty", "", false},
		{"garbage from a bad sret layout", "\x00\x00\x00\x00", false},
		{"unrelated string", "libclang", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, clangVersionLooksSane(tt.value))
		})
	}
}
