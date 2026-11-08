package gen

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// TestGeneratedPackageBuildsAndRuns is the end-to-end proof of the v4 pipeline
// on a project laid out the way a real one is: a header under include/, a
// source under src/, and a shared object built by the project's own command.
//
// The steps are the contract's three artefacts in order. The user's library is
// built first, by the project's Makefile, and never touched again. `cppgo gen`
// parses the header, decides the carriers, writes the Go package, the
// trampolines and the shim, and compiles the shim itself into
// libcppgo_analytic.so, linked against the user's library. The generated
// package is then built and run twice — with CGO_ENABLED=0 and with the race
// detector — against the two objects.
func TestGeneratedPackageBuildsAndRuns(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	require.NoError(t, copyTree("testdata/project", project))

	// 1. The user's build: their Makefile, their flags, their object.
	makeBin, err := exec.LookPath("make")
	require.NoError(t, err)
	runIn(t, project, makeBin, "library")
	library := filepath.Join(project, "build", "libanalytic.so")
	require.FileExists(t, library)
	before := fileDigest(t, library)

	header := filepath.Join(project, "include", "analytic", "analytic.hpp")
	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "analytic")
	var report bytes.Buffer
	require.NoError(t, Generate(Config{
		Headers: []string{header},
		Package: "analytic",
		Library: library,
		Out:     out,
		Report:  &report,
	}))

	// 2. The carriers are what the contract says, and the report names them:
	// the noexcept out-of-line method is direct, the inline accessor and the
	// throwing function are shims, and a callback forces the reentrant carrier.
	carrierOf(t, report.String(), "analytic::Signal::scaled(double) const noexcept", "direct")
	carrierOf(t, report.String(), "analytic::Signal::gain() const", "thunk")
	carrierOf(t, report.String(), "analytic::ratio(std::int32_t, std::int32_t)", "thunk")
	carrierOf(t, report.String(), "analytic::fold(", "reentrant")
	carrierOf(t, report.String(), "analytic::integrate(", "reentrant")

	// 3. cppgo built the shim itself, and the user's build was not touched: the
	// library carries no cppgo symbol and it is byte-identical to the object its
	// own build produced.
	shim := filepath.Join(out, ShimLibraryName("analytic"))
	require.FileExists(t, shim)
	assert.Equal(t, before, fileDigest(t, library), "the user's library must not change")
	assert.Empty(t, symbolsMatching(t, library, "cppgo"))
	assert.NotEmpty(t, symbolsMatching(t, shim, "cppgo_last_error"), "the shim is the cppgo artefact")

	// 4. The generated package builds and runs against the two objects, with
	// cgo off and with the race detector on.
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "analytic_test.go"), []byte(e2eTestSource), 0o644))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./analytic")
	ran := runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
		"test", "-count=1", "-v", "./analytic")
	assert.Contains(t, ran, "--- PASS: TestBindings")
	ran = runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
		"test", "-race", "-count=1", "-v", "./analytic")
	assert.Contains(t, ran, "--- PASS: TestBindings")
}

// TestMissingShimNamesTheArtefact proves the load-time diagnostic: the user's
// library loads, the shim does not, and Open names the shim path it looked for
// instead of failing later on a symbol nobody resolved.
func TestMissingShimNamesTheArtefact(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	project := filepath.Join(tmp, "project")
	require.NoError(t, copyTree("testdata/project", project))
	makeBin, err := exec.LookPath("make")
	require.NoError(t, err)
	runIn(t, project, makeBin, "library")

	moduleDir := filepath.Join(tmp, "module")
	out := filepath.Join(moduleDir, "analytic")
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join(project, "include", "analytic", "analytic.hpp")},
		Package: "analytic",
		Library: filepath.Join(project, "build", "libanalytic.so"),
		Out:     out,
	}))
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(out, "missing_test.go"), []byte(missingShimSource), 0o644))
	absent := filepath.Join(tmp, "absent", ShimLibraryName("analytic"))
	ran := runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + absent},
		"test", "-count=1", "-v", "-run", "^TestMissingShim$", "./analytic")
	assert.Contains(t, ran, "--- PASS: TestMissingShim")
}

