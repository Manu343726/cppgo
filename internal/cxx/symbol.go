package cxx

import (
	"strings"
)

// Kind is the C++ flavour of a declaration, which is what decides the ABI
// shape of the call: constructors and methods carry an implicit `this`,
// static methods and free functions do not.
type Kind uint8

const (
	// FreeFunc is a free function: no `this`.
	FreeFunc Kind = iota
	// StaticMethod is a static member function: no `this`.
	StaticMethod
	// Method is a non-static member function: `this` in the first integer slot.
	Method
	// Constructor is a constructor: `this` in the first integer slot, but no
	// return value, and the mangling uses C1/C2.
	Constructor
	// Destructor is a destructor: `this` in the first integer slot, no return
	// value, mangling D1/D2.
	Destructor
)

// String implements fmt.Stringer.
func (k Kind) String() string {
	switch k {
	case FreeFunc:
		return "free function"
	case StaticMethod:
		return "static method"
	case Method:
		return "method"
	case Constructor:
		return "constructor"
	case Destructor:
		return "destructor"
	default:
		return "unknown kind"
	}
}

// TypeKind is the ABI-facing classification of a parameter or result type. It
// is deliberately coarse: it is what decides whether a value travels in an
// integer register or in an XMM register, which is all the emitter needs.
type TypeKind string

const (
	// TypeVoid is void.
	TypeVoid TypeKind = "void"
	// TypeInteger covers bool, char, short, int, long, long long and their
	// unsigned variants, including enums that have already decayed.
	TypeInteger TypeKind = "integer"
	// TypeFloat covers float, double and any wider floating type. Long double
	// is X87-class rather than SSE-class; ClangKind says which it is.
	TypeFloat TypeKind = "float"
	// TypePointer covers pointers, references and nullptr_t: at the ABI level
	// they are all one INTEGER slot.
	TypePointer TypeKind = "pointer"
	// TypeRecord is a struct, class or union used by value or by pointer; the
	// Ptr field says which.
	TypeRecord TypeKind = "record"
	// TypeEnum is an enum that did not decay to an integer (rare, but legal).
	TypeEnum TypeKind = "enum"
	// TypeOther is everything this package does not classify: arrays, function
	// pointers, member pointers, templates, unknown types.
	TypeOther TypeKind = "other"
)

// Param is one parameter of a signature, or its result.
type Param struct {
	// Spelling is the type as written in the source, from libclang:
	// "const double *", "std::size_t", "Vector".
	Spelling string
	// Kind is the ABI classification, computed on the canonical type so that
	// typedefs and using-aliases are resolved.
	Kind TypeKind
	// ClangKind is libclang's own type-kind spelling of the canonical type
	// ("Double", "Pointer", "Record", ...). Useful in diagnostics, and the
	// only place where distinctions like long double vs double survive.
	ClangKind string
	// SizeBytes is sizeof of the canonical type, i.e. the width actually
	// passed for scalars and pointers. It is negative for incomplete types and
	// for types whose size libclang refuses to compute.
	SizeBytes int64
	// Ptr reports that the parameter is a pointer or a reference, so the
	// generator must pass an address rather than a value.
	Ptr bool
	// Float reports that the value is floating point and therefore travels in
	// an XMM register. It is false for a pointer to a float.
	Float bool
}

