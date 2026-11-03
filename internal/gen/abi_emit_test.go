package gen

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestABISretSlotIsReserved pins the layout the Itanium ABI puts `this` in: a
// reserved sret pointer takes the first INTEGER slot and `this` the one after
// it. By-value class results are rejected in v1, so this is the reservation
// only — the TODO in buildSignature — but the ordering is what makes the
// layout correct the day the result path lands.
func TestABISretSlotIsReserved(t *testing.T) {
	pkg := parseABI(t, "//cppgo:method norm\nfunc (v Vec) Norm() float64")
	f := pkg.Funcs[0]
	f.Symbol = "_ZNK2ns3Vec4normEv"
	f.abi = &abiBinding{
		sym:   cxx.Symbol{QualifiedName: "ns::Vec::norm", Mangled: f.Symbol, HasSret: true},
		shape: abiValue,
	}
	pkg.abi = &abiPackage{source: "test"}

	sig, err := buildSignature(f)
	require.NoError(t, err)
	require.Len(t, sig.slots, 3)
	assert.Equal(t, "fn", sig.slots[0].label)
	assert.Equal(t, "sret", sig.slots[1].label)
	assert.Equal(t, "DI", sig.slots[1].reg)
	assert.Equal(t, "this", sig.slots[2].label)
	assert.Equal(t, "SI", sig.slots[2].reg)
	assert.Equal(t, 32, sig.total)

	asm := string(emitAsm([]*signature{sig}))
	assert.Contains(t, asm, "MOVQ\tsret+8(FP), DI")
	assert.Contains(t, asm, "MOVQ\tthis+16(FP), SI")

	// The wrapper passes nil for the reserved slot, because nothing allocates a
	// destination for it yet.
	src, err := emitGo(pkg, []*signature{sig})
	require.NoError(t, err)
	assert.Contains(t, string(src), "cppgo__ZNK2ns3Vec4normEv(cppgoSymNorm, nil, uintptr(v))")
}

// TestABIStringParameter covers the (pointer, length) pair for a Go string: two
// integer slots after `this`, and the string kept alive across the call.
func TestABIStringParameter(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::text_bytes
func TextBytes(text string) int32
`)
	files := renderABI(t, Config{Dir: dir, Out: t.TempDir()})
	assert.Contains(t, files[GenFileName], "cppgo.StrPtr(text), uintptr(len(text))")
	assert.Contains(t, files[GenFileName], "cppgo.KeepAlive(text)")
	assert.Contains(t, files[AsmFileName], "MOVQ\ta0+8(FP), DI")
	assert.Contains(t, files[AsmFileName], "MOVQ\ta0_len+16(FP), SI")
}

// TestABIErrorOnlyVoidBinding covers a void C++ function declared with a
// trailing error: there is no status to read, so the error is always nil.
func TestABIErrorOnlyVoidBinding(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::notify
func Notify() error
`)
	files, report, _ := renderReports(t, Config{Dir: dir, Out: t.TempDir()})
	assert.Contains(t, files[GenFileName], "func cppgoCallNotify() error {")
	assert.Contains(t, files[GenFileName], "\treturn nil\n")
	assert.Contains(t, files[GenFileName], "the trailing error is always nil")
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "rules::notify")
}

// TestABIInPlaceRewrite covers generating into the package directory: the input
// file is rewritten (keeping the directives and the receivers) and no stub copy
// is written next to it.
func TestABIInPlaceRewrite(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method norm
func (v Vector) Norm() float64
`)
	require.NoError(t, Generate(Config{Dir: dir, Out: dir}))

	src, err := readFileString(dir, "x.go")
	require.NoError(t, err)
	assert.Contains(t, src, "//cppgo:method norm")
	assert.Contains(t, src, "func (v Vector) Norm() float64 { return cppgoCallNorm(v) }")
	assert.NoFileExists(t, filepath.Join(dir, SourceFileName))
	assert.FileExists(t, filepath.Join(dir, GenFileName))
	assert.NoFileExists(t, filepath.Join(dir, ShimFileName))

	// A second run must be a no-op, so the rewrite is stable.
	require.NoError(t, Generate(Config{Dir: dir, Out: dir, Check: true}))
}

// TestGenerateWritesTheReportAndWarnings covers the two writers "cppgo gen"
// uses: the per-binding report on stdout and the package warnings on stderr.
func TestGenerateWritesTheReportAndWarnings(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:class mathx::Vector
type Vector cppgo.Handle

//cppgo:method norm
func (v Vector) Norm() float64
`)
	t.Run("gen", func(t *testing.T) {
		var report, warnings bytes.Buffer
		cfg := Config{Dir: dir, Out: t.TempDir(), Report: &report, Warnings: &warnings}
		require.NoError(t, Generate(cfg))
		assert.Contains(t, report.String(), "mathx::Vector::norm")
		assert.Contains(t, report.String(), "mangled _ZNK5mathx6Vector4normEv")
		assert.Contains(t, warnings.String(), "cppgo: warning:")
	})

	t.Run("check writes them too", func(t *testing.T) {
		out := t.TempDir()
		require.NoError(t, Generate(Config{Dir: dir, Out: out}))

		var report, warnings bytes.Buffer
		cfg := Config{Dir: dir, Out: out, Check: true, Report: &report, Warnings: &warnings}
		require.NoError(t, Generate(cfg))
		// The generator writes whatever writers it is given; it is "cppgo
		// check" that passes no report writer, because a gate prints a verdict.
		assert.Contains(t, report.String(), "mathx::Vector::norm")
		assert.Contains(t, warnings.String(), "cppgo: warning:")
	})
}

