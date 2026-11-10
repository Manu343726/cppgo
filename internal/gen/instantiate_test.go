package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// instantiateConfig is the manifest-instantiation fixture's canonical run: the
// record is bound, and its member template plus the free function template are
// named by type argument.
func instantiateConfig() Config {
	cfg := fixtureConfig("instantiate", "instantiate.hpp")
	cfg.Bind = []string{"ins::Box"}
	cfg.Instantiate = []string{"ins::Box::get<int>()", "ins::twice<int>(int)"}
	return cfg
}

// TestParseTemplateRequest pins the manifest's syntax: the type argument list,
// the parameter count that selects an overload, the const marker, and every way
// the text can be wrong.
func TestParseTemplateRequest(t *testing.T) {
	req, err := parseTemplateRequest("cv::Mat::at<unsigned char>(int, int)")
	require.NoError(t, err)
	assert.Equal(t, "cv::Mat", req.Record)
	assert.Equal(t, "at", req.Member)
	assert.Equal(t, 2, req.Arity)
	require.Len(t, req.Args, 1)
	assert.Equal(t, "unsigned char", req.ArgSpellings[0])

	req, err = parseTemplateRequest("ns::thing<double>(double) const")
	require.NoError(t, err)
	// The parser cannot tell a namespace from a record: "ns" is the candidate
	// owner, and the resolver falls back to the free function when no record
	// named ns is bound.
	assert.Equal(t, "ns", req.Record)
	assert.Equal(t, "thing", req.Member)
	assert.Equal(t, "ns::thing", req.Name)
	assert.True(t, req.Const)
	assert.Equal(t, 1, req.Arity)

	req, err = parseTemplateRequest("ns::make<int>()")
	require.NoError(t, err)
	assert.Equal(t, 0, req.Arity, "an empty parameter list is arity zero")

	for _, bad := range []string{
		"ns::thing<>(int)",
		"ns::thing<int>(int",
		"ns::thing<int>)",
		"ns::nothing",
	} {
		_, err := parseTemplateRequest(bad)
		assert.Error(t, err, bad)
	}
}

// TestMatchingOpen covers the bracket matcher the parser leans on.
func TestMatchingOpen(t *testing.T) {
	assert.Equal(t, 4, matchingOpen("name(a, b)", 9, '(', ')'))
	assert.Equal(t, -1, matchingOpen("name(a, b", 8, '(', ')'))
	assert.Equal(t, 0, matchingOpen("(x)", 2, '(', ')'))
}

