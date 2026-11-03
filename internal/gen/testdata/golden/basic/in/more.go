package mathx

import (
	"fmt"
	"strings"
)

// Sum sums a byte slice.
//
//cppgo:external sum
func Sum(v []uint8) uint64

// Boom always fails. The body below is reference documentation: the generator
// replaces it with a delegation to the generated wrapper.
//
//cppgo:external boom
func Boom(code int32) error {
	if code > 0 {
		return fmt.Errorf("code %d", code)
	}
	return nil
}

// Lower is user code and must survive untouched.
func Lower(s string) string {
	return strings.ToLower(s)
}
