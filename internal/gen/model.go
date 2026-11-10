package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// carrier names how a binding reaches C++.
type carrier uint8

const (
	// direct calls the mangled C++ symbol from the assembly trampoline.
	direct carrier = iota
	// thunk calls a generated extern "C" function compiled into the shim.
	thunk
	// reentrant calls a generated thunk through runtime.cgocall, because the
	// callee may call back into Go and only that state accepts a callback.
	reentrant
)

// String implements fmt.Stringer.
func (c carrier) String() string {
	switch c {
	case direct:
		return "direct"
	case reentrant:
		return "reentrant"
	default:
		return "thunk"
	}
}

// shimCarried reports whether the binding's symbol is a thunk cppgo emits and
// compiles, as opposed to a mangled symbol of the user's library.
func (c carrier) shimCarried() bool { return c != direct }

// class is a SysV argument register class.
type class uint8

const (
	classInt class = iota
	classFloat
)

// stdRole says what one wire word is for a standard-library parameter: the
// primary word (which the shim turns into the C++ value) or an extra word such
// as the length.
type stdRole uint8

const (
	stdRoleNone stdRole = iota
	stdRolePrimary
	stdRoleExtra
)

// SysV AMD64 argument registers, assigned independently per class.
var (
	intRegs   = []string{"DI", "SI", "DX", "CX", "R8", "R9"}
	floatRegs = []string{"X0", "X1", "X2", "X3", "X4", "X5", "X6", "X7"}
)

const (
	maxIntRegs   = 6
	maxFloatRegs = 8
)

// slot is one argument or result word on the trampoline frame.
type slot struct {
	label  string
	goType string
	class  class
	size   int
	offset int
	reg    string
}

// align is the natural alignment of a slot, which for the scalar widths here is
// its size.
func (s *slot) align() int { return s.size }

// recordModel is one bound C++ record.
type recordModel struct {
	// GoName is the exported Go type name.
	GoName string
	// CxxName is the qualified C++ name.
	CxxName string
	// Mirrored reports whether the Go value is the C layout (a struct with the
	// exported fields), as opposed to an opaque value owning C++ storage.
	Mirrored bool
	// Size and Align are sizeof and alignof, valid for an opaque record that
	// NewObject has to allocate.
	Size  int64
	Align int64
	// Fields are the mirrored fields, in declaration order.
	Fields []*fieldModel
	// Ctors are the factory constructors. One gets New<Type>; several share
	// that base until the overload rule suffixes them. A copy constructor is
	// Clone, not a factory, and a move constructor is not exposed.
	Ctors []*binding
	// Clone is the copy constructor binding, nil when not copyable.
	Clone *binding
	// Dtor is the destructor binding, nil when the record declares none.
	Dtor *binding
	// Methods are the non-static member functions.
	Methods []*binding
	// Statics are the static member functions, emitted as package functions.
	Statics []*binding
	// DefaultConstructible and CopyConstructible are the model's promises:
	// New<Type>() and Clone() are generated only when they are true.
	DefaultConstructible bool
	CopyConstructible    bool
	// Comment is the C++ doc comment, normalised by the Go emitter.
	Comment string
	// Bases are the public base classes, transitively.
	Bases []*baseModel
}

// fieldModel is one exported field of a mirrored record.
type fieldModel struct {
	GoName  string
	Type    *goType
	Offset  int64
	Comment string
}

// goParam is one visible parameter of the Go API.
type goParam struct {
	Name string
	Type *goType
	// StdOut is true for a non-const standard-library reference: the callee
	// replaces the value, so the Go parameter is a pointer to the slice and the
	// thunk copies the callee's result back into it.
	StdOut bool
	// Slice is true when the C++ parameter was a pointer paired with a length.
	Slice bool
	// Group is the slice group a slice shares a length with, or nil.
	Group []*goParam
}

// allArgs is every wire word of a binding: the ones inside the argument block
// and the ones beside it. An over-full call moves its value words into the
// block, and a reader that looks only at Wire misses them.
func (b *binding) allArgs() []*wireArg {
	out := make([]*wireArg, 0, len(b.Block)+len(b.Wire))
	out = append(out, b.Block...)
	return append(out, b.Wire...)
}

// wireArg is one value the trampoline passes to C, in C++ parameter order.
type wireArg struct {
	label    string
	slotType string
	class    class
	size     int
	expr     string
	keep     string
	cType    string
	cName    string
	// param is the C++ parameter this argument fills, zero for this and for a
	// thunk's out-pointer.
	param cxx.Param
	// stdRole is set for a standard-library parameter's words.
	stdRole stdRole
	// stdKind, stdLocal, stdArgs, stdElem and stdType describe the C++ value
	// the shim builds from the primary word.
	stdKind  stdKind
	stdLocal string
	stdArgs  []string
	stdElem  *goType
	stdType  *goType
	// cstringKeeps are the byte slices that keep a const char * argument
	// NUL-terminated for the call; each must stay alive until it returns.
	cstringKeeps []string
	// stdOutCopy is true for the primary word of a standard-library parameter
	// the callee replaces: after the call, the shim copies the local's elements
	// into the block the out-parameters describe.
	stdOutCopy bool
	// stdOutData and stdOutSize are the thunk parameters the copied block is
	// written through, and stdGoName is the Go parameter they replace.
	stdOutData string
	stdOutSize string
	stdGoName  string
	// isOut marks a standard-library result's out-parameter.
	isOut bool
	// callable is set for a function-pointer or std::function parameter: the
	// wire word is the callback table's address.
	callable *callbackType
	// callbackParam is the Go name of the function value the closure calls.
	callbackParam string
}

// binding is one callable C++ declaration bound to Go.
type binding struct {
	// sym is the model symbol the binding was built from.
	sym cxx.Symbol
	// Owner is the record a method, constructor or destructor belongs to.
	Owner *recordModel
	// Template is the pattern a generic binding belongs to, nil otherwise.
	Template *templateModel
	// templateArgs are the explicit C++ template arguments a manifest
	// instantiation supplies. The call written into the shim spells them, which
	// is what makes the compiler instantiate the function.
	templateArgs []string
	// cstringKeeps are the byte slices that keep a const char * argument
	// NUL-terminated for the call; each must stay alive until it returns.
	cstringKeeps []string
	// GoName is the final Go identifier.
	GoName string

	// Params are the visible Go parameters.
	Params []*goParam
	// Result is the value result, nil for void. A constructor's result is its
	// owning Go value and is represented by IsConstructor.
	Result *goType
	// IsConstructor, IsDestructor and IsStatic decide the Go shape.
	IsConstructor bool
	IsDestructor  bool
	IsStatic      bool
	// IsClone is true for the binding emitted as Clone(): a copy constructor
	// whose single source argument is the Go receiver.
	IsClone bool
	// staticField is the static data member a binding reads or writes, nil for
	// every other binding. A static member is a package function
	// <Type><Field>(), plus <Type>Set<Field>(v) when it is mutable, and the
	// thunk is what reads or writes the storage: its address is not a function.
	staticField *cxx.StaticField
	// staticWrite is true for the setter half of a static data member.
	staticWrite bool
	// memberField is the public field a binding reads or writes, nil for every
	// other binding. The accessor is a method of the record, carried by a thunk
	// that reads or writes the member.
	memberField *cxx.Field
	// fieldWrite is true for the setter half of a field accessor.
	fieldWrite bool
	// fieldByValue is true when the member's record type is held by value but
	// the Go side reaches it through a pointer.
	fieldByValue bool
	// fieldName overrides the Go base name a static data member takes.
	fieldName string
	// defaultPrefix is true for a binding that omits trailing default
	// arguments: the full signature keeps the bare Go name, and a prefix takes
	// the overload suffix.
	defaultPrefix bool
	// base is the base record an inherited method was copied from, nil for a
	// method the record declares itself.
	base *recordModel
	// castTo is the record a base view converts to, nil for every other
	// binding; dynamicCast makes it a dynamic_cast that may fail.
	castTo      *recordModel
	dynamicCast bool

	Carrier carrier
	Reason  string
	// Thunk is the extern "C" symbol, empty for a direct binding.
	Thunk string
	// Symbol is the name resolved from the library: the mangled name for a
	// direct binding, the thunk name otherwise.
	Symbol string
	// ID is the stable hash naming the symbol variable and the trampoline.
	ID string
	// SymbolVar and Trampoline are the Go declarations' names.
	SymbolVar  string
	Trampoline string
	// Status is true when the thunk returns an int32_t status and delivers a
	// value through an out-pointer.
	Status bool
	// ErrorResult is true when the Go signature ends in an error.
	ErrorResult bool
	// Wire are the C call arguments in order, the hidden `this` included.
	Wire []*wireArg
	// This is true when the call takes an implicit this.
	This bool
	// Slots are the trampoline frame words, fn first.
	Slots []*slot
	// Frame is the trampoline frame size.
	Frame int
	// hasStd is true when the signature names a standard-library type.
	hasStd bool
	// hasByValue is true when a mirrored record is passed or returned by value,
	// which is a shim binding whatever the register class says.
	hasByValue bool
	// hasCallback is true when the signature takes a Go function: the call must
	// reach C through runtime.cgocall, so C++ may call back into Go.
	hasCallback bool
	// statusResult is true when the declaration's 32-bit return is a status
	// code from -status: the thunk returns the callee's own value and the Go
	// side reports it as an error.
	statusResult bool
	// outPointer is true when the result travels through a heap allocation
	// instead of a stack local, which a call that may run a callback requires:
	// the callback may grow the goroutine stack, and a stack address handed to
	// C++ would move with it.
	outPointer bool
	// StdResult is true when the result is a standard-library value, which the
	// thunk copies out through out-parameters.
	StdResult bool
	// ViewResult is true when the result is a borrowed pointer to a record:
	// the Go value is a view of the receiver's object, one reference more, at
	// the address C++ returned. It is how a method that returns `T&` (`T *`)
	// is exposed without handing Go an address nobody owns.
	ViewResult bool
	// stdDecls are the Go locals the out-parameters point at.
	stdDecls []string
	// usesFree is true when the binding's result is a heap copy that the Go
	// side must release with the cppgo_free helper.
	usesFree bool
	// Out is the result a thunk writes through an out-pointer, nil when the
	// value comes back in a register or there is no value.
	Out *goType
	// OutLocal is the Go local the out-pointer aims at.
	OutLocal string
	// OutObject is the record whose storage the thunk constructs a by-value
	// non-POD result into. The Go side allocates it before the call and returns
	// an owning *T; the destructor runs at Close.
	OutObject *recordModel
	// Block are the input arguments an over-full call packs into the argument
	// block the thunk unpacks, nil when the call fits the register file. When it
	// is set, Wire holds one pointer to the block plus the out-parameters.
	Block []*wireArg
	// BlockSize is the block struct's size in bytes and BlockOffsets each
	// field's offset, for the assertions both compilers check. The struct's name
	// is derived from the final ID (see blockType), because a default-argument
	// prefix is re-salted after it is built.
	BlockSize    int
	BlockOffsets []int
	// blockReads maps a thunk parameter name to the expression that reads it out
	// of the block, one cast from the block's word to the declaration's own
	// type.
	blockReads map[string]string
	// selected is set by the binding manifest.
	selected bool
}

