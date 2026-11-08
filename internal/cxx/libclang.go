package cxx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// ErrLibclangNotFound is wrapped by every error returned when the dynamic
// library cannot be located on this machine. Callers use it to degrade to the
// shim mode (errors.Is(err, ErrLibclangNotFound)).
var ErrLibclangNotFound = errors.New("cxx: libclang not found")

// ErrClosed is returned by a Libclang or TU method after Close.
var ErrClosed = errors.New("cxx: libclang handle is closed")

// NotFoundError reports where libclang was looked for.
type NotFoundError struct {
	// Paths are the paths and library names that were tried, in resolution
	// order. The first entries are bare names handed to dlopen, which is what
	// makes an installed libclang work without pinning a directory.
	Paths []string
	// Cause is the last dlopen or symbol-binding error, when a candidate file
	// existed but could not be used. It is nil when nothing existed at all.
	Cause error
}

func (e *NotFoundError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v (searched %s)", ErrLibclangNotFound, e.Cause, strings.Join(e.Paths, ", "))
	}
	return fmt.Sprintf("%s (searched %s; install libclang-dev or set CPPGO_LIBCLANG)", ErrLibclangNotFound, strings.Join(e.Paths, ", "))
}

// Unwrap makes errors.Is(err, ErrLibclangNotFound) true for every not-found
// error, whichever candidate failed last.
func (e *NotFoundError) Unwrap() error { return ErrLibclangNotFound }

// ---------------------------------------------------------------------------
// libclang's C structs, mirrored byte for byte.
//
// These layouts are ABI-critical: they are passed to and returned from
// libclang by value. If one of them is wrong the package does not crash, it
// returns garbage, which is why loadPath smoke-tests clang_getClangVersion
// before it hands the handle to anybody.
// ---------------------------------------------------------------------------

// CXString mirrors libclang's CXString: { const void *data; unsigned private_flags }.
// 16 bytes. Interchange class, returned in RAX/RDX, passed in RDI/RSI.
//
// The pointer words in these four structs are uintptr, never unsafe.Pointer, and
// that is not a style choice: libclang stuffs small integers into the data
// words of some cursors (a tagged cursor is a real thing in its C API), and a
// Go pointer-typed field holding 0x1 makes the runtime abort the process with
// "bad pointer in frame" the next time it scans or grows that stack frame.
// uintptr keeps the layout identical and keeps the collector out of it.
type CXString struct {
	Data         uintptr
	PrivateFlags uint32
}

// CXType mirrors libclang's CXType: { enum CXTypeKind kind; void *data[2] }.
// 24 bytes, MEMORY class: passed on the stack, returned through a hidden pointer.
type CXType struct {
	Kind int32
	Data [2]uintptr
}

// CXCursor mirrors libclang's CXCursor: { enum CXCursorKind kind; int xdata; const void *data[3] }.
// 32 bytes, MEMORY class.
type CXCursor struct {
	Kind  int32
	Xdata int32
	Data  [3]uintptr
}

// CXSourceLocation mirrors libclang's CXSourceLocation:
// { const void *ptr_data[2]; unsigned int_data }.
// 24 bytes, MEMORY class.
type CXSourceLocation struct {
	PtrData [2]uintptr
	IntData uint32
}

// CXSourceRange mirrors libclang's CXSourceRange:
// { const void *ptr_data[2]; unsigned begin_int_data; unsigned end_int_data; }.
// 24 bytes, MEMORY class.
type CXSourceRange struct {
	PtrData  [2]uintptr
	BeginInt uint32
	EndInt   uint32
}

// CXCursorKind values used by this package. They are part of libclang's stable
// C ABI; TestKindConstants checks every one of them against
// clang_getCursorKindSpelling at run time, so a wrong number cannot go unnoticed.
const (
	cursorInvalidFile     int32 = 70
	cursorNoDeclFound     int32 = 71
	cursorTranslationUnit int32 = 350

	cursorStructDecl       int32 = 2
	cursorUnionDecl        int32 = 3
	cursorClassDecl        int32 = 4
	cursorEnumDecl         int32 = 5
	cursorFieldDecl        int32 = 6
	cursorEnumConstantDecl int32 = 7
	cursorFunctionDecl     int32 = 8

	cursorCXXMethod          int32 = 21
	cursorNamespace          int32 = 22
	cursorVarDecl            int32 = 9
	cursorConversionFunction int32 = 26
	cursorLinkageSpec        int32 = 23
	cursorConstructor        int32 = 24
	cursorDestructor         int32 = 25
	cursorNamespaceAlias     int32 = 33
	cursorBaseSpecifier      int32 = 44

	// Templates. They are declarations with no layout and no symbol until they
	// are instantiated, and both the record and the function model have to say
	// so instead of leaving them out.
	cursorFunctionTemplate                   int32 = 30
	cursorClassTemplate                      int32 = 31
	cursorClassTemplatePartialSpecialization int32 = 32

	// Template parameters, in declaration order. They are what the model
	// substitutes when it reads a specialization through its pattern.
	cursorTemplateTypeParameter     int32 = 27
	cursorNonTypeTemplateParameter  int32 = 28
	cursorTemplateTemplateParameter int32 = 29
)

