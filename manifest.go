package cppgo

// ManifestSchemaVersion is the layout version of Manifest that this package
// documents. A Manifest carries its own Schema, so a consumer can read a
// manifest produced by another generator version without guessing.
const ManifestSchemaVersion = 1

// Manifest describes how a generated package was produced: the tools and their
// versions, the shared objects and headers it was generated from, the
// compilation flags the headers were read with, and every binding, type and
// construct the package exposes.
//
// A generated package returns its manifest as a value:
//
//	func Manifest() cppgo.Manifest
//
// The value is a plain struct with exported fields and no state behind them, so
// a caller reads it directly. Every slice is ordered by the generator: a reader
// never sorts one, and two generations from the same inputs produce the same
// order. The slices are shared between copies of the value, so a caller must
// treat a Manifest and everything reachable from it as read-only.
//
// The zero Manifest is empty: no schema, no libraries, no bindings. Reading it
// is safe, but it describes no package.
type Manifest struct {
	// Schema is the layout version of this manifest. ManifestSchemaVersion is
	// the version this package documents; a consumer that does not recognize
	// the value cannot interpret the rest of the struct.
	Schema int
	// GeneratorVersion is the version of the generator that produced the
	// package, as the generator reports its own version.
	GeneratorVersion string
	// ClangVersion is the clang banner the libclang used to read the headers
	// reported, for example "Debian clang version 22.1.8".
	ClangVersion string
	// CompilerVersion is the banner of the C++ compiler that compiled the
	// generated shim.
	CompilerVersion string
	// Libraries are the shared objects the package loads, sorted by soname.
	Libraries []LibraryInfo
	// Headers are the headers the package was generated from, in the order the
	// generation command named them.
	Headers []Header
	// Flags are the compilation flags the headers were read with, ordered by
	// Flag.Order. Flag order is part of the parse, so it is never sorted.
	Flags []Flag
	// Bindings are the callable declarations the package exposes, sorted by
	// C++ signature.
	Bindings []Binding
	// Types are the C++ types the package names, with their Go mapping, sorted
	// by C++ spelling.
	Types []Type
	// Constructs are the declarations the package covers, grouped by kind and
	// ordered within each group.
	Constructs Constructs
}

// Packaging is which shared objects a generated package carries. It is the
// policy the package was generated with, not a guess from what was found on
// disk.
type Packaging uint8

const (
	// PackagingUnspecified means no packaging policy was recorded.
	PackagingUnspecified Packaging = iota
	// PackagingLibraryOnly packages the bound library and leaves every
	// dependency to the system's loader.
	PackagingLibraryOnly
	// PackagingLibraryAndDependencies packages the bound library and the
	// dependencies the generation selected explicitly.
	PackagingLibraryAndDependencies
	// PackagingExceptRuntime packages the bound library and its dependencies
	// except the C and C++ runtime the platform provides.
	PackagingExceptRuntime
	// PackagingAll packages the bound library and its complete dependency
	// closure.
	PackagingAll
)

// String implements fmt.Stringer.
func (p Packaging) String() string {
	switch p {
	case PackagingLibraryOnly:
		return "library"
	case PackagingLibraryAndDependencies:
		return "library+dependencies"
	case PackagingExceptRuntime:
		return "except-runtime"
	case PackagingAll:
		return "all"
	default:
		return "unspecified"
	}
}

// Dependency is one shared object a packaged library needs.
type Dependency struct {
	// Soname is the name the object declares in DT_SONAME, or its file name
	// when it declares none.
	Soname string
	// Path is the absolute path the object was resolved to, empty when it was
	// not found.
	Path string
	// Runtime reports whether the object is part of the platform's C or C++
	// runtime rather than a library the package supplies.
	Runtime bool
	// Embedded reports whether the object is carried inside the program.
	Embedded bool
	// Needs are the sonames this object declares it needs directly, sorted.
	Needs []string
}

// LibraryInfo is one shared object the generated package loads.
type LibraryInfo struct {
	// Soname is the name the object declares in DT_SONAME, or its file name
	// when it declares none. It is the name the package and its shim link and
	// load, never a build path.
	Soname string
	// Path is the absolute path the object was resolved to when the package was
	// generated. It is empty when the library is named but was not found, which
	// is the case for a bare loader name and for a header-only package with no
	// object of its own.
	Path string
	// SHA256 is the lowercase hexadecimal SHA-256 of the object's bytes. It is
	// empty when Path is empty.
	SHA256 string
	// Embedded reports whether the object is part of the program rather than a
	// file the deployment supplies.
	Embedded bool
	// Packaging is the packaging policy recorded for the object and its
	// dependencies.
	Packaging Packaging
	// Dependencies is the object's complete dependency closure in load order:
	// a dependency appears after the objects it needs. The slice is empty for a
	// dependency-free library.
	Dependencies []Dependency
}

