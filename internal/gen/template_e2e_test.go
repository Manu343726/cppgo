package gen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// templateE2EProgram is the Go program the two-phase flow is measured on. The
// instantiations live in ordinary code, not in a test file, because that is
// what `cppgo build -scan` loads: the package as the program compiles it.
const templateE2EProgram = `package app

import (
	"fmt"

	"cppgo-e2e/box"
)

// Run instantiates the generic API with two concrete types and a function
// template. cppgo build collects exactly these three specializations.
func Run() (int32, float64, int64, int32, error) {
	b, err := box.NewBox[int32](7)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer b.Close()
	if err := b.Set(9); err != nil {
		return 0, 0, 0, 0, err
	}
	added, err := b.Add(4)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	got, err := b.Get()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	nine := int32(9)
	echoed, err := b.Echo(&nine)
	if err != nil || echoed != &nine {
		return 0, 0, 0, 0, fmt.Errorf("Echo = %v, %v", echoed, err)
	}
	d, err := box.NewBox[float64](2.5)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer d.Close()
	empty, err := box.NewBoxDefault[int32]()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if err := empty.Set(1); err != nil {
		return 0, 0, 0, 0, err
	}
	empty.Close()
	identity, err := box.BoxIdentity[int32](11)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	plain, err := box.NewPlain[float64](1.5)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	plainValue, err := plain.Value()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if err := plain.Close(); err != nil {
		return 0, 0, 0, 0, err
	}
	matrix, err := box.NewMatrix[float64, box.D4, box.D4](2.5)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer matrix.Close()
	if _, err := matrix.Get(); err != nil {
		return 0, 0, 0, 0, err
	}
	rows, err := box.MatrixRows[float64, box.D4, box.D4]()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	twice, err := box.Twice[int64](21)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return added + got + identity, plainValue, twice, rows, nil
}
`

// templateE2ETest is the assertion the program runs in both CGO modes.
const templateE2ETest = `package app

import (
	"os"
	"testing"

	"cppgo-e2e/box"
)

func TestRun(t *testing.T) {
	if err := box.Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}
	sum, want, twice, rows, err := Run()
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rows != 4 {
		t.Fatalf("MatrixRows[.., D4, D4] = %d, want 4", rows)
	}
	if sum != 13+9+11 {
		t.Fatalf("Add+Get+Identity = %d, want 9+4+9+11", sum)
	}
	if twice != 42 {
		t.Fatalf("Twice[int64](21) = %d, want 42", twice)
	}
	if want != 1.5 {
		t.Fatalf("the Plain[float64] value = %v", want)
	}
	if _, err := box.Twice[int32](3); err != nil {
		t.Fatalf("Twice[int32](3): %v", err)
	}
}
`

// templateE2EMissing is the third instantiation the shim was not built for.
// The error has to name the specialization the shim was not built for, not fail
// at the call.
const templateE2EMissing = `package app

import (
	"os"
	"strings"
	"testing"

	"cppgo-e2e/box"
)

func TestMissing(t *testing.T) {
	if err := box.Open(os.Getenv("CPPGO_E2E_SHIM")); err != nil {
		t.Fatalf("Open: %v", err)
	}
	_, err := box.NewBox[int64](7)
	if err == nil {
		t.Fatal("Box[int64] has no C++ counterpart and must fail")
	}
	want := "::box::Box<std::int64_t>"
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("the error must name %q, got: %v", want, err)
	}
}
`