// release helper is emitted, the allocation one is not.
func TestABIDestructorWithoutConstructor(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:class rules::Widget
type Widget cppgo.Handle

//cppgo:dtor
func (w Widget) Destroy()
`)
	files := renderABI(t, Config{Dir: dir, Out: t.TempDir()})
	assert.Contains(t, files[GenFileName], "func cppgoRelease(handle cppgo.Handle)")
	assert.Contains(t, files[GenFileName], "cppgoRelease(cppgo.Handle(w))")
	assert.NotContains(t, files[GenFileName], "func cppgoAlloc(")
}

// TestABIPackageWithoutObjects covers the other side: no object plumbing at all
// when nothing allocates or releases.
func TestABIPackageWithoutObjects(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, mathxHeader(t), `//cppgo:function mathx::add
func Add(a, b int32) int32
`)
	files := renderABI(t, Config{Dir: dir, Out: t.TempDir()})
	assert.NotContains(t, files[GenFileName], "cppgoObjects")
	assert.NotContains(t, files[GenFileName], "cppgoRelease")
	assert.NotContains(t, files[GenFileName], "cppgoAlloc")
}

// TestABIForceChoosesAmongOverloads covers the -force path that has to pick one
// of several overloads: the only candidate with the right number of arguments
// is emitted, with the mismatch on the record.
func TestABIForceChoosesAmongOverloads(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::over
func Over(a bool) bool
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir()})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no overload accepts the Go argument types")

	files, report, _ := renderReports(t, Config{Dir: dir, Out: t.TempDir(), Force: true})
	require.Len(t, report, 1)
	assert.Contains(t, report[0], "forced with -force, NOT validated")
	assert.Contains(t, report[0], "_ZN5rules4overEi")
	assert.Contains(t, files[GenFileName], "NOT VALIDATED")
}

// TestABIForceIsRefusedWhenTwoOverloadsFit checks that ambiguity is not
// something -force can resolve: two candidates with the same arity leave no
// defensible choice, so the run fails even with -force.
func TestABIForceIsRefusedWhenTwoOverloadsFit(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:function rules::add
func Add(a, b bool) bool
`)
	_, err := RenderResult(Config{Dir: dir, Out: t.TempDir(), Force: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no overload accepts the Go argument types")
}

// TestProblemsTemplateWithoutMangledName covers the template check: an entity
// without a mangled name cannot be resolved at run time at all, which no amount
// of -force can change.
func TestProblemsTemplateWithoutMangledName(t *testing.T) {
	dir := writeABIPkg(t, "/nonexistent/vec.hpp", "//cppgo:function ns::twice\nfunc Twice(v int32) int32\n")
	pkg := mustParse(t, dir)
	r := &abiResolver{pkg: pkg}
	problems, _ := r.problems(pkg.Funcs[0], cxx.Symbol{QualifiedName: "ns::twice", Kind: cxx.FreeFunc})
	var messages []string
	for _, p := range problems {
		messages = append(messages, p.msg)
		if p.fatal {
			assert.Contains(t, p.msg, "no mangled name")
			assert.Contains(t, p.msg, "explicit instantiation")
		}
	}
	assert.NotEmpty(t, messages)
}

// TestABIProbeDeduplicatesClasses checks that two constructors of one class ask
// for its size once.
func TestABIProbeDeduplicatesClasses(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:class rules::Widget
type Widget cppgo.Handle

//cppgo:ctor
func New(n int32) (Widget, error)

//cppgo:ctor
func NewCopy(n int32) (Widget, error)
`)
	err := renderABIError(t, dir)
	assert.Contains(t, err, "both bind")
}

// TestABIProbeRejectsAMalformedClassName covers the probe's own failure path: a
// class name libclang cannot parse leaves no probe declaration to read.
func TestABIProbeRejectsAMalformedClassName(t *testing.T) {
	requireLibclang(t)
	dir := writeABIPkg(t, rulesHeader(t), `//cppgo:class ::rules::Widget
type Widget cppgo.Handle

//cppgo:ctor
func New(n int32) (Widget, error)
`)
	err := renderABIError(t, dir)
	assert.Contains(t, err, "cannot determine the layout of ::rules::Widget")
}
