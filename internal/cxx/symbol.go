package cxx

import (
	"strings"
)

// Kind is the C++ flavor of a declaration, which is what decides the ABI
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
	// TypeArray is a C array: fixed size, incomplete, variable or dependent.
	TypeArray TypeKind = "array"
	// TypeOther is everything this package does not classify: arrays, function
	// pointers, member pointers, templates, unknown types.
	TypeOther TypeKind = "other"
)

// Param is one parameter of a signature, its result, or a field of a record.
type Param struct {
	// Name is the parameter's declared name, empty when the declaration
	// omitted it or when the value is a result. It is what lets the generator
	// name the Go parameter the way the C++ header did.
	Name string
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
	// Unsigned is the signedness of an integer or enum type. It is false for
	// every other kind.
	Unsigned bool
	// IsConst is top-level const qualification: true for "const int", false
	// for "const int *" (the pointee is const, the pointer is not).
	IsConst bool
	// RecordName is the qualified C++ name of a record or enum type
	// ("mathx::Point", "mathx::Axis"), empty for every other kind. It is what
	// lets the type mapper find the generated Go type without re-parsing a
	// spelling.
	RecordName string
	// Pointee describes what a pointer or reference points at. It is nil for
	// every other kind. The pointee of "const double *" carries IsConst.
	Pointee *Param
	// Element describes an array's element type, nil otherwise.
	Element *Param
	// ArraySize is the element count of a fixed array, or -1 for an
	// incomplete, variable or dependent array. A dependent size is resolved
	// when a template argument supplies it.
	ArraySize int64
}

// IsStream reports whether the value is a canonical C++ stream reference or
// value: std::basic_ostream<C,T> or std::basic_istream<C,T>, which is what the
// std::ostream/istream aliases resolve to. The operator naming rule uses it.
func (p Param) IsStream() bool {
	if p.Ptr && p.Pointee != nil {
		return p.Pointee.stream()
	}
	return p.stream()
}

// stream checks the declaration spelling and the canonical record name. The
// spelling is the reliable half: libclang's canonical form of a reference to
// the std::ostream alias can be wrong (it resolves to int), so an alias is
// recognized by how it is written.
func (p Param) stream() bool {
	if strings.HasPrefix(p.RecordName, "std::basic_ostream<") ||
		strings.HasPrefix(p.RecordName, "std::basic_istream<") {
		return true
	}
	spelling := strings.TrimSpace(p.Spelling)
	for {
		switch {
		case strings.HasPrefix(spelling, "const "):
			spelling = strings.TrimSpace(spelling[len("const "):])
		case strings.HasPrefix(spelling, "volatile "):
			spelling = strings.TrimSpace(spelling[len("volatile "):])
		default:
			goto trimmed
		}
	}
trimmed:
	spelling = strings.TrimSuffix(spelling, "&")
	spelling = strings.TrimSpace(spelling)
	switch spelling {
	case "std::ostream", "std::istream", "std::wostream", "std::wistream":
		return true
	}
	return strings.HasPrefix(spelling, "std::basic_ostream<") ||
		strings.HasPrefix(spelling, "std::basic_istream<")
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
	// Kind is the declaration flavor.
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
	// Parent is the qualified name of the semantic parent: the class for a
	// member, the namespace for a free function. It is how a generator groups
	// a class's constructors and methods, and it is empty only when the
	// declaration could not be qualified.
	Parent string
	// Access is the C++ access control of the declaration within its parent.
	// It is AccessNone for a namespace-scope declaration.
	Access Access
	// IsPureVirtual is true for a pure virtual member: declared, never
	// defined, so it has no address to call.
	IsPureVirtual bool
	// IsNoExcept is true when the declaration promises that nothing escapes.
	// It is derived from ExceptionSpec: only the specifications known not to
	// throw permit the direct call path.
	IsNoExcept bool
	// ExceptionSpec is the declared exception specification. It decides the
	// carrier: a direct trampoline has no handler, so only a specification
	// that cannot throw may use it.
	ExceptionSpec ExceptionSpec
	// IsVariadic is true for a C variadic function (`...`), which has no
	// fixed argument list to lay out.
	IsVariadic bool
	// IsDeleted is true for a `= delete`d special member. A deleted
	// constructor or copy constructor is what makes a record not
	// default-constructible or not copyable.
	IsDeleted bool
	// IsDefaulted is true for a special member declared `= default`.
	IsDefaulted bool
	// IsOperator is true for a C++ operator member (including a conversion
	// operator). Operator is the operator as written, without the "operator"
	// keyword: "+", "==", "[]", "()", "=", or a conversion's target type.
	IsOperator bool
	// Operator is the operator token or conversion target, empty otherwise.
	Operator string
	// IsConversion is true for a conversion operator (operator T).
	IsConversion bool
	// IsTemplate is true when the declaration is a template. It has no
	// mangled name and no layout until it is instantiated.
	IsTemplate bool
	// Params are the explicit parameters, `this` excluded.
	Params []Param
	// Result is the return type. Zero for constructors and destructors.
	Result Param
	// HasSret is true when the result is a record returned indirectly, through
	// a hidden pointer in the first integer argument slot.
	HasSret bool
	// IsCopyConstructor is true for a copy constructor: Clone() is generated
	// from it, not a New<Type> factory.
	IsCopyConstructor bool
	// IsMoveConstructor is true for a move constructor, which is not exposed
	// separately because Go has no rvalue overload.
	IsMoveConstructor bool

	// DeclFile and DeclLine locate the declaration that was found.
	DeclFile string
	DeclLine int
	// DefFile and DefLine locate the out-of-line definition, when there is one.
	DefFile string
	DefLine int
}