// CXTypeKind values used by this package, also checked at run time by
// TestKindConstants.
const (
	typeInvalid int32 = 0

	typeVoid       int32 = 2
	typeBool       int32 = 3
	typeCharU      int32 = 4
	typeUChar      int32 = 5
	typeChar16     int32 = 6
	typeChar32     int32 = 7
	typeUShort     int32 = 8
	typeUInt       int32 = 9
	typeULong      int32 = 10
	typeULongLong  int32 = 11
	typeUInt128    int32 = 12
	typeCharS      int32 = 13
	typeSChar      int32 = 14
	typeWChar      int32 = 15
	typeShort      int32 = 16
	typeInt        int32 = 17
	typeLong       int32 = 18
	typeLongLong   int32 = 19
	typeInt128     int32 = 20
	typeFloat      int32 = 21
	typeDouble     int32 = 22
	typeLongDouble int32 = 23

	typePointer         int32 = 101
	typeBlockPointer    int32 = 102
	typeLValueReference int32 = 103
	typeRValueReference int32 = 104
	typeRecord          int32 = 105
	typeEnum            int32 = 106
	typeTypedef         int32 = 107

	typeConstantArray       int32 = 112
	typeIncompleteArray     int32 = 114
	typeVariableArray       int32 = 115
	typeDependentSizedArray int32 = 116

	typeNullPtr int32 = 24
)

// CX_StorageClass values, as returned by clang_Cursor_getStorageClass.
const storageClassStatic int32 = 3

// CX_CXXAccessSpecifier values, as returned by clang_getCXXAccessSpecifier.
// accessInvalid is what a namespace-scope declaration reports.
const (
	accessInvalid   int32 = 0
	accessPublic    int32 = 1
	accessProtected int32 = 2
	accessPrivate   int32 = 3
)

// CXCursor_ExceptionSpecificationKind values, as returned by
// clang_getCursorExceptionSpecificationType. Only the three spellings that are
// known to promise nothing escapes are treated as such: a computed noexcept
// is evaluated by the compiler and libclang does not expose the result, so it
// is conservatively allowed to throw.
const (
	exceptionSpecNone             int32 = 0
	exceptionSpecDynamicNone      int32 = 1
	exceptionSpecDynamic          int32 = 2
	exceptionSpecMSAny            int32 = 3
	exceptionSpecBasicNoexcept    int32 = 4
	exceptionSpecComputedNoexcept int32 = 5
	exceptionSpecUnevaluated      int32 = 6
	exceptionSpecUninstantiated   int32 = 7
	exceptionSpecUnparsed         int32 = 8
	exceptionSpecNoThrow          int32 = 9
)

// CXTemplateArgumentKind values, as returned by
// clang_Cursor_getTemplateArgumentKind.
const (
	templateArgumentNull        int32 = 0
	templateArgumentType        int32 = 1
	templateArgumentDeclaration int32 = 2
	templateArgumentNullPtr     int32 = 3
	templateArgumentIntegral    int32 = 4
	templateArgumentTemplate    int32 = 5
	templateArgumentExpansion   int32 = 6
	templateArgumentExpression  int32 = 7
	templateArgumentPack        int32 = 8
	templateArgumentInvalid     int32 = 9
)

// CXChildVisitResult, the return value of the visitor callback.
const (
	visitBreak    int32 = 0
	visitContinue int32 = 1
	visitRecurse  int32 = 2
)

// CXErrorCode values returned by clang_parseTranslationUnit2.
const (
	parseOK               int32 = 0
	parseFailure          int32 = 1
	parseCrashed          int32 = 2
	parseInvalidArguments int32 = 3
	parseASTReadError     int32 = 4
)

// parseErrorName names a CXErrorCode for error messages.
func parseErrorName(code int32) string {
	switch code {
	case parseOK:
		return "success"
	case parseFailure:
		return "a generic error occurred while parsing"
	case parseCrashed:
		return "the translation unit crashed while parsing"
	case parseInvalidArguments:
		return "invalid arguments were passed to the parser"
	case parseASTReadError:
		return "the AST file could not be read"
	default:
		return fmt.Sprintf("unknown CXErrorCode %d", code)
	}
}

