//go:build stackalloc

package stackalloc

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C++ type ids used by the fixture's trait table.
const (
	typeCell    int32 = 0
	typePoint   int32 = 1
	typeSelfref int32 = 2
	typeText    int32 = 3
	typePoly    int32 = 4
)

// Trait bits reported by cppgo_traits.
const (
	traitTriviallyCopyable = 1 << 0
	traitPolymorphic       = 1 << 1
	traitTriviallyDestr    = 1 << 2
	traitStandardLayout    = 1 << 3
	traitTriviallyReloc    = 1 << 4
)

// TestTraitTable is the compiler's answer, not the analysis's: which of the
// fixture's types a byte move is defined for.
func TestTraitTable(t *testing.T) {
	sym := fixture(t)
	traits := sym("cppgo_traits")

	cases := []struct {
		name string
		id   int32
		want uint32
	}{
		// A real constructor and destructor: not trivially copyable, so not a
		// candidate for a value form at all.
		{name: "cell", id: typeCell, want: traitStandardLayout},
		// The shape cppgo already mirrors as a Go struct.
		{name: "point", id: typePoint, want: traitTriviallyCopyable | traitTriviallyDestr | traitStandardLayout},
		// Trivially copyable and still broken by a move: it stores its own
		// address in a member.
		{name: "selfref", id: typeSelfref, want: traitTriviallyCopyable | traitTriviallyDestr | traitStandardLayout},
		// Owns a std::string, so it is standard layout and nothing else: the
		// copy of the bytes is not a copy of the object.
		{name: "text", id: typeText, want: traitStandardLayout},
		// A class with virtual functions has no trivial copy constructor, so
		// polymorphic types never qualify.
		{name: "poly", id: typePoly, want: traitPolymorphic},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := uint32(callWith(traits, [6]uintptr{uintptr(tc.id)}))
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestTrivialRelocatabilityMacro reports what the compiler implements. The
// document quotes this number rather than assuming a standard: the experiment
// is written against C++17, and the C++26 trait is only a measurement when the
// library has it.
func TestTrivialRelocatabilityMacro(t *testing.T) {
	sym := fixture(t)
	got := int64(callNoArgs(sym("cppgo_trivial_relocatability_macro")))
	t.Logf("__cpp_trivial_relocatability = %d", got)
	assert.True(t, got == -1 || got > 0, "unexpected macro value %d", got)
}

// TestValueReturnRelocatesTheConstructor is the value form's central defect,
// measured: the C++ constructor runs at the storage of a local inside the
// constructor function, and the value the caller receives is a copy of those
// bytes at another address. For cppgo_cell that copy is not a copy of a
// trivially copyable object: the class has a non-trivial destructor, so the
// standard does not define it.
func TestValueReturnRelocatesTheConstructor(t *testing.T) {
	sym := fixture(t)
	construct := sym("cppgo_cell_construct")
	constructedAt := sym("cppgo_construct_this")

	var c Cell
	c = NewCell(construct, 3, 4)

	at := callNoArgs(constructedAt)
	require.NotZero(t, at, "the constructor did not run")
	assert.NotEqual(t, c.Addr(), at,
		"the constructor ran at the caller's address, so the value was not copied")

	// The bytes arrived, which is why the defect is invisible from Go.
	assert.Equal(t, 25.0, c.Norm(sym("cppgo_cell_norm")))
}

// TestSelfReferenceBreaksOnALegalByteCopy separates the two questions the trait
// answers and the invariant the object has: cppgo_selfref is trivially
// copyable, so copying its object representation is defined, and the copy no
// longer points at itself.
func TestSelfReferenceBreaksOnALegalByteCopy(t *testing.T) {
	sym := fixture(t)
	construct := sym("cppgo_selfref_construct")
	valid := sym("cppgo_selfref_valid")
	move := sym("cppgo_bytes_move")

	src := make([]byte, 32)
	dst := make([]byte, 32)
	require.EqualValues(t, 0, callWith(construct, [6]uintptr{uintptr(unsafe.Pointer(&src[0]))}))

	_, live := callInt32Out(valid, uintptr(unsafe.Pointer(&src[0])))
	assert.Equal(t, int32(1), live)

	// The same bytes, at another address: a legal copy of a trivially copyable
	// object, and the object is no longer valid.
	callVoid(move, uintptr(unsafe.Pointer(&dst[0])), uintptr(unsafe.Pointer(&src[0])), 16)

	_, copied := callInt32Out(valid, uintptr(unsafe.Pointer(&dst[0])))
	assert.Equal(t, int32(0), copied,
		"a byte-copied self-reference still points at the source")
}
