package gen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestABIResolveMathx is the resolution half of the end-to-end story: the real
// mathx class, every supported declaration form, and the report DESIGN.md
// specifies.
func TestABIResolveMathx(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:ctor
func New(x, y float64) (Vector, error)

//cppgo:method norm
func (v Vector) Norm() float64

//cppgo:method dot
func (v Vector) Dot(ys []float64) float64

//cppgo:dtor
func (v Vector) Destroy()

//cppgo:function mathx::add
func Add(a, b int32) int32

//cppgo:function mathx::Vector::dims
func Dims() int32
`)
	files, report, warnings := renderReports(t, Config{Dir: dir, Out: t.TempDir()})

	// The report names the C++ declaration, the mangled name and the tier.
	joined := strings.Join(report, "\n")
	assert.Contains(t, joined, "mathx::Vector::Vector")
	assert.Contains(t, joined, "mangled _ZN5mathx6VectorC1Edd")
	assert.Contains(t, joined, "mathx::Vector::norm")
	assert.Contains(t, joined, "mangled _ZNK5mathx6Vector4normEv")
	assert.Contains(t, joined, "mathx::Vector::dot")
	assert.Contains(t, joined, "mangled _ZNK5mathx6Vector3dotEPKdm")
	assert.Contains(t, joined, "mathx::Vector::~Vector")
	assert.Contains(t, joined, "mangled _ZN5mathx6VectorD1Ev")
	assert.Contains(t, joined, "abi from")
	// The size probe told the constructor how much to allocate.
	assert.Contains(t, joined, "24 bytes, alignment 8")

	// Fallback flags are an assumption, and the assumption is on the record.
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "assumption")
	assert.Contains(t, warnings[0], "-std=c++17")

	// No shim header in this mode.
	assert.NotContains(t, files, ShimFileName)
	assert.Contains(t, files[GenFileName], "cppgoCallNorm")
	assert.Contains(t, files[AsmFileName], "MOVQ\tthis+8(FP), DI")
}

// TestABIResolveRulesRejections is the validation table: every construct the
// direct ABI mode refuses, with the message a user gets. The fixture is
// testdata/abi/rules/rules.hpp.
//
// The bindings of one group share a package, and therefore one parse of the
// fixture: a resolved binding is independent of the ones next to it, so a group
// is exactly the set of rejections that fit in one header parse.
func TestABIResolveRulesRejections(t *testing.T) {
	requireLibclang(t)
	widget := "//cppgo:class rules::Widget\ntype Widget cppgo.Handle\n\n"
	groups := []struct {
		name  string
		body  string
		cases map[string]string
	}{
		{
			name: "symbols that are not there",
			body: "//cppgo:function rules::internal_counter\nfunc InternalCounter() int32\n\n" +
				"//cppgo:function rules::hidden_helper\nfunc HiddenHelper()\n\n" +
				"//cppgo:function rules::nope\nfunc Nope()\n",
			cases: map[string]string{
				"internal linkage":              "internal linkage",
				"anonymous namespace":           "is not declared",
				"not declared":                  "is not declared",
				"not declared names the header": "rules.hpp",
			},
		},
		{
			name: "members",
			body: widget +
				"//cppgo:method inline_size\nfunc (w Widget) InlineSize() int32\n\n" +
				"//cppgo:method area\nfunc (w Widget) Area() int32\n\n" +
				"//cppgo:method dims\nfunc (w Widget) Dims() int32\n\n" +
				"//cppgo:function rules::Widget::size\nfunc Size(v Widget) int32\n",
			cases: map[string]string{
				"inline member":                    "defined inside its class definition",
				"inline member says how to fix it": "move the definition out into a source file",
				"virtual member":                   "it is virtual",
				"static method bound as a method":  "static member function, which has no this",
				"member bound as a free function":  "needs this",
			},
		},
		{
			name: "types that cannot cross",
			body: "//cppgo:function rules::sum_pod\nfunc SumPod(p int32) int32\n\n" +
				"//cppgo:function rules::text_length\nfunc TextLength(s *byte) uint64\n\n" +
				"//cppgo:function rules::make_pod\nfunc MakePod(a, b int32) (int32, error)\n\n" +
				"//cppgo:function rules::pick\nfunc Pick(w cppgo.Handle, member int32) int32\n\n" +
				"//cppgo:function rules::pick_one\nfunc PickOne(member int32) int32\n",
			cases: map[string]string{
				"struct passed by value":                      "a struct passed by value",
				"struct passed by value, first of two":        "a struct passed by value",
				"std type parameter":                          "a standard-library type",
				"struct returned by value":                    "returns Pod by value",
				"struct returned by value points at the shim": "use the shim mode",
				"member pointer parameter":                    "cannot classify",
			},
		},
		{
			name: "signatures that do not fit",
			body: widget +
				"//cppgo:ctor\nfunc New(n int64) (Widget, error)\n\n" +
				"//cppgo:function rules::ratio\nfunc Ratio(a float64, b int32) float64\n\n" +
				"//cppgo:function rules::over\nfunc Over(a float64, b float64, c float64) float64\n",
			cases: map[string]string{
				"width mismatch":          "the Go type is int64 (8-byte) but the C++ type is \"int\" (4-byte)",
				"register class mismatch": "travels in the other register class",
				"arity mismatch":          "no overload takes 3 arguments",
			},
		},
	}
	for _, group := range groups {
		dir := writeABIPkg(t, rulesHeader(t), group.body)
		err := renderABIError(t, dir)
		for name, want := range group.cases {
			t.Run(name, func(t *testing.T) {
				assert.Contains(t, err, want)
				assert.Contains(t, err, "x.go:", "every rejection points at the directive")
			})
		}
	}
}

// TestABIRegisterExhaustion checks the budget after `this` has taken a slot.
func TestABIRegisterExhaustion(t *testing.T) {
	requireLibclang(t)

	t.Run("a member with six integer arguments", func(t *testing.T) {
		dir := writeABIPkg(t, rulesHeader(t), `//cppgo:class rules::Widget
type Widget cppgo.Handle

//cppgo:method six
func (w Widget) Six(a, b, c, d, e, f int32) int32
`)
		err := renderABIError(t, dir)
		assert.Contains(t, err, "needs 7 integer argument registers")
		assert.Contains(t, err, "x.go:")
	})

	t.Run("a static member with the same six fits", func(t *testing.T) {
		// The budget is per call, and a static member spends no slot on this:
		// the same six arguments are fine when there is no receiver.
		dir := writeABIPkg(t, mathxHeader(t), `//cppgo:function mathx::add
func Add(a, b int32) int32
`)
		files := renderABI(t, Config{Dir: dir, Out: t.TempDir()})
		assert.Contains(t, files[AsmFileName], "MOVQ\ta1+16(FP), SI")
	})
}

// TestABIProbeFailsForAnIncompleteClass covers the object-size probe: a class
// that is only forward-declared has no size, and guessing one would corrupt
// memory.
func TestABIProbeFailsForAnIncompleteClass(t *testing.T) {
	requireLibclang(t)
	dir := writeProbeFixture(t)
	err := renderABIError(t, dir)
	assert.Contains(t, err, "cannot determine the layout of incomplete::Node")
	assert.Contains(t, err, "is the class complete")
	assert.Contains(t, err, "x.go:")
}

// TestABIProbeTempDirFailure covers the probe's own bookkeeping: it writes a
// temporary translation unit, and a broken TMPDIR is reported rather than
// silently producing a zero size.
func TestABIProbeTempDirFailure(t *testing.T) {
	requireLibclang(t)
	dir := writeProbeFixture(t)
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating the size probe")
}

// TestABIForceDowngradesValidation checks the escape hatch: the binding is
// emitted, and both the report and the generated file say it was not validated.
func TestABIForceDowngradesValidation(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:class rules::Widget
type Widget cppgo.Handle

//cppgo:method inline_size
func (w Widget) InlineSize() int32
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err, "without -force an inline member is a build error")

	files, report, _ := renderReports(t, Config{Dir: dir, Out: t.TempDir(), Force: true})
	assert.Contains(t, files[GenFileName], "NOT VALIDATED: -force accepted this binding as written")
	assert.Contains(t, files[GenFileName], "defined inside its class definition")
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "forced with -force, NOT validated")
	// The mangled name is still the one libclang reported, so the failure is a
	// load-time one, which is exactly what -force means.
	assert.Contains(t, files[GenFileName], "_ZNK5rules6Widget11inline_sizeEv")
}

// TestABIForceCannotRescueAFatal checks that -force does not pretend: a binding
// the generator cannot express at all still fails.
func TestABIForceCannotRescueAFatal(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::make_pod
func MakePod(a, b int32) (int32, error)
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir(), Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "returns Pod by value")
}

// TestABIStatusWidths checks the status convention's width: an int is read in
// the low 32 bits, a long in all 64.
func TestABIStatusWidths(t *testing.T) {
	requireLibclang(t)

	intStatus := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::add
func Add(a, b int32) error
`)
	files := renderABI(t, Config{Dir: intStatus, Out: t.TempDir()})
	assert.Contains(t, files[AsmFileName], "MOVL\tAX, ret+")

	longStatus := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::count