// ---------------------------------------------------------------------------
// The binding layer.
//
// # ABI inventory
//
// libclang's C API is not "a list of uintptrs": nine of the calls below move a
// C struct by value across the boundary, which the SysV AMD64 ABI classifies as
// follows (see DESIGN.md, "libclang, without cgo"). This inventory is the
// reason the call layer is purego rather than a hand-rolled syscall, and it is
// what any future FFI work would have to reproduce exactly.
//
// Structs passed BY VALUE as arguments:
//
//	CXString (16 B, INTEGER,INTEGER -> RDI/RSI), by:
//	  clang_getCString, clang_disposeString
//
//	CXCursor (32 B, MEMORY -> stack), by:
//	  clang_getCursorKind, clang_getCursorSpelling, clang_getCursorDisplayName,
//	  clang_getCursorType, clang_Cursor_getMangling, clang_Cursor_isFunctionInlined,
//	  clang_isCursorDefinition, clang_getCursorLocation, clang_getCursorSemanticParent,
//	  clang_getTypeDeclaration, clang_isVirtualBase, clang_Cursor_getStorageClass,
//	  clang_CXXMethod_isStatic,
//	  clang_CXXMethod_isVirtual, clang_CXXMethod_isConst, clang_CXXMethod_isDefaulted,
//	  clang_CXXConstructor_isCopyConstructor, clang_CXXConstructor_isMoveConstructor,
//	  clang_visitChildren
//
//	CXType (24 B, MEMORY -> stack), by:
//	  clang_getCanonicalType, clang_getTypeSpelling, clang_getResultType,
//	  clang_getNumArgTypes, clang_getArgType, clang_Type_getSizeOf,
//	  clang_getTypeDeclaration
//
//	CXSourceLocation (24 B, MEMORY -> stack), by:
//	  clang_getFileLocation, clang_Location_isFromMainFile,
//	  clang_Location_isInSystemHeader
//
// Structs RETURNED IN MEMORY through a hidden pointer (the caller passes the
// destination in RDI and the callee returns it in RAX):
//
//	CXCursor (32 B): clang_getTranslationUnitCursor, clang_getCursorSemanticParent,
//	                 clang_getTypeDeclaration
//	CXType    (24 B): clang_getCursorType, clang_getCanonicalType,
//	                 clang_getResultType, clang_getArgType
//	CXSourceLocation (24 B): clang_getCursorLocation
//
// Structs returned in registers (16 B, RAX:RDX) — the CXString producers:
//
//	clang_getClangVersion, clang_getCursorSpelling, clang_getCursorDisplayName,
//	clang_getTypeSpelling, clang_getTypeKindSpelling, clang_getCursorKindSpelling,
//	clang_Cursor_getMangling, clang_getFileName
//
// And the one place where libclang calls *into Go*: clang_visitChildren invokes
// a C function pointer whose first two parameters are CXCursor values BY VALUE,
// i.e. two MEMORY-class aggregates on the stack. That only works if the
// callback machinery reads struct arguments out of the incoming stack frame.
// ---------------------------------------------------------------------------
type libclangFuncs struct {
	// Version and strings.
	getClangVersion func() CXString
	getCString      func(CXString) *byte
	disposeString   func(CXString)

	// Index and translation units.
	createIndex              func(excludeDeclsFromPCH, displayDiagnostics int32) uintptr
	disposeIndex             func(index uintptr)
	parseTranslationUnit2    func(index uintptr, sourceFilename *byte, args **byte, numArgs int32, unsavedFiles uintptr, numUnsavedFiles, options uint32, outTU *uintptr) int32
	disposeTranslationUnit   func(tu uintptr)
	getTranslationUnitCursor func(tu uintptr) CXCursor

	// Cursor inspection.
	visitChildren        func(parent CXCursor, visitor uintptr, clientData unsafe.Pointer) uint32
	getCursorKind        func(CXCursor) int32
	getCursorSpelling    func(CXCursor) CXString
	getCursorDisplayName func(CXCursor) CXString
	getCursorType        func(CXCursor) CXType
	getCursorLocation    func(CXCursor) CXSourceLocation
	getCursorExtent      func(CXCursor) CXSourceRange
	getRangeStart        func(CXSourceRange) CXSourceLocation
	getRangeEnd          func(CXSourceRange) CXSourceLocation
	isCursorDefinition   func(CXCursor) int32
	isFunctionInlined    func(CXCursor) int32
	getSemanticParent    func(CXCursor) CXCursor
	getCursorDefinition  func(CXCursor) CXCursor
	cursorMangling       func(CXCursor) CXString
	isVirtualBase        func(CXCursor) int32
	cursorGetArgument    func(CXCursor, uint32) CXCursor

	// Methods and linkage.
	getCursorStorageClass        func(CXCursor) int32
	methodIsStatic               func(CXCursor) int32
	methodIsVirtual              func(CXCursor) int32
	methodIsConst                func(CXCursor) int32
	methodIsDefaulted            func(CXCursor) int32
	methodIsDeleted              func(CXCursor) int32
	constructorIsCopyConstructor func(CXCursor) int32
	constructorIsMoveConstructor func(CXCursor) int32

	// Types.
	getCanonicalType      func(CXType) CXType
	getPointeeType        func(CXType) CXType
	getNonReferenceType   func(CXType) CXType
	getArrayElementType   func(CXType) CXType
	getArraySize          func(CXType) int64
	isConstQualifiedType  func(CXType) int32
	isPODType             func(CXType) int32
	getTypeSpelling       func(CXType) CXString
	getTypeKindSpelling   func(kind int32) CXString
	getCursorKindSpelling func(kind int32) CXString
	getResultType         func(CXType) CXType
	getNumArgTypes        func(CXType) int32
	getArgType            func(CXType, uint32) CXType
	typeGetSizeOf         func(CXType) int64
	typeGetAlignOf        func(CXType) int64
	typeGetOffsetOf       func(CXType, *byte) int64
	getTypeDeclaration    func(CXType) CXCursor

	// Records.
	getCXXAccessSpecifier func(CXCursor) int32
	cursorOffsetOfField   func(CXCursor) int64
	cursorIsBitField      func(CXCursor) int32
	fieldDeclBitWidth     func(CXCursor) int32
	cursorIsAnonymous     func(CXCursor) int32
	methodIsPureVirtual   func(CXCursor) int32
	cxxRecordIsAbstract   func(CXCursor) int32
	getOffsetOfBase       func(CXCursor, CXCursor) int64

	// Enums.
	getEnumDeclIntegerType       func(CXCursor) CXType
	getEnumConstantValue         func(CXCursor) int64
	getEnumConstantUnsignedValue func(CXCursor) uint64
	enumDeclIsScoped             func(CXCursor) int32
	getTemplateCursorKind        func(CXCursor) int32

	// Function properties.
	cursorIsVariadic    func(CXCursor) int32
	cursorExceptionSpec func(CXCursor) int32

	// Templates.
	getNumTemplateArguments          func(CXCursor) int32
	getTemplateArgumentKind          func(CXCursor, uint32) int32
	getTemplateArgumentType          func(CXCursor, uint32) CXType
	getTemplateArgumentValue         func(CXCursor, uint32) int64
	getTemplateArgumentUnsignedValue func(CXCursor, uint32) uint64
	getSpecializedCursorTemplate     func(CXCursor) CXCursor

	// Diagnostics.
	getNumDiagnostics     func(uintptr) uint32
	getDiagnostic         func(uintptr, uint32) uintptr
	getDiagnosticSeverity func(uintptr) int32
	getDiagnosticSpelling func(uintptr) CXString
	getDiagnosticLocation func(uintptr) CXSourceLocation
	disposeDiagnostic     func(uintptr)

	// Locations.
	getFileLocation          func(CXSourceLocation, *uintptr, *uint32, *uint32, *uint32)
	locationIsFromMainFile   func(CXSourceLocation) int32
	locationIsInSystemHeader func(CXSourceLocation) int32
	getFileName              func(uintptr) CXString
}

