package cppgo

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The black-box tests in loadfs_test.go cover the loader through its exported
// surface. These reach the extractor's own file-system failures, which a
// well-behaved file system does not produce: a directory that is gone by the
// time an object is written, a path whose parent is a file, and a digest that
// cannot be read.

func TestMaterialiseReportsAnUnusableDirectory(t *testing.T) {
	fsys := fstest.MapFS{"libx.so": &fstest.MapFile{Data: []byte("payload"), Mode: 0o644}}
	missing := filepath.Join(t.TempDir(), "absent")

	err := materialize(fsys, missing, object{name: "libx.so", source: "libx.so", size: 7})
	require.Error(t, err)

	var extractErr *ExtractError
	require.ErrorAs(t, err, &extractErr)
	assert.Equal(t, "libx.so", extractErr.Name)
	assert.Equal(t, missing, extractErr.Dir)
	assert.Contains(t, err.Error(), missing)
}

func TestReusableRejectsWhatItCannotInspect(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	tests := []struct {
		name    string
		target  string
		wantErr bool
	}{
		{name: "absent", target: filepath.Join(dir, "absent")},
		{name: "parent is a file", target: filepath.Join(file, "child"), wantErr: true},
		{name: "a directory", target: dir},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reuse, err := reusable(test.target, object{name: "libx.so", size: 7})
			if test.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			assert.False(t, reuse)
		})
	}
}

func TestDigestPathReportsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()

	_, err := digestPath(filepath.Join(dir, "absent"))
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// A directory opens and then refuses to be read.
	_, err = digestPath(dir)
	require.Error(t, err)
}
