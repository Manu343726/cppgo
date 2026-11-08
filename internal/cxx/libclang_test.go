package cxx

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadAndVersion(t *testing.T) {
	library := skipWithoutLibclang(t)

	assert.NotEmpty(t, library.Path())
	// The very first thing every test relies on: the CXString layout and the
	// memory-class returns. If Version() is garbage, nothing else can be right.
	assert.Contains(t, library.Version(), "clang version")
}

func TestLoadIsIdempotent(t *testing.T) {
	library := skipWithoutLibclang(t)

	second, err := Load()
	require.NoError(t, err)
	assert.Same(t, library, second, "Load must return the same handle, not a second one")
	assert.Equal(t, library.Path(), second.Path())

	// Closing one reference leaves the other alive.
	require.NoError(t, second.Close())
	assert.NotEmpty(t, library.Version())
	require.NoError(t, library.Close())

	// Both references are gone: the library can be loaded again.
	third, err := Load()
	require.NoError(t, err)
	require.NoError(t, third.Close())
}

func TestCloseTwiceIsAnError(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Skipf("libclang is not installed: %v", err)
	}
	require.NoError(t, library.Close())
	assert.Error(t, library.Close(), "closing an already closed handle must be an error, not a panic")
	assert.Empty(t, library.Version(), "a closed handle has no version")
	assert.True(t, library.isClosed())
}

func TestRetainOnClosedHandle(t *testing.T) {
	library, err := Load()
	if err != nil {
		t.Skipf("libclang is not installed: %v", err)
	}
	require.NoError(t, library.Close())
	_, err = library.retain()
	assert.ErrorIs(t, err, ErrClosed)
}

func TestLoadMissingEnvironmentPath(t *testing.T) {
	// The search runs once per process, so drop the cache first.
	resetLibclangCache(t)
	missing := filepath.Join(t.TempDir(), "libclang-not-here.so")
	t.Setenv("CPPGO_LIBCLANG", missing)

	_, err := Load()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	assert.Contains(t, err.Error(), missing, "the error must name the path it was told to use")
	var notFound *NotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, []string{missing}, notFound.Paths)
}

func TestLoadEnvironmentOverride(t *testing.T) {
	library := skipWithoutLibclang(t)
	path := library.Path()

	resetLibclangCache(t)
	t.Setenv("CPPGO_LIBCLANG", path)
	overridden, err := Load()
	require.NoError(t, err)
	t.Cleanup(func() { _ = overridden.Close() })
	assert.Equal(t, path, overridden.Path())
	assert.Contains(t, overridden.Version(), "clang version")
}

func TestLoadPathMissing(t *testing.T) {
	_, err := LoadPath(filepath.Join(t.TempDir(), "nope.so"))
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	var notFound *NotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.NotNil(t, notFound.Cause, "the dlopen error must be reported")
}

func TestLoadPathNotLibclang(t *testing.T) {
	// A real shared object that is not libclang: binding must fail with the
	// name of the first missing symbol.
	candidates := []string{"/lib/x86_64-linux-gnu/libc.so.6", "/lib64/libc.so.6", "/usr/lib/libm.so.6"}
	var path string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			path = c
			break
		}
	}
	if runtime.GOOS != "linux" {
		t.Skip("no system shared library to misuse on this platform")
	}
	require.NotEmpty(t, path, "no libc to load")

	_, err := LoadPath(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	assert.Contains(t, err.Error(), "clang_")
}

func TestFakeLibclangIsRejectedByTheSmokeTest(t *testing.T) {
	// A library that exports every symbol we bind but returns garbage is the
	// only way to exercise the layout smoke test. It is built here so the check
	// cannot silently rot.
	compiler := ccCompiler(t)
	dir := t.TempDir()
	source := filepath.Join(dir, "fake.c")
	require.NoError(t, os.WriteFile(source, []byte(fakeLibclangSource()), 0o644))
	library := filepath.Join(dir, "libfakeclang.so")
	output, err := exec.Command(compiler, "-shared", "-fPIC", "-o", library, source).CombinedOutput()
	require.NoErrorf(t, err, "building the fake library: %s", output)

	_, err = LoadPath(library)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrLibclangNotFound)
	assert.Contains(t, err.Error(), "ABI does not match")
}