// functionTable is the name -> receiver pair list used to bind libclang.
// Binding is done field by field so a missing symbol names itself in the error.
func (f *libclangFuncs) bind(handle uintptr) error {
	bindings := []struct {
		name string
		into any
	}{
		{"clang_getClangVersion", &f.getClangVersion},
		{"clang_getCString", &f.getCString},
		{"clang_disposeString", &f.disposeString},
		{"clang_createIndex", &f.createIndex},
		{"clang_disposeIndex", &f.disposeIndex},
		{"clang_parseTranslationUnit2", &f.parseTranslationUnit2},
		{"clang_disposeTranslationUnit", &f.disposeTranslationUnit},
		{"clang_getTranslationUnitCursor", &f.getTranslationUnitCursor},
		{"clang_visitChildren", &f.visitChildren},
		{"clang_getCursorKind", &f.getCursorKind},
		{"clang_getCursorSpelling", &f.getCursorSpelling},
		{"clang_getCursorDisplayName", &f.getCursorDisplayName},
		{"clang_getCursorType", &f.getCursorType},
		{"clang_getCursorLocation", &f.getCursorLocation},
		{"clang_getCursorExtent", &f.getCursorExtent},
		{"clang_getRangeStart", &f.getRangeStart},
		{"clang_getRangeEnd", &f.getRangeEnd},
		{"clang_isCursorDefinition", &f.isCursorDefinition},
		{"clang_Cursor_isFunctionInlined", &f.isFunctionInlined},
		{"clang_getCursorSemanticParent", &f.getSemanticParent},
		{"clang_getCursorDefinition", &f.getCursorDefinition},
		{"clang_Cursor_getMangling", &f.cursorMangling},
		{"clang_isVirtualBase", &f.isVirtualBase},
		{"clang_Cursor_getArgument", &f.cursorGetArgument},
		{"clang_Cursor_getStorageClass", &f.getCursorStorageClass},
		{"clang_CXXMethod_isStatic", &f.methodIsStatic},
		{"clang_CXXMethod_isVirtual", &f.methodIsVirtual},
		{"clang_CXXMethod_isConst", &f.methodIsConst},
		{"clang_CXXMethod_isDefaulted", &f.methodIsDefaulted},
		{"clang_CXXMethod_isDeleted", &f.methodIsDeleted},
		{"clang_CXXConstructor_isCopyConstructor", &f.constructorIsCopyConstructor},
		{"clang_CXXConstructor_isMoveConstructor", &f.constructorIsMoveConstructor},
		{"clang_getCanonicalType", &f.getCanonicalType},
		{"clang_getPointeeType", &f.getPointeeType},
		{"clang_getNonReferenceType", &f.getNonReferenceType},
		{"clang_getArrayElementType", &f.getArrayElementType},
		{"clang_getArraySize", &f.getArraySize},
		{"clang_isConstQualifiedType", &f.isConstQualifiedType},
		{"clang_isPODType", &f.isPODType},
		{"clang_getTypeSpelling", &f.getTypeSpelling},
		{"clang_getTypeKindSpelling", &f.getTypeKindSpelling},
		{"clang_getCursorKindSpelling", &f.getCursorKindSpelling},
		{"clang_getResultType", &f.getResultType},
		{"clang_getNumArgTypes", &f.getNumArgTypes},
		{"clang_getArgType", &f.getArgType},
		{"clang_Type_getSizeOf", &f.typeGetSizeOf},
		{"clang_Type_getAlignOf", &f.typeGetAlignOf},
		{"clang_Type_getOffsetOf", &f.typeGetOffsetOf},
		{"clang_getTypeDeclaration", &f.getTypeDeclaration},
		{"clang_getCXXAccessSpecifier", &f.getCXXAccessSpecifier},
		{"clang_Cursor_getOffsetOfField", &f.cursorOffsetOfField},
		{"clang_Cursor_isBitField", &f.cursorIsBitField},
		{"clang_getFieldDeclBitWidth", &f.fieldDeclBitWidth},
		{"clang_Cursor_isAnonymous", &f.cursorIsAnonymous},
		{"clang_CXXMethod_isPureVirtual", &f.methodIsPureVirtual},
		{"clang_CXXRecord_isAbstract", &f.cxxRecordIsAbstract},
		{"clang_getOffsetOfBase", &f.getOffsetOfBase},
		{"clang_getEnumDeclIntegerType", &f.getEnumDeclIntegerType},
		{"clang_getEnumConstantDeclValue", &f.getEnumConstantValue},
		{"clang_getEnumConstantDeclUnsignedValue", &f.getEnumConstantUnsignedValue},
		{"clang_EnumDecl_isScoped", &f.enumDeclIsScoped},
		{"clang_getTemplateCursorKind", &f.getTemplateCursorKind},
		{"clang_Cursor_isVariadic", &f.cursorIsVariadic},
		{"clang_getCursorExceptionSpecificationType", &f.cursorExceptionSpec},
		{"clang_Cursor_getNumTemplateArguments", &f.getNumTemplateArguments},
		{"clang_Cursor_getTemplateArgumentKind", &f.getTemplateArgumentKind},
		{"clang_Cursor_getTemplateArgumentType", &f.getTemplateArgumentType},
		{"clang_Cursor_getTemplateArgumentValue", &f.getTemplateArgumentValue},
		{"clang_Cursor_getTemplateArgumentUnsignedValue", &f.getTemplateArgumentUnsignedValue},
		{"clang_getSpecializedCursorTemplate", &f.getSpecializedCursorTemplate},
		{"clang_getNumDiagnostics", &f.getNumDiagnostics},
		{"clang_getDiagnostic", &f.getDiagnostic},
		{"clang_getDiagnosticSeverity", &f.getDiagnosticSeverity},
		{"clang_getDiagnosticSpelling", &f.getDiagnosticSpelling},
		{"clang_getDiagnosticLocation", &f.getDiagnosticLocation},
		{"clang_disposeDiagnostic", &f.disposeDiagnostic},
		{"clang_getFileLocation", &f.getFileLocation},
		{"clang_Location_isFromMainFile", &f.locationIsFromMainFile},
		{"clang_Location_isInSystemHeader", &f.locationIsInSystemHeader},
		{"clang_getFileName", &f.getFileName},
	}
	for _, b := range bindings {
		if err := bindFunc(b.into, handle, b.name); err != nil {
			return err
		}
	}
	return nil
}

