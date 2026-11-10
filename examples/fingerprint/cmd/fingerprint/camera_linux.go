//go:build linux

// The camera frame source: V4L2 read straight from the device, with no cgo and
// no dependency.
//
// Why here and not in the C++ library: the library's job is the image work, and
// reading a device is I/O. The boundary between them is what makes the rule
// hold — this file produces the frame's bytes exactly as the kernel handed them
// over, and the C++ side decodes or converts them. It never looks at a pixel:
// the only thing it does with the buffer is copy it.
//
// The ioctls and the offsets below are the kernel's UAPI, taken from
// <linux/videodev2.h>. They are numbers rather than cgo structs because the
// example is built with CGO_ENABLED=0 in one of its two test runs: a struct
// cannot cross that line, and a byte buffer with the fields written at their
// offsets can. Every offset is named with the field it is, and `make cpp-test`
// is not what proves them — a device would be. The adapter asks for MJPEG
// first, because that is the format whose decode the C++ side pays at a reduced
// scale, and falls back to YUYV, whose conversion is C++'s too.

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const (
	// VIDIOC_* ioctl requests, as <linux/videodev2.h> defines them:
	// _IOWR('V', n, struct) with the struct's size in the size field.
	vidiocQueryCap  = 0x80685600
	vidiocSFmt      = 0xc0d05605
	vidiocQueryBuf  = 0xc0585609
	vidiocReqBufs   = 0xc0145608
	vidiocQBuf      = 0xc058560f
	vidiocDQBuf     = 0xc0585611
	vidiocStreamOn  = 0x40045612
	vidiocStreamOff = 0x40045613

	// The struct sizes the requests above encode.
	sizeCapability     = 104
	sizeFormat         = 208
	sizeRequestBuffers = 20
	sizeBuffer         = 88

	// Field offsets inside those structs.
	capabilityCapabilities = 84
	capabilityDeviceCaps   = 88

	formatType        = 0
	formatWidth       = 8
	formatHeight      = 12
	formatPixelFormat = 16
	formatField       = 20
	formatSizeImage   = 28

	bufferIndex     = 0
	bufferType      = 4
	bufferBytesUsed = 8
	bufferMemory    = 60
	bufferOffset    = 64
	bufferLength    = 72

	bufferTypeVideoCapture = 1
	memoryMMAP             = 1
	fieldNone              = 1
	pixelFormatMJPEG       = 0x47504a4d
	pixelFormatYUYV        = 0x56595559
	capabilityStreaming    = 0x04000000
	capabilityVideoCapture = 0x00000001

	// The size the adapter asks for. A device that answers with another size
	// is used as it answered: the trigger works at a fixed size of its own.
	cameraWidth   = 640
	cameraHeight  = 480
	cameraBuffers = 4
)

// cameraSource is a V4L2 device streaming mmap'd buffers.
type cameraSource struct {
	file    *os.File
	fd      int
	index   int
	width   int
	height  int
	yuyv    bool
	buffers [][]byte
	frame   int
}

// newCameraSource opens /dev/video<index> and starts a stream.
func newCameraSource(index int) (FrameSource, error) {
	path := fmt.Sprintf("/dev/video%d", index)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: camera %d is %s (%v)", errSourceAbsent, index, path, err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("camera %d: %w", index, err)
	}
	source := &cameraSource{file: file, fd: int(file.Fd()), index: index}
	if err := source.start(); err != nil {
		file.Close()
		return nil, fmt.Errorf("camera %d: %w", index, err)
	}
	return source, nil
}