// fakeLibclangSource declares every symbol libclangFuncs binds. Each returns
// zero, so clang_getClangVersion yields an empty CXString and the smoke test
// rejects the library before anything relies on the layout.
func fakeLibclangSource() string {
	names := []string{
		"clang_getClangVersion", "clang_getCString", "clang_disposeString",
		"clang_createIndex", "clang_disposeIndex", "clang_parseTranslationUnit2",
		"clang_disposeTranslationUnit", "clang_getTranslationUnitCursor",
		"clang_visitChildren", "clang_getCursorKind", "clang_getCursorSpelling",
		"clang_getCursorDisplayName", "clang_getCursorType", "clang_getCursorLocation",
		"clang_getCursorExtent", "clang_getRangeStart", "clang_getRangeEnd",
		"clang_isCursorDefinition", "clang_Cursor_isFunctionInlined",
		"clang_getCursorSemanticParent", "clang_getCursorDefinition",
		"clang_Cursor_getMangling", "clang_isVirtualBase", "clang_Cursor_getArgument",
		"clang_Cursor_getStorageClass", "clang_CXXMethod_isStatic",
		"clang_CXXMethod_isVirtual", "clang_CXXMethod_isConst",
		"clang_CXXMethod_isDefaulted", "clang_CXXMethod_isDeleted",
		"clang_CXXConstructor_isCopyConstructor",
		"clang_CXXConstructor_isMoveConstructor", "clang_getCanonicalType",
		"clang_getPointeeType", "clang_getNonReferenceType",
		"clang_getArrayElementType", "clang_getArraySize", "clang_isConstQualifiedType",
		"clang_isPODType", "clang_getTypeSpelling", "clang_getTypeKindSpelling",
		"clang_getCursorKindSpelling", "clang_getResultType", "clang_getNumArgTypes",
		"clang_getArgType", "clang_Type_getSizeOf", "clang_Type_getAlignOf",
		"clang_Type_getOffsetOf",
		"clang_getTypeDeclaration", "clang_getCXXAccessSpecifier",
		"clang_Cursor_getOffsetOfField", "clang_Cursor_isBitField",
		"clang_getFieldDeclBitWidth", "clang_Cursor_isAnonymous",
		"clang_CXXMethod_isPureVirtual", "clang_CXXRecord_isAbstract",
		"clang_getOffsetOfBase", "clang_getEnumDeclIntegerType",
		"clang_getEnumConstantDeclValue", "clang_getEnumConstantDeclUnsignedValue",
		"clang_EnumDecl_isScoped", "clang_getTemplateCursorKind",
		"clang_Cursor_isVariadic", "clang_getCursorExceptionSpecificationType",
		"clang_Cursor_getNumTemplateArguments", "clang_Cursor_getTemplateArgumentKind",
		"clang_Cursor_getTemplateArgumentType", "clang_Cursor_getTemplateArgumentValue",
		"clang_Cursor_getTemplateArgumentUnsignedValue", "clang_getSpecializedCursorTemplate",
		"clang_getFileLocation", "clang_Location_isFromMainFile",
		"clang_Location_isInSystemHeader", "clang_getFileName",
	}
	var b strings.Builder
	for _, name := range names {
		b.WriteString("void " + name + "(void) { }\n")
	}
	return b.String()
}

func TestVersionRejectsForeignLibraryOnPurpose(t *testing.T) {
	library := skipWithoutLibclang(t)
	// A CXString whose data pointer is nil must render as the empty string
	// rather than crash: that is the path the smoke test takes for a fake.
	assert.Empty(t, library.str(CXString{}))
}

