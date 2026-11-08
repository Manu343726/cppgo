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
	// thunk calls a generated extern "C" function compiled into the library.
	thunk
)

// String implements fmt.Stringer.
func (c carrier) String() string {
	if c == direct {
		return "direct"
	}
	return "thunk"
}

// class is a SysV argument register class.
type class uint8

const (
	classInt class = iota
	classFloat
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
}

// fieldModel is one exported field of a mirrored record.
type fieldModel struct {
	GoName string
	Type   *goType
	Offset int64
}

// goParam is one visible parameter of the Go API.
type goParam struct {
	Name string
	Type *goType
	// Slice is true when the C++ parameter was a pointer paired with a length.
	Slice bool
	// Group is the slice group a slice shares a length with, or nil.
	Group []*goParam
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
}

// binding is one callable C++ declaration bound to Go.
type binding struct {
	// sym is the model symbol the binding was built from.
	sym cxx.Symbol
	// Owner is the record a method, constructor or destructor belongs to.
	Owner *recordModel
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
	// Out is the result a thunk writes through an out-pointer, nil when the
	// value comes back in a register or there is no value.
	Out *goType
	// OutLocal is the Go local the out-pointer aims at.
	OutLocal string
	// selected is set by the binding manifest.
	selected bool
}

// packageModel is everything one generator run emits.
type packageModel struct {
	cfg      Config
	mapper   *mapper
	name     string
	flags    []string
	library  string
	headers  []string
	records  []*recordModel
	bindings []*binding
	verified string
	warn     []string
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
	// order records the insertion order of records, for deterministic output.
	order    []string
	bindings []*binding
	// synthMembers are the members of included template specialisations. The
	// model reads them from the pattern, because libclang exposes no cursor for
	// the instantiated definition, so they are not in the project's symbol list.
	synthMembers []cxx.Symbol
	// instantiateMatched tracks the -instantiate entries a record matched.
	instantiateMatched []bool
	verified           string
	warnings           []string
	// flags are the compilation flags the headers were parsed with. The shim
	// self-check parses the generated translation unit with the same ones.
	flags []string
}

