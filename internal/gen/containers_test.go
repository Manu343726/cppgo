package gen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// containersConfig is the recursive-container fixture's canonical run.
func containersConfig() Config {
	return fixtureConfig("containers", "containers.hpp")
}

// TestContainerShapesAreRecursive pins the contract's rule for std containers:
// a vector maps to a slice, and applying it again gives a slice of slices — for
// the plain, the nested and the record shapes, as a parameter, an out-parameter
// and a result.
func TestContainerShapesAreRecursive(t *testing.T) {
	rendered, err := RenderResult(containersConfig())
	require.NoError(t, err)
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	for _, want := range []string{
		// A vector of a scalar and of a mirrored record.
		"func Total(values []int32) (int32, error)",
		"func Fill(values *[]int32) error",
		"func Sequence(count int32) ([]int32, error)",
		"func TotalPoints(points []Point) (int32, error)",
		"func Points(count int32) ([]Point, error)",
		// A vector of vectors, which the same rule makes [][]T.
		"func TotalNested(values [][]int32) (int32, error)",
		"func FillNested(values *[][]int32) error",
		"func Nested(count int32) ([][]int32, error)",
		"func TotalNestedPoints(points [][]Point) (int32, error)",
		"func NestedPoints(count int32) ([][]Point, error)",
		// A vector of opaque records: one Go value per element.
		"func TotalCounters(counters []*Counter) (int32, error)",
		"func AddCounter(counters *[]*Counter) error",
		"func Counters(count int32) ([]*Counter, error)",
	} {
		assert.Contains(t, goFile, want)
	}
}

// TestNestedContainerWire pins the wire a vector of vectors crosses as: one flat
// element buffer and the bounds of every inner vector, so the C++ side can
// rebuild the nesting without the register file having to describe it.
func TestNestedContainerWire(t *testing.T) {
	rendered, err := RenderResult(containersConfig())
	require.NoError(t, err)
	goFile, shim, header := "", "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case GenFileName:
			goFile = string(out.Data)
		case ShimSourceFileName:
			shim = string(out.Data)
		case ShimHeaderFileName:
			header = string(out.Data)
		}
	}
	// The Go side flattens the caller's slices and records each inner length.
	assert.Contains(t, goFile, "values_bounds = make([]uint64, len(values)+1)")
	assert.Contains(t, goFile, "copy(values_data[cppgo_at:], cppgo_inner)")
	assert.Contains(t, goFile, "values_bounds[cppgo_i+1] = uint64(cppgo_at)")
	// The thunk takes the buffer, the bounds and the outer length, and the C++
	// side rebuilds the inner vectors from them.
	assert.Contains(t, header, "const size_t * values_bounds")
	assert.Contains(t, shim, "std::vector<std::vector<int32_t>> cppgo_values;")
	assert.Contains(t, shim, "cppgo_values.emplace_back(static_cast<const int32_t *>(values_data) + values_bounds[cppgo_i], static_cast<const int32_t *>(values_data) + values_bounds[cppgo_i + 1]);")
	// The result comes back the same way: a buffer plus bounds, freed by the Go
	// side once it has copied the nesting out.
	assert.Contains(t, goFile, "cppgoBounds := unsafe.Slice((*uint64)(cppgoBounds), int(cppgoSize)+1)")
}

// TestOpaqueContainerWire pins the two directions of a vector of records: the
// element addresses in, a block of objects out whose destructors the Go owner
// runs once, at the last view's Close.
func TestOpaqueContainerWire(t *testing.T) {
	rendered, err := RenderResult(containersConfig())
	require.NoError(t, err)
	goFile, shim, header := "", "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case GenFileName:
			goFile = string(out.Data)
		case ShimSourceFileName:
			shim = string(out.Data)
		case ShimHeaderFileName:
			header = string(out.Data)
		}
	}
	assert.Contains(t, header, "void * const * counters_elems")
	assert.Contains(t, shim, "cppgo_counters.push_back(*static_cast<const ::containers::Counter *>(static_cast<void * const *>(counters_elems)[cppgo_i]));")
	assert.Contains(t, shim, "new (cppgo_block + cppgo_i) ::containers::Counter(cppgo_value[cppgo_i]);")
	// The Go owner: one object, one reference per element, and the destructor of
	// every element at the last Close.
	assert.Contains(t, goFile, "cppgoBlock := cppgo.NewObject(1, 1, func() {")
	assert.Contains(t, goFile, "cppgoBlock.Retain()")
	assert.Contains(t, goFile, "cppgoValue[cppgo_i] = &Counter{obj: cppgoBlock, addr: uintptr(cppgoData) + uintptr(cppgo_i)*")
	assert.Contains(t, goFile, "cppgoFree(cppgoFreeSym, cppgoData)")
}

