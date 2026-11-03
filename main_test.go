package cppgo_test

import (
	"os"
	"testing"

	"github.com/Manu343726/cppgo/internal/testlib"
)

// TestMain removes the C++ fixture testlib compiles on demand.
func TestMain(m *testing.M) {
	code := m.Run()
	testlib.Cleanup()
	os.Exit(code)
}