// build loads the headers and turns the model into a package model.
func build(cfg Config) (*packageModel, error) {
	if len(cfg.Headers) == 0 {
		return nil, fmt.Errorf("cppgo: -header is required: name the C++ header to bind")
	}
	if !validGoPackage(cfg.Package) {
		return nil, fmt.Errorf("cppgo: -package %q is not a valid Go package name", cfg.Package)
	}
	if cfg.Library == "" {
		return nil, fmt.Errorf("cppgo: -library is required: name the shared object Open loads")
	}
	b := &builder{cfg: cfg, records: map[string]*recordModel{}, instantiateMatched: make([]bool, len(cfg.Instantiate))}
	if err := b.load(); err != nil {
		return nil, err
	}
	if err := b.rejectUnimplemented(); err != nil {
		return nil, err
	}
	b.mapper = &mapper{records: b.records}
	if err := b.buildRecords(); err != nil {
		return nil, err
	}
	if err := b.buildBindings(); err != nil {
		return nil, err
	}
	if err := b.applySelection(); err != nil {
		return nil, err
	}
	if err := b.assignNames(); err != nil {
		return nil, err
	}
	if err := b.crossCheck(); err != nil {
		return nil, err
	}
	b.layout()
	return b.model(), nil
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
		_ = project.Close()
	}
	return nil
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
			// A specialisation is part of the API only when the manifest names
			// it: the user chooses which concrete types to emit.
			if !b.instantiationNamed(rec.QualifiedName) {
				continue
			}
			b.synthMembers = append(b.synthMembers, rec.Members...)
		}
		model := &recordModel{
			GoName:               goRecordName(rec.QualifiedName),
			CxxName:              rec.QualifiedName,
			Size:                 rec.SizeBytes,
			Align:                rec.AlignBytes,
			DefaultConstructible: rec.IsDefaultConstructible,
			CopyConstructible:    rec.IsCopyConstructible,
		}
		model.Mirrored = b.mirrorable(rec)
		if !model.Mirrored {
			for _, field := range rec.Fields {
				if field.Access == cxx.AccessPublic {
					return recordError(rec, pending(
						fmt.Sprintf("the public field %s of an opaque record is not emitted by this build yet", field.Name),
						"make the field private and expose an accessor in C++, or make the record a POD"))
				}
			}
		}
		for _, field := range rec.Fields {
			if !model.Mirrored {
				continue
			}
			model.Fields = append(model.Fields, &fieldModel{
				GoName: exportName(field.Name),
				Type:   mirrorFieldType(field.Type),
				Offset: field.OffsetBytes,
			})
		}
		b.records[rec.QualifiedName] = model
		b.order = append(b.order, rec.QualifiedName)
	}
	for i, entry := range b.cfg.Instantiate {
		if !b.instantiateMatched[i] {
			return fmt.Errorf("cppgo: -instantiate %s: it names no instantiation this header declares; add an explicit instantiation such as 'template struct ...;' to the header", entry)
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
// type map carries as a value. The generated file asserts the size, alignment
// and field offsets at compile time, so this predicate is proven, not assumed.
func (b *builder) mirrorable(rec cxx.Record) bool {
	if !rec.LayoutKnown || !rec.IsPOD || rec.Kind == cxx.Union || len(rec.Bases) > 0 {
		return false
	}
	for _, field := range rec.Fields {
		if field.Access != cxx.AccessPublic || field.IsBitField || field.IsAnonymous || field.OffsetBytes < 0 {
			return false
		}
		switch field.Type.Kind {
		case cxx.TypeInteger, cxx.TypeFloat:
		default:
			return false
		}
	}
	return naturalLayout(rec)
}

// naturalLayout reports whether the C++ layout is the one Go would give the
// generated struct: each field at its natural offset, the size a multiple of
// the largest alignment. A packed record or one with padding Go would not
// reproduce is not mirrored; it takes the opaque path instead.
func naturalLayout(rec cxx.Record) bool {
	var offset, maxAlign int64 = 0, 1
	for _, field := range rec.Fields {
		size, align := scalarWidth(field.Type)
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

// scalarWidth is the size and alignment of a scalar field in Go, which is its
// width for every type the mirror predicate accepts.
func scalarWidth(p cxx.Param) (size, align int64) {
	if p.Kind == cxx.TypeInteger && p.ClangKind == "Bool" {
		return 1, 1
	}
	return p.SizeBytes, p.SizeBytes
}

// mirrorFieldType maps a field the mirror predicate already accepted: a bool,
// an integer of a width the type map covers, or a float. It has no failure path
// because mirrorable checked the kind and the width.
func mirrorFieldType(p cxx.Param) *goType {
	if p.Kind == cxx.TypeInteger && p.ClangKind == "Bool" {
		return &goType{Kind: goBool, Go: "bool", C: "bool", Cxx: "bool", Param: p}
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
// selection and naming.
func (b *builder) buildBindings() error {
	symbols := append(append([]cxx.Symbol(nil), b.symbols...), b.synthMembers...)
	for _, sym := range symbols {
		owner := b.records[sym.Parent]
		if owner == nil {
			if sym.Kind != cxx.FreeFunc {
				// A member of a record this package does not model: nothing
				// to bind it to.
				continue
			}
			binding, err := b.newBinding(sym, nil)
			if err != nil {
				return symbolError(sym, err)
			}
			b.bindings = append(b.bindings, binding)
			continue
		}
		if sym.Access != cxx.AccessPublic {
			continue
		}
		switch sym.Kind {
		case cxx.Constructor:
			if sym.IsDeleted {
				continue
			}
			switch {
			case sym.IsCopyConstructor:
				clone, err := b.newBinding(sym, owner)
				if err != nil {
					return symbolError(sym, err)
				}
				clone.IsConstructor = false
				clone.IsClone = true
				clone.Params = nil
				owner.Clone = clone
				b.bindings = append(b.bindings, clone)
			case sym.IsMoveConstructor:
				// Go has no rvalue overload: a move constructor is not
				// exposed separately.
			default:
				ctor, err := b.newBinding(sym, owner)
				if err != nil {
					return symbolError(sym, err)
				}
				owner.Ctors = append(owner.Ctors, ctor)
				b.bindings = append(b.bindings, ctor)
			}
		case cxx.Destructor:
			if sym.IsDeleted {
				continue
			}
			// A destructor has no parameters and a void result, so building it
			// cannot fail.
			dtor, _ := b.newBinding(sym, owner)
			owner.Dtor = dtor
			b.bindings = append(b.bindings, dtor)
		case cxx.Method, cxx.StaticMethod:
			if sym.IsPureVirtual || sym.IsDeleted {
				continue
			}
			method, err := b.newBinding(sym, owner)
			if err != nil {
				return symbolError(sym, err)
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
		if !b.defaultConstructible(owner) {
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
		if owner.Mirrored || owner.Clone != nil || !owner.CopyConstructible {
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

// newBinding computes one binding's Go shape, carrier and wire arguments.
func (b *builder) newBinding(sym cxx.Symbol, owner *recordModel) (*binding, error) {
	binding := &binding{sym: sym, Owner: owner, This: sym.Kind == cxx.Method || sym.Kind == cxx.Constructor || sym.Kind == cxx.Destructor}
	binding.IsConstructor = sym.Kind == cxx.Constructor
	binding.IsDestructor = sym.Kind == cxx.Destructor
	binding.IsStatic = sym.Kind == cxx.StaticMethod

	if err := b.lowerParams(binding); err != nil {
		return nil, err
	}
	if !binding.IsConstructor && !binding.IsDestructor {
		result, err := b.mapper.mapType(sym.Result)
		if err != nil {
			return nil, fmt.Errorf("result: %w", err)
		}
		if result.Kind != goVoid {
			if result.Kind == goRecord {
				return nil, pending(fmt.Sprintf("%q is returned by value", sym.Result.Spelling), "return it through an out parameter or a reference at the C++ boundary")
			}
			if result.Kind == goPointer && result.Record != nil && !result.Record.Mirrored {
				return nil, pending(fmt.Sprintf("%q is a pointer to an opaque record", sym.Result.Spelling), "return it as a std::unique_ptr or std::shared_ptr result")
			}
			binding.Result = result
		}
	}
	b.chooseCarrier(binding)
	if err := b.checkRegisters(binding); err != nil {
		return nil, err
	}
	if binding.Carrier == thunk && binding.Status && binding.Result != nil {
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
	binding.ID = shortHash(displaySignature(sym))
	binding.SymbolVar = "cppgoSym_" + binding.ID
	binding.Trampoline = "cppgoCall_" + binding.ID
	if binding.Carrier == thunk {
		binding.Thunk = "cppgo_thunk_" + binding.ID
		binding.Symbol = binding.Thunk
	} else {
		binding.Symbol = sym.Mangled
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
		if mapped.Kind == goRecord {
			return pending(fmt.Sprintf("the record parameter %q is passed by value", p.Spelling), "take it by const reference or by pointer at the C++ boundary")
		}
		name := uniqueName(paramName(p.Name, visible), taken)
		visible++
		binding.Params = append(binding.Params, &goParam{Name: name, Type: mapped})
		wire := b.paramWire(name, mapped, p)
		wire.label = "a" + itoa(len(binding.Wire))
		addWire(wire)
	}
	return nil
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
	case goInteger, goFloat, goBool:
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
	case !sym.HasOutOfLineBody:
		binding.Carrier = thunk
		binding.Reason = "inline body: the library exports no symbol"
	case sym.HasSret:
		binding.Carrier = thunk
		binding.Reason = "the result is returned through a hidden pointer"
	default:
		binding.Carrier = direct
		binding.Reason = "noexcept, out of line, not virtual, register class"
	}
	if binding.Carrier == thunk {
		binding.Status = !sym.IsNoExcept
	}
	if binding.IsConstructor || binding.IsDestructor {
		binding.ErrorResult = true
	} else {
		binding.ErrorResult = binding.Carrier == thunk && binding.Status
	}
}

// checkRegisters rejects a signature the generated trampoline cannot pass. An
// argument block is the contract's answer; it is phase 2b, so this build names
// the limit instead of emitting a call with arguments on the stack.
func (b *builder) checkRegisters(binding *binding) error {
	ints, floats := 0, 0
	for _, arg := range binding.Wire {
		switch arg.class {
		case classInt:
			ints++
		case classFloat:
			floats++
		}
	}
	if ints > maxIntRegs || floats > maxFloatRegs {
		return fmt.Errorf("cppgo: %s: the call needs %d INTEGER and %d SSE argument registers; passing an argument block is not implemented by this build yet", binding.sym.QualifiedName, ints, floats)
	}
	return nil
}

// displaySignature renders the C++ declaration the evidence comment names.
func displaySignature(sym cxx.Symbol) string {
	params := make([]string, len(sym.Params))
	for i, p := range sym.Params {
		params[i] = p.Spelling
	}
	list := "(" + strings.Join(params, ", ") + ")"
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
		cfg:      b.cfg,
		mapper:   b.mapper,
		name:     b.cfg.Package,
		library:  b.cfg.Library,
		headers:  b.cfg.Headers,
		records:  records,
		bindings: b.bindings,
		verified: b.verified,
		warn:     b.warnings,
		flags:    b.flags,
	}
}

// warnings returns the package-level warnings, sorted for determinism.
func (m *packageModel) warnings() []string { return m.warn }

// report renders one line per binding: the carrier, the symbol and the reason.
func (m *packageModel) report() []string {
	lines := make([]string, 0, len(m.bindings))
	for _, binding := range m.bindings {
		symbol := binding.Symbol
		lines = append(lines, fmt.Sprintf("%-44s %-6s %-28s %s", displaySignature(binding.sym), binding.Carrier, symbol, binding.Reason))
	}
	return lines
}

// crossCheck verifies every direct binding against the library's dynamic symbol
// table when -library names a readable file. Thunk symbols exist only after the
// user compiles the generated shim, so they are not checked here.
func (b *builder) crossCheck() error {
	info, err := os.Stat(b.cfg.Library)
	if err != nil || info.IsDir() {
		return nil
	}
	names, err := cxx.DefinedDynamicSymbols(b.cfg.Library)
	if err != nil {
		return fmt.Errorf("cppgo: %s: %w", b.cfg.Library, err)
	}
	exported := make(map[string]bool, len(names))
	for _, name := range names {
		exported[name] = true
	}
	for _, binding := range b.bindings {
		if binding.Carrier != direct {
			continue
		}
		if !exported[binding.Symbol] {
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
		if binding.Carrier == thunk && binding.Status {
			// The status is the only result register use; a value, if any,
			// travels through the out-pointer already in the wire.
			slots = append(slots, &slot{label: "ret", goType: "int32", class: classInt, size: 4})
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
				slot.reg = intRegs[ints]
				ints++
			case classFloat:
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
		if result.Param.Unsigned {
			slot.goType = "uint64"
		}
	}
	return slot
}

// alignUp rounds n up to a multiple of a. Every slot is 1, 2, 4 or 8 bytes, so
// a guard for a <= 1 is dead code.
func alignUp(n, a int) int { return (n + a - 1) / a * a }
