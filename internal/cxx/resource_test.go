package cxx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestResourceDirDiscovered checks that the platform's clang resource
// directory is found next to the loaded libclang.
func TestResourceDirDiscovered(t *testing.T) {
	library := skipWithoutLibclang(t)
	t.Setenv(ResourceDirEnv, "")
	dir := resourceDir(library.Path())
	requireNotEmpty := dir != ""
	if !requireNotEmpty {
		t.Skipf("no clang resource directory next to %s", library.Path())
	}
	_, err := os.Stat(filepath.Join(dir, "include", "stddef.h"))
	assert.NoError(t, err)
}

// TestResourceDirEnvOverride checks the escape hatch.
func TestResourceDirEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(ResourceDirEnv, dir)
	assert.Equal(t, dir, resourceDir("/nonexistent/libclang.so"))
}

// TestResourceDirNotFound drives the search to a miss and checks it answers "".
func TestResourceDirNotFound(t *testing.T) {
	t.Setenv(ResourceDirEnv, "")
	saved := resourcePatterns
	resourcePatterns = func(string) []string { return nil }
	t.Cleanup(func() { resourcePatterns = saved })
	assert.Equal(t, "", resourceDir("/nonexistent/libclang.so"))
}

// TestResourceDirHandle checks the accessor the shim self-check uses.
func TestResourceDirHandle(t *testing.T) {
	library := skipWithoutLibclang(t)
	t.Setenv(ResourceDirEnv, "")
	assert.Equal(t, resourceDir(library.Path()), library.ResourceDir())
}

// TestResourceDirGlobError covers a malformed pattern: the search skips it
// instead of panicking.
func TestResourceDirGlobError(t *testing.T) {
	t.Setenv(ResourceDirEnv, "")
	saved := resourcePatterns
	resourcePatterns = func(string) []string { return []string{"["} }
	t.Cleanup(func() { resourcePatterns = saved })
	assert.Equal(t, "", resourceDir("/nonexistent/libclang.so"))
}
