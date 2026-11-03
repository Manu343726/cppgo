// Package cppgo is a stand-in for the runtime package, for generator tests.
package cppgo

import (
	"errors"
	"fmt"
	"unsafe"
)

type Handle uintptr

type Library struct {
	path string
	syms map[string]uintptr
}

var Symbols = map[string]uintptr{}
var lastErr string

func Open(path string) (*Library, error) { return &Library{path: path}, nil }
func (l *Library) Close() error          { return nil }
func (l *Library) Path() string          { return l.path }
func SetLastError(s string)              { lastErr = s }
func ProbeStack()                        {}
func KeepAlive(args ...any)              {}
func BoolInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
func Ptr[T any](s []T) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(&s[0])
}
func StrPtr(s string) unsafe.Pointer {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Pointer(unsafe.StringData(s))
}

func (l *Library) Symbol(name string) (uintptr, error) {
	a, ok := Symbols[name]
	if !ok {
		return 0, fmt.Errorf("%s: symbol %q not found", l.path, name)
	}
	return a, nil
}

func LastError(l *Library) error {
	if lastErr == "" {
		return nil
	}
	return errors.New(lastErr)
}