// TestFixturePackagesCompile builds every fixture's generated Go package in a
// throwaway module: go vet runs asmdecl, which checks each trampoline's frame
// size and FP offsets against the Go declaration, so a widened or narrowed
// signature cannot pass unnoticed.
func TestFixturePackagesCompile(t *testing.T) {
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	for name, cfg := range goldenFixtures() {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			moduleDir := t.TempDir()
			require.NoError(t, writeModule(t, moduleDir, repo))
			cfg.Out = filepath.Join(moduleDir, "genpkg")
			require.NoError(t, Generate(cfg))
			runGoEnv(t, goBinary, moduleDir, nil, "vet", "./genpkg")
			runGoEnv(t, goBinary, moduleDir, nil, "build", "./genpkg")
		})
	}
}

// requireEndToEndTools skips a test whose toolchain is absent, naming it.
func requireEndToEndTools(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"g++", "go", "make", "nm"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not in PATH", tool)
		}
	}
	if _, err := cxx.Load(); err != nil {
		t.Skipf("libclang is not available: %v", err)
	}
}

// goTool is the go tool, which every end-to-end test needs.
func goTool(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	require.NoError(t, err)
	return path
}

// carrierOf asserts that the report's line for a declaration names the carrier
// and the reason the contract gives it.
func carrierOf(t *testing.T, report, declaration, carrier string) {
	t.Helper()
	for _, line := range strings.Split(report, "\n") {
		if !strings.HasPrefix(line, declaration) {
			continue
		}
		assert.Contains(t, line, " "+carrier+" ",
			"%s must be carried by %s", declaration, carrier)
		return
	}
	t.Fatalf("%s is not in the report:\n%s", declaration, report)
}

// runIn runs a command with a working directory and fails with its output.
func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "%s %s failed:\n%s", name, strings.Join(args, " "), output)
}

// symbolsMatching returns the dynamic symbols of path whose name contains
// substr, which is how "no cppgo symbol is in the user's library" is measured.
func symbolsMatching(t *testing.T, path, substr string) []string {
	t.Helper()
	nm, err := exec.LookPath("nm")
	require.NoError(t, err)
	out, err := exec.Command(nm, "-D", path).Output()
	require.NoError(t, err)
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, substr) {
			found = append(found, strings.TrimSpace(line))
		}
	}
	return found
}

// fileDigest returns the file's bytes, which is how "the user's build was not
// touched" is measured.
func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

// writeModule writes the throwaway module that holds the generated package. It
// needs the real runtime, wired up with a replace directive exactly as a user
// would.
func writeModule(t *testing.T, dir, repo string) error {
	t.Helper()
	gomod := "module cppgo-e2e\n\ngo 1.27.0\n\nrequire github.com/Manu343726/cppgo v0.0.0\n\nreplace github.com/Manu343726/cppgo => " + repo + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	sum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
}

