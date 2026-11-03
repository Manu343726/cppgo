// Package mathx holds the test the generated bindings are checked with: it
// reaches the real mathx::Vector inside a shared object built from
// internal/cxx/testdata/mathx, so what is proven is the ABI, not the text.
package mathx

import (
	"os"
	"testing"

	"github.com/Manu343726/cppgo"
)

// init points the generated bindings at the library the test built. The
// generated file declares CppgoLibraryPath for exactly this.
func init() {
	if path := os.Getenv("CPPGO_TEST_LIB"); path != "" {
		CppgoLibraryPath = path
	}
}

func TestVectorABI(t *testing.T) {
	// A constructor: `this` in RDI, x and y in XMM0 and XMM1, writing into
	// storage the generated wrapper allocated in Go.
	v, err := New(3, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if cppgo.Handle(v).IsNil() {
		t.Fatal("New returned a null handle")
	}

	// A const method returning a double: `this` in RDI, the value in XMM0.
	if got := v.Norm(); got != 5 {
		t.Fatalf("Norm = %v, want 5", got)
	}

	// A method taking a slice: (pointer, length) after `this`, in RSI and RDX.
	if got := v.Dot([]float64{2, 1}); got != 10 {
		t.Fatalf("Dot = %v, want 10", got)
	}
	if got := v.Dot(nil); got != 0 {
		t.Fatalf("Dot(nil) = %v, want 0", got)
	}

	// A second object, to prove the storage the constructor allocated is per
	// object and that the two are independent.
	w, err := New(-1, 0)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := w.Norm(); got != 1 {
		t.Fatalf("Norm of the second vector = %v, want 1", got)
	}
	if got := v.Norm(); got != 5 {
		t.Fatalf("the first vector changed: Norm = %v, want 5", got)
	}

	// A free function with two int arguments: RDI and RSI.
	if got := Add(2, 40); got != 42 {
		t.Fatalf("Add = %v, want 42", got)
	}

	// The destructors. Vector has a virtual destructor, so ~Vector is the
	// complete-object D1 entry: it destroys the object and leaves the Go
	// storage to the package, which drops it from its registry.
	v.Destroy()
	w.Destroy()
	cppgoObjectsMu.Lock()
	live := len(cppgoObjects)
	cppgoObjectsMu.Unlock()
	if live != 0 {
		t.Fatalf("%d objects still registered after both destructors", live)
	}
}