func TestKindConstants(t *testing.T) {
	library := skipWithoutLibclang(t)

	// The numeric enums are part of libclang's ABI. Read them back through
	// libclang's own spelling functions instead of trusting the transcription.
	for _, tc := range []struct {
		kind int32
		want string
	}{
		{cursorStructDecl, "StructDecl"},
		{cursorUnionDecl, "UnionDecl"},
		{cursorClassDecl, "ClassDecl"},
		{cursorEnumDecl, "EnumDecl"},
		{cursorFieldDecl, "FieldDecl"},
		{cursorEnumConstantDecl, "EnumConstantDecl"},
		{cursorFunctionDecl, "FunctionDecl"},
		{cursorCXXMethod, "CXXMethod"},
		{cursorNamespace, "Namespace"},
		{cursorLinkageSpec, "LinkageSpec"},
		{cursorConstructor, "CXXConstructor"},
		{cursorDestructor, "CXXDestructor"},
		{cursorNamespaceAlias, "NamespaceAlias"},
		{cursorBaseSpecifier, "C++ base class specifier"},
		{cursorFunctionTemplate, "FunctionTemplate"},
		{cursorClassTemplate, "ClassTemplate"},
		{cursorClassTemplatePartialSpecialization, "ClassTemplatePartialSpecialization"},
		{cursorInvalidFile, "InvalidFile"},
		{cursorNoDeclFound, "NoDeclFound"},
		{cursorTranslationUnit, "TranslationUnit"},
	} {
		assert.Equal(t, tc.want, library.str(library.f.getCursorKindSpelling(tc.kind)), "cursor kind %d", tc.kind)
	}
	for _, tc := range []struct {
		kind int32
		want string
	}{
		{typeInvalid, "Invalid"},
		{typeVoid, "Void"},
		{typeBool, "Bool"},
		{typeCharU, "Char_U"},
		{typeUChar, "UChar"},
		{typeChar16, "Char16"},
		{typeChar32, "Char32"},
		{typeUShort, "UShort"},
		{typeUInt, "UInt"},
		{typeULong, "ULong"},
		{typeULongLong, "ULongLong"},
		{typeUInt128, "UInt128"},
		{typeCharS, "Char_S"},
		{typeSChar, "SChar"},
		{typeWChar, "WChar"},
		{typeShort, "Short"},
		{typeInt, "Int"},
		{typeLong, "Long"},
		{typeLongLong, "LongLong"},
		{typeInt128, "Int128"},
		{typeFloat, "Float"},
		{typeDouble, "Double"},
		{typeLongDouble, "LongDouble"},
		{typePointer, "Pointer"},
		{typeBlockPointer, "BlockPointer"},
		{typeLValueReference, "LValueReference"},
		{typeRValueReference, "RValueReference"},
		{typeRecord, "Record"},
		{typeEnum, "Enum"},
		{typeTypedef, "Typedef"},
		{typeNullPtr, "NullPtr"},
	} {
		assert.Equal(t, tc.want, library.str(library.f.getTypeKindSpelling(tc.kind)), "type kind %d", tc.kind)
	}
	assert.Equal(t, storageClassStatic, int32(3))
}

func TestParseTranslationUnitErrors(t *testing.T) {
	library := skipWithoutLibclang(t)

	_, err := library.ParseTranslationUnit("", nil)
	assert.Error(t, err)

	_, err = library.ParseTranslationUnit(filepath.Join(t.TempDir(), "missing.hpp"), nil)
	assert.Error(t, err)

	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.cpp")
	require.NoError(t, os.WriteFile(broken, []byte("int f() { return 0; }\n"), 0o644))
	// An unknown -std makes clang's driver refuse the command line outright.
	_, err = library.ParseTranslationUnit(broken, []string{"-std=c++99"})
	if err != nil {
		assert.Contains(t, err.Error(), "parsing")
	}

	closed, err := LoadPath("/usr/lib/libclang.so")
	if err != nil {
		t.Skip("second libclang handle not loadable here")
	}
	require.NoError(t, closed.Close())
	_, err = closed.ParseTranslationUnit(broken, nil)
	assert.ErrorIs(t, err, ErrClosed)
}

func TestParseErrorNames(t *testing.T) {
	assert.Equal(t, "success", parseErrorName(parseOK))
	assert.Contains(t, parseErrorName(parseFailure), "generic error")
	assert.Contains(t, parseErrorName(parseCrashed), "crashed")
	assert.Contains(t, parseErrorName(parseInvalidArguments), "invalid arguments")
	assert.Contains(t, parseErrorName(parseASTReadError), "AST file")
	assert.Contains(t, parseErrorName(99), "unknown CXErrorCode 99")
}

func TestTranslationUnitCloseTwice(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), []string{"-std=c++17", "-I" + dir})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "mathx.hpp"), tu.File)
	assert.Equal(t, []string{"-std=c++17", "-I" + dir}, tu.Args)
	require.NoError(t, tu.Close())
	assert.Error(t, tu.Close())
}

func TestTranslationUnitSymbolsAfterClose(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), []string{"-std=c++17", "-I" + dir})
	require.NoError(t, err)
	require.NoError(t, tu.Close())
	// A visit through a closed handle must fail quietly, not crash.
	assert.Empty(t, tu.Symbols())
	_, ok := tu.Lookup("mathx::Vector::norm")
	assert.False(t, ok)
	assert.Empty(t, tu.Lookups("mathx::add"))
	_, ok = tu.LookupMangled("_ZN5mathx4makeEdd")
	assert.False(t, ok)
}