// runGoEnv runs the go tool in a throwaway module, with the environment a
// consumer has: no network, no workspace, a replace directive instead of a
// published module.
func runGoEnv(t *testing.T, goBinary, dir string, extra []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(goBinary, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off", "GOPROXY=off")
	cmd.Env = append(cmd.Env, extra...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "go %s failed:\n%s", strings.Join(args, " "), output)
	return string(output)
}

// copyTree copies a directory tree.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

// e2eTestSource is the program the generated package runs. Every assertion is a
// value the C++ produced, and each one is labelled with the construct it
// measures.
const e2eTestSource = `package analytic

import (
	"math"
	"os"
	"testing"
)

func TestBindings(t *testing.T) {
	if err := Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// The opaque class: private members, an out-of-line constructor, an inline
	// accessor (shim), a throwing method (shim, status) and a noexcept one
	// (direct).
	s, err := NewSignal(3, 4)
	if err != nil {
		t.Fatalf("NewSignal: %v", err)
	}
	if g, err := s.Gain(); err != nil || g != 3 {
		t.Fatalf("Gain = %v, %v", g, err) // inline body, through the shim
	}
	if e, err := s.Energy(); err != nil || e != 12 {
		t.Fatalf("Energy = %v, %v", e, err)
	}
	if got := s.Scaled(2.5); got != 7.5 {
		t.Fatalf("Scaled = %v, want 7.5 (the direct carrier)", got)
	}
	if d, err := s.Dot([]float64{1, 1, 1, 1, 1}); err != nil || d != 12 {
		t.Fatalf("Dot = %v, %v", d, err) // a (pointer, length) slice, capped by taps
	}
	if v, err := SignalVersion(); err != nil || v != 4 {
		t.Fatalf("SignalVersion = %v, %v", v, err)
	}
	// The copy constructor adds one to the gain: the C++ copy really ran.
	clone, err := s.Clone()
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if g, err := clone.Gain(); err != nil || g != 4 {
		t.Fatalf("cloned Gain = %v, %v", g, err)
	}
	if err := clone.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := clone.Close(); err == nil {
		t.Fatal("a second Close must report ErrClosed")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A 16-byte POD by value in and out.
	mid, err := Midpoint(Point{X: 0, Y: 0}, Point{X: 4, Y: 8})
	if err != nil || mid != (Point{X: 2, Y: 4}) {
		t.Fatalf("Midpoint = %+v, %v", mid, err)
	}
	// A 24-byte POD by value in and out, noexcept: no error channel.
	blended := Blend(Sample{A: 0, B: 0, C: 0}, Sample{A: 10, B: 20, C: 30}, 0.5)
	if blended != (Sample{A: 5, B: 10, C: 15}) {
		t.Fatalf("Blend = %+v", blended)
	}
	// A POD by reference, through the direct carrier.
	scaled := ScaledPoint(&Point{X: 1, Y: 2}, 3)
	if scaled != (Point{X: 3, Y: 6}) {
		t.Fatalf("ScaledPoint = %+v", scaled)
	}

	// std::string in and out.
	text, err := Describe("signal", 2.5)
	if err != nil || text != "signal=2.500000" {
		t.Fatalf("Describe = %q, %v", text, err)
	}
	// std::vector in and out.
	values, err := Window(4)
	if err != nil || len(values) != 4 || math.Abs(values[3]-1.5) > 1e-12 {
		t.Fatalf("Window = %v, %v", values, err)
	}
	if empty, err := Window(0); err != nil || len(empty) != 0 {
		t.Fatalf("Window(0) = %v, %v", empty, err)
	}

	// A C++ exception becomes a Go error.
	if _, err := Ratio(6, 0); err == nil {
		t.Fatal("Ratio(6, 0) must report the C++ exception")
	}
	if q, err := Ratio(6, 3); err != nil || q != 2 {
		t.Fatalf("Ratio = %v, %v", q, err)
	}

	// A std::function the shim builds from the Go function.
	folded, err := Fold(func(a, b int32) int32 { return a*10 + b }, 4, 2)
	if err != nil || folded != 42 {
		t.Fatalf("Fold = %v, %v", folded, err)
	}

	// A plain function pointer, called twice by the callee. The expected value
	// is computed here: the integral of x over [0, 2] is 2.
	integral, err := Integrate(func(x float64) float64 { return x }, 0, 2)
	if err != nil || math.Abs(integral-2) > 1e-9 {
		t.Fatalf("Integrate = %v, %v", integral, err)
	}

	// A panic inside a callback reaches the caller instead of the process.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("a panicking callback must re-panic in Go")
			}
		}()
		_, _ = Integrate(func(float64) float64 { panic("callback exploded") }, 0, 1)
	}()

	// The package was opened explicitly; a second Open is a no-op.
	if err := Open(""); err != nil {
		t.Fatalf("Open after a successful explicit Open: %v", err)
	}
	if err := Open(""); err != nil {
		t.Fatalf("and again: %v", err)
	}
}
`

// missingShimSource asserts Open's diagnostic for a shim that is not there.
const missingShimSource = `package analytic

import (
	"os"
	"strings"
	"testing"
)

func TestMissingShim(t *testing.T) {
	err := Open(os.Getenv("CPPGO_E2E_SHIM"))
	if err == nil {
		t.Fatal("Open must fail when the shim does not exist")
	}
	if !strings.Contains(err.Error(), "libcppgo_analytic.so") {
		t.Fatalf("the load error must name the shim, got: %v", err)
	}
}
`