// bindFunc is a thin wrapper over purego.RegisterLibFunc so that a missing
// symbol becomes a readable error instead of a panic.
func bindFunc(into any, handle uintptr, name string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("cxx: binding %s: %v", name, r)
		}
	}()
	purego.RegisterLibFunc(into, handle, name)
	return nil
}

// Libclang is a loaded libclang.
//
// Handles are reference counted: Load returns the same handle to every caller
// and only the last Close unloads the library. A Libclang is safe for concurrent
// use; the indexes and translation units created from it are not.
type Libclang struct {
	path   string
	handle uintptr
	f      libclangFuncs

	mu     sync.Mutex
	refs   int
	closed bool
}

var (
	loadMu sync.Mutex
	loaded *Libclang // the process-wide handle for the default search path
)

// Load locates libclang and returns a handle to it.
//
// The search runs once per process: repeated calls return the same handle with
// an incremented reference count (Close decrements it, and closes the library
// when it reaches zero). Use LoadPath to load a specific library, and Close
// followed by Load to re-run the search after changing CPPGO_LIBCLANG.
func Load() (*Libclang, error) {
	loadMu.Lock()
	defer loadMu.Unlock()
	if loaded != nil {
		return loaded.retain()
	}
	l, err := loadFirstAvailable(candidatePaths())
	if err != nil {
		return nil, err
	}
	loaded = l
	return l.retain()
}