// Symbol is one bindable C++ function.
//
// The field that matters most is HasOutOfLineBody: a member defined inside its
// class definition is implicitly inline, emits no dynamic symbol, and binding
// it produces a trampoline that fails at load time. See Project for how the
// answer is computed across translation units.
type Symbol struct {
	// QualifiedName is the C++ name built from semantic parents:
	// "mathx::Vector::norm", "mathx::Vector::Vector", "mathx::make".
	QualifiedName string
	// Mangled is the Itanium C++ ABI mangled name, taken from libclang and
	// never assembled by hand: "_ZNK5mathx6Vector4normEv".
	Mangled string
	// Kind is the declaration flavour.
	Kind Kind
	// IsConst is true for a const member function: the mangling carries K.
	IsConst bool
	// IsVirtual is true when the ABI dispatch goes through the vtable. Such a
	// function cannot be called directly from a fixed symbol: the vtable has to
	// be walked, so the generator has to reject it.
	IsVirtual bool
	// IsStatic is true for static member functions and for free functions with
	// internal linkage. Static members need no `this`.
	IsStatic bool
	// HasOutOfLineBody is true only when a translation unit contains a
	// definition of this function whose body is not inside the class
	// definition. Only those emit a dynamic symbol inside a library.
	HasOutOfLineBody bool
	// Params are the explicit parameters, `this` excluded.
	Params []Param
	// Result is the return type. Zero for constructors and destructors.
	Result Param
	// HasSret is true when the result is a record returned indirectly, through
	// a hidden pointer in the first integer argument slot.
	HasSret bool

	// DeclFile and DeclLine locate the declaration that was found.
	DeclFile string
	DeclLine int
	// DefFile and DefLine locate the out-of-line definition, when there is one.
	DefFile string
	DefLine int
}

// HasExportedSymbol reports whether a library built from these sources must
// export Mangled: the function needs a definition outside its class definition
// and external linkage. A namespace-scope `static` function has the first and
// not the second.
//
// One corner is not covered: for an abstract class, clang++ emits only the
// base-object constructor (C2) and not the complete-object one (C1), which is
// what libclang's mangling names. g++ emits both. Nothing a binding tool can do
// about it beyond failing at load time, which it does loudly.
func (s Symbol) HasExportedSymbol() bool {
	return s.HasOutOfLineBody && !(s.Kind == FreeFunc && s.IsStatic)
}

// Signature is a copy of the parameters and result, for printing.
func (s Symbol) Signature() string {
	parts := make([]string, len(s.Params))
	for i, p := range s.Params {
		parts[i] = p.Spelling
	}
	sig := s.Result.Spelling
	if sig == "" {
		sig = "void"
	}
	out := sig + "(" + strings.Join(parts, ", ") + ")"
	if s.IsConst {
		out += " const"
	}
	return out
}

// extractor walks one translation unit and turns function cursors into Symbols.
type extractor struct {
	l         *Libclang
	tu        *TU
	symbols   []Symbol
	byName    map[string][]int
	byMangled map[string]int
}

// maxParentDepth bounds the walk up the semantic parent chain.
const maxParentDepth = 32

// maxTrivialityDepth bounds the recursion into base classes and members when
// deciding whether a record can be returned in registers.
const maxTrivialityDepth = 4

// extract fills tu.symbols and the lookup indexes.
func (tu *TU) extract() {
	if tu.isClosed() {
		// libclang owns the translation unit's memory: walking a disposed one is
		// a use-after-free that ends in SIGABRT inside libclang, so the closed
		// state is checked here as well as on the Libclang handle.
		return
	}
	ex := &extractor{
		l:         tu.l,
		tu:        tu,
		byName:    make(map[string][]int),
		byMangled: make(map[string]int),
	}
	if err := tu.l.visit(tu.l.f.getTranslationUnitCursor(tu.tu), ex.visit); err != nil {
		return
	}
	tu.symbols = ex.symbols
	tu.byName = ex.byName
	tu.byNameMangled = ex.byMangled
}

// visit is the clang_visitChildren callback. It records every function-ish
// declaration and descends only into namespaces and class definitions, so a
// method body is never mistaken for the API.
func (e *extractor) visit(cursor, parent CXCursor) int32 {
	switch kind := e.l.f.getCursorKind(cursor); kind {
	case cursorFunctionDecl, cursorCXXMethod, cursorConstructor, cursorDestructor:
		e.collect(cursor)
		return visitContinue
	case cursorNamespace, cursorClassDecl, cursorStructDecl, cursorUnionDecl,
		cursorLinkageSpec, cursorNamespaceAlias:
		return visitRecurse
	default:
		return visitContinue
	}
}

