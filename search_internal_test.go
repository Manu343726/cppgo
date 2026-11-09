package cppgo

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLibraryPathEntries(t *testing.T) {
	separator := string(os.PathListSeparator)

	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{"unset", "", nil},
		{"one directory", "/opt/lib", []string{"/opt/lib"}},
		{"a list", "/a" + separator + "/b", []string{"/a", "/b"}},
		{"an object", "/opt/lib/libmathx.so.1", []string{"/opt/lib/libmathx.so.1"}},
		{
			"empty entries are dropped",
			separator + "/a" + separator + separator + "/b" + separator,
			[]string{"/a", "/b"},
		},
		{"separators only", separator + separator, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, libraryPathEntries(tc.value))
		})
	}
}

func TestIsSharedObject(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"libmathx.so", true},
		{"libmathx.so.1", true},
		{"libmathx.so.1.2.3", true},
		{"libcppgo_chain_zzz_base.so.1", true},
		{"libmathx.a", false},
		{"libmathx.so.bak", false},
		{"libmathx.so.debug", false},
		{"libmathx.so.1a", false},
		{"README.md", false},
		{"", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isSharedObject(tc.name))
		})
	}
}