// TestTwoPhaseGenerics is the end-to-end proof of phase 4b-1: the generic API
// compiles with no concrete type, the scan collects the program's
// instantiations, the shim instantiates exactly those, the program runs in both
// CGO modes, and an instantiation the shim was not built for is a named
// load-time failure.
func TestTwoPhaseGenerics(t *testing.T) {
	requireEndToEndTools(t)
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	tmp := t.TempDir()
	moduleDir := filepath.Join(tmp, "module")
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "app"), 0o755))
	require.NoError(t, writeModule(t, moduleDir, repo))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "app", "app.go"), []byte(templateE2EProgram), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "app", "app_test.go"), []byte(templateE2ETest), 0o644))

	boxDir := filepath.Join(moduleDir, "box")
	cfg := Config{
		Headers: []string{filepath.Join("testdata", "box", "box.hpp")},
		Package: "box",
		Out:     boxDir,
	}

	// Phase one: the generic API, with no instantiation at all. It compiles
	// and vets on its own, which is the property the first phase promises.
	require.NoError(t, Generate(cfg))
	runGoEnv(t, goTool(t), moduleDir, nil, "vet", "./box")

	// Phase two's collection: the type checker is the evidence. The list is
	// exactly the program's instantiations, in a deterministic order.
	names := map[string]string{"Box": "box::Box", "NewBox": "box::Box", "Plain": "box::Plain", "NewPlain": "box::Plain", "Matrix": "box::Matrix", "NewMatrix": "box::Matrix", "MatrixRows": "box::Matrix", "Twice": "box::twice"}
	found, err := scanInstantiations(moduleDir, []string{"./app"}, boxDir, names)
	require.NoError(t, err)
	require.Equal(t, []Instantiation{
		{Template: "box::Box", Args: []string{"float64"}},
		{Template: "box::Box", Args: []string{"int32"}},
		{Template: "box::Matrix", Args: []string{"float64", "box.D4", "box.D4"}},
		{Template: "box::Plain", Args: []string{"float64"}},
		{Template: "box::twice", Args: []string{"int32"}},
		{Template: "box::twice", Args: []string{"int64"}},
	}, found)
	again, err := scanInstantiations(moduleDir, []string{"./app"}, boxDir, names)
	require.NoError(t, err)
	assert.Equal(t, found, again, "the scan is deterministic")

	// The generation side of phase two: the same config, plus the scan. The
	// shim now carries a thunk per member of each collected instantiation.
	cfg.Scan = []string{"./app"}
	cfg.ScanDir = moduleDir
	var report strings.Builder
	cfg.Report = &report
	require.NoError(t, Generate(cfg))
	assert.Contains(t, report.String(), "box::Box<std::int32_t>")
	assert.Contains(t, report.String(), "box::Box<double>")
	assert.Contains(t, report.String(), "generic API")

	shim := filepath.Join(boxDir, ShimLibraryName("box"))
	require.FileExists(t, shim)
	exported := symbolsMatching(t, shim, "cppgo_tmpl_")
	assert.NotEmpty(t, exported, "the shim exports the instantiation's thunks")

	// The program runs against the shim, with cgo off and with -race.
	ran := runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
		"test", "-count=1", "-v", "./app")
	assert.Contains(t, ran, "--- PASS: TestRun")
	ran = runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=1", "CPPGO_E2E_SHIM=" + shim},
		"test", "-race", "-count=1", "-v", "./app")
	assert.Contains(t, ran, "--- PASS: TestRun")

	// The third instantiation: the same generic package and shim, a program
	// that was not scanned. The failure names the line to add.
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "missing"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "missing", "missing_test.go"), []byte(templateE2EMissing), 0o644))
	ran = runGoEnv(t, goTool(t), moduleDir, []string{"CGO_ENABLED=0", "CPPGO_E2E_SHIM=" + shim},
		"test", "-count=1", "-v", "./missing")
	assert.Contains(t, ran, "--- PASS: TestMissing")
}

// TestScanErrors covers the collection's failure paths: a pattern that matches
// nothing, a package that does not compile, and a directory that is not there.
func TestScanErrors(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)

	t.Run("no pattern", func(t *testing.T) {
		found, err := scanInstantiations(".", nil, "", nil)
		require.NoError(t, err)
		assert.Empty(t, found)
	})

	t.Run("unmatched pattern", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeModule(t, dir, repo))
		_, err := scanInstantiations(dir, []string{"./absent"}, dir, map[string]string{"Box": "box::Box"})
		require.Error(t, err)
	})

	t.Run("package with an error", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, writeModule(t, dir, repo))
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "broken"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "broken", "broken.go"), []byte("package broken\nfunc F() { return 1 }\n"), 0o644))
		_, err := scanInstantiations(dir, []string{"./broken"}, dir, map[string]string{"Box": "box::Box"})
		require.Error(t, err)
	})

	t.Run("missing directory", func(t *testing.T) {
		_, err := scanInstantiations(filepath.Join(t.TempDir(), "absent"), []string{"."}, "", nil)
		require.Error(t, err)
	})
}

// TestScanNamedArguments covers the type checker's named-type spelling (the
// qualifier's package-name branch) and the empty -scan-dir default. A named
// argument is collected like any other; the build step later refuses a type it
// cannot name in C++, which is a different failure with its own message.
func TestScanNamedArguments(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not in PATH")
	}
	repo, err := filepath.Abs("../..")
	require.NoError(t, err)
	moduleDir := t.TempDir()
	require.NoError(t, writeModule(t, moduleDir, repo))
	boxDir := filepath.Join(moduleDir, "box")
	require.NoError(t, Generate(Config{
		Headers: []string{filepath.Join("testdata", "box", "box.hpp")},
		Package: "box",
		Out:     boxDir,
	}))
	require.NoError(t, os.MkdirAll(filepath.Join(moduleDir, "named"), 0o755))
	program := `package named

import "cppgo-e2e/box"

type Distance int32

func F() {
	_, _ = box.NewBox[Distance](0)
	var err error
	_, _ = box.NewBox[error](err)
}
`
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "named", "named.go"), []byte(program), 0o644))
	names := map[string]string{"Box": "box::Box", "NewBox": "box::Box"}
	found, err := scanInstantiations(moduleDir, []string{"./named"}, boxDir, names)
	require.NoError(t, err)
	require.Len(t, found, 2)
	assert.Equal(t, []string{"error"}, found[0].Args)
	assert.Equal(t, []string{"named.Distance"}, found[1].Args)

	// An empty -scan-dir means the process's working directory. The package
	// here has no generic instantiation of ours, so the list is empty.
	found, err = scanInstantiations("", []string{"."}, "", nil)
	require.NoError(t, err)
	assert.Empty(t, found)
}