// collect turns one function cursor into a Symbol.
func (e *extractor) collect(cursor CXCursor) {
	loc := e.l.f.getCursorLocation(cursor)
	if e.l.f.locationIsInSystemHeader(loc) != 0 {
		// std:: and friends: not ours to bind, and they would bury the
		// interesting symbols in noise.
		return
	}
	name, ok := e.qualifiedName(cursor)
	if !ok {
		return
	}
	sym := Symbol{
		QualifiedName: name,
		Mangled:       e.l.str(e.l.f.cursorMangling(cursor)),
		Kind:          e.kind(cursor),
	}
	sym.IsConst, sym.IsVirtual, sym.IsStatic = e.qualifiers(cursor, sym.Kind)
	sym.DeclFile, sym.DeclLine = e.locationValues(loc)

	if e.l.f.isCursorDefinition(cursor) != 0 {
		// A definition inside the class definition is implicitly inline and
		// emits no dynamic symbol; libclang reports exactly that through
		// clang_Cursor_isFunctionInlined. An out-of-line definition in the same
		// header is not inlined and does emit one.
		if e.l.f.isFunctionInlined(cursor) == 0 {
			sym.HasOutOfLineBody = true
			sym.DefFile, sym.DefLine = sym.DeclFile, sym.DeclLine
		}
	}

	funcType := e.l.f.getCursorType(cursor)
	resultType := e.l.f.getResultType(funcType)
	sym.Result = e.param(resultType)
	if sym.Kind == Constructor || sym.Kind == Destructor {
		// Constructors and destructors have no result at all; saying "void"
		// is more useful to a reader than an empty Param.
		sym.Result = Param{Kind: TypeVoid, ClangKind: "Void", Spelling: "void", SizeBytes: 0}
	} else {
		sym.HasSret = e.indirectResult(resultType)
	}
	if n := e.l.f.getNumArgTypes(funcType); n > 0 {
		sym.Params = make([]Param, 0, n)
		for i := uint32(0); i < uint32(n); i++ {
			sym.Params = append(sym.Params, e.param(e.l.f.getArgType(funcType, i)))
		}
	}
	e.add(sym)
}

// add records a symbol, merging with an earlier sighting of the same mangled
// name (a declaration and its definition, or the same declaration reached
// through two translation units).
func (e *extractor) add(sym Symbol) {
	if sym.Mangled != "" {
		if i, ok := e.byMangled[sym.Mangled]; ok {
			mergeInto(&e.symbols[i], sym)
			return
		}
	}
	i := len(e.symbols)
	e.symbols = append(e.symbols, sym)
	e.byName[sym.QualifiedName] = append(e.byName[sym.QualifiedName], i)
	if sym.Mangled != "" {
		e.byMangled[sym.Mangled] = i
	}
}

// mergeInto folds a second sighting of a symbol into the first. Definitions win
// over declarations; everything else is OR-ed, because the two sightings agree.
func mergeInto(into *Symbol, from Symbol) {
	if from.HasOutOfLineBody && !into.HasOutOfLineBody {
		into.HasOutOfLineBody = true
		into.DefFile, into.DefLine = from.DefFile, from.DefLine
	}
	if len(into.Params) == 0 && len(from.Params) > 0 {
		into.Params = from.Params
	}
	if into.Result.Kind == "" {
		into.Result = from.Result
	}
	into.IsConst = into.IsConst || from.IsConst
	into.IsStatic = into.IsStatic || from.IsStatic
	into.IsVirtual = into.IsVirtual || from.IsVirtual
	into.HasSret = into.HasSret || from.HasSret
	if into.DeclFile == "" {
		into.DeclFile, into.DeclLine = from.DeclFile, from.DeclLine
	}
}

// kind maps a cursor kind (plus the storage class) to a Symbol Kind.
func (e *extractor) kind(cursor CXCursor) Kind {
	switch e.l.f.getCursorKind(cursor) {
	case cursorConstructor:
		return Constructor
	case cursorDestructor:
		return Destructor
	case cursorCXXMethod:
		if e.l.f.methodIsStatic(cursor) != 0 {
			return StaticMethod
		}
		return Method
	default:
		return FreeFunc
	}
}