// packageModel is everything one generator run emits.
type packageModel struct {
	cfg     Config
	mapper  *mapper
	name    string
	flags   []string
	library string
	// libraryName is the recorded soname of the user's library: what the shim
	// names in DT_NEEDED and what the generated package loads. It is a name,
	// never a build path, which is what makes a generated package movable.
	libraryName string
	headers     []string
	records     []*recordModel
	enums       []*enumModel
	bindings    []*binding
	// templates are the class and function templates the package exposes
	// generically, and instantiations are the concrete specializations the
	// second phase collected from the Go program.
	templates      []*templateModel
	instantiations []*templateInstantiation
	verified       string
	warn           []string
	// usesFree is true when a result is a heap copy that Go releases with
	// cppgo_free.
	usesFree bool
	// usesSpan is true when the generated shim needs <span> (C++20).
	usesSpan bool
	// usesCString is true when a C++ `char *` crosses as a Go string, which
	// makes the generated package carry the NUL-termination helpers.
	usesCString bool
	// spans is the line range of every thunk in the generated translation unit,
	// which is how a compiler diagnostic names the binding it belongs to.
	spans []thunkSpan
	// build is the shim compile Generate ran, nil in check mode.
	build *shimBuild
	// coverage is the machine-readable per-declaration report, written when
	// the manifest asks for it with -coverage.
	coverage []byte
}

// builder carries the state of one model build.
type builder struct {
	cfg     Config
	mapper  *mapper
	records map[string]*recordModel
	// symbols and rawRecords are the merged declarations of every header.
	symbols    []cxx.Symbol
	rawRecords []cxx.Record
	rawEnums   []cxx.Enum
	// enums are the enum models by qualified name, and enumOrder their
	// insertion order for deterministic output.
	enums     map[string]*enumModel
	enumOrder []string
	// recordGoNames and enumGoNames are the Go type name of every record and
	// enum, computed before any model exists because a nested type's name is
	// its parent's.
	recordGoNames map[string]string
	enumGoNames   map[string]string
	// order records the insertion order of records, for deterministic output.
	order    []string
	bindings []*binding
	// templates are the generic models of every template pattern, and
	// templateByName finds one by its qualified C++ name for the scan.
	templates      []*templateModel
	templateByName map[string]*templateModel
	// instantiations are the concrete specializations the Go program uses, as
	// collected by the two-phase scan.
	instantiations []*templateInstantiation
	// synthMembers are the members of included template specializations. The
	// model reads them from the pattern, because libclang exposes no cursor for
	// the instantiated definition, so they are not in the project's symbol list.
	synthMembers []cxx.Symbol
	// instantiateMatched tracks the -instantiate entries a record matched.
	instantiateMatched []bool
	// instantiated is the set of specialization records this run synthesized
	// from a pattern, which buildRecords keeps like a named one.
	instantiated map[string]bool
	// bindMatched and excludeMatched track the -bind and -exclude entries a
	// declaration matched, which the pre-selection records before anything is
	// lowered: an entry that names a declaration outside what this build can
	// lower is still an entry that named something.
	bindMatched    []bool
	excludeMatched []bool
	// statusMatched tracks the -status entries a binding matched.
	statusMatched []bool
	verified      string
	warnings      []string
	// exported is the library's dynamic symbol table, read once, or nil when
	// -library names no readable file. It is the evidence the carrier decision
	// needs: a declaration with no visible definition is out of line only when
	// the library exports its mangled name.
	exported map[string]bool
	// linkedName is the user library's soname (or its base name when it
	// declares none): the DT_NEEDED entry the shim is linked with and the name
	// the generated package records. Empty when there is no library to load.
	linkedName string
	// flags are the compilation flags the headers were parsed with. The shim is
	// compiled with the same ones.
	flags       []string
	usesFree    bool
	usesCString bool
	// projects are the parsed translation units. They stay open until the
	// manifest's function instantiations are resolved, because only the project
	// can synthesize one: it holds the pattern cursors libclang does not visit.
	projects []*cxx.Project
	// templateRequests are the -instantiate entries that name a function or
	// member function instantiation.
	templateRequests []*templateRequest
	// neededRecords are records a manifest request depends on (the receiver of
	// an instantiated member), which are roots even without a -bind entry.
	neededRecords map[string]bool
	// selectedSymbols are the declarations the manifest selected, by symbol
	// identity. A selected declaration that produced no binding is a refusal,
	// and that is what the coverage report counts.
	selectedSymbols map[string]bool
	// refusalReasons is the sentence recorded for each refused declaration.
	refusalReasons map[string]string
	// skipReasons and skipClasses explain a selected declaration the generator
	// deliberately does not emit: a mirrored record's C++ constructor, a move
	// overload, a deleted member. A skip is not a refusal, and the coverage
	// report keeps the two apart.
	skipReasons map[string]string
	skipClasses map[string]string
	// manifestErr is the first request the manifest made that a declaration
	// cannot satisfy. It is fatal, unlike a declaration the build cannot carry.
	manifestErr error
}

// build loads the headers and turns the model into a package model.
func build(cfg Config) (*packageModel, error) {
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("cppgo: -header is required: name the C++ header to bind")
	}
	if !validGoPackage(cfg.Package) {
		return nil, fmt.Errorf("cppgo: -package %q is not a valid Go package name", cfg.Package)
	}
	b := &builder{
		cfg:                cfg,
		records:            map[string]*recordModel{},
		enums:              map[string]*enumModel{},
		neededRecords:      map[string]bool{},
		selectedSymbols:    map[string]bool{},
		refusalReasons:     map[string]string{},
		skipReasons:        map[string]string{},
		skipClasses:        map[string]string{},
		instantiateMatched: make([]bool, len(cfg.Instantiate)),
		statusMatched:      make([]bool, len(cfg.Status)),
		bindMatched:        make([]bool, len(cfg.Bind)),
		excludeMatched:     make([]bool, len(cfg.Exclude)),
	}
	if err := b.load(); err != nil {
		return nil, err
	}
	if err := b.resolveRecordRequests(); err != nil {
		return nil, err
	}
	// The library's dynamic symbol table is read before anything is classified:
	// whether a declaration has an out-of-line body is what decides between the
	// two carriers, and a header that only declares a function does not prove
	// one.
	if err := b.loadExporteds(); err != nil {
		return nil, err
	}
	// Type names come first: an enum nested in a record is named after it, and
	// a field's Go type needs the enum model the name produces.
	b.assignTypeNames()
	b.buildEnums()
	b.mapper = &mapper{records: b.records, enums: b.enums}
	if err := b.buildRecords(); err != nil {
		return nil, err
	}
	if err := b.buildTemplates(); err != nil {
		return nil, err
	}
	if err := b.loadInstantiations(); err != nil {
		return nil, err
	}
	if err := b.resolveTemplateRequests(); err != nil {
		return nil, err
	}
	b.closeProjects()
	if err := b.buildBindings(); err != nil {
		return nil, err
	}
	if b.manifestErr != nil {
		return nil, b.manifestErr
	}
	b.buildStaticFields()
	b.expandDefaults()
	for i, entry := range b.cfg.Status {
		if !b.statusMatched[i] {
			return nil, fmt.Errorf("cppgo: -status %s: it names nothing this header declares", entry)
		}
	}
	if err := b.applySelection(); err != nil {
		return nil, err
	}
	b.buildFieldAccessors()
	if err := b.buildInheritance(); err != nil {
		return nil, err
	}
	if err := b.assignNames(); err != nil {
		return nil, err
	}
	if err := b.checkInstantiateRequests(); err != nil {
		return nil, err
	}
	if err := b.crossCheck(); err != nil {
		return nil, err
	}
	b.layout()
	model := b.model()
	if cfg.Coverage != "" {
		model.coverage = []byte(b.coverageReport())
	}
	return model, nil
}

// load parses every header and merges the declarations. The merge is by
// mangled name for symbols and by qualified name for types, so two headers that
// include each other do not produce a declaration twice.
func (b *builder) load() error {
	seenSymbol := map[string]bool{}
	seenRecord := map[string]bool{}
	for _, header := range b.cfg.Headers {
		project, err := cxx.Open(cxx.Options{Header: header, CompileCommands: b.cfg.CompileCommands})
		if err != nil {
			return fmt.Errorf("cppgo: %s: %w", header, err)
		}
		if b.flags == nil {
			b.flags = append([]string(nil), project.Flags.Args...)
		}
		if project.Flags.Assumed() {
			b.warnings = append(b.warnings, fmt.Sprintf("the compilation flags for %s are an assumption (%s); a parse without the project's real flags can misread the header", header, project.Flags.Detail))
		}
		for _, sym := range project.Symbols() {
			key := symbolKey(sym)
			if seenSymbol[key] {
				continue
			}
			seenSymbol[key] = true
			b.symbols = append(b.symbols, sym)
		}
		for _, rec := range project.Records() {
			if seenRecord[rec.QualifiedName] {
				continue
			}
			seenRecord[rec.QualifiedName] = true
			b.rawRecords = append(b.rawRecords, rec)
		}
		b.rawEnums = append(b.rawEnums, project.Enums()...)
		b.projects = append(b.projects, project)
	}
	return nil
}

// closeProjects releases the parse trees the instantiation requests needed.
func (b *builder) closeProjects() {
	for _, project := range b.projects {
		_ = project.Close()
	}
	b.projects = nil
}

// symbolKey is the identity of a symbol across headers.
func symbolKey(sym cxx.Symbol) string {
	if sym.Mangled != "" {
		return sym.Mangled
	}
	return sym.QualifiedName + "|" + sym.Signature()
}

