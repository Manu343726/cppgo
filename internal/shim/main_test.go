package shim_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// The two artifacts are built once per test binary: compiling them is the point
// of the fixture, not something each test should pay for.
var (
	buildOnce sync.Once
	buildDir  string
	buildErr  error
)

func buildFixture() (string, error) {
	gxx, err := lookPath("g++")
	if err != nil {
		return "", err
	}

	dir, err := os.MkdirTemp("", "cppgo-shim-")
	if err != nil {
		return "", err
	}

	user := filepath.Join(dir, "libuser.so")
	shim := filepath.Join(dir, "libcppgo_shim.so")

	// The user's build: their source, their header, no cppgo file in it.
	if out, err := runCommand(gxx, "-O1", "-fPIC", "-shared", "-std=c++17", "-I", "testdata",
		"-o", user, "testdata/user.cpp"); err != nil {
		return dir, fmt.Errorf("build user library: %w\n%s", err, out)
	}

	// cppgo's build: its own translation unit, linked against the user's
	// library, produced as an object of its own.
	if out, err := runCommand(gxx, "-O1", "-fPIC", "-shared", "-std=c++17", "-I", "testdata",
		"-o", shim, "testdata/shim.cpp", user, "-Wl,-rpath,"+dir); err != nil {
		return dir, fmt.Errorf("build shim: %w\n%s", err, out)
	}

	return dir, nil
}

// TestMain removes the built artifacts once the suite is done.
func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}