// TestInstantiateBuild binds the member and the free function instantiation and
// checks the Go API and the C++ call the manifest produced.
func TestInstantiateBuild(t *testing.T) {
	rendered, err := RenderResult(instantiateConfig())
	require.NoError(t, err)
	goFile, shim := "", ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
		if out.Name == ShimSourceFileName {
			shim = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func (b *Box) GetInt32() int32")
	assert.Contains(t, goFile, "func TwiceInt32(arg0 int32) int32")
	assert.Contains(t, shim, "->get<int>()", "the explicit template argument reaches the call")
	assert.Contains(t, shim, "::ins::twice<int>(")

	// The two instantiations are distinct bindings: the same signature with two
	// type arguments must not share a thunk.
	cfg := instantiateConfig()
	cfg.Instantiate = append(cfg.Instantiate, "ins::Box::get<double>()")
	rendered, err = RenderResult(cfg)
	require.NoError(t, err)
	goFile = ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func (b *Box) GetInt32() int32")
	assert.Contains(t, goFile, "func (b *Box) GetFloat64() float64")
}

// TestInstantiateBuildErrors covers each manifest mistake the resolution
// reports: an unknown template, a member of a record the manifest does not bind,
// a member template the record does not declare, and the wrong argument count.
func TestInstantiateBuildErrors(t *testing.T) {
	for _, tc := range []struct {
		name      string
		bind      []string
		instances []string
		want      string
	}{
		{"unknown function", []string{"ins::Box"}, []string{"ins::nope<int>(int)"}, "no declaration in the headers matches"},
		{"unknown member", []string{"ins::Box"}, []string{"ins::Box::nope<int>()"}, "no declaration in the headers matches"},
		{"wrong arity", []string{"ins::Box"}, []string{"ins::Box::get<int>(int)"}, "no declaration in the headers matches"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := fixtureConfig("instantiate", "instantiate.hpp")
			cfg.Bind = tc.bind
			cfg.Instantiate = tc.instances
			_, err := Render(cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestResolveRecordRequests covers the record half of the same mechanism with
// the fixture's Fixed<T, N>: the entry synthesizes the record, and a repeat is
// not added twice.
func TestResolveRecordRequests(t *testing.T) {
	cfg := fixtureConfig("params", "params.hpp")
	cfg.Bind = []string{"params::Holder"}
	cfg.Instantiate = []string{"params::Fixed<double, 4>", "params::Fixed<double, 4>"}
	model, err := build(cfg)
	require.NoError(t, err)
	found := false
	for _, record := range model.records {
		if record.CxxName == "params::Fixed<double,4>" {
			found = true
		}
	}
	assert.True(t, found, "the specialization is part of the emitted API")

	// A record the pattern does not declare is reported, not silently ignored.
	cfg.Instantiate = []string{"params::Nope<int>"}
	_, err = build(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no instantiation")
}

// TestSplitRecordInstantiation pins the record entry's parser.
func TestSplitRecordInstantiation(t *testing.T) {
	name, args, ok := splitRecordInstantiation("params::Fixed<double, 4>")
	require.True(t, ok)
	assert.Equal(t, "params::Fixed", name)
	require.Len(t, args, 2)
	assert.Equal(t, "double", args[0].Spelling)
	assert.Equal(t, int64(4), args[1].Value)

	for _, bad := range []string{"params::Fixed", "params::Fixed<>", ""} {
		_, _, ok := splitRecordInstantiation(bad)
		assert.False(t, ok, bad)
	}
}

// TestStdVectorOutParameter covers the non-const vector reference: the Go
// parameter is a pointer to the slice, the shim builds the vector and copies the
// callee's result back through a block the Go side frees.
func TestStdVectorOutParameter(t *testing.T) {
	cfg := fixtureConfig("outvector", "outvector.hpp")
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	goFile, shim := "", ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
		if out.Name == ShimSourceFileName {
			shim = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func Fill(values *[]int32) error", "an out vector is a pointer to a slice")
	assert.Contains(t, goFile, "cppgoFree(", "the copied block is released")
	assert.Contains(t, shim, "std::vector<int32_t> cppgo_values;")
	assert.Contains(t, shim, "cppgo_copy", "the vector is copied out element by element")
	assert.Contains(t, goFile, "func Total(values []int32) (int32, error)", "a const vector stays an input slice")
}

// TestStdStringOutReferenceIsRefused keeps the other standard-library reference
// a warning: only a vector has a copy-back shape.
func TestStdStringOutReferenceIsRefused(t *testing.T) {
	cfg := fixtureConfig("errors", "stdinout.hpp")
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	assert.Contains(t, joinWarnings(rendered.Warnings), "non-const standard-library reference")
}

// TestSelectionHelpers covers the collision and template-suffix rules directly.
func TestSelectionHelpers(t *testing.T) {
	clone := &binding{GoName: "Clone", IsClone: true}
	b := &builder{mapper: &mapper{records: map[string]*recordModel{}, enums: map[string]*enumModel{}}}
	taken := map[string]bool{"Clone": true}
	assert.Equal(t, "Copy", b.collisionName(clone, taken))
	taken["Copy"] = true
	assert.True(t, strings.HasPrefix(b.collisionName(clone, taken), "Copy"))

	constMethod := &binding{sym: cxx.Symbol{QualifiedName: "ns::T::m", IsConst: true}, Params: []*goParam{{Type: &goType{Go: "int32"}}}}
	constMethod.GoName = "M"
	taken = map[string]bool{"M": true}
	assert.Equal(t, "MInt32", b.collisionName(constMethod, taken), "the parameter suffix is tried first")
	taken["MInt32"] = true
	assert.Equal(t, "MInt32Const", b.collisionName(constMethod, taken), "then the const marker")
	taken["MInt32Const"] = true
	assert.True(t, strings.HasPrefix(b.collisionName(constMethod, taken), "MInt32Const"))

	withArgs := &binding{templateArgs: []string{"unsigned char", "double"}}
	assert.Equal(t, "Uint8Float64", b.templateSuffix(withArgs))
	assert.Equal(t, "", b.templateSuffix(&binding{}))
}

// TestInstantiateNestedContainerTemplate covers a function template whose
// parameter is a container of containers: the substitution produces a
// standard-library spelling, and the recursive mapping has to recognise it after
// the substitution, not only at parse time.
func TestInstantiateNestedContainerTemplate(t *testing.T) {
	cfg := fixtureConfig("instantiate", "instantiate.hpp")
	cfg.Bind = []string{"ins::Box"}
	cfg.Instantiate = []string{"ins::totalNested<int>(const std::vector<std::vector<int>> &)"}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func TotalNestedInt32(arg0 [][]int32) (int32, error)")
}

// TestInstantiateRecordTypeArgument covers a record as a template argument: the
// OpenCV proxies take containers of records, and the argument is the type the
// header declares, not a scalar.
func TestInstantiateRecordTypeArgument(t *testing.T) {
	header := filepath.Join(t.TempDir(), "proxy.hpp")
	require.NoError(t, os.WriteFile(header, []byte(`
#include <vector>
namespace proxy {
struct Point {
    int x;
    int y;
};
class Proxy {
public:
    Proxy() noexcept;
    template <typename T>
    Proxy(const std::vector<T> &values) noexcept;
};
int count(const Proxy &values) noexcept;
}
`), 0o644))
	cfg := Config{
		Headers:     []string{header},
		Package:     "proxy",
		Library:     "libproxy",
		Out:         ".",
		Bind:        []string{"proxy::count"},
		Instantiate: []string{"proxy::Proxy::Proxy<proxy::Point>(const std::vector<proxy::Point> &)"},
	}
	rendered, err := RenderResult(cfg)
	require.NoError(t, err)
	goFile := ""
	for _, out := range rendered.Outputs {
		if out.Name == GenFileName {
			goFile = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func NewProxyPoint(arg0 []Point)")
}