// buildRecords classifies every record definition: a mirrorable aggregate
// becomes a Go struct with the exported fields, everything else an opaque value
// type owning C++ storage.
func (b *builder) buildRecords() error {
	sort.Slice(b.rawRecords, func(i, j int) bool {
		return b.rawRecords[i].QualifiedName < b.rawRecords[j].QualifiedName
	})
	for _, rec := range b.rawRecords {
		if rec.IsTemplate {
			continue
		}
		if rec.IsTemplateSpecialization {
			// A specialization is part of the API only when the manifest names
			// it: the user chooses which concrete types to emit. One the
			// manifest named by pattern was synthesized before this point.
			if !b.instantiationNamed(rec.QualifiedName) && !b.instantiated[rec.QualifiedName] {
				continue
			}
			b.synthMembers = append(b.synthMembers, rec.Members...)
		}
		model := &recordModel{
			GoName:               b.recordGoNames[rec.QualifiedName],
			CxxName:              rec.QualifiedName,
			Size:                 rec.SizeBytes,
			Align:                rec.AlignBytes,
			DefaultConstructible: rec.IsDefaultConstructible,
			CopyConstructible:    rec.IsCopyConstructible,
			Comment:              rec.Comment,
		}
		model.Mirrored = b.mirrorable(rec)
		b.records[rec.QualifiedName] = model
		b.order = append(b.order, rec.QualifiedName)
	}
	// A member function template has no signature of its own, so it is left out
	// of the API and reported: the non-template overloads beside it are bound,
	// and an instantiation is a separate declaration.
	for _, name := range b.order {
		raw := b.rawRecord(name)
		for _, member := range raw.MemberTemplates {
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s is a member function template and is not bound: name its concrete instantiation as a non-template member beside it",
				member.QualifiedName))
		}
	}

	// The fields are mapped once every model exists: a mirrored field may be a
	// record whose own model has not been built yet when its container is.
	for _, rec := range b.rawRecords {
		model, ok := b.records[rec.QualifiedName]
		if !ok || !model.Mirrored {
			continue
		}
		for _, field := range rec.Fields {
			model.Fields = append(model.Fields, &fieldModel{
				GoName:  exportName(field.Name),
				Type:    b.mirrorFieldType(field.Type),
				Offset:  field.OffsetBytes,
				Comment: field.Comment,
			})
		}
	}
	return nil
}

// checkInstantiateRequests reports an -instantiate entry nothing matched, after
// both paths had their chance: a class specialization the records named, and a
// function or member instantiation a project could synthesize.
func (b *builder) checkInstantiateRequests() error {
	for i, entry := range b.cfg.Instantiate {
		if !b.instantiateMatched[i] {
			return fmt.Errorf("cppgo: -instantiate %s: it names no instantiation this header declares; add an explicit instantiation such as 'template struct ...;' to the header, or write a function instantiation as <qualified><T1, T2>(<parameters>)", entry)
		}
	}
	return nil
}

// simpleName is the last component of a qualified name.
func simpleName(qualified string) string {
	if i := strings.LastIndex(qualified, "::"); i >= 0 {
		return qualified[i+2:]
	}
	return qualified
}

// mirrorable reports whether a record's Go value can be its C layout. Every
// field must be public, at a known offset, not a bitfield, and of a type the
// type map carries as a value — a scalar, an enum, or a record that is itself
// mirrorable. The generated file asserts the size, alignment and field offsets
// at compile time, so this predicate is proven, not assumed.
func (b *builder) mirrorable(rec cxx.Record) bool {
	return b.mirrorableRecord(rec, 0)
}

// maxMirrorDepth bounds the recursion through nested record fields. A record
// cannot contain itself by value, so the bound is a guard against a malformed
// model rather than a real limit.
const maxMirrorDepth = 8

func (b *builder) mirrorableRecord(rec cxx.Record, depth int) bool {
	if depth > maxMirrorDepth || !rec.LayoutKnown || !rec.IsPOD || rec.Kind == cxx.Union {
		return false
	}
	// A base is allowed when its fields were flattened into this record (the
	// generator's synthesized specializations do that); the natural-layout
	// check below is what proves the flattening is exact.
	for _, base := range rec.Bases {
		if base.IsVirtual || base.OffsetBytes != 0 {
			return false
		}
	}
	for _, field := range rec.Fields {
		if field.Access != cxx.AccessPublic || field.IsBitField || field.IsAnonymous || field.OffsetBytes < 0 {
			return false
		}
		switch field.Type.Kind {
		case cxx.TypeInteger, cxx.TypeFloat, cxx.TypeEnum:
		case cxx.TypeArray:
			// A fixed array of scalars is a Go array: [4]float64 is the same
			// bytes as double[4], and the size assertion proves it.
			if field.Type.ArraySize <= 0 || field.Type.Element == nil {
				return false
			}
			switch field.Type.Element.Kind {
			case cxx.TypeInteger, cxx.TypeFloat, cxx.TypeEnum:
			default:
				return false
			}
		case cxx.TypeRecord:
			inner, ok := b.rawRecordByName(field.Type.RecordName)
			if !ok || !b.mirrorableRecord(inner, depth+1) {
				return false
			}
		default:
			return false
		}
	}
	return b.naturalLayout(rec)
}

// rawRecordByName finds a record definition by qualified name.
func (b *builder) rawRecordByName(name string) (cxx.Record, bool) {
	for _, rec := range b.rawRecords {
		if rec.QualifiedName == name {
			return rec, true
		}
	}
	return cxx.Record{}, false
}

// naturalLayout reports whether the C++ layout is the one Go would give the
// generated struct: each field at its natural offset, the size a multiple of
// the largest alignment. A packed record or one with padding Go would not
// reproduce is not mirrored; it takes the opaque path instead.
func (b *builder) naturalLayout(rec cxx.Record) bool {
	var offset, maxAlign int64 = 0, 1
	for _, field := range rec.Fields {
		size, align := b.fieldWidth(field)
		if size < 0 {
			return false
		}
		offset = (offset + align - 1) / align * align
		if field.OffsetBytes != offset {
			return false
		}
		offset += size
		if align > maxAlign {
			maxAlign = align
		}
	}
	offset = (offset + maxAlign - 1) / maxAlign * maxAlign
	return offset == rec.SizeBytes && maxAlign == rec.AlignBytes
}

// fieldWidth is the size and alignment of a mirrored field: its own width for a
// scalar, and the record's for a nested mirrored record.
func (b *builder) fieldWidth(field cxx.Field) (size, align int64) {
	if field.Type.Kind == cxx.TypeArray {
		if field.Type.Element == nil || field.Type.ArraySize <= 0 {
			return -1, 1
		}
		elemSize, elemAlign := scalarWidth(*field.Type.Element)
		if elemSize < 0 {
			return -1, 1
		}
		return elemSize * field.Type.ArraySize, elemAlign
	}
	if field.Type.Kind == cxx.TypeRecord {
		if inner, ok := b.rawRecordByName(field.Type.RecordName); ok {
			return inner.SizeBytes, inner.AlignBytes
		}
		return -1, 1
	}
	return scalarWidth(field.Type)
}

// scalarWidth is the size and alignment of a scalar field in Go, which is its
// width for every type the mirror predicate accepts.
func scalarWidth(p cxx.Param) (size, align int64) {
	if p.Kind == cxx.TypeInteger && p.ClangKind == "Bool" {
		return 1, 1
	}
	return p.SizeBytes, p.SizeBytes
}

// mirrorFieldType maps a field the mirror predicate already accepted: a bool,
// an integer of a width the type map covers, a float, or an enum. It has no
// failure path because mirrorable checked the kind and the width.
func (b *builder) mirrorFieldType(p cxx.Param) *goType {
	if p.Kind == cxx.TypeArray && p.Element != nil && p.ArraySize > 0 {
		elem := b.mirrorFieldType(*p.Element)
		return &goType{Kind: goArray, Go: fmt.Sprintf("[%d]%s", p.ArraySize, elem.Go), Elem: elem, ArrayLen: p.ArraySize, Param: p}
	}
	if p.Kind == cxx.TypeInteger && p.ClangKind == "Bool" {
		return &goType{Kind: goBool, Go: "bool", C: "bool", Cxx: "bool", Param: p}
	}
	if p.Kind == cxx.TypeEnum {
		// An enum's Go type is its named integer type, which has the same
		// width as the underlying C++ integer, so the layout still holds.
		if enum, ok := b.enums[p.RecordName]; ok {
			return &goType{Kind: goEnum, Go: enum.GoName, C: enum.C, Cxx: "::" + enum.CxxName, Enum: enum, Param: p}
		}
	}
	if p.Kind == cxx.TypeRecord {
		if record, ok := b.records[p.RecordName]; ok {
			return &goType{Kind: goRecord, Go: record.GoName, C: "void *", Cxx: "::" + record.CxxName, Record: record, Param: p}
		}
	}
	if p.Kind == cxx.TypeFloat {
		if p.SizeBytes == 4 {
			return &goType{Kind: goFloat, Go: "float32", C: "float", Cxx: "float", Param: p}
		}
		return &goType{Kind: goFloat, Go: "float64", C: "double", Cxx: "double", Param: p}
	}
	goName, cName, _ := integerType(p.SizeBytes, p.Unsigned)
	return &goType{Kind: goInteger, Go: goName, C: cName, Cxx: cName, Param: p}
}

// buildBindings turns every public declaration into a binding, before
// selection and naming. A declaration the manifest does not select is not
// lowered at all: -bind and -exclude narrow what is emitted, and a construct
// outside the selection cannot fail the run for a signature nobody asked to
// bind.
func (b *builder) buildBindings() error {
	if b.selectedSymbols == nil {
		b.selectedSymbols = map[string]bool{}
	}
	symbols := append(append([]cxx.Symbol(nil), b.symbols...), b.synthMembers...)
	for _, req := range b.templateRequests {
		// A manifest instantiation is a root of its own: the entry asked for it
		// by name, and selection already marked it matched.
		symbols = append(symbols, req.Symbol)
	}
	for _, sym := range symbols {
		if sym.IsTemplate {
			// A template is the generic API's input; buildTemplates binds it.
			continue
		}
		request := b.requestFor(sym)
		if request == nil && !b.symbolSelected(sym) {
			continue
		}
		b.selectedSymbols[symbolKey(sym)] = true
		if sym.IsVariadic {
			// The ellipsis has no fixed argument list to lay out, so there is
			// no thunk to write. The overloads beside it still bind.
			b.unbound(sym, pending(
				"a variadic function has no fixed argument list to lay out",
				"replace the ellipsis with a fixed parameter list or a span"))
			continue
		}
		owner := b.records[sym.Parent]
		if owner == nil {
			if sym.Kind != cxx.FreeFunc {
				// A member of a record this package does not model: nothing
				// to bind it to.
				continue
			}
			binding, err := b.newBinding(sym, nil)
			if err != nil {
				b.unbound(sym, err)
				continue
			}
			b.bindings = append(b.bindings, binding)
			continue
		}
		if sym.Access != cxx.AccessPublic {
			continue
		}
		if owner.Mirrored && (sym.Kind == cxx.Constructor || sym.Kind == cxx.Destructor) {
			// A mirrored record is its Go value: the C++ constructor that fills
			// the same bytes is `X{...}` in Go, and there is no object to
			// destroy. Emitting one would allocate a cppgo.Object for a struct
			// that has none.
			b.skipped(sym, "not-needed", "a mirrored record is its Go value; its constructors and destructor are the Go struct")
			continue
		}
		switch sym.Kind {
		case cxx.Constructor:
			if sym.IsDeleted {
				b.skipped(sym, "not-needed", "the constructor is deleted in C++")
				continue
			}
			if !b.allocatable(owner) {
				b.skipped(sym, "not-needed", "the record has no layout Go can allocate")
				continue
			}
			switch {
			case sym.IsCopyConstructor:
				clone, err := b.newBinding(sym, owner)
				if err != nil {
					b.unbound(sym, err)
					continue
				}
				clone.IsConstructor = false
				clone.IsClone = true
				clone.Params = nil
				owner.Clone = clone
				b.bindings = append(b.bindings, clone)
			case sym.IsMoveConstructor:
				// Go has no rvalue overload: a move constructor is not
				// exposed separately.
				b.skipped(sym, "not-needed", "Go has no rvalue overload")
			default:
				ctor, err := b.newBinding(sym, owner)
				if err != nil {
					b.unbound(sym, err)
					continue
				}
				owner.Ctors = append(owner.Ctors, ctor)
				b.bindings = append(b.bindings, ctor)
			}
		case cxx.Destructor:
			if sym.IsDeleted {
				b.skipped(sym, "not-needed", "the destructor is deleted in C++")
				continue
			}
			// A destructor has no parameters and a void result, so building it
			// cannot fail.
			dtor, _ := b.newBinding(sym, owner)
			owner.Dtor = dtor
			b.bindings = append(b.bindings, dtor)
		case cxx.Method, cxx.StaticMethod:
			if sym.IsPureVirtual || sym.IsDeleted {
				b.skipped(sym, "not-needed", "the member is pure virtual or deleted")
				continue
			}
			method, err := b.newBinding(sym, owner)
			if err != nil {
				b.unbound(sym, err)
				continue
			}
			if sym.Kind == cxx.StaticMethod {
				owner.Statics = append(owner.Statics, method)
			} else {
				owner.Methods = append(owner.Methods, method)
			}
			b.bindings = append(b.bindings, method)
		}
	}
	// A record with no declared factory constructor still gets one when it is
	// default-constructible: the contract promises New<Type> for every record
	// the model classified as such.
	for _, name := range b.order {
		owner := b.records[name]
		if len(owner.Ctors) > 0 || owner.Mirrored {
			continue
		}
		if !b.defaultConstructible(owner) || !b.allocatable(owner) {
			continue
		}
		ctor := b.syntheticConstructor(owner)
		owner.Ctors = []*binding{ctor}
		b.bindings = append(b.bindings, ctor)
	}
	// The same for Clone(): a copy-constructible record with no declared copy
	// constructor still gets one, because the implicit copy is callable.
	for _, name := range b.order {
		owner := b.records[name]
		if owner.Mirrored || owner.Clone != nil || !owner.CopyConstructible || !b.allocatable(owner) {
			continue
		}
		if b.rawRecord(owner.CxxName).IsAbstract {
			continue
		}
		clone := b.syntheticClone(owner)
		owner.Clone = clone
		b.bindings = append(b.bindings, clone)
	}
	return nil
}