// TestContainerElementRefusals reports the elements a container cannot hold yet:
// a standard-library element with no element-wise mapping, and a type the model
// does not know. Each is one declaration the emitter cannot carry, so it is
// named and the rest of the header binds.
func TestContainerElementRefusals(t *testing.T) {
	for _, tc := range []struct {
		name      string
		parameter string
		want      string
		class     string
	}{
		{"vector of strings", "std::vector<std::string> &", "standard-library element", "unsupported-std"},
		{"vector of unknown", "std::vector<Missing> &", "not a scalar or a known record", "unmodeled-type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			header := filepath.Join(t.TempDir(), "refused.hpp")
			require.NoError(t, os.WriteFile(header, []byte(`
#include <string>
#include <vector>
namespace refused {
struct Missing;
void take(`+tc.parameter+`);
}
`), 0o644))
			rendered, err := RenderResult(Config{Headers: []string{header}, Package: "refused", Library: "librefused", Out: "."})
			require.NoError(t, err)
			joined := joinWarnings(rendered.Warnings)
			assert.Contains(t, joined, "refused::take")
			assert.Contains(t, joined, tc.want)
			assert.Equal(t, tc.class, classifyRefusal("refused::take("+tc.parameter+")", joined))
		})
	}
}

// TestElementShapeAndCTypes covers the two small decisions every container wire
// makes: how its element crosses, and the C type the prototype names for it.
func TestElementShapeAndCTypes(t *testing.T) {
	assert.Equal(t, containerFlat, elementShape(nil))
	assert.Equal(t, containerNested, elementShape(&goType{Kind: goSlice}))
	assert.Equal(t, containerOpaque, elementShape(&goType{Record: &recordModel{GoName: "Thing"}}))
	assert.Equal(t, containerFlat, elementShape(&goType{Record: &recordModel{GoName: "Point", Mirrored: true}}))

	mirrored := &goType{Kind: goRecord, Cxx: "::ns::Point", Record: &recordModel{GoName: "Point", Mirrored: true}}
	assert.Equal(t, "void *", containerCTypes(mirrored), "a record crosses as a pointer to void")
	assert.Equal(t, "int32_t *", containerCTypes(&goType{Kind: goInteger, Cxx: "int32_t"}))
	assert.Equal(t, "const int32_t *", containerCTypes(&goType{Kind: goInteger, Cxx: "int32_t", Const: true}))
}

// TestNilElementCheck covers both answers a container of records gives to a nil
// or closed element: an error in a binding that has an error channel, and a
// panic in one that does not. C++ would dereference the address either way.
func TestNilElementCheck(t *testing.T) {
	b := &builder{}
	errorForm := b.nilElementCheck(&binding{ErrorResult: true}, "counters")
	assert.Contains(t, errorForm, "cppgo_item == nil || cppgo_item.obj == nil")
	assert.Contains(t, errorForm, "return cppgo.ErrClosed")

	panicForm := b.nilElementCheck(&binding{sym: cxx.Symbol{Kind: cxx.FreeFunc, QualifiedName: "ns::take"}}, "counters")
	assert.Contains(t, panicForm, "panic(")
	assert.Contains(t, panicForm, "ns::take")
	assert.Contains(t, panicForm, "a nil element in counters")
	assert.NotContains(t, panicForm, "return")
}