// isDeleted and isDefaulted read the special-member flags, but only for the
// cursor kinds where the clang_CXXMethod_* casts are valid.
func (l *Libclang) isDeleted(cursor CXCursor, kind Kind) bool {
	switch kind {
	case Method, StaticMethod, Constructor, Destructor:
		return l.f.methodIsDeleted(cursor) != 0
	default:
		return false
	}
}

// isDefaulted reports a special member declared `= default`.
func (l *Libclang) isDefaulted(cursor CXCursor, kind Kind) bool {
	switch kind {
	case Method, StaticMethod, Constructor, Destructor:
		return l.f.methodIsDefaulted(cursor) != 0
	default:
		return false
	}
}

// isPureVirtual reads the pure-virtual flag, but only for the cursor kinds
// where clang_CXXMethod_isPureVirtual's cast to a CXXMethodDecl is valid.
func (l *Libclang) isPureVirtual(cursor CXCursor) bool {
	switch l.f.getCursorKind(cursor) {
	case cursorCXXMethod, cursorDestructor:
		return l.f.methodIsPureVirtual(cursor) != 0
	default:
		return false
	}
}

// HasExportedSymbol reports whether a library built from these sources must
// export Mangled: the function needs a definition outside its class definition
// and external linkage. A namespace-scope `static` function has the first and
// not the second, and a template pattern has neither until it is instantiated.
//
// One corner is not covered: for an abstract class, clang++ emits only the
// base-object constructor (C2) and not the complete-object one (C1), which is
// what libclang's mangling names. g++ emits both. Nothing a binding tool can do
// about it beyond failing at load time, which it does loudly.
func (s Symbol) HasExportedSymbol() bool {
	if s.IsTemplate {
		return false
	}
	return s.HasOutOfLineBody && (s.Kind != FreeFunc || !s.IsStatic)
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

// maxHierarchyDepth bounds the recursion through base classes when answering
// whether a record has a vtable.
const maxHierarchyDepth = 16

// polymorphic reports whether a record has a vtable: it declares a virtual
// function, has a virtual base, or inherits either from a base. libclang has no
// direct query, so it is evaluated structurally.
func (l *Libclang) polymorphic(decl CXCursor, depth int) bool {
	switch l.f.getCursorKind(decl) {
	case cursorClassDecl, cursorStructDecl, cursorUnionDecl:
	default:
		return false
	}
	if depth > maxHierarchyDepth {
		// Out of budget: assume the worst, because guessing "not polymorphic"
		// would generate a destructor that does not dispatch.
		return true
	}
	found := false
	_ = l.visit(decl, func(child, parent CXCursor) int32 {
		switch l.f.getCursorKind(child) {
		case cursorCXXMethod, cursorConstructor, cursorDestructor:
			if l.f.methodIsVirtual(child) != 0 {
				found = true
			}
		case cursorBaseSpecifier:
			if l.f.isVirtualBase(child) != 0 {
				found = true
			} else if l.polymorphic(l.f.getTypeDeclaration(l.f.getCursorType(child)), depth+1) {
				found = true
			}
		}
		return visitContinue
	})
	return found
}

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
	tu.extractRecords()
}