func (c *cameraSource) start() error {
	capability := make([]byte, sizeCapability)
	if err := ioctl(c.fd, vidiocQueryCap, capability); err != nil {
		return fmt.Errorf("VIDIOC_QUERYCAP: %w", err)
	}
	caps := getU32(capability, capabilityCapabilities)
	if deviceCaps := getU32(capability, capabilityDeviceCaps); deviceCaps != 0 {
		caps = deviceCaps
	}
	if caps&capabilityStreaming == 0 {
		return errors.New("the device does not support streaming I/O")
	}

	format := make([]byte, sizeFormat)
	found := false
	for _, candidate := range []uint32{pixelFormatMJPEG, pixelFormatYUYV} {
		for i := range format {
			format[i] = 0
		}
		putU32(format, formatType, bufferTypeVideoCapture)
		putU32(format, formatWidth, cameraWidth)
		putU32(format, formatHeight, cameraHeight)
		putU32(format, formatPixelFormat, candidate)
		putU32(format, formatField, fieldNone)
		if err := ioctl(c.fd, vidiocSFmt, format); err != nil {
			continue
		}
		if getU32(format, formatPixelFormat) != candidate {
			continue
		}
		c.width = int(getU32(format, formatWidth))
		c.height = int(getU32(format, formatHeight))
		c.yuyv = candidate == pixelFormatYUYV
		found = true
		break
	}
	if !found {
		return errors.New("the device offers neither MJPEG nor YUYV")
	}

	request := make([]byte, sizeRequestBuffers)
	putU32(request, 0, cameraBuffers)
	putU32(request, 4, bufferTypeVideoCapture)
	putU32(request, 8, memoryMMAP)
	if err := ioctl(c.fd, vidiocReqBufs, request); err != nil {
		return fmt.Errorf("VIDIOC_REQBUFS: %w", err)
	}
	count := int(getU32(request, 0))
	if count < 1 {
		return errors.New("the device granted no buffers")
	}

	buffer := make([]byte, sizeBuffer)
	c.buffers = make([][]byte, count)
	for i := 0; i < count; i++ {
		for j := range buffer {
			buffer[j] = 0
		}
		putU32(buffer, bufferIndex, uint32(i))
		putU32(buffer, bufferType, bufferTypeVideoCapture)
		putU32(buffer, bufferMemory, memoryMMAP)
		if err := ioctl(c.fd, vidiocQueryBuf, buffer); err != nil {
			c.unmap()
			return fmt.Errorf("VIDIOC_QUERYBUF %d: %w", i, err)
		}
		offset := int64(getU32(buffer, bufferOffset))
		length := int(getU32(buffer, bufferLength))
		mapped, err := syscall.Mmap(c.fd, offset, length, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			c.unmap()
			return fmt.Errorf("mmap buffer %d: %w", i, err)
		}
		c.buffers[i] = mapped
		if err := ioctl(c.fd, vidiocQBuf, buffer); err != nil {
			c.unmap()
			return fmt.Errorf("VIDIOC_QBUF %d: %w", i, err)
		}
	}

	streamType := make([]byte, 4)
	putU32(streamType, 0, bufferTypeVideoCapture)
	if err := ioctl(c.fd, vidiocStreamOn, streamType); err != nil {
		c.unmap()
		return fmt.Errorf("VIDIOC_STREAMON: %w", err)
	}
	return nil
}

func (c *cameraSource) Name() string { return fmt.Sprintf("camera:%d", c.index) }

func (c *cameraSource) Next() (Frame, error) {
	buffer := make([]byte, sizeBuffer)
	putU32(buffer, bufferType, bufferTypeVideoCapture)
	putU32(buffer, bufferMemory, memoryMMAP)
	if err := ioctl(c.fd, vidiocDQBuf, buffer); err != nil {
		return Frame{}, fmt.Errorf("VIDIOC_DQBUF: %w", err)
	}
	index := int(getU32(buffer, bufferIndex))
	used := int(getU32(buffer, bufferBytesUsed))
	if index < 0 || index >= len(c.buffers) || used <= 0 || used > len(c.buffers[index]) {
		// Hand the buffer back and say so: a frame of no bytes is a frame the
		// trigger would refuse anyway.
		_ = ioctl(c.fd, vidiocQBuf, buffer)
		return Frame{}, errors.New("the device returned an empty frame")
	}
	// The buffer goes back to the driver before anything else touches it, so
	// the device can fill it while this copy is on its way to the library.
	data := make([]byte, used)
	copy(data, c.buffers[index][:used])
	if err := ioctl(c.fd, vidiocQBuf, buffer); err != nil {
		return Frame{}, fmt.Errorf("VIDIOC_QBUF: %w", err)
	}
	frame := Frame{Index: c.frame, Name: fmt.Sprintf("camera:%d#%d", c.index, c.frame), Data: data}
	if c.yuyv {
		frame.YUYV = true
		frame.Width = c.width
		frame.Height = c.height
	}
	c.frame++
	return frame, nil
}

func (c *cameraSource) Close() error {
	streamType := make([]byte, 4)
	putU32(streamType, 0, bufferTypeVideoCapture)
	_ = ioctl(c.fd, vidiocStreamOff, streamType)
	c.unmap()
	return c.file.Close()
}

func (c *cameraSource) unmap() {
	for i, mapped := range c.buffers {
		if mapped != nil {
			_ = syscall.Munmap(mapped)
			c.buffers[i] = nil
		}
	}
}

// ioctl runs one request over a byte buffer. The buffer's own length is what
// the request above encodes, which is why they are checked together.
func ioctl(fd int, request uintptr, buffer []byte) error {
	if len(buffer) == 0 {
		return errors.New("ioctl: empty buffer")
	}
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		return errno
	}
	return nil
}

func putU32(buffer []byte, at int, value uint32) {
	binary.LittleEndian.PutUint32(buffer[at:], value)
}

func getU32(buffer []byte, at int) uint32 {
	return binary.LittleEndian.Uint32(buffer[at:])
}
