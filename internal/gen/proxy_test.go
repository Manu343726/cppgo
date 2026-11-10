package gen

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxyConfig is the proxy fixture's canonical run: the two declarations are
// bound with the container the manifest names in place of the proxy parameter.
func proxyConfig() Config {
	cfg := fixtureConfig("proxy", "proxy.hpp")
	cfg.Proxy = []string{
		"prx::trace@0=std::vector<std::vector<prx::Point>> &",
		"prx::total@0=std::vector<std::vector<prx::Point>>",
	}
	return cfg
}

// TestProxyParameterIsTheContainer pins the shape: the binding takes the
// container, the out-parameter form is a pointer to it, and the shim constructs
// the declared proxy around the C++ container it built.
func TestProxyParameterIsTheContainer(t *testing.T) {
	rendered, err := RenderResult(proxyConfig())
	require.NoError(t, err)
	goFile, shim := "", ""
	for _, out := range rendered.Outputs {
		switch out.Name {
		case GenFileName:
			goFile = string(out.Data)
		case ShimSourceFileName:
			shim = string(out.Data)
		}
	}
	assert.Contains(t, goFile, "func Trace(sink *[][]Point) error")
	assert.Contains(t, goFile, "func Total(source [][]Point) (int32, error)")
	assert.Contains(t, shim, "::prx::Sink(cppgo_sink)")
	assert.Contains(t, shim, "::prx::Source(cppgo_source)")
	// The proxy is a temporary: the container lives in the thunk, so both are
	// alive at once and nothing outlives the call.
	assert.Contains(t, shim, "std::vector<std::vector<::prx::Point>> cppgo_sink;")
}

// TestParseProxy pins the entry grammar and every way it is wrong.
func TestParseProxy(t *testing.T) {
	adapted, err := parseProxy("cv::findContours@1=std::vector<std::vector<cv::Point_<int>>> &")
	require.NoError(t, err)
	assert.Equal(t, "cv::findContours", adapted.Selector)
	assert.Equal(t, 1, adapted.Index)
	assert.Equal(t, "std::vector<std::vector<cv::Point_<int>>> &", adapted.Spelling)

	for _, bad := range []string{
		"cv::findContours=std::vector<int>",
		"cv::findContours@=std::vector<int>",
		"cv::findContours@x=std::vector<int>",
		"cv::findContours@1=",
		"cv::findContours@-1=std::vector<int>",
	} {
		_, err := parseProxy(bad)
		assert.Error(t, err, bad)
	}
}

// TestProxyUnmatchedIsAManifestError reports an adaptation nothing used: a
// manifest that names a parameter no selected declaration has is a mistake, and
// it is named with the declaration it was meant for.
func TestProxyUnmatchedIsAManifestError(t *testing.T) {
	cfg := proxyConfig()
	cfg.Proxy = append(cfg.Proxy, "prx::missing@0=std::vector<int> &")
	_, err := Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "prx::missing")
	assert.Contains(t, err.Error(), "no selected declaration matches")
}

// TestProxyOfANonRecordIsReported covers the other mistake: an adaptation of a
// parameter that is not a proxy at all, which the shim could not construct. The
// error names the parameter.
func TestProxyOfANonRecordIsReported(t *testing.T) {
	cfg := proxyConfig()
	cfg.Bind = []string{"prx::traceWithFlags"}
	cfg.Proxy = []string{"prx::traceWithFlags@1=std::vector<int> &"}
	_, err := Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a record parameter")

	cfg = proxyConfig()
	cfg.Proxy = append(cfg.Proxy, "prx::missing@0=std::vector<int> &")
	_, err = Render(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no selected declaration matches")
}
