package gen

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestModuleOf pins the module a declaration's header belongs to. The
// declaration's file is what says it: the component under the library's include
// root for OpenCV, the directory for the other two targets, and the file's own
// name for a module's public header.
func TestModuleOf(t *testing.T) {
	for _, tc := range []struct {
		file string
		want string
	}{
		{"/usr/include/opencv4/opencv2/core/mat.hpp", "core"},
		{"/usr/include/opencv4/opencv2/core.hpp", "core"},
		{"/usr/include/opencv4/opencv2/./imgproc/segmentation.hpp", "imgproc"},
		{"/usr/include/opencv4/opencv2/imgproc.hpp", "imgproc"},
		{"/usr/include/opencv4/opencv2/imgcodecs.hpp", "imgcodecs"},
		{"/usr/local/include/af/array.h", "arrayfire"},
		{"/usr/include/eigen3/Eigen/Dense", "eigen"},
		{"/src/internal/gen/testdata/mathx/mathx.hpp", "mathx"},
		{"", "unknown"},
		{".", "unknown"},
	} {
		assert.Equal(t, tc.want, moduleOf(tc.file), tc.file)
	}
	assert.Equal(t, "core", moduleName("core.hpp"))
	assert.Equal(t, "core", moduleName("core"))
	assert.Equal(t, "unknown", moduleName(".."))
	assert.Equal(t, "unknown", moduleName(""))
}

// TestNamespaceModule covers the fallback for a declaration libclang gave no
// location: the namespace says which module it is.
func TestNamespaceModule(t *testing.T) {
	assert.Equal(t, "segmentation", namespaceModule("cv::segmentation::Thing::method()"))
	assert.Equal(t, "unknown", namespaceModule("cv::Mat"))
	assert.Equal(t, "unknown", namespaceModule("plain()"))
}

// TestClassifyRefusal is the classification table: every reason the generator
// can produce belongs to exactly one class, and the class is what a fix or a
// decision is filed under.
func TestClassifyRefusal(t *testing.T) {
	for _, tc := range []struct {
		declaration string
		reason      string
		want        string
	}{
		{"cv::cuda::GpuMat::upload(const Mat &)", "", "gpu-only"},
		{"cv::Mat::Mat(const cuda::GpuMat &)", `parameter m: "const cuda::GpuMat" is not part of the generated API`, "gpu-only"},
		{"cv::ogl::Texture2D::copyFrom(const Mat &)", "", "gpu-only"},
		{"cv::cudev::load(const PtrStepSz<T> &)", "", "gpu-only"},
		{"cv::ocl::Context::getDefault()", "", "gpu-only"},
		{"ns::log(int, ...)", "a variadic function has no fixed argument list to lay out", "variadic-c"},
		{"ns::T::method", "a member function template and is not bound", "template-needs-instantiation"},
		{"ns::fill(void *)", `parameter data: "void *" is a void pointer`, "untyped-pointer"},
		{"af::array(void *)", `"af_array" is a void pointer`, "untyped-pointer"},
		{"ns::consume(int[4])", "an array parameter is a C array with no Go slice shape", "c-array"},
		{"ns::fill(std::string &)", `"std::string &" is a non-const standard-library reference`, "unsupported-std"},
		{"ns::take(Thing)", "Thing is not a POD", "by-value-record"},
		{"ns::take(Thing)", `"Thing" is passed by value but its layout is unknown`, "by-value-record"},
		{"ns::take(Thing)", `"Thing" is not a type this build classifies`, "by-value-record"},
		{"ns::use(Range)", `"cv::Range" is not part of the generated API`, "unmodeled-type"},
		{"ns::use(Range)", `"Range" is not a scalar or a known record`, "unmodeled-type"},
		{"ns::use(Flavor)", "an enum the generated API does not hold", "unmodeled-type"},
		{"ns::use(Thing *)", "points at a record that is not part of the API", "unmodeled-type"},
		{"cv::Mat::getDefaultAllocator()", `"MatAllocator *" is a pointer to an opaque record`, "unowned-pointer"},
		{"ns::T::operator->*", "an operator outside the naming table", "operator"},
		{"ns::watch(Handler)", "the callback's parameter shape is not carried", "callback"},
		{"ns::odd()", "something else entirely", "other"},
	} {
		t.Run(tc.declaration+"/"+tc.want, func(t *testing.T) {
			assert.Equal(t, tc.want, classifyRefusal(tc.declaration, tc.reason))
		})
	}
}