// symbolSelected reports whether the manifest asks for this declaration, before
// anything is lowered. With no -bind every declaration is a root; an entry
// selects a declaration or everything inside it, so naming a record selects its
// members, and a full signature selects one overload of a set. -exclude wins, as
// it does in applySelection.
func (b *builder) symbolSelected(sym cxx.Symbol) bool {
	if sym.Parent != "" && b.instantiated[sym.Parent] {
		// The members of a specialization the manifest named are the API of
		// that type: the entry selected the type, not one member of it.
		return true
	}
	keep := len(b.cfg.Bind) == 0
	for i, entry := range b.cfg.Bind {
		if matchesSignature(entry, sym) || matchesName(entry, sym.QualifiedName) || (sym.Parent != "" && matchesName(entry, sym.Parent)) {
			b.bindMatched[i] = true
			keep = true
		}
	}
	if !keep {
		return false
	}
	for i, entry := range b.cfg.Exclude {
		if matchesSignature(entry, sym) || matchesName(entry, sym.QualifiedName) {
			b.excludeMatched[i] = true
			return false
		}
	}
	return true
}

// allocatable reports whether the Go side can allocate storage for a record:
// New<Type>, Clone and a factory in general need sizeof and alignof, and a
// record whose layout libclang could not complete has neither. Such a record is
// still reachable as a view of a field or of a borrowed result, so the emitter
// omits the factory and says so rather than emitting an allocation of -1 bytes.
func (b *builder) allocatable(owner *recordModel) bool {
	if owner.Size > 0 && owner.Align > 0 {
		return true
	}
	b.warnings = append(b.warnings, fmt.Sprintf(
		"%s has no layout the Go side can allocate (sizeof or alignof is unknown), so no factory is generated for it; reach it as a view of a field or of a borrowed result",
		owner.CxxName))
	return false
}

// defaultConstructible reports whether the implicit default constructor can be
// called for a record that declares none.
func (b *builder) defaultConstructible(owner *recordModel) bool {
	raw := b.rawRecord(owner.CxxName)
	return !raw.IsAbstract && raw.IsDefaultConstructible
}

// rawRecord finds the raw model record by qualified name.
func (b *builder) rawRecord(name string) cxx.Record {
	for _, rec := range b.rawRecords {
		if rec.QualifiedName == name {
			return rec
		}
	}
	return cxx.Record{}
}

// syntheticConstructor is the New<Type>() binding of a default-constructible
// record that declares no constructor of its own.
func (b *builder) syntheticConstructor(owner *recordModel) *binding {
	sym := cxx.Symbol{
		QualifiedName:    owner.CxxName + "::" + simpleName(owner.CxxName),
		Kind:             cxx.Constructor,
		Parent:           owner.CxxName,
		Access:           cxx.AccessPublic,
		IsNoExcept:       true,
		ExceptionSpec:    cxx.ExceptionNoexcept,
		HasOutOfLineBody: false,
	}
	sym.Result = cxx.Param{Kind: cxx.TypeVoid, ClangKind: "Void", Spelling: "void"}
	// newBinding cannot fail here: there are no parameters to lower, the result
	// is void, and the carrier needs no register checks.
	ctor, _ := b.newBinding(sym, owner)
	return ctor
}

// syntheticClone is the Clone() binding of a record that is copy-constructible
// without declaring a copy constructor. The thunk placement-news the copy, which
// is exactly what the implicit copy constructor does.
func (b *builder) syntheticClone(owner *recordModel) *binding {
	pointee := cxx.Param{
		Kind:       cxx.TypeRecord,
		Spelling:   "const " + owner.CxxName,
		ClangKind:  "Record",
		SizeBytes:  owner.Size,
		RecordName: owner.CxxName,
		IsConst:    true,
	}
	reference := cxx.Param{
		Kind:      cxx.TypePointer,
		Spelling:  "const " + owner.CxxName + " &",
		ClangKind: "LValueReference",
		SizeBytes: 8,
		Ptr:       true,
		Pointee:   &pointee,
	}
	sym := cxx.Symbol{
		QualifiedName:     owner.CxxName + "::" + simpleName(owner.CxxName),
		Kind:              cxx.Constructor,
		Parent:            owner.CxxName,
		Access:            cxx.AccessPublic,
		IsCopyConstructor: true,
		Params:            []cxx.Param{reference},
	}
	sym.Result = cxx.Param{Kind: cxx.TypeVoid, ClangKind: "Void", Spelling: "void"}
	clone, _ := b.newBinding(sym, owner)
	clone.IsConstructor = false
	clone.IsClone = true
	clone.Params = nil
	return clone
}

// qualifyNestedParams rewrites a type name that libclang spelled against the
// template pattern — Box<T>::Slot inside Box<double>::slot — to the
// specialization's own nested type, which is what the model indexes. A name the
// model already knows is left alone, so a namespace-scope type keeps its name.
func (b *builder) qualifyNestedParams(sym cxx.Symbol, owner *recordModel) cxx.Symbol {
	if owner == nil {
		return sym
	}
	var fix func(p *cxx.Param)
	fix = func(p *cxx.Param) {
		if p == nil {
			return
		}
		if p.RecordName != "" {
			_, recordKnown := b.records[p.RecordName]
			_, enumKnown := b.enums[p.RecordName]
			if !recordKnown && !enumKnown {
				candidate := owner.CxxName + "::" + simpleName(p.RecordName)
				if _, ok := b.records[candidate]; ok {
					p.RecordName = candidate
				} else if _, ok := b.enums[candidate]; ok {
					p.RecordName = candidate
				}
			}
		}
		fix(p.Pointee)
		fix(p.Element)
	}
	fix(&sym.Result)
	for i := range sym.Params {
		fix(&sym.Params[i])
	}
	return sym
}

// crossesOpaqueByValue reports whether the signature passes or returns a record
// Go cannot mirror by value. Such a call is shim-carried and, unlike a shared
// standard-library type, its copy constructor can throw: the thunk catches.
func crossesOpaqueByValue(binding *binding) bool {
	if binding.Result != nil && binding.Result.ByValue && binding.Result.Record != nil && !binding.Result.Record.Mirrored {
		return true
	}
	for _, param := range binding.Params {
		if param.Type.ByValue && param.Type.Record != nil && !param.Type.Record.Mirrored {
			return true
		}
	}
	return false
}