// Header is one C++ header the generated package was produced from.
type Header struct {
	// Path is the absolute path of the header.
	Path string
	// RelativePath is Path relative to the directory the package was generated
	// in, with forward slashes. It is empty when the header lies outside that
	// directory.
	RelativePath string
	// SHA256 is the lowercase hexadecimal SHA-256 of the header's bytes.
	SHA256 string
}

// FlagSource identifies the rule that produced a compilation flag.
type FlagSource uint8

const (
	// FlagSourceUnknown means the flag's provenance was not recorded.
	FlagSourceUnknown FlagSource = iota
	// FlagSourceCompileCommands means the flag came from a
	// compile_commands.json database.
	FlagSourceCompileCommands
	// FlagSourceClangd means the flag came from a .clangd file.
	FlagSourceClangd
	// FlagSourceOverride means the caller supplied the flag on the command
	// line.
	FlagSourceOverride
	// FlagSourceAssumed means no project configuration was available and the
	// flag is an assumption.
	FlagSourceAssumed
)

// String implements fmt.Stringer.
func (s FlagSource) String() string {
	switch s {
	case FlagSourceCompileCommands:
		return "compile_commands.json"
	case FlagSourceClangd:
		return ".clangd"
	case FlagSourceOverride:
		return "override"
	case FlagSourceAssumed:
		return "assumed"
	default:
		return "unknown"
	}
}

// Flag is one compilation flag the headers were read with.
type Flag struct {
	// Value is the argument exactly as the parser received it, including a
	// leading dash and any joined value.
	Value string
	// Source identifies the rule that produced the flag.
	Source FlagSource
	// SourcePath is the file the flag was read from: a compile_commands.json
	// or a .clangd path. It is empty for a command-line override and for an
	// assumed flag.
	SourcePath string
	// Order is the flag's zero-based position in the parser's argument list.
	// The position is part of the parse, because a later flag overrides an
	// earlier one.
	Order int
}

// Carrier names the call path a binding takes to C++.
type Carrier uint8

const (
	// CarrierDirect calls the mangled C++ symbol through a generated assembly
	// trampoline.
	CarrierDirect Carrier = iota
	// CarrierThunk calls a generated extern "C" function compiled into the
	// cppgo-built shim.
	CarrierThunk
	// CarrierReentrant calls a generated thunk through the Go runtime, so C++
	// may call back into Go during the call.
	CarrierReentrant
)

// String implements fmt.Stringer.
func (c Carrier) String() string {
	switch c {
	case CarrierThunk:
		return "thunk"
	case CarrierReentrant:
		return "reentrant"
	default:
		return "direct"
	}
}

// Wire is how one parameter or result crosses the C++ ABI.
type Wire uint8

const (
	// WireNone carries no value, which is the case for a void result.
	WireNone Wire = iota
	// WireInteger travels in one INTEGER register word.
	WireInteger
	// WireFloat travels in one SSE register word.
	WireFloat
	// WirePointer travels as an address in one INTEGER register word.
	WirePointer
	// WirePointerLength travels as an address followed by a length, which is
	// how a string or a slice crosses the boundary.
	WirePointerLength
	// WireIndirect travels through an out-parameter rather than in a result
	// register.
	WireIndirect
)

// String implements fmt.Stringer.
func (w Wire) String() string {
	switch w {
	case WireInteger:
		return "integer"
	case WireFloat:
		return "float"
	case WirePointer:
		return "pointer"
	case WirePointerLength:
		return "pointer+length"
	case WireIndirect:
		return "indirect"
	default:
		return "none"
	}
}

// ErrorConvention is how a C++ declaration reports failure.
type ErrorConvention uint8

const (
	// ErrorNone means the declaration has no failure channel of its own.
	ErrorNone ErrorConvention = iota
	// ErrorException means the declaration can throw, and the shim turns the
	// exception into a Go error.
	ErrorException
	// ErrorCode means the declaration returns a std::error_code: zero is
	// success.
	ErrorCode
	// ErrorExpected means the declaration returns a std::expected or an
	// equivalent result type.
	ErrorExpected
	// ErrorOptional means the declaration returns a std::optional, whose empty
	// value becomes an error.
	ErrorOptional
	// ErrorStatus means the declaration returns a status integer: zero is
	// success.
	ErrorStatus
	// ErrorErrno means the declaration reports failure through errno.
	ErrorErrno
	// ErrorOutParam means the declaration reports failure through a return
	// value that is not itself the error, with the value and the failure in out
	// parameters.
	ErrorOutParam
	// ErrorCustom means the declaration returns a project-specific result type
	// whose accessors name the value and the failure.
	ErrorCustom
	// ErrorVariant means the declaration returns a variant with a known error
	// alternative.
	ErrorVariant
)