// LoadPath loads the libclang at path, without consulting the environment or
// the usual locations and without caching the result. It is what tests use to
// point the package at a specific library; production code wants Load.
func LoadPath(path string) (*Libclang, error) {
	l, err := loadPath(path)
	if err != nil {
		return nil, err
	}
	l.refs = 1
	return l, nil
}

// loadPath dlopens path and binds the functions we use.
func loadPath(path string) (*Libclang, error) {
	handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return nil, &NotFoundError{Paths: []string{path}, Cause: err}
	}
	l := &Libclang{path: path, handle: handle}
	if err := l.f.bind(handle); err != nil {
		_ = purego.Dlclose(handle)
		return nil, &NotFoundError{Paths: []string{path}, Cause: err}
	}
	// Smoke-test the struct layouts before anybody relies on them: a wrong
	// CXString or a missing hidden-pointer return shows up here as garbage
	// rather than as a mystery three layers down.
	if v := l.str(l.f.getClangVersion()); !clangVersionLooksSane(v) {
		_ = purego.Dlclose(handle)
		return nil, &NotFoundError{
			Paths: []string{path},
			Cause: fmt.Errorf("clang_getClangVersion returned %q: libclang ABI does not match (bad CXString or sret layout)", v),
		}
	}
	return l, nil
}

// clangVersionLooksSane reports whether v is a plausible clang version banner.
//
// It is a smoke test for the CXString layout: a wrong struct would give garbage
// or an empty string. It must not be a prefix test, because distributions patch
// the banner - Debian's libclang answers "Debian clang version 14.0.6" and
// Ubuntu's "Ubuntu clang version 18.1.3" - and rejecting those made every
// introspection test skip on a stock CI runner.
func clangVersionLooksSane(v string) bool {
	return strings.Contains(v, "clang version")
}

func (l *Libclang) retain() (*Libclang, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, ErrClosed
	}
	l.refs++
	return l, nil
}

// Path is the file libclang was loaded from.
func (l *Libclang) Path() string { return l.path }

// Version is clang_getClangVersion(), e.g. "Debian clang version 22.1.8".
func (l *Libclang) Version() string {
	if l.isClosed() {
		return ""
	}
	return l.str(l.f.getClangVersion())
}

// Close releases this handle. The library is unloaded when the last reference
// goes away; closing an already-closed handle is an error, not a panic.
func (l *Libclang) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return errors.New("cxx: libclang handle already closed")
	}
	l.refs--
	if l.refs > 0 {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	handle := l.handle
	l.mu.Unlock()

	loadMu.Lock()
	if loaded == l {
		loaded = nil
	}
	loadMu.Unlock()

	// dlclose only fails for a handle that was never opened, which the
	// reference count makes impossible.
	_ = purego.Dlclose(handle)
	return nil
}

func (l *Libclang) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// str converts a CXString to a Go string and disposes it. The bytes have to be
// copied out *before* clang_disposeString runs: the CXString owns its buffer
// (private_flags says so) and the pointer is dangling afterwards.
func (l *Libclang) str(s CXString) string {
	p := l.f.getCString(s)
	if p == nil {
		l.f.disposeString(s)
		return ""
	}
	var n int
	for *(*byte)(unsafe.Add(unsafe.Pointer(p), n)) != 0 {
		n++
	}
	out := string(unsafe.Slice(p, n))
	l.f.disposeString(s)
	return out
}

// loadFirstAvailable implements the library search: CPPGO_LIBCLANG first
// (exclusively), then the usual names and directories, newest version first.
// Each candidate is fully bound and smoke-tested before it is accepted, so a
// library that only looks like libclang is not mistaken for one.
func loadFirstAvailable(candidates []string) (*Libclang, error) {
	if env := os.Getenv("CPPGO_LIBCLANG"); env != "" {
		if _, err := os.Stat(env); err != nil {
			return nil, &NotFoundError{Paths: []string{env}, Cause: err}
		}
		return loadPath(env)
	}
	var lastErr error
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		l, err := loadPath(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		return l, nil
	}
	return nil, &NotFoundError{Paths: candidates, Cause: lastErr}
}