func TestVisitorsAreIsolated(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), []string{"-std=c++17", "-I" + dir})
	require.NoError(t, err)
	defer func() { require.NoError(t, tu.Close()) }()

	// Two goroutines visiting through the same trampoline must not see each
	// other's cursors: the registry key is the client data, not a global.
	var wg sync.WaitGroup
	counts := make([]int, 4)
	for i := range counts {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			var seen int
			err := library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
				_ = cursor
				seen++
				return visitRecurse
			})
			assert.NoError(t, err)
			counts[slot] = seen
		}(i)
	}
	wg.Wait()
	for _, count := range counts {
		assert.Positive(t, count)
		assert.Equal(t, counts[0], count, "every concurrent visitor must see the same number of cursors")
	}
}

func TestVisitBreakAndErrors(t *testing.T) {
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), []string{"-std=c++17", "-I" + dir})
	require.NoError(t, err)
	defer func() { require.NoError(t, tu.Close()) }()

	count := 0
	require.NoError(t, library.visit(library.f.getTranslationUnitCursor(tu.tu), func(cursor, parent CXCursor) int32 {
		count++
		return visitBreak
	}))
	assert.Equal(t, 1, count, "visitBreak must stop the traversal immediately")
}

func TestCandidatePaths(t *testing.T) {
	paths := candidatePaths()
	require.NotEmpty(t, paths)
	assert.Equal(t, "libclang.so", paths[0], "the unversioned name must be tried first")
	for _, p := range paths {
		assert.NotEmpty(t, p)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		require.False(t, seen[p], "duplicate candidate %s", p)
		seen[p] = true
	}
}

// resetLibclangCache drops the process-wide handle so a test can change how the
// search resolves, and registers a cleanup that puts the environment back.
func resetLibclangCache(t *testing.T) {
	t.Helper()
	loadMu.Lock()
	saved := loaded
	loaded = nil
	loadMu.Unlock()
	t.Cleanup(func() {
		loadMu.Lock()
		loaded = saved
		loadMu.Unlock()
	})
}

// ccCompiler returns a C compiler, or skips. It is used to build the fake
// libclang, which is C and needs no C++ standard library.
func ccCompiler(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{os.Getenv("CC"), "cc", "gcc", "clang"} {
		if candidate == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("no C compiler available to build the fake libclang")
	return ""
}

func TestStructLayouts(t *testing.T) {
	// These four structs are the whole reason this package is ABI-sensitive.
	assert.Equal(t, uintptr(16), unsafe.Sizeof(CXString{}), "CXString is {ptr, unsigned}")
	assert.Equal(t, uintptr(24), unsafe.Sizeof(CXType{}), "CXType is {int, ptr[2]}")
	assert.Equal(t, uintptr(32), unsafe.Sizeof(CXCursor{}), "CXCursor is {int, int, ptr[3]}")
	assert.Equal(t, uintptr(24), unsafe.Sizeof(CXSourceLocation{}), "CXSourceLocation is {ptr[2], unsigned}")

	assert.Equal(t, uintptr(8), unsafe.Offsetof(CXType{}.Data), "CXType.data follows a padded int")
	assert.Equal(t, uintptr(8), unsafe.Offsetof(CXCursor{}.Data), "CXCursor.data follows two ints")
	assert.Equal(t, uintptr(16), unsafe.Offsetof(CXSourceLocation{}.IntData))

	// The pointer words must be uintptr, not unsafe.Pointer: libclang tags some
	// cursors with small integers, and a scanned pointer slot holding 0x1 makes
	// the runtime abort with "bad pointer in frame" when it grows that stack.
	for _, field := range []reflect.Type{
		reflect.TypeOf(CXString{}.Data),
		reflect.TypeOf(CXType{}.Data[0]),
		reflect.TypeOf(CXCursor{}.Data[0]),
		reflect.TypeOf(CXSourceLocation{}.PtrData[0]),
	} {
		assert.Equal(t, reflect.Uintptr, field.Kind(), "pointer words must not be GC-visible")
	}
}

func TestExtractionSurvivesGarbageCollection(t *testing.T) {
	// A regression test for the tagged-cursor abort: parse, extract and collect
	// in a loop, forcing the collector to scan and shrink the frames that hold
	// CXCursor values.
	library := skipWithoutLibclang(t)
	dir := copyFixture(t)
	args := []string{"-std=c++17", "-I" + dir}
	for i := 0; i < 8; i++ {
		tu, err := library.ParseTranslationUnit(filepath.Join(dir, "mathx.hpp"), args)
		require.NoError(t, err)
		require.NotEmpty(t, tu.Symbols())
		runtime.GC()
		require.NoError(t, tu.Close())
		runtime.GC()
	}
}