// newBinding computes one binding's Go shape, carrier and wire arguments.
func (b *builder) newBinding(sym cxx.Symbol, owner *recordModel) (*binding, error) {
	sym = b.qualifyNestedParams(sym, owner)
	binding := &binding{sym: sym, Owner: owner, This: sym.Kind == cxx.Method || sym.Kind == cxx.Constructor || sym.Kind == cxx.Destructor}
	if request := b.requestFor(sym); request != nil {
		binding.templateArgs = append([]string(nil), request.ArgSpellings...)
	}
	binding.IsConstructor = sym.Kind == cxx.Constructor
	binding.IsDestructor = sym.Kind == cxx.Destructor
	binding.IsStatic = sym.Kind == cxx.StaticMethod

	if err := b.lowerParams(binding); err != nil {
		return nil, err
	}
	for _, param := range binding.Params {
		if param.Type.Std != stdNone {
			binding.hasStd = true
		}
	}
	if !binding.IsConstructor && !binding.IsDestructor {
		result, err := b.mapper.mapType(sym.Result)
		if err != nil {
			return nil, fmt.Errorf("result: %w", err)
		}
		if result.Kind != goVoid {
			if result.Kind == goFunc {
				return nil, pending(fmt.Sprintf("%q is a function or std::function result", sym.Result.Spelling),
					"take it as a callback parameter, or expose a C++ wrapper that owns it")
			}
			if result.Std == stdUniquePtr || result.Std == stdSharedPtr {
				return nil, pending(fmt.Sprintf("%q is a smart-pointer result", sym.Result.Spelling), "return the object by value or through a reference at the C++ boundary")
			}
			if result.Kind == goRecord {
				// A mirrored record comes back through a pointer the Go side
				// owns, so its size and its sret form are the thunk's problem.
				binding.hasByValue = true
			}
			if result.ByValue {
				// A record Go cannot mirror returns as an owning *T: the shim
				// constructs it into Go's storage, and the copy that happens
				// there can throw whatever its copy constructor throws.
				binding.hasByValue = true
			}
			if result.Kind == goPointer && result.Record != nil && !result.Record.Mirrored && !result.ByValue {
				// A method's borrowed pointer is a view of the receiver: the
				// object is Go's, and the returned address is inside it. A free
				// function's is an address nobody owns, which Go cannot hold.
				if owner == nil || owner.Mirrored || sym.Kind != cxx.Method {
					return nil, pending(fmt.Sprintf("%q is a pointer to an opaque record", sym.Result.Spelling), "return it through a reference at the C++ boundary")
				}
				binding.ViewResult = true
			}
			binding.Result = result
			if result.Std != stdNone {
				binding.StdResult = true
				binding.hasStd = true
			}
			if result.CString {
				b.usesCString = true
			}
		}
	}
	b.chooseCarrier(binding)
	if crossesOpaqueByValue(binding) {
		// A record Go cannot mirror is copied by the shim, in one direction or
		// the other, and that copy can throw whatever the declaration promises.
		// The call carries an error channel so the failure is a Go error and
		// not an exception unwinding into Go.
		binding.Status = true
		binding.ErrorResult = true
		binding.Reason += "; the by-value copy runs in the shim, which catches a throwing copy"
	}
	if b.statusNamed(sym) {
		// The 32-bit return is the status, not a value: the Go signature keeps
		// the error and loses the integer, and the thunk returns the callee's
		// own code so a non-zero one is the failure.
		if err := b.applyStatus(binding); err != nil {
			return nil, manifestError{err}
		}
	}
	if err := b.checkReentrant(binding); err != nil {
		return nil, err
	}
	if binding.Carrier.shimCarried() && binding.Result != nil {
		switch {
		case binding.StdResult:
			b.addStdResult(binding)
		case binding.Result.ByValue && binding.Result.Record != nil && !binding.Result.Record.Mirrored:
			// The shim constructs the result into the storage the Go side
			// allocated; the pointer is the object's address, and the object
			// owns the destructor's call.
			binding.OutObject = binding.Result.Record
			binding.Wire = append(binding.Wire, &wireArg{
				label:    "out",
				slotType: "uintptr",
				class:    classInt,
				size:     8,
				expr:     "cppgoObject.Addr()",
				keep:     "cppgoObject",
				cType:    "void *",
				cName:    "cppgo_result",
			})
		case binding.Status || binding.Result.Kind == goRecord || binding.ViewResult || binding.Result.CString:
			binding.Out = binding.Result
			binding.OutLocal = outLocalType(binding.Result)
			out := &wireArg{
				label:    "out",
				slotType: "unsafe.Pointer",
				class:    classInt,
				size:     8,
				expr:     "unsafe.Pointer(&cppgoResult)",
				keep:     "&cppgoResult",
				cType:    outParamType(binding.Result),
				cName:    "cppgo_result",
			}
			if binding.Carrier == reentrant {
				// A callback may grow the goroutine stack, so the block the
				// thunk writes into is a heap allocation, never a stack local.
				binding.outPointer = true
				out.expr = "unsafe.Pointer(cppgoResult)"
				out.keep = "cppgoResult"
			}
			binding.Wire = append(binding.Wire, out)
		}
	}
	// The register check runs last: a standard-library result adds its
	// out-parameters to the wire, and those are INTEGER words like any other.
	// The identity includes the explicit template arguments: at<unsigned char>
	// and at<double> are two functions with one signature, and their thunks
	// must not share a symbol.
	id := displaySignature(sym)
	if len(binding.templateArgs) > 0 {
		id += "<" + strings.Join(binding.templateArgs, ", ") + ">"
	}
	binding.ID = shortHash(id)
	binding.SymbolVar = "cppgoSym_" + binding.ID
	binding.Trampoline = "cppgoCall_" + binding.ID
	if binding.Carrier.shimCarried() {
		binding.Thunk = "cppgo_thunk_" + binding.ID
		binding.Symbol = binding.Thunk
	} else {
		binding.Symbol = sym.Mangled
	}
	if err := b.routeRegisters(binding); err != nil {
		return nil, err
	}
	return binding, nil
}

// lowerParams maps the C++ parameter list to visible Go parameters and to the
// wire arguments the trampoline passes. A pointer followed by a length becomes
// a slice; one length may serve several pointers, which is the saxpy shape.
func (b *builder) lowerParams(binding *binding) error {
	params := binding.sym.Params
	taken := map[string]bool{}
	if binding.This && binding.Owner != nil {
		taken[receiverName(binding.Owner.GoName)] = true
	}
	visible := 0
	// The thunk's own parameter names must not collide with the implicit this
	// and the out-pointer.
	cNames := map[string]bool{"cppgo_result": true}
	if binding.This {
		cNames["self"] = true
	}
	addWire := func(arg *wireArg) {
		arg.cName = uniqueName(cIdent(arg.cName), cNames)
		binding.Wire = append(binding.Wire, arg)
	}
	if binding.This {
		thisType := "void *"
		if binding.sym.IsConst {
			thisType = "const void *"
		}
		binding.Wire = append(binding.Wire, &wireArg{
			label:    "this",
			slotType: "uintptr",
			class:    classInt,
			size:     8,
			cType:    thisType,
			cName:    "self",
		})
	}
	for i := 0; i < len(params); i++ {
		p := params[i]
		if _, isStd := stdTypeName(p); isStd {
			mapped, err := b.mapper.mapType(p)
			if err != nil {
				return fmt.Errorf("parameter %s: %w", p.Name, err)
			}
			if mapped.Std == stdUniquePtr || mapped.Std == stdSharedPtr {
				return fmt.Errorf("parameter %s: %w", p.Name, pending(
					fmt.Sprintf("%q is a smart-pointer parameter", p.Spelling),
					"take the object by reference or by pointer at the C++ boundary"))
			}
			name := uniqueName(paramName(p.Name, visible), taken)
			visible++
			if isStdOutRef(p) && mapped.Std != stdVector {
				return fmt.Errorf("parameter %s: %w", p.Name, pending(
					fmt.Sprintf("%q is a non-const standard-library reference", p.Spelling),
					"take it by value or by const reference; only a std::vector has a copy-back shape"))
			}
			out := isStdOutRef(p) && mapped.Std == stdVector
			param := &goParam{Name: name, Type: mapped, StdOut: out}
			binding.Params = append(binding.Params, param)
			if mapped.Kind == goFunc {
				b.callbackWire(binding, name, mapped, p, addWire)
				continue
			}
			if out {
				b.stdOutParamWire(binding, p, mapped, name, addWire)
				continue
			}
			b.stdParamWire(binding, p, mapped, name, addWire)
			continue
		}
		if elem, ok := b.sliceElement(p); ok {
			j := i
			for j < len(params) {
				next, ok := b.sliceElement(params[j])
				if !ok || next.Go != elem.Go {
					break
				}
				j++
			}
			if j < len(params) && isLengthParam(params[j]) {
				group := make([]*goParam, 0, j-i)
				for k := i; k < j; k++ {
					name := uniqueName(paramName(params[k].Name, visible), taken)
					visible++
					param := &goParam{Name: name, Type: &goType{Kind: goSlice, Go: "[]" + elem.Go, Elem: elem, Record: elem.Record, Param: params[k]}, Slice: true}
					binding.Params = append(binding.Params, param)
					group = append(group, param)
					cType := elem.Cxx + " *"
					if params[k].Pointee.IsConst {
						cType = "const " + cType
					}
					addWire(&wireArg{
						label:    "a" + itoa(len(binding.Wire)),
						slotType: "unsafe.Pointer",
						class:    classInt,
						size:     8,
						expr:     "cppgo.Ptr(" + name + ")",
						keep:     name,
						cType:    cType,
						cName:    params[k].Name,
						param:    params[k],
					})
				}
				for _, param := range group {
					param.Group = group
				}
				length := params[j]
				addWire(&wireArg{
					label:    "a" + itoa(len(binding.Wire)),
					slotType: "uint64",
					class:    classInt,
					size:     8,
					expr:     "uint64(len(" + group[0].Name + "))",
					cType:    "size_t",
					cName:    length.Name,
					param:    length,
				})
				i = j
				continue
			}
		}
		mapped, err := b.mapper.mapType(p)
		if err != nil {
			return fmt.Errorf("parameter %s: %w", p.Name, err)
		}
		name := uniqueName(paramName(p.Name, visible), taken)
		visible++
		binding.Params = append(binding.Params, &goParam{Name: name, Type: mapped})
		if mapped.Kind == goFunc {
			b.callbackWire(binding, name, mapped, p, addWire)
			continue
		}
		if mapped.CString {
			if !p.Pointee.IsConst {
				return fmt.Errorf("parameter %s: %w", p.Name, pending(
					fmt.Sprintf("%q is a non-const char pointer", p.Spelling),
					"take it as const char *: the Go string is immutable, so the callee's writes would not reach it"))
			}
			b.cStringParam(binding, name, mapped, p, addWire)
			continue
		}
		if mapped.Kind == goRecord {
			// A mirrored record is a Go value with the C layout: the thunk
			// takes its address and copies it into the call, which is the one
			// place the C++ copy constructor exists.
			b.byValueParam(binding, name, mapped, p, addWire)
			continue
		}
		if mapped.ByValue {
			// A record Go cannot mirror: the wire word is the object's own
			// address and the shim copies it into the call, so the call is
			// shim-carried whatever the register class says.
			binding.hasByValue = true
		}
		wire := b.paramWire(name, mapped, p)
		wire.label = "a" + itoa(len(binding.Wire))
		addWire(wire)
	}
	return nil
}

// byValueParam lowers a mirrored record passed by value: the wire word is a
// pointer to the Go value, and the thunk copies the record into the call. The
// caller keeps the value alive across the call with KeepAlive, and no pointer
// into a Go frame outlives the thunk's first statement.
func (b *builder) byValueParam(binding *binding, name string, mapped *goType, p cxx.Param, add func(*wireArg)) {
	arg := &wireArg{
		label:    "a" + itoa(len(binding.Wire)),
		slotType: "unsafe.Pointer",
		class:    classInt,
		size:     8,
		expr:     "unsafe.Pointer(&" + name + ")",
		keep:     name,
		cType:    "void *",
		cName:    name,
		param:    p,
	}
	add(arg)
	binding.hasByValue = true
}

// cStringParam lowers a `const char *` parameter: the Go value is a string, and
// the wire word is a NUL-terminated copy of it that the caller keeps alive for
// the whole call. The copy is a declaration beside the call, not an argument
// expression, because the callee may read the bytes until it returns.
func (b *builder) cStringParam(binding *binding, name string, mapped *goType, p cxx.Param, add func(*wireArg)) {
	ptr := "cppgo_" + name + "_cstr"
	keep := ptr + "_keep"
	binding.stdDecls = append(binding.stdDecls, ptr+", "+keep+" := cppgoCString("+name+")")
	binding.cstringKeeps = append(binding.cstringKeeps, keep)
	b.usesCString = true
	add(&wireArg{
		label:    "a" + itoa(len(binding.Wire)),
		slotType: "unsafe.Pointer",
		class:    classInt,
		size:     8,
		expr:     ptr,
		keep:     keep,
		cType:    mapped.C,
		cName:    p.Name,
		param:    p,
	})
}