// qualifiers reads the const/virtual/static flags. The clang_CXXMethod_*
// accessors cast the cursor to a CXXMethodDecl, so they are only called for
// kinds where that cast is valid.
func (e *extractor) qualifiers(cursor CXCursor, kind Kind) (isConst, isVirtual, isStatic bool) {
	switch kind {
	case Method, StaticMethod:
		isConst = e.l.f.methodIsConst(cursor) != 0
		isVirtual = e.l.f.methodIsVirtual(cursor) != 0
		isStatic = e.l.f.methodIsStatic(cursor) != 0
	case Constructor:
		// Constructors are never const, and the only "virtual" a constructor
		// can be is a virtual base, which is not a vtable entry.
		isVirtual = false
	case Destructor:
		// A destructor can be virtual, and that is worth reporting.
		isVirtual = e.l.f.methodIsVirtual(cursor) != 0
	case FreeFunc:
		// A namespace-scope `static` function has internal linkage and emits no
		// exported symbol. clang_CXXMethod_isStatic only understands methods,
		// so the storage class is the right question here.
		isStatic = e.l.f.getCursorStorageClass(cursor) == storageClassStatic
	}
	return isConst, isVirtual, isStatic
}

// qualifiedName builds "ns::Class::member" from the semantic parent chain.
func (e *extractor) qualifiedName(cursor CXCursor) (string, bool) {
	spelling := e.l.str(e.l.f.getCursorSpelling(cursor))
	parents := make([]parentName, 0, 4)
	for i := 0; i < maxParentDepth; i++ {
		cursor = e.l.f.getSemanticParent(cursor)
		kind := e.l.f.getCursorKind(cursor)
		parents = append(parents, parentName{kind: kind, spelling: e.l.str(e.l.f.getCursorSpelling(cursor))})
		if kind == cursorTranslationUnit || kind == cursorInvalidFile || kind == cursorNoDeclFound {
			break
		}
	}
	return qualify(spelling, parents)
}

// parentName is one step of the semantic parent chain.
type parentName struct {
	kind     int32
	spelling string
}

// qualify joins a cursor's spelling and its parent chain (innermost first) into
// a qualified C++ name, rejecting the entities that cannot have an exported
// symbol: an unnamed entity, anything inside an anonymous namespace (internal
// linkage), and a chain that never reaches the translation unit.
func qualify(spelling string, parents []parentName) (string, bool) {
	if spelling == "" {
		// An unnamed entity: there is no name to look a symbol up by.
		return "", false
	}
	parts := []string{spelling}
	for _, parent := range parents {
		switch parent.kind {
		case cursorTranslationUnit, cursorInvalidFile, cursorNoDeclFound:
			// Reverse the collected parts into a qualifier.
			for lo, hi := 0, len(parts)-1; lo < hi; lo, hi = lo+1, hi-1 {
				parts[lo], parts[hi] = parts[hi], parts[lo]
			}
			return strings.Join(parts, "::"), true
		case cursorNamespace:
			if parent.spelling == "" {
				// An anonymous namespace has internal linkage.
				return "", false
			}
		}
		if parent.spelling != "" {
			parts = append(parts, parent.spelling)
		}
	}
	return "", false
}

// param classifies one type.
func (e *extractor) param(t CXType) Param {
	canonical := e.l.f.getCanonicalType(t)
	p := Param{
		Spelling:  e.l.str(e.l.f.getTypeSpelling(t)),
		ClangKind: e.l.str(e.l.f.getTypeKindSpelling(canonical.Kind)),
		SizeBytes: e.l.f.typeGetSizeOf(canonical),
	}
	switch canonical.Kind {
	case typeVoid:
		p.Kind = TypeVoid
	case typeBool, typeCharU, typeUChar, typeChar16, typeChar32, typeUShort, typeUInt,
		typeULong, typeULongLong, typeUInt128, typeCharS, typeSChar, typeWChar, typeShort,
		typeInt, typeLong, typeLongLong, typeInt128:
		p.Kind = TypeInteger
	case typeFloat, typeDouble, typeLongDouble:
		p.Kind = TypeFloat
		p.Float = true
	case typePointer, typeBlockPointer, typeLValueReference, typeRValueReference, typeNullPtr:
		p.Kind = TypePointer
		p.Ptr = true
	case typeRecord:
		p.Kind = TypeRecord
	case typeEnum:
		p.Kind = TypeEnum
	default:
		p.Kind = TypeOther
	}
	return p
}