// visit is the clang_visitChildren callback. It records every function-ish
// declaration and descends only into namespaces and class definitions, so a
// method body is never mistaken for the API.
func (e *extractor) visit(cursor, parent CXCursor) int32 {
	switch kind := e.l.f.getCursorKind(cursor); kind {
	case cursorFunctionDecl, cursorCXXMethod, cursorConversionFunction, cursorConstructor, cursorDestructor:
		e.collect(cursor, parent)
		return visitContinue
	case cursorFunctionTemplate:
		// libclang exposes a function template only as the template cursor; its
		// function type is complete, so the pattern is collected from here. It
		// has no symbol until it is instantiated.
		e.collect(cursor, parent)
		return visitRecurse
	case cursorClassTemplate:
		// The pattern of a class template has no symbol until it is
		// instantiated. Its members are collected and marked.
		return visitRecurse
	case cursorClassTemplatePartialSpecialization:
		// A partial specialization is a pattern, not a type: its members are
		// read from its cursor when an instantiation matches it.
		return visitContinue
	case cursorNamespace, cursorClassDecl, cursorStructDecl, cursorUnionDecl,
		cursorLinkageSpec, cursorNamespaceAlias:
		return visitRecurse
	default:
		return visitContinue
	}
}

// collect turns one function cursor into a Symbol.
func (e *extractor) collect(cursor, parent CXCursor) {
	loc := e.l.f.getCursorLocation(cursor)
	if e.l.f.locationIsInSystemHeader(loc) != 0 {
		// std:: and friends: not ours to bind, and they would bury the
		// interesting symbols in noise.
		return
	}
	name, ok := e.l.qualifiedName(cursor)
	if !ok {
		return
	}
	spec := e.l.exceptionSpec(cursor)
	symKind := e.l.kind(cursor)
	sym := Symbol{
		QualifiedName: name,
		Mangled:       e.l.str(e.l.f.cursorMangling(cursor)),
		Kind:          symKind,
		Parent:        parentOf(name),
		Access:        e.l.access(cursor),
		IsPureVirtual: e.l.isPureVirtual(cursor),
		IsDeleted:     e.l.isDeleted(cursor, symKind),
		IsDefaulted:   e.l.isDefaulted(cursor, symKind),
		IsNoExcept:    !spec.CanThrow(),
		ExceptionSpec: spec,
		IsVariadic:    e.l.f.cursorIsVariadic(cursor) != 0,
		IsTemplate:    isTemplateParent(e.l.f.getCursorKind(cursor)) || isTemplateParent(e.l.f.getCursorKind(parent)),
	}
	sym.IsConst, sym.IsVirtual, sym.IsStatic = e.l.qualifiers(cursor, sym.Kind)
	sym.DeclFile, sym.DeclLine = e.l.locationValues(loc)

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
	if sym.IsTemplate {
		// A template pattern emits no symbol of its own, even when its body is
		// written outside a class: only an instantiation does. Saying it has an
		// exported body would send the generator looking for a dlsym target.
		sym.HasOutOfLineBody = false
	}

	funcType := e.l.f.getCursorType(cursor)
	resultType := e.l.f.getResultType(funcType)
	sym.Result = e.l.param(resultType)
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
			sym.Params = append(sym.Params, e.l.param(e.l.f.getArgType(funcType, i)))
		}
		e.l.paramNames(cursor, sym.Params)
	}
	if sym.Kind == Constructor {
		// A copy constructor is Clone(), not a factory, and a move
		// constructor is not exposed separately: Go has no rvalue overload.
		sym.IsCopyConstructor = e.l.f.constructorIsCopyConstructor(cursor) != 0
		sym.IsMoveConstructor = e.l.f.constructorIsMoveConstructor(cursor) != 0
	}
	sym.IsOperator, sym.Operator, sym.IsConversion = operatorInfo(e.l.str(e.l.f.getCursorSpelling(cursor)), sym.Result)
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
	if into.Parent == "" {
		into.Parent = from.Parent
	}
	if into.Access == AccessNone {
		into.Access = from.Access
	}
	into.IsPureVirtual = into.IsPureVirtual || from.IsPureVirtual
	into.IsDeleted = into.IsDeleted || from.IsDeleted
	into.IsDefaulted = into.IsDefaulted || from.IsDefaulted
	into.IsOperator = into.IsOperator || from.IsOperator
	into.IsConversion = into.IsConversion || from.IsConversion
	if into.Operator == "" {
		into.Operator = from.Operator
	}
	into.IsVariadic = into.IsVariadic || from.IsVariadic
	into.IsTemplate = into.IsTemplate || from.IsTemplate
	into.IsCopyConstructor = into.IsCopyConstructor || from.IsCopyConstructor
	into.IsMoveConstructor = into.IsMoveConstructor || from.IsMoveConstructor
	if into.ExceptionSpec == ExceptionUnspecified && from.ExceptionSpec != ExceptionUnspecified {
		into.ExceptionSpec = from.ExceptionSpec
	}
	into.IsNoExcept = !into.ExceptionSpec.CanThrow()
}