// callbackWire lowers a function-pointer or std::function parameter: the wire
// word is the address the callback table hands out, and the Go wrapper
// registers the function before the call and releases it after.
func (b *builder) callbackWire(binding *binding, name string, mapped *goType, p cxx.Param, add func(*wireArg)) {
	index := 0
	for _, arg := range binding.Wire {
		if arg.callable != nil {
			index++
		}
	}
	arg := &wireArg{
		label:         "a" + itoa(len(binding.Wire)),
		slotType:      "uintptr",
		class:         classInt,
		size:          8,
		expr:          fmt.Sprintf("cppgoCallback%d", index),
		cType:         "void *",
		cName:         p.Name,
		param:         p,
		callable:      mapped.Callback,
		callbackParam: name,
	}
	if mapped.Callback.Std {
		// The shim builds the std::function from the plain function pointer.
		arg.stdRole = stdRolePrimary
		arg.stdKind = stdFunction
		arg.stdLocal = "cppgo_" + cIdent(p.Name)
		arg.stdArgs = []string{arg.cName}
	}
	add(arg)
	binding.hasCallback = true
}

// isStdOutRef reports whether a parameter is a standard-library value the
// callee replaces: a non-const reference to one. The generator can only offer
// this for a std::vector, whose elements it copies back into a fresh Go slice;
// every other standard-library value has no copy-back shape.
func isStdOutRef(p cxx.Param) bool {
	if p.Pointee == nil || p.Pointee.IsConst {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(p.Spelling), "&")
}

// stdOutParamWire appends the wire words of a standard-library parameter the
// callee replaces: the caller's current value (so an in-out call sees it), the
// two out-parameters the shim writes the new value through, and the C++ local
// the call reads and writes.
func (b *builder) stdOutParamWire(binding *binding, p cxx.Param, mapped *goType, goName string, add func(*wireArg)) {
	base := cIdent(p.Name)
	local := "cppgo_" + base
	pointer := func(expr, keep, cType, cName string) *wireArg {
		return &wireArg{
			slotType: "unsafe.Pointer", class: classInt, size: 8,
			expr: expr, keep: keep, cType: cType, cName: cName, param: p,
		}
	}
	elem := mapped.Elem
	cType := elem.Cxx + " *"
	if elem.Const {
		cType = "const " + cType
	}
	data := base + "_data"
	size := base + "_size"
	primary := pointer("cppgo.Ptr(*"+goName+")", "*"+goName, cType, data)
	primary.label = "a" + itoa(len(binding.Wire))
	add(primary)
	add(&wireArg{
		slotType: "uint64", class: classInt, size: 8,
		expr: "uint64(len(*" + goName + "))", cType: "size_t", cName: size, param: p,
		stdRole: stdRoleExtra,
	})
	outData := base + "_out_data"
	outSize := base + "_out_size"
	outDataArg := pointer("unsafe.Pointer(&"+outData+")", "&"+outData, "void **", outData)
	outDataArg.isOut = true
	outDataArg.label = "a" + itoa(len(binding.Wire))
	add(outDataArg)
	outSizeArg := pointer("unsafe.Pointer(&"+outSize+")", "&"+outSize, "size_t *", outSize)
	outSizeArg.isOut = true
	outSizeArg.label = "a" + itoa(len(binding.Wire))
	add(outSizeArg)
	primary.stdRole = stdRolePrimary
	primary.stdKind = mapped.Std
	primary.stdLocal = local
	primary.stdArgs = []string{data, size}
	primary.stdElem = elem
	primary.stdType = mapped
	primary.stdOutCopy = true
	primary.stdOutData = outData
	primary.stdOutSize = outSize
	primary.stdGoName = goName
	binding.stdDecls = append(binding.stdDecls,
		"var "+outData+" unsafe.Pointer",
		"var "+outSize+" uint64")
	binding.usesFree = true
	b.usesFree = true
}

// stdParamWire lowers one standard-library parameter. The Go value is the
// table's shape; the wire words are what the shim needs to build the C++
// value, and the primary word carries the construction recipe.
func (b *builder) stdParamWire(binding *binding, p cxx.Param, mapped *goType, goName string, add func(*wireArg)) {
	base := cIdent(p.Name)
	local := "cppgo_" + base
	next := func(arg *wireArg) *wireArg {
		arg.label = "a" + itoa(len(binding.Wire))
		add(arg)
		return arg
	}
	pointer := func(expr, keep, cType, cName string) *wireArg {
		return &wireArg{
			slotType: "unsafe.Pointer", class: classInt, size: 8,
			expr: expr, keep: keep, cType: cType, cName: cName, param: p,
		}
	}
	switch mapped.Std {
	case stdString, stdStringView:
		data := base + "_data"
		size := base + "_size"
		primary := next(pointer("cppgo.StrPtr("+goName+")", goName, "const char *", data))
		next(&wireArg{
			slotType: "uint64", class: classInt, size: 8,
			expr: "uint64(len(" + goName + "))", cType: "size_t", cName: size, param: p,
			stdRole: stdRoleExtra,
		})
		primary.stdRole = stdRolePrimary
		primary.stdKind = mapped.Std
		primary.stdLocal = local
		primary.stdArgs = []string{data, size}
		primary.stdType = mapped
	case stdVector, stdSpan:
		elem := mapped.Elem
		data := base + "_data"
		size := base + "_size"
		cType := elem.Cxx + " *"
		if elem.Const {
			cType = "const " + cType
		}
		primary := next(pointer("cppgo.Ptr("+goName+")", goName, cType, data))
		next(&wireArg{
			slotType: "uint64", class: classInt, size: 8,
			expr: "uint64(len(" + goName + "))", cType: "size_t", cName: size, param: p,
			stdRole: stdRoleExtra,
		})
		primary.stdRole = stdRolePrimary
		primary.stdKind = mapped.Std
		primary.stdLocal = local
		primary.stdArgs = []string{data, size}
		primary.stdElem = elem
		primary.stdType = mapped
	case stdArray:
		data := base + "_data"
		primary := next(pointer("unsafe.Pointer(&"+goName+"[0])", goName, "const "+mapped.Elem.Cxx+" *", data))
		primary.stdRole = stdRolePrimary
		primary.stdKind = mapped.Std
		primary.stdLocal = local
		primary.stdArgs = []string{data}
		primary.stdElem = mapped.Elem
		primary.stdType = mapped
	case stdOptional:
		value := base + "_value"
		primary := next(pointer("unsafe.Pointer("+goName+")", goName, "const "+mapped.Elem.Cxx+" *", value))
		primary.stdRole = stdRolePrimary
		primary.stdKind = mapped.Std
		primary.stdLocal = local
		primary.stdArgs = []string{value}
		primary.stdElem = mapped.Elem
		primary.stdType = mapped
	}
}

// addStdResult appends the out-parameters a standard-library result crosses
// through, and the Go locals they aim at.
func (b *builder) addStdResult(binding *binding) {
	result := binding.Result
	out := func(expr, keep, cType, cName string) *wireArg {
		arg := &wireArg{
			label:    "a" + itoa(len(binding.Wire)),
			slotType: "unsafe.Pointer",
			class:    classInt,
			size:     8,
			expr:     expr,
			keep:     keep,
			cType:    cType,
			cName:    cName,
			isOut:    true,
		}
		binding.Wire = append(binding.Wire, arg)
		return arg
	}
	switch result.Std {
	case stdString, stdStringView:
		binding.stdDecls = []string{"var cppgoData unsafe.Pointer", "var cppgoSize uint64"}
		out("unsafe.Pointer(&cppgoData)", "&cppgoData", "void **", "cppgo_out_data")
		out("unsafe.Pointer(&cppgoSize)", "&cppgoSize", "size_t *", "cppgo_out_size")
		binding.usesFree = true
		b.usesFree = true
	case stdVector, stdSpan:
		binding.stdDecls = []string{"var cppgoData unsafe.Pointer", "var cppgoSize uint64"}
		out("unsafe.Pointer(&cppgoData)", "&cppgoData", "void **", "cppgo_out_data")
		out("unsafe.Pointer(&cppgoSize)", "&cppgoSize", "size_t *", "cppgo_out_size")
		binding.usesFree = true
		b.usesFree = true
	case stdArray:
		binding.stdDecls = []string{"var cppgoResult " + result.Go}
		out("unsafe.Pointer(&cppgoResult[0])", "&cppgoResult", "void *", "cppgo_out_data")
	case stdOptional:
		binding.stdDecls = []string{"var cppgoResult " + result.Elem.Go, "var cppgoHas int32"}
		out("unsafe.Pointer(&cppgoResult)", "&cppgoResult", "void *", "cppgo_out_value")
		out("unsafe.Pointer(&cppgoHas)", "&cppgoHas", "int32_t *", "cppgo_out_has")
	}
}

// paramWire builds the wire argument for one ordinary parameter.
func (b *builder) paramWire(name string, mapped *goType, param cxx.Param) *wireArg {
	arg := &wireArg{label: "", cType: mapped.C, cName: name, param: param}
	switch mapped.Kind {
	case goInteger:
		arg.slotType = "int64"
		if mapped.Param.Unsigned {
			arg.slotType = "uint64"
		}
		arg.class = classInt
		arg.size = 8
		arg.expr = arg.slotType + "(" + name + ")"
	case goEnum:
		// An enum crosses as its underlying integer: the C++ side casts back
		// to the enum type where its name exists, and the direct ABI passes
		// exactly the bits the callee reads.
		arg.slotType = "int64"
		if mapped.Enum.Unsigned {
			arg.slotType = "uint64"
		}
		arg.class = classInt
		arg.size = 8
		arg.expr = arg.slotType + "(" + name + ")"
	case goBool:
		arg.slotType = "int64"
		arg.class = classInt
		arg.size = 8
		arg.expr = "cppgoBool(" + name + ")"
	case goFloat:
		arg.slotType = mapped.Go
		arg.class = classFloat
		arg.size = 8
		if mapped.Go == "float32" {
			arg.size = 4
		}
		arg.expr = name
	case goPointer:
		if mapped.Record != nil && !mapped.Record.Mirrored {
			arg.slotType = "uintptr"
			arg.class = classInt
			arg.size = 8
			arg.expr = name + ".cppgoAddr()"
			arg.keep = name
			return arg
		}
		arg.slotType = "unsafe.Pointer"
		arg.class = classInt
		arg.size = 8
		arg.expr = "unsafe.Pointer(" + name + ")"
		arg.keep = name
	}
	return arg
}

// sliceElement reports whether p is a pointer to a slice element, and returns
// the mapped element type.
func (b *builder) sliceElement(p cxx.Param) (*goType, bool) {
	if p.Kind != cxx.TypePointer || p.Pointee == nil {
		return nil, false
	}
	elem, err := b.mapper.mapType(*p.Pointee)
	if err != nil {
		return nil, false
	}
	switch elem.Kind {
	case goInteger, goFloat, goBool, goEnum:
		return elem, true
	case goRecord:
		if elem.Record != nil && elem.Record.Mirrored {
			return elem, true
		}
	}
	return nil, false
}

