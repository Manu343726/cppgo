package cppgo_test

import (
	"errors"
	"fmt"

	"github.com/Manu343726/cppgo"
)

// ExampleNewObject shows the ownership rule Object implements: the release
// function is the destructor, and it runs when the last reference goes, not
// when the first one is dropped.
func ExampleNewObject() {
	obj := cppgo.NewObject(16, 8, func() { fmt.Println("destructor ran") })
	fmt.Println("address is set:", obj.Addr() != 0)

	shared := obj
	shared.Retain()

	obj.Release() // the shared reference keeps the storage alive
	fmt.Println("still alive")

	shared.Release() // the last reference runs the destructor
	// Output:
	// address is set: true
	// still alive
	// destructor ran
}

// ExamplePtr shows the one thing Ptr and StrPtr do without a foreign call: an
// empty slice or string becomes the null pointer, so the callee sees a zero
// length and an address it must not dereference.
func ExamplePtr() {
	values := []float64{1, 2, 3}

	fmt.Println(cppgo.Ptr(values) != nil)
	fmt.Println(cppgo.Ptr[float64](nil) == nil)
	fmt.Println(cppgo.StrPtr("") == nil)
	// Output:
	// true
	// true
	// true
}

// ExampleOpen shows the failure Open reports, and that the cause stays in the
// error chain, so a caller can tell a missing file from an empty path.
func ExampleOpen() {
	_, err := cppgo.Open("")

	fmt.Println(err)

	var openErr *cppgo.OpenError
	fmt.Println(errors.As(err, &openErr), openErr.Path == "")
	// Output:
	// cppgo: open "": empty library path
	// true true
}

// ExampleCallError shows the error shape a generated binding returns: the C++
// declaration in the message, and the native failure reachable with errors.As.
func ExampleCallError() {
	err := &cppgo.CallError{
		Symbol: "mathx::checked_div",
		Err:    &cppgo.NativeError{Message: "division by zero"},
	}

	fmt.Println(err)

	var native *cppgo.NativeError
	if errors.As(err, &native) {
		fmt.Println("native message:", native.Message)
	}
	// Output:
	// cppgo: mathx::checked_div: division by zero
	// native message: division by zero
}