// TestCoverageReport writes the report for the type-map fixture and reads its
// three sections: the per-module summary, the classification counts, and one
// line per declaration that is not bound.
func TestCoverageReport(t *testing.T) {
	cfg := fixtureConfig("types", "types.hpp")
	cfg.Coverage = "coverage.txt"
	model, err := build(cfg)
	require.NoError(t, err)
	require.NotEmpty(t, model.coverage)
	report := string(model.coverage)

	header := "# cppgo coverage: package types\n"
	assert.True(t, strings.HasPrefix(report, header), "the report names the package: %s", report)
	assert.Contains(t, report, "# header: ")
	assert.Contains(t, report, "# module\tselected\tbound\trefused\tskipped\ttemplates\tnot_selected\tbound_ratio")

	// The summary has one row per module, and the ratios are bounded.
	var rows int
	for _, line := range strings.Split(report, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 8 || fields[0] == "" || strings.HasPrefix(fields[0], "#") {
			continue
		}
		rows++
		for _, field := range fields[1:7] {
			assert.NotEmpty(t, field)
		}
		ratio := fields[7]
		assert.Regexp(t, `^(0|1)\.[0-9]{4}$`, ratio, line)
	}
	assert.GreaterOrEqual(t, rows, 1, "the report has a module row")

	// Every declaration of the fixture is in the declarations section, and the
	// ones that did not bind carry a class: the special members the model
	// deliberately does not emit are skips, with the reason spelled out.
	assert.Contains(t, report, "# declarations\tkind\tmodule\tclass\treason")
	assert.Contains(t, report, "types::DeletedCtor::DeletedCtor()\tskipped\ttypes\tnot-needed\tthe constructor is deleted in C++")
	assert.Contains(t, report, "types::Mover::Mover(Mover &&)\tskipped\ttypes\tnot-needed\tGo has no rvalue overload")
	assert.Contains(t, report, "types::Abstract::area() const\tskipped\ttypes\tnot-needed\tthe member is pure virtual or deleted")
	assert.Contains(t, report, "# class\tcount")
}

// TestCoverageReportCounts checks the numbers themselves: the module's selected
// count is its bound, refused, skipped and template entries, and the ratio is
// bound over bound+refused+templates, which is the definition the README uses.
func TestCoverageReportCounts(t *testing.T) {
	cfg := fixtureConfig("selection", "selection.hpp")
	cfg.Coverage = "coverage.txt"
	model, err := build(cfg)
	require.NoError(t, err)
	report := string(model.coverage)

	var bound, refused, skipped, templates, selected int
	var ratio string
	for _, line := range strings.Split(report, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 8 || fields[0] != "selection" {
			continue
		}
		selected = atoiField(t, fields[1])
		bound = atoiField(t, fields[2])
		refused = atoiField(t, fields[3])
		skipped = atoiField(t, fields[4])
		templates = atoiField(t, fields[5])
		ratio = fields[7]
	}
	require.NotZero(t, selected, "the fixture's module has a row: %s", report)
	assert.Equal(t, selected, bound+refused+skipped+templates)
	assert.NotZero(t, bound)
	assert.NotZero(t, refused, "the fixture's uncarryable declarations are counted")
	assert.Equal(t, "0.6000", ratio, "three of the five declarations the fixture selects bind")
}

// TestCoverageReportMarksTemplatesAndSkips covers the two kinds a selected
// declaration can take that are not a refusal: a template pattern, which is
// bound through -instantiate, and a mirrored record's C++ member, which has no
// Go counterpart.
func TestCoverageReportMarksTemplatesAndSkips(t *testing.T) {
	cfg := fixtureConfig("tmpl", "cell.hpp")
	cfg.Coverage = "coverage.txt"
	model, err := build(cfg)
	require.NoError(t, err)
	report := string(model.coverage)
	assert.Contains(t, report, "tmpl::Cell::set(T)\ttemplate\tcell\ttemplate\t")

	cfg = fixtureConfig("params", "params.hpp")
	cfg.Bind = []string{"params::Holder"}
	cfg.Instantiate = []string{"params::Fixed<double, 4>"}
	cfg.Coverage = "coverage.txt"
	model, err = build(cfg)
	require.NoError(t, err)
	report = string(model.coverage)
	// The specialization's own members are declarations of the API: the
	// synthesized record carries them, and the summary counts the one that
	// bound. The pattern's member is the declaration libclang reported, and it
	// stays not-selected because the manifest asked for the concrete type.
	assert.Contains(t, report, "params::Fixed::get(int) const noexcept\tnot-selected\tparams")
	row := ""
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(line, "params\t") {
			row = line
		}
	}
	require.NotEmpty(t, row, "the specialization's module has a row: %s", report)
	fields := strings.Split(row, "\t")
	require.Len(t, fields, 8)
	assert.NotZero(t, atoiField(t, fields[2]), "the specialization's member binds: %s", row)
}

// atoiField reads one integer column of the report.
func atoiField(t *testing.T, field string) int {
	t.Helper()
	value := 0
	for _, r := range field {
		if r < '0' || r > '9' {
			t.Fatalf("the report's column %q is not a count", field)
		}
		value = value*10 + int(r-'0')
	}
	return value
}