// isLengthParam reports whether p is the size_t half of a (pointer, length)
// pair.
func isLengthParam(p cxx.Param) bool {
	return p.Kind == cxx.TypeInteger && p.SizeBytes == 8
}

// chooseCarrier assigns the binding's carrier and records why.
func (b *builder) chooseCarrier(binding *binding) {
	sym := binding.sym
	switch {
	case binding.hasCallback:
		binding.Carrier = reentrant
		binding.Reason = "callback: the call enters C through runtime.cgocall, so C++ may call back into Go"
	case binding.hasStd:
		binding.Carrier = thunk
		binding.Reason = "standard-library type: the thunk constructs, copies and destroys it"
	case binding.hasByValue:
		binding.Carrier = thunk
		binding.Reason = "a record passed or returned by value: the thunk copies it"
	case binding.Result != nil && binding.Result.CString:
		binding.Carrier = thunk
		binding.Reason = "a C string result: the thunk hands the bytes out and Go copies them"
	case sym.IsTemplate:
		binding.Carrier = thunk
		binding.Reason = "template: no symbol until it is instantiated"
	case sym.Mangled == "":
		binding.Carrier = thunk
		binding.Reason = "no mangled symbol"
	case sym.IsVirtual:
		binding.Carrier = thunk
		binding.Reason = "virtual: the call dispatches through the vtable"
	case !sym.IsNoExcept:
		binding.Carrier = thunk
		binding.Reason = "not noexcept: an exception would unwind into Go"
	case !b.outOfLine(sym):
		binding.Carrier = thunk
		binding.Reason = "inline body: the library exports no symbol"
	case sym.HasSret:
		binding.Carrier = thunk
		binding.Reason = "the result is returned through a hidden pointer"
	default:
		binding.Carrier = direct
		binding.Reason = "noexcept, out of line, not virtual, register class"
		if !sym.HasOutOfLineBody {
			// The header only declared it; the library's dynamic symbol table
			// proved the definition and the external linkage.
			binding.Reason = "noexcept, not virtual, register class; the library exports the symbol"
		}
	}
	if binding.Carrier.shimCarried() {
		// A standard-library argument is marshaled inside the thunk, and that
		// marshaling allocates, so such a call can fail even when the C++
		// declaration is noexcept.
		binding.Status = !sym.IsNoExcept || binding.hasStd
	}
	if binding.IsConstructor || binding.IsDestructor {
		binding.ErrorResult = true
	} else {
		binding.ErrorResult = binding.Carrier.shimCarried() && binding.Status
	}
}

// forceThunk re-routes a binding that was classified as direct to the shim. It
// exists for the bindings whose call cannot be a transparent trampoline: a
// static data member (the symbol is data), a field accessor (the address is a
// member) and an inherited method (the receiver is a base subobject at an offset
// only C++ knows).
//
// The status and the out-pointer are recomputed, because a direct binding never
// had them.
func (b *builder) forceThunk(binding *binding, reason string) {
	binding.Carrier = thunk
	binding.Reason = reason
	binding.Status = !binding.sym.IsNoExcept || binding.hasStd || binding.fieldByValue
	if !binding.Status {
		binding.ErrorResult = false
	} else {
		binding.ErrorResult = true
		if binding.Result != nil && binding.Out == nil && binding.OutObject == nil && !binding.StdResult && !binding.ViewResult {
			binding.Out = binding.Result
			binding.OutLocal = outLocalType(binding.Result)
			binding.Wire = append(binding.Wire, &wireArg{
				label:    "out",
				slotType: "unsafe.Pointer",
				class:    classInt,
				size:     8,
				expr:     "unsafe.Pointer(&cppgoResult)",
				keep:     "&cppgoResult",
				cType:    outParamType(binding.Result),
				cName:    "cppgo_result",
			})
		}
	}
	binding.Thunk = "cppgo_thunk_" + binding.ID
	binding.Symbol = binding.Thunk
}

// statusNamed reports whether -status names this declaration, by qualified name
// or by its full signature. It marks the entry as matched.
func (b *builder) statusNamed(sym cxx.Symbol) bool {
	for i, entry := range b.cfg.Status {
		if entry == sym.QualifiedName || entry == displaySignature(sym) {
			b.statusMatched[i] = true
			return true
		}
	}
	return false
}

// applyStatus turns a binding whose return is a status code into one that
// reports an error instead of the integer. The return type has to be a 32-bit
// integer: that is the width the contract names, and the width the thunk
// returns.
func (b *builder) applyStatus(binding *binding) error {
	if binding.Result == nil || binding.Result.Kind == goVoid {
		return fmt.Errorf("cppgo: -status %s: the declaration returns nothing, so there is no status to read", displaySignature(binding.sym))
	}
	if binding.Result.Kind != goInteger || binding.Result.Param.SizeBytes != 4 {
		return fmt.Errorf("cppgo: -status %s: the return type is %s, and a status is a 32-bit integer", displaySignature(binding.sym), binding.Result.Param.Spelling)
	}
	binding.statusResult = true
	binding.Status = true
	binding.ErrorResult = true
	// The value is the status: nothing is copied out and the Go result is the
	// error alone.
	binding.Result = nil
	binding.StdResult = false
	binding.Out = nil
	binding.OutLocal = ""
	return nil
}

// checkReentrant refuses the combinations the reentrant carrier cannot carry
// yet: a call that runs a callback must receive no pointer into Go memory (the
// callback may grow the goroutine stack and move it), and a standard-library
// result would be copied out through locals the same growth invalidates.
func (b *builder) checkReentrant(binding *binding) error {
	if binding.Carrier != reentrant {
		return nil
	}
	if binding.StdResult {
		return pending(fmt.Sprintf("%q returns a standard-library value and takes a callback", displaySignature(binding.sym)),
			"return the value through an out parameter the callee fills, or drop the callback from this declaration")
	}
	for _, param := range binding.Params {
		switch param.Type.Kind {
		case goString, goSlice, goArray, goPointer, goOptional:
			return pending(fmt.Sprintf("the parameter %q of a callback binding is a %s", param.Name, param.Type.Go),
				"pass a scalar: a pointer into Go memory would move when the callback grows the stack")
		}
	}
	return nil
}

// routeRegisters decides how a call whose arguments do not fit the SysV
// register file reaches C++. The register file is six INTEGER and eight SSE
// words; a signature past it is carried by the shim, whose thunk takes a pointer
// to an argument block and unpacks it, so the trampoline still passes one word.
// Only the input arguments go into the block: the out-parameters a
// standard-library result needs stay thunk parameters, and a call whose
// out-parameters alone fill the file is refused rather than mis-passed.
func (b *builder) routeRegisters(binding *binding) error {
	// Every wire word is a parameter of the thunk the call goes through, and
	// every parameter takes a register: an out-pointer is as much an argument
	// as the value it receives. Counting only the value words undercounted the
	// frame, and a call at the register limit overflowed in layout instead of
	// taking the argument block.
	ints, floats := 0, 0
	for _, arg := range binding.Wire {
		if arg.label == "this" && arg.cName == "self" {
			// `this` is still a word, but it is the receiver of a method and is
			// counted below like any other.
		}
		switch arg.class {
		case classInt:
			ints++
		case classFloat:
			floats++
		}
	}
	if ints <= maxIntRegs && floats <= maxFloatRegs {
		return nil
	}
	if binding.Carrier == reentrant {
		// checkReentrant already refuses every parameter shape a callback can
		// take that would overflow the frame; this is the belt to that brace.
		return fmt.Errorf("cppgo: %s: the callback call needs %d INTEGER and %d SSE words, past the register file", binding.sym.QualifiedName, ints, floats)
	}
	b.forceThunk(binding, "more than 6 INTEGER or 8 SSE arguments: the thunk takes an argument block and unpacks it")
	return b.buildArgumentBlock(binding)
}

// buildArgumentBlock replaces the input wire words with one pointer to a block
// the thunk reads them from. The block's fields keep the order of the C++
// parameters and the shape of the register words, so the shim's unpack is one
// cast per argument and the Go side fills it with the expression it already
// had.
func (b *builder) buildArgumentBlock(binding *binding) error {
	var inputs, rest []*wireArg
	for _, arg := range binding.Wire {
		if arg.isOut || arg.label == "out" {
			rest = append(rest, arg)
			continue
		}
		inputs = append(inputs, arg)
	}
	if 1+len(rest) > maxIntRegs {
		return fmt.Errorf("cppgo: %s: the call needs %d words after the argument block, past the register file", binding.sym.QualifiedName, 1+len(rest))
	}
	binding.Block = inputs
	offset, align := 0, 1
	for _, arg := range inputs {
		size := wireWordSize(arg.slotType)
		if size == 0 {
			return fmt.Errorf("cppgo: %s: the argument block cannot carry the word %q", binding.sym.QualifiedName, arg.slotType)
		}
		offset = alignUp(offset, size)
		binding.BlockOffsets = append(binding.BlockOffsets, offset)
		offset += size
		if size > align {
			align = size
		}
	}
	binding.BlockSize = alignUp(offset, align)
	reads := make(map[string]string, len(inputs))
	for _, arg := range inputs {
		reads[arg.cName] = blockRead(arg)
	}
	// A standard-library value is built by the shim from the words beside it;
	// those words now live in the block, so the local's initializer reads them
	// out with the same cast.
	for _, arg := range inputs {
		for i, name := range arg.stdArgs {
			if expr, ok := reads[name]; ok {
				arg.stdArgs[i] = expr
			}
		}
	}
	binding.blockReads = reads
	block := &wireArg{
		label:    "block",
		slotType: "unsafe.Pointer",
		class:    classInt,
		size:     8,
		expr:     "unsafe.Pointer(&cppgoBlock)",
		keep:     "&cppgoBlock",
		cType:    "void *",
		cName:    "cppgo_block",
	}
	binding.Wire = append([]*wireArg{block}, rest...)
	return nil
}

// blockRead is the C++ expression that reads one block field as the type the
// declaration names. A word is unsigned by construction, so a pointer field is a
// reinterpret_cast and every other field a static_cast.
func blockRead(arg *wireArg) string {
	field := "cppgo_args->" + arg.cName
	switch arg.slotType {
	case "unsafe.Pointer", "uintptr":
		return "reinterpret_cast<" + arg.cType + ">(" + field + ")"
	default:
		return "static_cast<" + arg.cType + ">(" + field + ")"
	}
}

// wireWordSize is the size and alignment of one block word: the same for both,
// because every word this build packs is a scalar of its own width.
func wireWordSize(slotType string) int {
	switch slotType {
	case "int64", "uint64", "uintptr", "unsafe.Pointer", "float64":
		return 8
	case "float32":
		return 4
	default:
		return 0
	}
}