// String implements fmt.Stringer.
func (e ErrorConvention) String() string {
	switch e {
	case ErrorException:
		return "exception"
	case ErrorCode:
		return "error_code"
	case ErrorExpected:
		return "expected"
	case ErrorOptional:
		return "optional"
	case ErrorStatus:
		return "status"
	case ErrorErrno:
		return "errno"
	case ErrorOutParam:
		return "out_param"
	case ErrorCustom:
		return "custom"
	case ErrorVariant:
		return "variant"
	default:
		return "none"
	}
}

// Param is one visible parameter or value result of a binding.
type Param struct {
	// Name is the parameter's Go name. It is empty for a result.
	Name string
	// CppType is the type as the C++ declaration spells it.
	CppType string
	// GoType is the Go type the parameter or result is exposed as.
	GoType string
	// Wire is how the value crosses the ABI.
	Wire Wire
}

// Binding is one callable C++ declaration the generated package exposes.
type Binding struct {
	// CppName is the qualified C++ name of the declaration, for example
	// "mathx::Vector::norm".
	CppName string
	// Symbol is the dynamic symbol the binding resolves: the mangled C++ name
	// for a direct carrier, or the generated thunk for the other two carriers.
	Symbol string
	// Carrier is the call path to C++.
	Carrier Carrier
	// GoName is the Go identifier the binding is exposed as, without its
	// receiver.
	GoName string
	// GoSignature is the Go declaration as the generated package writes it,
	// receiver, parameters and results included.
	GoSignature string
	// CppSignature is the C++ declaration as the generated evidence comment
	// writes it: qualified name, parameter spellings, const and noexcept. The
	// result is in Results.
	CppSignature string
	// Params are the visible parameters, in declaration order. A method's
	// receiver is not repeated here.
	Params []Param
	// Results are the value results, in Go order. A result delivered as a pair,
	// such as (T, bool), has one entry per value. When Error is not ErrorNone,
	// the trailing error is described by Error and does not appear here.
	Results []Param
	// Error is the C++ failure convention the binding uses. ErrorNone means the
	// declaration has no failure channel of its own; a constructor or a
	// destructor can still report a lifecycle failure as a Go error, which
	// GoSignature shows.
	Error ErrorConvention
}

// TypeKind is what a C++ type becomes on the Go side.
type TypeKind uint8

const (
	// TypeOther means the type is exposed as something none of the other kinds
	// describes.
	TypeOther TypeKind = iota
	// TypeScalar means the type is a Go scalar: a boolean, an integer or a
	// float.
	TypeScalar
	// TypeEnum means the type is a C++ enum exposed as a named Go integer type.
	TypeEnum
	// TypeMirror means the type is a record whose Go value is its C++ layout.
	TypeMirror
	// TypeOpaque means the type is a record the Go side owns without seeing its
	// layout.
	TypeOpaque
	// TypeView means the type is a borrowed view of a C++ object the Go side
	// does not own.
	TypeView
	// TypeContainer means the type is a standard-library container, such as
	// std::string or std::vector, exposed as its Go equivalent.
	TypeContainer
	// TypeTemplate means the type is a C++ template exposed through the generic
	// Go API.
	TypeTemplate
)

// String implements fmt.Stringer.
func (k TypeKind) String() string {
	switch k {
	case TypeScalar:
		return "scalar"
	case TypeEnum:
		return "enum"
	case TypeMirror:
		return "mirror"
	case TypeOpaque:
		return "opaque"
	case TypeView:
		return "view"
	case TypeContainer:
		return "container"
	case TypeTemplate:
		return "template"
	default:
		return "other"
	}
}

// Type is one C++ type the generated package names and the Go type it maps to.
type Type struct {
	// CppType is the qualified C++ spelling of the type.
	CppType string
	// GoType is the Go type the C++ type maps to.
	GoType string
	// Kind is what the Go side does with a value of this type.
	Kind TypeKind
}

// Constructs groups the declarations a generated package covers, by kind.
// Every slice holds qualified C++ names and is sorted.
type Constructs struct {
	// Classes are the C++ classes the package exposes.
	Classes []string
	// Structs are the C++ structs the package exposes.
	Structs []string
	// Functions are the free functions the package exposes.
	Functions []string
	// Templates are the class and function templates the package exposes
	// through its generic API.
	Templates []string
	// Instantiations are the concrete template specializations the package was
	// built for.
	Instantiations []string
}