// operatorInfo names a C++ operator member. A conversion operator's name is
// its target type, which is the result type libclang reports.
func operatorInfo(spelling string, result Param) (isOperator bool, operator string, isConversion bool) {
	const prefix = "operator"
	if !strings.HasPrefix(spelling, prefix) {
		return false, "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(spelling, prefix))
	switch rest {
	case "()":
		return true, "()", false
	case "[]":
		return true, "[]", false
	case "":
		// "operator" alone is malformed; treat it as an unnamed operator.
		return true, "", false
	}
	if result.Kind != "" && result.Kind != TypeVoid && !startsWithOperatorToken(rest) && !isNamedOperator(rest) {
		// A conversion operator: "operator int", "operator std::string".
		target := result.Spelling
		if target == "" {
			target = rest
		}
		return true, target, true
	}
	return true, rest, false
}

// isNamedOperator reports the alphabetic operators, which are not conversions
// and not punctuation.
func isNamedOperator(rest string) bool {
	for _, name := range []string{"new", "delete", "co_await"} {
		if strings.HasPrefix(rest, name) {
			return true
		}
	}
	return false
}

// startsWithOperatorToken reports whether text begins with one of the
// punctuation operators, which is how a non-conversion operator is told apart
// from a conversion target.
func startsWithOperatorToken(text string) bool {
	switch text[0] {
	case '+', '-', '*', '/', '%', '^', '&', '|', '~', '!', '=', '<', '>', ',', '(', '[':
		return true
	default:
		return false
	}
}

// parentOf returns the qualified name without its last component:
// "mathx::Vector::norm" -> "mathx::Vector", "mathx::add" -> "mathx". A name
// with no scope, or an empty one, has no parent.
func parentOf(qualified string) string {
	if i := strings.LastIndex(qualified, "::"); i >= 0 {
		return qualified[:i]
	}
	return ""
}

// isTemplateParent reports whether a declaration's traversal parent is a
// template, which makes the declaration part of the pattern: it has no layout
// and no symbol of its own.
func isTemplateParent(kind int32) bool {
	return kind == cursorFunctionTemplate ||
		kind == cursorClassTemplate ||
		kind == cursorClassTemplatePartialSpecialization
}

// kind maps a cursor kind (plus the storage class) to a Symbol Kind.
func (l *Libclang) kind(cursor CXCursor) Kind {
	switch l.f.getCursorKind(cursor) {
	case cursorConstructor:
		return Constructor
	case cursorDestructor:
		return Destructor
	case cursorCXXMethod:
		if l.f.methodIsStatic(cursor) != 0 {
			return StaticMethod
		}
		return Method
	case cursorConversionFunction:
		// A conversion operator is a method with a target type and `this`.
		return Method
	case cursorFunctionTemplate:
		switch l.f.getCursorKind(l.f.getSemanticParent(cursor)) {
		case cursorClassDecl, cursorStructDecl, cursorClassTemplate, cursorClassTemplatePartialSpecialization:
			// A member function template needs `this` like any method.
			return Method
		default:
			return FreeFunc
		}
	default:
		return FreeFunc
	}
}

// qualifiers reads the const/virtual/static flags. The clang_CXXMethod_*
// accessors cast the cursor to a CXXMethodDecl, so they are only called for
// kinds where that cast is valid.
func (l *Libclang) qualifiers(cursor CXCursor, kind Kind) (isConst, isVirtual, isStatic bool) {
	if l.f.getCursorKind(cursor) == cursorFunctionTemplate {
		// The clang_CXXMethod_* accessors cast the cursor to a CXXMethodDecl;
		// a template cursor is not one, so none of them is asked.
		return false, false, false
	}
	switch kind {
	case Method, StaticMethod:
		isConst = l.f.methodIsConst(cursor) != 0
		isVirtual = l.f.methodIsVirtual(cursor) != 0
		isStatic = l.f.methodIsStatic(cursor) != 0
	case Constructor:
		// Constructors are never const, and the only "virtual" a constructor
		// can be is a virtual base, which is not a vtable entry.
		isVirtual = false
	case Destructor:
		// A destructor can be virtual, and that is worth reporting.
		isVirtual = l.f.methodIsVirtual(cursor) != 0
	case FreeFunc:
		// A namespace-scope `static` function has internal linkage and emits no
		// exported symbol. clang_CXXMethod_isStatic only understands methods,
		// so the storage class is the right question here.
		isStatic = l.f.getCursorStorageClass(cursor) == storageClassStatic
	}
	return isConst, isVirtual, isStatic
}

// qualifiedName builds "ns::Class::member" from the semantic parent chain.
func (l *Libclang) qualifiedName(cursor CXCursor) (string, bool) {
	spelling := l.str(l.f.getCursorSpelling(cursor))
	parents := make([]parentName, 0, 4)
	for i := 0; i < maxParentDepth; i++ {
		cursor = l.f.getSemanticParent(cursor)
		kind := l.f.getCursorKind(cursor)
		spelling := l.str(l.f.getCursorSpelling(cursor))
		// A member of a full or partial specialization is named after the
		// specialization, not after the pattern: Q<int>::Kind, not Q::Kind.
		// libclang spells the parent "Q" for all three, so the arguments are
		// what tell them apart.
		if isSpecializationRecordKind(kind) {
			if args := l.templateArguments(cursor); len(args) > 0 {
				spelling = specializationName(spelling, args)
			}
		}
		parents = append(parents, parentName{kind: kind, spelling: spelling})
		if kind == cursorTranslationUnit || kind == cursorInvalidFile || kind == cursorNoDeclFound {
			break
		}
	}
	return qualify(spelling, parents)
}

// isSpecializationRecordKind reports the cursor kinds a specialization can be
// declared with. The primary pattern is a ClassTemplate and is not one.
func isSpecializationRecordKind(kind int32) bool {
	switch kind {
	case cursorClassDecl, cursorStructDecl, cursorUnionDecl, cursorClassTemplatePartialSpecialization:
		return true
	default:
		return false
	}
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
	if spelling == "" || strings.HasPrefix(spelling, "(") {
		// An unnamed entity. libclang synthesizes a spelling for these —
		// "(unnamed enum at /tmp/…/mathx.hpp:183:1)" — and a source path is
		// not a name: it differs between machines and would make generated
		// output non-deterministic. There is nothing to bind under it either.
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

// param classifies one type. It is the single place where a CXType becomes
// model data, so a parameter, a result and a record field describe the same
// type in exactly the same way.
func (l *Libclang) param(t CXType) Param {
	canonical := l.f.getCanonicalType(t)
	p := Param{
		Spelling:  l.str(l.f.getTypeSpelling(t)),
		ClangKind: l.str(l.f.getTypeKindSpelling(canonical.Kind)),
		SizeBytes: l.f.typeGetSizeOf(canonical),
		IsConst:   l.f.isConstQualifiedType(t) != 0,
	}
	switch canonical.Kind {
	case typeVoid:
		p.Kind = TypeVoid
	case typeBool, typeCharU, typeUChar, typeChar16, typeChar32, typeUShort, typeUInt,
		typeULong, typeULongLong, typeUInt128, typeCharS, typeSChar, typeWChar, typeShort,
		typeInt, typeLong, typeLongLong, typeInt128:
		p.Kind = TypeInteger
		p.Unsigned = isUnsignedKind(canonical.Kind)
	case typeFloat, typeDouble, typeLongDouble:
		p.Kind = TypeFloat
		p.Float = true
	case typePointer, typeBlockPointer, typeLValueReference, typeRValueReference, typeNullPtr:
		p.Kind = TypePointer
		p.Ptr = true
	case typeConstantArray, typeIncompleteArray, typeVariableArray, typeDependentSizedArray:
		p.Kind = TypeArray
		element := l.param(l.f.getArrayElementType(canonical))
		p.Element = &element
		p.ArraySize = l.f.getArraySize(canonical)
	case typeRecord:
		p.Kind = TypeRecord
		p.RecordName = l.declarationName(canonical)
	case typeEnum:
		p.Kind = TypeEnum
		p.RecordName = l.declarationName(canonical)
		p.Unsigned = l.enumIsUnsigned(canonical)
	default:
		p.Kind = TypeOther
	}
	if p.Ptr {
		// The pointee is read from the surface type: libclang's canonical form
		// of a reference to a typedef of a class template specialization can
		// be wrong (std::ostream & canonically becomes int &).
		p.Pointee = l.pointee(t)
	}
	return p
}

// declarationName returns the qualified name of the record or enum a type
// refers to. It is empty for an unnamed type, whose only libclang spelling
// embeds a source path: the generator refuses those rather than inventing a
// name that changes between checkouts.
func (l *Libclang) declarationName(t CXType) string {
	decl := l.f.getTypeDeclaration(t)
	if template, ok := l.specialization(decl); ok {
		if args := l.fillArgumentSpellings(l.templateArguments(decl), t); len(args) > 0 {
			name, _ := l.qualifiedName(template)
			return specializationName(name, args)
		}
	}
	name, _ := l.qualifiedName(decl)
	return name
}

// pointee describes what a pointer or reference points at, or nil when the
// type is not one. It is what lets the type mapper collapse a (pointer, length)
// pair into a Go slice without re-parsing a spelling.
func (l *Libclang) pointee(surface CXType) *Param {
	var inner CXType
	switch l.f.getCanonicalType(surface).Kind {
	case typePointer, typeBlockPointer:
		inner = l.f.getPointeeType(surface)
	case typeLValueReference, typeRValueReference:
		inner = l.f.getNonReferenceType(surface)
	default:
		return nil
	}
	p := l.param(inner)
	return &p
}

// paramNames fills each parameter's declared name from the function cursor's
// argument declarations, so the generated Go signature uses the names the C++
// header used. A declaration that omitted a name yields "", and a cursor whose
// arguments libclang does not expose leaves them all empty; the generator then
// invents a name.
func (l *Libclang) paramNames(cursor CXCursor, params []Param) {
	for i := range params {
		arg := l.f.cursorGetArgument(cursor, uint32(i))
		if !l.cursorIsValid(arg) {
			continue
		}
		params[i].Name = l.str(l.f.getCursorSpelling(arg))
	}
}

// isUnsignedKind reports whether an integer type kind is unsigned.
func isUnsignedKind(kind int32) bool {
	switch kind {
	case typeBool, typeCharU, typeUChar, typeChar16, typeChar32, typeUShort, typeUInt,
		typeULong, typeULongLong, typeUInt128:
		return true
	default:
		return false
	}
}

// enumIsUnsigned reports the signedness of an enum's underlying integer type.
func (l *Libclang) enumIsUnsigned(t CXType) bool {
	underlying := l.f.getCanonicalType(l.f.getEnumDeclIntegerType(l.f.getTypeDeclaration(t)))
	return isUnsignedKind(underlying.Kind)
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
	return e.l.nonTrivialRecord(e.l.f.getTypeDeclaration(canonical), 0)
}

// nonTrivialRecord reports whether a record is non-trivial for the purposes of
// calls: it is polymorphic, or it declares a copy/move constructor or a
// destructor that is not `= default`, or a base or member is like that.
func (l *Libclang) nonTrivialRecord(decl CXCursor, depth int) bool {
	kind := l.f.getCursorKind(decl)
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
	err := l.visit(decl, func(child, parent CXCursor) int32 {
		switch l.f.getCursorKind(child) {
		case cursorDestructor:
			if l.f.methodIsDefaulted(child) == 0 {
				nonTrivial = true
			}
		case cursorConstructor:
			copyOrMove := l.f.constructorIsCopyConstructor(child) != 0 ||
				l.f.constructorIsMoveConstructor(child) != 0
			if copyOrMove && l.f.methodIsDefaulted(child) == 0 {
				nonTrivial = true
			}
		case cursorCXXMethod:
			if l.f.methodIsVirtual(child) != 0 {
				nonTrivial = true
			}
		case cursorBaseSpecifier:
			if l.f.isVirtualBase(child) != 0 {
				nonTrivial = true
				return visitContinue
			}
			if l.nonTrivialRecord(l.f.getTypeDeclaration(l.f.getCursorType(child)), depth+1) {
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
		if l.nonTrivialRecord(l.f.getTypeDeclaration(l.f.getCursorType(field)), depth+1) {
			return true
		}
	}
	return false
}

// locationValues turns a source location into file and line. A location with no
// file (a built-in, or a cursor from an unsaved buffer) yields "".
func (l *Libclang) locationValues(loc CXSourceLocation) (string, int) {
	var file uintptr
	var line, column, offset uint32
	l.f.getFileLocation(loc, &file, &line, &column, &offset)
	// clang_getFileName(NULL) is documented to return an empty string, so no
	// null check is needed here.
	return l.str(l.f.getFileName(file)), int(line)
}
