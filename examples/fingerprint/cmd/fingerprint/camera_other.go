//go:build !linux

// The camera source on a platform without V4L2: the adapter is Linux's, and
// this is the honest answer everywhere else. A directory of frames still works,
// so the example is usable on any machine.

package main

import "fmt"

func newCameraSource(index int) (FrameSource, error) {
	return nil, fmt.Errorf("%w: camera %d: this build reads cameras through V4L2, which is Linux's", errSourceAbsent, index)
}