func Count() error
`)
	files = renderABI(t, Config{Dir: longStatus, Out: t.TempDir()})
	assert.Contains(t, files[AsmFileName], "MOVQ\tAX, ret+")
	assert.Contains(t, files[GenFileName], "cppgoStatus := cppgo__ZN5rules5countEv(cppgoSymCount)")
	assert.Contains(t, files[GenFileName], `fmt.Errorf("rules::count: call failed with status %d", cppgoStatus)`)
}

// TestABICompileCommandsReachesCxx checks that the flag override is passed
// through instead of being silently ignored.
func TestABICompileCommandsReachesCxx(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method norm
func (v Vector) Norm() float64
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir(), CompileCommands: filepath.Join(t.TempDir(), "nope", "compile_commands.json")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile_commands.json")
}

// TestABIMissingHeader covers a //cppgo:header that names a file that is not
// there: the error has to name it.
func TestABIMissingHeader(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, "does/not/exist.hpp", `//cppgo:function ns::f
func F() int32
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exist.hpp")
}

// TestABIStaleShimHeaderIsReported covers a package that used to be in shim
// mode: the leftover header is not written, and the run says so.
func TestABIStaleShimHeaderIsReported(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method norm
func (v Vector) Norm() float64
`)
	out := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(out, ShimFileName), []byte("// stale\n"), 0o644))

	res, err := RenderResult(Config{Dir: dir, Out: out})
	require.NoError(t, err)
	require.Len(t, res.Warnings, 2)
	assert.Contains(t, res.Warnings[0], "assumption")
	assert.Contains(t, res.Warnings[1], "left by an earlier shim-mode run")
	for _, o := range res.Outputs {
		assert.NotEqual(t, ShimFileName, o.Name)
	}
}