// indirectResult reports whether a return type comes back through a hidden
// pointer instead of in registers.
//
// Two independent rules make a record indirect, and both are needed:
//
//   - Itanium/SysV memory class: more than two eightbytes cannot ride in
//     RAX:RDX, so the caller passes a destination pointer in RDI (§3.2.3).
//   - Itanium "non-trivial for the purposes of calls": a class with a
//     user-provided copy/move constructor or destructor, or one that is
//     polymorphic, is returned indirectly however small it is. Measured on
//     this machine: a 4-byte struct with a user-declared destructor comes back
//     through RDI, and so does a 4-byte struct with a virtual member, while a
//     16-byte POD comes back in XMM0:XMM1.
//
// libclang exposes no "trivial for the purposes of calls" query, so the second
// rule is evaluated structurally, see nonTrivialRecord.
func (e *extractor) indirectResult(t CXType) bool {
	canonical := e.l.f.getCanonicalType(t)
	if canonical.Kind != typeRecord {
		return false
	}
	if size := e.l.f.typeGetSizeOf(canonical); size < 0 || size > 16 {
		return true
	}
	return e.nonTrivialRecord(e.l.f.getTypeDeclaration(canonical), 0)
}

// nonTrivialRecord reports whether a record is non-trivial for the purposes of
// calls: it is polymorphic, or it declares a copy/move constructor or a
// destructor that is not `= default`, or a base or member is like that.
func (e *extractor) nonTrivialRecord(decl CXCursor, depth int) bool {
	kind := e.l.f.getCursorKind(decl)
	if kind != cursorClassDecl && kind != cursorStructDecl && kind != cursorUnionDecl {
		return false
	}
	if depth > maxTrivialityDepth {
		// Out of budget: assume the worst, because guessing "trivial" would
		// generate a call with a missing hidden pointer.
		return true
	}
	nonTrivial := false
	fields := make([]CXCursor, 0, 4)
	err := e.l.visit(decl, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorDestructor:
			if e.l.f.methodIsDefaulted(child) == 0 {
				nonTrivial = true
			}
		case cursorConstructor:
			copyOrMove := e.l.f.constructorIsCopyConstructor(child) != 0 ||
				e.l.f.constructorIsMoveConstructor(child) != 0
			if copyOrMove && e.l.f.methodIsDefaulted(child) == 0 {
				nonTrivial = true
			}
		case cursorCXXMethod:
			if e.l.f.methodIsVirtual(child) != 0 {
				nonTrivial = true
			}
		case cursorBaseSpecifier:
			if e.l.f.isVirtualBase(child) != 0 {
				nonTrivial = true
				return visitContinue
			}
			if e.nonTrivialRecord(e.l.f.getTypeDeclaration(e.l.f.getCursorType(child)), depth+1) {
				nonTrivial = true
			}
		case cursorFieldDecl:
			fields = append(fields, child)
		}
		return visitContinue
	})
	if err != nil || nonTrivial {
		return true
	}
	for _, field := range fields {
		if e.nonTrivialRecord(e.l.f.getTypeDeclaration(e.l.f.getCursorType(field)), depth+1) {
			return true
		}
	}
	return false
}

// locationValues turns a source location into file and line. A location with no
// file (a built-in, or a cursor from an unsaved buffer) yields "".
func (e *extractor) locationValues(loc CXSourceLocation) (string, int) {
	var file uintptr
	var line, column, offset uint32
	e.l.f.getFileLocation(loc, &file, &line, &column, &offset)
	// clang_getFileName(NULL) is documented to return an empty string, so no
	// null check is needed here.
	return e.l.str(e.l.f.getFileName(file)), int(line)
}