// blockFieldType is the C++ type of one block field: the word's own type, so the
// Go struct and the C++ struct describe the same layout.
func blockFieldType(slotType string) string {
	switch slotType {
	case "int64":
		return "int64_t"
	case "uint64":
		return "uint64_t"
	case "uintptr":
		return "uintptr_t"
	case "unsafe.Pointer":
		return "void *"
	case "float64":
		return "double"
	case "float32":
		return "float"
	default:
		return "void *"
	}
}

// blockType is the name the block struct has in both languages: it is derived
// from the binding's final ID, so a binding re-salted for an overloaded default
// argument never names a struct another binding also declares.
func (b *binding) blockType() string { return "cppgoArgs_" + b.ID }

// inputs are the arguments the callee receives, in declaration order: the wire
// when the register file carries them, and the block fields when it does not.
func (b *binding) inputs() []*wireArg {
	if len(b.Block) > 0 {
		return b.Block
	}
	return b.Wire
}

// displaySignature renders the C++ declaration the evidence comment names.
func displaySignature(sym cxx.Symbol) string {
	params := make([]string, len(sym.Params))
	for i, p := range sym.Params {
		params[i] = p.Spelling
	}
	list := "(" + strings.Join(params, ", ") + ")"
	if sym.IsConversion {
		// libclang spells the conversion's name as "operator basic_string";
		// the declaration a reader wrote is "operator std::string".
		parent := sym.QualifiedName
		if i := strings.LastIndex(parent, "::"); i >= 0 {
			parent = parent[:i]
		}
		return parent + "::operator " + sym.Result.Spelling + "()" + constSuffix(sym)
	}
	switch sym.Kind {
	case cxx.Constructor:
		return sym.QualifiedName + list
	case cxx.Destructor:
		return sym.QualifiedName + "()"
	}
	out := sym.QualifiedName + list
	if sym.IsConst {
		out += " const"
	}
	if sym.IsNoExcept {
		out += " noexcept"
	}
	return out
}

// constSuffix renders the qualifiers displaySignature appends.
func constSuffix(sym cxx.Symbol) string {
	out := ""
	if sym.IsConst {
		out += " const"
	}
	if sym.IsNoExcept {
		out += " noexcept"
	}
	return out
}

// itoa is strconv.Itoa without the import at every call site.
func itoa(n int) string { return fmt.Sprintf("%d", n) }

// model assembles the final package model.
func (b *builder) model() *packageModel {
	records := make([]*recordModel, 0, len(b.order))
	for _, name := range b.order {
		records = append(records, b.records[name])
	}
	sort.Slice(records, func(i, j int) bool { return records[i].GoName < records[j].GoName })
	sort.Slice(b.bindings, func(i, j int) bool {
		return displaySignature(b.bindings[i].sym) < displaySignature(b.bindings[j].sym)
	})
	return &packageModel{
		cfg:            b.cfg,
		mapper:         b.mapper,
		name:           b.cfg.Package,
		library:        b.cfg.Library,
		libraryName:    b.linkedName,
		headers:        b.cfg.Headers,
		records:        records,
		enums:          b.selectedEnums(),
		bindings:       b.bindings,
		templates:      b.templates,
		instantiations: b.instantiations,
		verified:       b.verified,
		warn:           b.warnings,
		flags:          b.flags,
		usesFree:       b.usesFree,
		usesSpan:       b.mapper.usesSpan,
		usesCString:    b.usesCString,
	}
}

// warnings returns the package-level warnings, sorted for determinism.
func (m *packageModel) warnings() []string { return m.warn }

// report renders one line per binding: the carrier, the symbol and the reason.
func (m *packageModel) report() []string {
	lines := make([]string, 0, len(m.bindings)+len(m.templates)+len(m.instantiations))
	for _, binding := range m.bindings {
		symbol := binding.Symbol
		declaration := displaySignature(binding.sym)
		if binding.staticField != nil {
			declaration = binding.sym.QualifiedName
			if binding.staticWrite {
				declaration += " = <value>"
			}
		}
		lines = append(lines, fmt.Sprintf("%-44s %-6s %-28s %s", declaration, binding.Carrier, symbol, binding.Reason))
	}
	for _, tmpl := range m.templates {
		lines = append(lines, fmt.Sprintf("%-44s %-6s %-28s generic API, %d binding(s)", tmpl.CxxName+"<"+paramNames(tmpl)+">", "tmpl", "-", len(tmpl.bindings())))
	}
	for _, inst := range m.instantiations {
		lines = append(lines, fmt.Sprintf("%-44s %-6s %-28s %s", inst.cxxName(), "tmpl", inst.Key, "members instantiated where each thunk calls them"))
	}
	return lines
}

// loadExporteds reads the dynamic symbol table of -library once. It is the
// evidence the carrier decision needs: a header that only declares a function
// does not prove an out-of-line body, and the library does. A -library that is
// not a readable file (a bare loader name, or a header-only library with no
// object at all) contributes nothing, and the declarations are then routed by
// what the header itself shows.
//
// The library's soname is read here too, because it is what the generated shim
// links against and what the generated Go records: a build path never reaches
// either artifact.
func (b *builder) loadExporteds() error {
	info, err := os.Stat(b.cfg.Library)
	if err != nil || info.IsDir() {
		return nil
	}
	names, err := cxx.DefinedDynamicSymbols(b.cfg.Library)
	if err != nil {
		return fmt.Errorf("cppgo: %s: %w", b.cfg.Library, err)
	}
	b.exported = make(map[string]bool, len(names))
	for _, name := range names {
		b.exported[name] = true
	}
	if b.linkedName, err = cxx.Soname(b.cfg.Library); err != nil {
		return fmt.Errorf("cppgo: %s: %w", b.cfg.Library, err)
	}
	return nil
}

// outOfLine reports whether a dynamic symbol exists for the declaration: the
// header saw a definition, or the library exports the mangled name. Both halves
// matter and both are kept: a header-only declaration whose symbol happens to
// be exported is direct, and a declaration whose definition the shim has but the
// library does not is not.
func (b *builder) outOfLine(sym cxx.Symbol) bool {
	return sym.HasOutOfLineBody || (sym.Mangled != "" && b.exported[sym.Mangled])
}

// crossCheck verifies every direct binding against the library's dynamic symbol
// table when -library names a readable file. A thunk is cppgo's own symbol,
// emitted into the shim the generator compiles, so it is not checked here.
func (b *builder) crossCheck() error {
	if b.exported == nil {
		// A bare loader name cannot be inspected here, and a header-only
		// library has no object at all; a direct binding needs the latter.
		if b.cfg.Library == "" {
			for _, binding := range b.bindings {
				if binding.Carrier == direct {
					return fmt.Errorf("cppgo: %s: a direct binding needs -library: name the shared object its symbol is exported by", displaySignature(binding.sym))
				}
			}
		}
		return nil
	}
	for _, binding := range b.bindings {
		if binding.Carrier != direct {
			continue
		}
		if !b.exported[binding.Symbol] {
			return fmt.Errorf("cppgo: %s: the direct binding's symbol %s is not exported by %s", displaySignature(binding.sym), binding.Symbol, b.cfg.Library)
		}
	}
	b.verified = filepath.Base(b.cfg.Library)
	return nil
}

// layout assigns the trampoline frame offsets and argument registers.
func (b *builder) layout() {
	for _, binding := range b.bindings {
		slots := []*slot{{label: "fn", goType: "uintptr", class: classInt, size: 8}}
		for _, arg := range binding.Wire {
			slots = append(slots, &slot{label: arg.label, goType: arg.slotType, class: arg.class, size: arg.size})
		}
		if binding.Carrier.shimCarried() && binding.Status {
			// The status is the only result register use; a value, if any,
			// travels through the out-pointer already in the wire.
			slots = append(slots, &slot{label: "ret", goType: "int32", class: classInt, size: 4})
		} else if binding.Out != nil {
			// A value delivered through an out-pointer has no register result:
			// a record above 16 bytes is returned through memory by C++ itself,
			// and a record the caller owns is written where the caller says.
		} else if binding.Result != nil {
			slots = append(slots, resultSlot(binding.Result))
		}
		off := 0
		for _, slot := range slots {
			off = alignUp(off, slot.align())
			slot.offset = off
			off += slot.size
		}
		binding.Frame = off
		ints, floats := 0, 0
		for _, slot := range slots[1:] {
			if slot.label == "ret" {
				continue
			}
			switch slot.class {
			case classInt:
				if ints >= len(intRegs) {
					panic(fmt.Sprintf("cppgo: %s: %d INTEGER words past the register file: routeRegisters must have taken the argument block", displaySignature(binding.sym), ints+1))
				}
				slot.reg = intRegs[ints]
				ints++
			case classFloat:
				if floats >= len(floatRegs) {
					panic(fmt.Sprintf("cppgo: %s: %d SSE words", displaySignature(binding.sym), floats+1))
				}
				slot.reg = floatRegs[floats]
				floats++
			}
		}
		binding.Slots = slots
	}
}

// outLocalType is the Go type of the local a thunk writes through its
// out-pointer. It must have exactly the C type's width and representation.
func outLocalType(result *goType) string {
	if result.CString {
		// The thunk writes the library's pointer; the wrapper copies the bytes
		// out of it before the call returns.
		return "unsafe.Pointer"
	}
	switch result.Kind {
	case goBool:
		// A C++ bool is one byte; an int8 local is the honest mirror and the
		// wrapper converts it to a Go bool.
		return "int8"
	case goPointer:
		return "unsafe.Pointer"
	default:
		return result.Go
	}
}

// outParamType is the C type of the result pointer in a thunk prototype.
func outParamType(result *goType) string {
	switch result.Kind {
	case goBool:
		return "bool *"
	case goRecord:
		// The record's layout is never written in the C prototype: the thunk
		// casts the pointer in the translation unit where the layout exists.
		return "void *"
	case goPointer:
		return result.C + "*"
	default:
		return result.C + " *"
	}
}

// resultSlot describes the register a value result comes back in.
func resultSlot(result *goType) *slot {
	slot := &slot{label: "ret", class: classInt, size: 8}
	switch result.Kind {
	case goFloat:
		slot.goType = result.Go
		slot.class = classFloat
		slot.size = 8
		if result.Go == "float32" {
			slot.size = 4
		}
	case goPointer:
		slot.goType = "unsafe.Pointer"
	case goBool:
		// bool returns in AL with the rest of RAX unspecified; the wrapper
		// masks the low byte.
		slot.goType = "int64"
	default:
		slot.goType = "int64"
		if result.Param.Unsigned || (result.Enum != nil && result.Enum.Unsigned) {
			slot.goType = "uint64"
		}
	}
	return slot
}

// alignUp rounds n up to a multiple of a. Every slot is 1, 2, 4 or 8 bytes, so
// a guard for a <= 1 is dead code.
func alignUp(n, a int) int { return (n + a - 1) / a * a }