// candidatePaths is the ordered list of places to look for libclang. It is a
// variable so that tests can assert the ordering, and drive the search to a
// failure, without uninstalling anything.
var candidatePaths = func() []string {
	var out []string
	dirs := []string{"", "/usr/lib", "/usr/lib64", "/usr/local/lib", "/usr/lib/x86_64-linux-gnu", "/lib/x86_64-linux-gnu", "/usr/lib/aarch64-linux-gnu"}
	// Unversioned names first: a user who installed a development package
	// means that one.
	for _, name := range []string{"libclang.so", "libclang.dylib"} {
		for _, dir := range dirs {
			out = append(out, filepath.Join(dir, name))
		}
	}
	// Then versioned ones, newest first, including the per-LLVM layouts used
	// by distro packages (/usr/lib/llvm-22/lib) and Homebrew.
	patterns := []string{
		"/usr/lib/libclang-*.so*",
		"/usr/lib/llvm-*/lib/libclang.so*",
		"/usr/local/Cellar/llvm/*/lib/libclang.dylib",
		"/usr/local/opt/llvm/lib/libclang.dylib",
	}
	for _, pattern := range patterns {
		// The patterns are constants, so a malformed one is impossible.
		matches, _ := filepath.Glob(pattern)
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		out = append(out, matches...)
	}
	// os.Stat on the empty-dir joins above is harmless; dedupe anyway so the
	// error message stays readable.
	seen := make(map[string]bool, len(out))
	uniq := make([]string, 0, len(out))
	for _, p := range out {
		if p != "" && !seen[p] {
			seen[p] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// ---------------------------------------------------------------------------
// Visiting children: libclang calls back into Go with two CXCursor values by
// value. One trampoline is created per process and dispatches through a
// registry keyed by the CXClientData pointer, so no callback slots leak and
// nested visits work.
// ---------------------------------------------------------------------------

// visitorFunc is called for every child cursor. Returning visitRecurse descends
// into the child.
type visitorFunc func(cursor, parent CXCursor) int32

var visitors struct {
	sync.Mutex
	ptr   uintptr
	funcs map[*visitKey]visitorFunc
	ids   uint64
}

// visitKey is the CXClientData cookie handed to libclang: an opaque pointer
// that identifies the running visit. It is a real Go object so that the
// registry key needs no unsafe arithmetic, and it is kept alive by the map.
type visitKey struct {
	id uint64
}

func visitorPtr() uintptr {
	visitors.Lock()
	defer visitors.Unlock()
	if visitors.ptr == 0 {
		visitors.funcs = make(map[*visitKey]visitorFunc)
		visitors.ptr = purego.NewCallback(dispatchVisit)
	}
	return visitors.ptr
}

// dispatchVisit is the one C -> Go trampoline: it looks the running visit up by
// its client-data cookie. Cursors arrive here by value, in memory.
func dispatchVisit(cursor, parent CXCursor, clientData unsafe.Pointer) int32 {
	key := (*visitKey)(clientData)
	visitors.Lock()
	fn := visitors.funcs[key]
	visitors.Unlock()
	if fn == nil {
		return visitBreak
	}
	return fn(cursor, parent)
}

// nextVisitID is a monotonic counter, useful only in debugger output.
func nextVisitID() uint64 {
	visitors.Lock()
	defer visitors.Unlock()
	visitors.ids++
	return visitors.ids
}

// visit calls fn for every child of parent, descending when fn asks for it.
func (l *Libclang) visit(parent CXCursor, fn visitorFunc) error {
	if l.isClosed() {
		return ErrClosed
	}
	// Create the trampoline (and its registry) before registering the visit, so
	// that a concurrent first visit cannot replace the map underneath us.
	ptr := visitorPtr()
	key := &visitKey{id: nextVisitID()}
	visitors.Lock()
	visitors.funcs[key] = fn
	visitors.Unlock()
	defer func() {
		visitors.Lock()
		delete(visitors.funcs, key)
		visitors.Unlock()
	}()
	l.f.visitChildren(parent, ptr, unsafe.Pointer(key))
	return nil
}

// ---------------------------------------------------------------------------
// Translation units.
// ---------------------------------------------------------------------------

// TU is a parsed translation unit. It must be closed.
type TU struct {
	l               *Libclang
	index           uintptr
	tu              uintptr
	File            string   // the main file, as given
	Args            []string // the flags the unit was parsed with
	once            sync.Once
	symbols         []Symbol
	byName          map[string][]int
	byNameMangled   map[string]int
	memberTemplates map[string]memberTemplate
	records         []Record
	byRecord        map[string]int
	enums           []Enum
	byEnum          map[string]int
	closed          bool
	mu              sync.Mutex
}

// ParseTranslationUnit parses file with the given command-line flags. The flags
// are the ones a compiler would accept, excluding the input file itself.
func (l *Libclang) ParseTranslationUnit(file string, args []string) (*TU, error) {
	if l.isClosed() {
		return nil, ErrClosed
	}
	if file == "" {
		return nil, errors.New("cxx: no file to parse")
	}
	if _, err := os.Stat(file); err != nil {
		return nil, fmt.Errorf("cxx: %w", err)
	}
	if abs, err := filepath.Abs(file); err == nil {
		file = abs
	}
	index := l.f.createIndex(0, 0)
	if index == 0 {
		return nil, errors.New("cxx: clang_createIndex returned a null index")
	}

	name := append([]byte(file), 0)
	keep := make([][]byte, 0, len(args))
	argv := make([]*byte, 0, len(args))
	for _, a := range args {
		b := append([]byte(a), 0)
		keep = append(keep, b)
		argv = append(argv, &keep[len(keep)-1][0])
	}
	var argPtr **byte
	if len(argv) > 0 {
		argPtr = &argv[0]
	}
	var tu uintptr
	code := l.f.parseTranslationUnit2(index, &name[0], argPtr, int32(len(argv)), 0, 0, 0, &tu)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(argv)
	runtime.KeepAlive(name)
	if code != parseOK {
		l.f.disposeIndex(index)
		return nil, fmt.Errorf("cxx: parsing %s: %s", file, parseErrorName(code))
	}
	return &TU{l: l, index: index, tu: tu, File: file, Args: append([]string(nil), args...)}, nil
}

// Close disposes the translation unit and its index.
func (tu *TU) Close() error {
	tu.mu.Lock()
	if tu.closed {
		tu.mu.Unlock()
		return errors.New("cxx: translation unit already closed")
	}
	tu.closed = true
	tu.mu.Unlock()
	tu.l.f.disposeTranslationUnit(tu.tu)
	tu.l.f.disposeIndex(tu.index)
	return nil
}

// isClosed reports whether Close has run.
func (tu *TU) isClosed() bool {
	tu.mu.Lock()
	defer tu.mu.Unlock()
	return tu.closed
}

// Symbols returns every function, method, constructor and destructor declared
// in the translation unit, in source order, excluding system headers and
// anything with internal linkage.
func (tu *TU) Symbols() []Symbol {
	tu.once.Do(tu.extract)
	return append([]Symbol(nil), tu.symbols...)
}

// Lookup finds the first symbol with the given qualified C++ name, e.g.
// "mathx::Vector::norm". Overloads share a qualified name; use Lookups to get
// all of them or LookupMangled for an exact one.
func (tu *TU) Lookup(name string) (Symbol, bool) {
	tu.once.Do(tu.extract)
	idxs := tu.byName[name]
	if len(idxs) == 0 {
		return Symbol{}, false
	}
	return tu.symbols[idxs[0]], true
}

// Lookups returns every overload with the given qualified C++ name.
func (tu *TU) Lookups(name string) []Symbol {
	tu.once.Do(tu.extract)
	idxs := tu.byName[name]
	out := make([]Symbol, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, tu.symbols[i])
	}
	return out
}

// LookupMangled finds a symbol by its Itanium ABI mangled name.
func (tu *TU) LookupMangled(mangled string) (Symbol, bool) {
	tu.once.Do(tu.extract)
	i, ok := tu.byNameMangled[mangled]
	if !ok {
		return Symbol{}, false
	}
	return tu.symbols[i], true
}

// Records returns every class, struct and union definition in the translation
// unit, in source order, excluding system headers. Templates are included with
// IsTemplate set and no layout: a declaration that cannot be laid out is still
// a declaration the generator has to report.
func (tu *TU) Records() []Record {
	tu.once.Do(tu.extract)
	return append([]Record(nil), tu.records...)
}

// Record finds a record by its qualified C++ name, e.g. "mathx::Vector".
func (tu *TU) Record(name string) (Record, bool) {
	tu.once.Do(tu.extract)
	i, ok := tu.byRecord[name]
	if !ok {
		return Record{}, false
	}
	return tu.records[i], true
}

// Enums returns every enum definition in the translation unit, in source
// order, excluding system headers.
func (tu *TU) Enums() []Enum {
	tu.once.Do(tu.extract)
	return append([]Enum(nil), tu.enums...)
}

// Enum finds an enum by its qualified C++ name.
func (tu *TU) Enum(name string) (Enum, bool) {
	tu.once.Do(tu.extract)
	i, ok := tu.byEnum[name]
	if !ok {
		return Enum{}, false
	}
	return tu.enums[i], true
}
