package cxx

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// RecordKind is the C++ keyword a record was declared with, which decides the
// default access of its members and how the generator names the Go type.
type RecordKind uint8

const (
	// Class is a `class`: members are private by default.
	Class RecordKind = iota
	// Struct is a `struct`: members are public by default.
	Struct
	// Union is a `union`: members share storage, so no layout a Go struct can
	// mirror.
	Union
)

// String implements fmt.Stringer.
func (k RecordKind) String() string {
	switch k {
	case Class:
		return "class"
	case Struct:
		return "struct"
	case Union:
		return "union"
	default:
		return "unknown record kind"
	}
}

// Access is the C++ access control of a declaration within its parent.
type Access uint8

const (
	// AccessNone is what a namespace-scope declaration reports.
	AccessNone Access = iota
	// AccessPublic is `public`.
	AccessPublic
	// AccessProtected is `protected`.
	AccessProtected
	// AccessPrivate is `private`.
	AccessPrivate
)

// String implements fmt.Stringer.
func (a Access) String() string {
	switch a {
	case AccessPublic:
		return "public"
	case AccessProtected:
		return "protected"
	case AccessPrivate:
		return "private"
	default:
		return "none"
	}
}

// access maps libclang's CX_CXXAccessSpecifier onto the model.
func (l *Libclang) access(cursor CXCursor) Access {
	switch l.f.getCXXAccessSpecifier(cursor) {
	case accessPublic:
		return AccessPublic
	case accessProtected:
		return AccessProtected
	case accessPrivate:
		return AccessPrivate
	default:
		return AccessNone
	}
}

// Field is one data member of a record, with everything the generator needs to
// place it in a Go mirror. Those are its type, its offset, its visibility, and
// whether it is a bitfield (which no Go field can represent).
type Field struct {
	// Name is the member name as written, without a trailing underscore being
	// interpreted: "x_", "tag".
	Name string
	// Type is the classified type, the same shape a parameter has.
	Type Param
	// Access is the C++ visibility inside the record.
	Access Access
	// OffsetBytes is the distance from the start of the record, or -1 when
	// libclang cannot compute it (an incomplete type, a bitfield).
	OffsetBytes int64
	// IsBitField is true for a bitfield, whose offset and width are not a Go
	// field boundary.
	IsBitField bool
	// BitWidth is the declared width of a bitfield, 0 otherwise.
	BitWidth int
	// IsAnonymous is true for an unnamed member (an anonymous struct or union).
	IsAnonymous bool
	// Comment is the member's raw doc comment, empty when it has none.
	Comment string
}

// Base is one base class of a record, with the access it was inherited under
// and its offset in the derived layout.
type Base struct {
	// QualifiedName is the base record's qualified C++ name.
	QualifiedName string
	// Access is the inheritance access (public, protected or private).
	Access Access
	// IsVirtual is true for a virtual base, whose offset is resolved through
	// the vtable rather than being a constant of the derived layout.
	IsVirtual bool
	// OffsetBytes is the base subobject's offset, or -1 when it cannot be
	// computed.
	OffsetBytes int64
}

// Record is one class, struct or union definition. It carries the layout the ABI
// needs plus the visibility the generator needs.
type Record struct {
	// QualifiedName is the C++ name, e.g. "mathx::Vector".
	QualifiedName string
	// Kind is the keyword it was declared with.
	Kind RecordKind
	// IsTemplate is true for a template pattern. Its layout and its members'
	// symbols do not exist until an instantiation names them.
	IsTemplate bool
	// IsTemplateSpecialization is true for a concrete instantiation, e.g.
	// Box<int>. It has a layout and a name built from its arguments.
	IsTemplateSpecialization bool
	// Synthesized is true when the record was built from a pattern (or a
	// partial specialization) because libclang has no definition cursor for
	// the instantiation. A full explicit specialization has its own definition
	// and is not synthesized; it wins over a synthesized sighting.
	Synthesized bool
	// TemplateName is the qualified name of the template a specialization
	// instantiates.
	TemplateName string
	// TemplateArguments are the concrete arguments of a specialization.
	TemplateArguments []TemplateArgument
	// TemplateParameters are the parameters of a template pattern, in
	// declaration order. They are empty for a non-template record and for a
	// specialization, whose arguments are in TemplateArguments instead.
	TemplateParameters []TemplateParameter
	// SizeBytes is sizeof, or -1 when the layout is not known (a template, an
	// incomplete type, or a record with a member whose layout libclang could
	// not complete).
	SizeBytes int64
	// AlignBytes is alignof, or -1 when the layout is not known.
	AlignBytes int64
	// LayoutKnown reports whether SizeBytes, AlignBytes and every field offset
	// are the real C++ layout. It is false for a template and for a record
	// with an incomplete member, where libclang can return a plausible but
	// wrong size; the generator then takes the opaque, C++-owned path instead
	// of mirroring the fields.
	LayoutKnown bool
	// IsPOD is libclang's POD classification: standard layout and trivial.
	IsPOD bool
	// IsTrivial is the Itanium "trivial for the purposes of calls" rule: no
	// user-provided copy/move operation, no user-provided destructor, not
	// polymorphic, and every base and member trivial too. A non-trivial record
	// is returned and passed indirectly however small it is.
	IsTrivial bool
	// IsPolymorphic is true when the record has a virtual function or a
	// virtual base, and therefore a vtable pointer in its layout.
	IsPolymorphic bool
	// HasVirtualFunction is true when the record declares or inherits a
	// virtual function: calls through it dispatch through the vtable.
	HasVirtualFunction bool
	// HasVirtualBase is true when one of the bases is virtual: its offset is
	// resolved through the vtable rather than being a constant.
	HasVirtualBase bool
	// IsAbstract is true when the record has a pure virtual function and
	// cannot be instantiated.
	IsAbstract bool
	// Fields are the non-static data members in declaration order.
	Fields []Field
	// Bases are the base classes in declaration order.
	Bases []Base
	// StaticFields are the static data members, in declaration order.
	StaticFields []StaticField
	// MemberTemplates are the member function templates of a specialization,
	// as patterns. A concrete instantiation is obtained with
	// TU.InstantiateMember; the pattern alone is not bindable.
	MemberTemplates []Symbol
	// Members are the constructors, destructor and methods of a template
	// specialization, synthesized from its pattern. libclang does not expose
	// the instantiated definition's cursor children, so the pattern is the only
	// place its members can be read from. For a non-template record the members
	// are in the project's symbol list instead, and this stays empty.
	Members []Symbol
	// IsDefaultConstructible reports whether a zero-argument `New<Type>()` can
	// be generated: a non-deleted zero-argument constructor, or no
	// user-declared constructor and fields that are trivially constructible.
	// The generated shim's compile is the final check; this is what decides
	// whether the promise is made at all.
	IsDefaultConstructible bool
	// IsCopyConstructible reports whether `Clone()` can be generated: a
	// non-deleted copy constructor, or no user-declared copy/move constructor
	// and trivially copyable fields.
	IsCopyConstructible bool
	// DeclFile and DeclLine locate the definition.
	DeclFile string
	DeclLine int
	// Comment is the record's raw doc comment, empty when it has none.
	Comment string
}

// StaticField is one static data member. It is a value that belongs to the
// class. The generated Go API reads it through a package function
// `<Type><Field>()` and writes it through `<Type>Set<Field>` only when it is
// non-const.
type StaticField struct {
	// Name is the member name as written.
	Name string
	// Type is the classified type, the same shape a parameter has.
	Type Param
	// Access is the C++ visibility inside the record.
	Access Access
	// IsConst is true for a `static const` member: no setter is generated.
	IsConst bool
	// Mangled is the symbol of the definition, or empty when libclang has none
	// (a template pattern's static member is spelled per instantiation).
	Mangled string
	// Comment is the member's raw doc comment, empty when it has none.
	Comment string
}

// memberTemplate is a member function template kept for on-demand
// instantiation: the pattern cursor plus the class-level substitution that
// turns its owner's parameters into the specialization's arguments.
type memberTemplate struct {
	cursor CXCursor
	class  substitution
	record string
	member string
}

// Enumerator is one value of an enum.
type Enumerator struct {
	// Name is the enumerator as written.
	Name string
	// Value is the value read as a signed integer.
	Value int64
	// UnsignedValue is the same bits read as unsigned, which is the honest
	// reading when the underlying type is unsigned.
	UnsignedValue uint64
	// IsUnsigned reports which of the two readings is the type's own.
	IsUnsigned bool
	// Comment is the enumerator's raw doc comment, empty when it has none.
	Comment string
}

// Enum is one enum definition. It carries the underlying integer type and the
// values.
type Enum struct {
	// QualifiedName is the C++ name, e.g. "mathx::Axis".
	QualifiedName string
	// IsScoped is true for `enum class`, whose enumerators do not leak into
	// the enclosing scope.
	IsScoped bool
	// Underlying is the integer type the compiler chose or the declaration
	// fixed.
	Underlying Param
	// Constants are the enumerators in declaration order.
	Constants []Enumerator
	// ConstantsKnown reports whether the enumerator values are the
	// instantiation's. libclang does not evaluate an initializer in a template
	// pattern, so a specialization's nested enum is re-evaluated from its
	// source tokens; an initializer the model cannot read makes this false and
	// the generator exposes the enum type without constants.
	ConstantsKnown bool
	// DeclFile and DeclLine locate the definition.
	DeclFile string
	DeclLine int
	// Comment is the definition's raw doc comment, empty when it has none.
	Comment string
}

// recordExtractor walks one translation unit and turns record and enum
// definitions into the model. It is a second pass over the same cursors the
// symbol extractor sees: a function cursor and a record cursor need different
// questions asked of them, and keeping the walks apart keeps each one readable.
type recordExtractor struct {
	l       *Libclang
	tu      *TU
	records []Record
	byName  map[string]int
	enums   []Enum
	byEnum  map[string]int
	// sources caches the files whose text is read to evaluate enum
	// initializers, so one parse reads each file once.
	sources map[string][]byte
	// partials are the partial specializations by primary template name, in
	// declaration order.
	partials map[string][]partialSpecialization
}

// partialSpecialization is one `template <typename T> struct Q<T *>` pattern.
// It has its own cursor and children; an instantiation that matches it is
// synthesized from this cursor, not from the primary.
type partialSpecialization struct {
	cursor CXCursor
	args   []TemplateArgument
}

// extractRecords fills tu.records and tu.enums. It shares the lazy extraction
// with the symbol pass: TU.Records and TU.Symbols both force the same parse.
func (tu *TU) extractRecords() {
	ex := &recordExtractor{
		l:        tu.l,
		tu:       tu,
		byName:   make(map[string]int),
		byEnum:   make(map[string]int),
		partials: make(map[string][]partialSpecialization),
	}
	tu.memberTemplates = make(map[string]memberTemplate)
	// The symbol walk has already proven the handle open; a second walk of the
	// same translation unit cannot fail for a different reason, so its error is
	// the same "the handle is closed" answer the symbol walk already returned.
	_ = tu.l.visit(tu.l.f.getTranslationUnitCursor(tu.tu), ex.visit)
	tu.records = ex.records
	tu.byRecord = ex.byName
	tu.enums = ex.enums
	tu.byEnum = ex.byEnum
}

// visit is the clang_visitChildren callback for records and enums.
func (e *recordExtractor) visit(cursor, parent CXCursor) int32 {
	if !e.tu.selectable(cursor) {
		return visitContinue
	}
	switch kind := e.l.f.getCursorKind(cursor); kind {
	case cursorClassDecl, cursorStructDecl, cursorUnionDecl:
		// A specialization is a declaration, not a definition, but its layout
		// is complete and it is part of the API: "template struct Box<int>;"
		// must be generated like any other type.
		if _, specialization := e.l.specialization(cursor); e.l.f.isCursorDefinition(cursor) != 0 || specialization {
			e.addRecord(e.collectRecord(cursor, false))
		}
		// Recurse so nested records are found; the field walk itself does not
		// descend into them.
		return visitRecurse
	case cursorClassTemplate:
		e.addRecord(e.collectRecord(cursor, true))
		return visitContinue
	case cursorClassTemplatePartialSpecialization:
		e.collectPartial(cursor)
		return visitContinue
	case cursorEnumDecl:
		if e.l.f.isCursorDefinition(cursor) != 0 {
			e.addEnum(e.collectEnum(cursor))
		}
		return visitContinue
	case cursorNamespace, cursorLinkageSpec, cursorNamespaceAlias:
		return visitRecurse
	default:
		return visitContinue
	}
}

// collectRecord reads one record definition.
func (e *recordExtractor) collectRecord(cursor CXCursor, isTemplate bool) Record {
	name, ok := e.l.qualifiedName(cursor)
	if !ok {
		return Record{}
	}
	rec := Record{
		QualifiedName: name,
		Kind:          e.recordKind(cursor),
		IsTemplate:    isTemplate,
		SizeBytes:     -1,
		AlignBytes:    -1,
	}
	rec.DeclFile, rec.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(cursor))
	rec.Comment = e.l.comment(cursor)

	pattern, isSpecialization := e.l.specialization(cursor)
	if isSpecialization {
		rec.IsTemplateSpecialization = true
		if templateName, qualified := e.l.qualifiedName(pattern); qualified {
			rec.TemplateName = templateName
		}
		rec.TemplateArguments = e.l.fillArgumentSpellings(e.l.templateArguments(cursor), e.l.f.getCursorType(cursor))
		if len(rec.TemplateArguments) > 0 {
			// The cursor is spelled "Box" for both the pattern and the
			// instantiation; the arguments are the identity.
			rec.QualifiedName = specializationName(rec.TemplateName, rec.TemplateArguments)
		}
	}

	if isTemplate {
		// The pattern's layout and field types depend on the parameter: there
		// is nothing to read out of it, and pretending otherwise would put a
		// T-sized field in a Go struct. The parameter list and the fields are
		// still read, because the generic API has to reject a public field it
		// cannot expose and name the type parameters a specialization replaces.
		rec.TemplateParameters = e.l.templateParameters(cursor)
		_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
			if e.l.f.getCursorKind(child) == cursorFieldDecl {
				rec.Fields = append(rec.Fields, e.collectField(child))
			}
			return visitContinue
		})
		return rec
	}

	// An explicit instantiation is a declaration cursor: its definition (the
	// instantiated class body, with its fields) is a different cursor, and its
	// type must be canonicalised before sizeof/alignof answer.
	decl := cursor
	if definition := e.l.f.getCursorDefinition(cursor); e.l.cursorIsValid(definition) {
		decl = definition
	}
	typ := e.l.f.getCanonicalType(e.l.f.getCursorType(decl))
	if e.l.f.typeGetSizeOf(typ) < 0 {
		// An explicit instantiation's definition cursor is the pattern, whose
		// type is dependent and has no size. The instantiation's own type is
		// the one the compiler completed.
		if direct := e.l.f.getCanonicalType(e.l.f.getCursorType(cursor)); e.l.f.typeGetSizeOf(direct) >= 0 {
			typ = direct
		}
	}
	rec.SizeBytes = e.l.f.typeGetSizeOf(typ)
	rec.AlignBytes = e.l.f.typeGetAlignOf(typ)
	rec.IsPOD = e.l.f.isPODType(typ) != 0
	rec.IsAbstract = e.l.f.cxxRecordIsAbstract(decl) != 0
	rec.IsTrivial = !e.l.nonTrivialRecord(decl, 0)
	rec.IsPolymorphic = e.l.polymorphic(decl, 0)

	facts := ctorFacts{}
	switch {
	case isSpecialization && e.isFullExplicitSpecialization(cursor):
		// A full explicit specialization has its own body: read it, not the
		// pattern. Its members and nested types are its own.
		e.collectChildren(&rec, decl, typ, &facts)
		if rec.TemplateName != "" && len(rec.TemplateArguments) > 0 {
			rec.QualifiedName = specializationName(rec.TemplateName, rec.TemplateArguments)
		}
	case isSpecialization:
		// An implicit or explicit instantiation with no definition cursor:
		// select a partial specialization if one matches, else the primary.
		rec.Synthesized = true
		patternCursor, sub := e.selectPattern(rec.TemplateName, rec.TemplateArguments, pattern)
		e.synthesizeSpecialization(&rec, patternCursor, typ, &facts, sub)
	default:
		e.collectChildren(&rec, decl, typ, &facts)
	}

	e.finishRecord(&rec, facts)
	return rec
}

// finishRecord applies the rules that are the same whether a record came from
// its own definition cursor or was synthesized from a pattern.
func (e *recordExtractor) finishRecord(rec *Record, facts ctorFacts) {
	for _, base := range rec.Bases {
		if base.IsVirtual {
			rec.HasVirtualBase = true
			rec.IsPolymorphic = true
		}
	}
	rec.IsDefaultConstructible, rec.IsCopyConstructible = e.constructible(facts, rec.Fields)
	rec.LayoutKnown = rec.SizeBytes > 0 && rec.AlignBytes > 0
	for _, field := range rec.Fields {
		if field.Type.SizeBytes < 0 || field.OffsetBytes < 0 && !field.IsBitField {
			// A member libclang could not complete makes the whole layout
			// untrustworthy: it reports a small size rather than an error.
			rec.LayoutKnown = false
		}
	}
	if !rec.LayoutKnown {
		rec.SizeBytes = -1
		rec.AlignBytes = -1
	}
}

// addTypeSpecialization records a specialization that is reachable through a
// field type, even when libclang emits no cursor for the instantiation itself
// (it does not for a template with a non-type or template-template parameter).
func (e *recordExtractor) addTypeSpecialization(field CXCursor) {
	typ := e.l.f.getTypeDeclaration(e.l.f.getCursorType(field))
	if !e.tu.selectable(typ) {
		// The instantiation of a standard-library type reached through a field:
		// not the API the caller named.
		return
	}
	pattern, ok := e.l.specialization(typ)
	if !ok {
		return
	}
	if name, _ := e.l.qualifiedName(pattern); name == "" {
		return
	}
	rec, ok := e.recordFromSpecialization(typ, pattern)
	if !ok {
		return
	}
	if _, exists := e.byName[rec.QualifiedName]; exists {
		return
	}
	e.addRecord(rec)
}

// isFullExplicitSpecialization distinguishes `template <> struct Q<int> {...}`
// from `template struct Q<int>;`: libclang reports both as a StructDecl with a
// definition flag in some contexts, but only the first has a body of its own.
// The source text of the cursor's extent is the discriminator; when it cannot
// be read (a macro or an unsaved buffer), a declaration with member children
// is treated as a body, because an instantiation cursor exposes none.
func (e *recordExtractor) isFullExplicitSpecialization(cursor CXCursor) bool {
	if e.l.f.isCursorDefinition(cursor) == 0 {
		return false
	}
	text, ok := e.sourceText(cursor)
	if ok {
		head := text
		if len(head) > 64 {
			head = head[:64]
		}
		head = strings.Join(strings.Fields(head), " ")
		if strings.HasPrefix(head, "template <>") || strings.HasPrefix(head, "template<>") {
			return true
		}
		if strings.HasPrefix(head, "template struct") || strings.HasPrefix(head, "template class") {
			return false
		}
	}
	return e.cursorDeclaresMembers(cursor)
}

// cursorDeclaresMembers reports whether a record cursor exposes any field or
// function child.
func (e *recordExtractor) cursorDeclaresMembers(cursor CXCursor) bool {
	found := false
	_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorFieldDecl, cursorCXXMethod, cursorConversionFunction,
			cursorConstructor, cursorDestructor, cursorEnumDecl, cursorVarDecl:
			found = true
		}
		return visitContinue
	})
	return found
}

// collectPartial keeps a partial specialization for matching. It is a pattern,
// not a record a Go type is generated from: only an instantiation is.
func (e *recordExtractor) collectPartial(cursor CXCursor) {
	primary := e.l.f.getSpecializedCursorTemplate(cursor)
	if !e.l.cursorIsValid(primary) {
		return
	}
	primaryName, ok := e.l.qualifiedName(primary)
	if !ok {
		return
	}
	args := e.l.fillArgumentSpellings(e.l.templateArguments(cursor), e.l.f.getCursorType(cursor))
	if len(args) == 0 {
		return
	}
	e.partials[primaryName] = append(e.partials[primaryName], partialSpecialization{cursor: cursor, args: args})
}

// selectPattern chooses where an instantiation's body comes from: the best
// matching partial specialization when there is one, else the primary pattern.
// A full explicit specialization is not selected here: it is a real record with
// its own definition and wins through addRecord's precedence.
func (e *recordExtractor) selectPattern(primary string, args []TemplateArgument, pattern CXCursor) (CXCursor, substitution) {
	if cursor, sub, ok := e.matchPartial(primary, args); ok {
		return cursor, sub
	}
	return pattern, e.l.substitution(pattern, args)
}

// matchPartial finds the most specific partial specialization that accepts an
// instantiation. The rule is deterministic: the argument counts must match, a
// component that is not a bare parameter counts one point, and the highest
// score wins; ties go to the first declared.
func (e *recordExtractor) matchPartial(primary string, args []TemplateArgument) (CXCursor, substitution, bool) {
	bestScore := -1
	var bestCursor CXCursor
	var bestBound []TemplateArgument
	for _, partial := range e.partials[primary] {
		if len(partial.args) != len(args) {
			continue
		}
		bound, score, ok := matchPartialArgs(partial.args, args)
		if !ok || score <= bestScore {
			continue
		}
		bestScore, bestCursor, bestBound = score, partial.cursor, bound
	}
	if bestScore < 0 {
		return CXCursor{}, substitution{}, false
	}
	return bestCursor, e.l.substitution(bestCursor, bestBound), true
}

// matchPartialArgs matches one partial's argument list against a concrete
// instantiation, returning the binding of the partial's parameters.
func matchPartialArgs(patterns, concrete []TemplateArgument) ([]TemplateArgument, int, bool) {
	bound := make([]TemplateArgument, len(patterns))
	score := 0
	for i := range patterns {
		pattern, actual := patterns[i], concrete[i]
		switch {
		case isBareParameter(pattern.Type):
			bound[i] = actual
		case pattern.Type.Kind == TypePointer && pattern.Type.Pointee != nil && isBareParameter(*pattern.Type.Pointee):
			if actual.Type.Kind != TypePointer || actual.Type.Pointee == nil {
				return nil, 0, false
			}
			bound[i] = TemplateArgument{
				Kind:     TemplateArgumentType,
				Spelling: actual.Type.Pointee.Spelling,
				Type:     *actual.Type.Pointee,
			}
			score++
		default:
			if baseTypeName(pattern.Type.Spelling) != baseTypeName(actual.Type.Spelling) {
				return nil, 0, false
			}
			score++
		}
	}
	return bound, score, true
}

// isBareParameter reports whether a type is a template parameter used whole,
// as in Q<T>, rather than inside another type, as in Q<T *>.
func isBareParameter(p Param) bool {
	if p.Kind != TypeOther && p.ClangKind != "Unexposed" {
		return false
	}
	name := baseTypeName(p.Spelling)
	return name != "" && !strings.ContainsAny(name, " *&<>[],")
}

// recordFromSpecialization builds the record for a specialization declaration
// reached through a type.
func (e *recordExtractor) recordFromSpecialization(decl, pattern CXCursor) (Record, bool) {
	templateName, ok := e.l.qualifiedName(pattern)
	if !ok {
		return Record{}, false
	}
	args := e.l.fillArgumentSpellings(e.l.templateArguments(decl), e.l.f.getCursorType(decl))
	if len(args) == 0 {
		return Record{}, false
	}
	rec := Record{
		QualifiedName:            specializationName(templateName, args),
		Kind:                     e.recordKind(decl),
		IsTemplateSpecialization: true,
		TemplateName:             templateName,
		TemplateArguments:        args,
		SizeBytes:                -1,
		AlignBytes:               -1,
	}
	rec.DeclFile, rec.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(decl))
	typ := e.l.f.getCanonicalType(e.l.f.getCursorType(decl))
	rec.SizeBytes = e.l.f.typeGetSizeOf(typ)
	rec.AlignBytes = e.l.f.typeGetAlignOf(typ)
	rec.IsPOD = e.l.f.isPODType(typ) != 0
	rec.IsAbstract = e.l.f.cxxRecordIsAbstract(decl) != 0
	rec.IsTrivial = !e.l.nonTrivialRecord(decl, 0)
	rec.IsPolymorphic = e.l.polymorphic(decl, 0)
	rec.Synthesized = true
	facts := ctorFacts{}
	patternCursor, sub := e.selectPattern(templateName, args, pattern)
	e.synthesizeSpecialization(&rec, patternCursor, typ, &facts, sub)
	e.finishRecord(&rec, facts)
	return rec, true
}

// collectChildren reads a non-template record's fields, bases and
// constructors from its own definition cursor.
func (e *recordExtractor) collectChildren(rec *Record, decl CXCursor, typ CXType, facts *ctorFacts) {
	_ = e.l.visit(decl, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorFieldDecl:
			rec.Fields = append(rec.Fields, e.collectField(child))
			e.addTypeSpecialization(child)
		case cursorBaseSpecifier:
			rec.Bases = append(rec.Bases, e.collectBase(decl, child))
		case cursorVarDecl:
			field := e.collectStaticField(child)
			if field.Name != "" {
				rec.StaticFields = append(rec.StaticFields, field)
			}
		case cursorCXXMethod, cursorConversionFunction, cursorConstructor, cursorDestructor:
			if e.l.f.methodIsVirtual(child) != 0 {
				rec.HasVirtualFunction = true
				rec.IsPolymorphic = true
			}
			if e.l.f.getCursorKind(child) == cursorConstructor {
				facts.add(e.l, child)
			}
		case cursorFunctionTemplate:
			e.registerMemberTemplate(rec.QualifiedName, child, substitution{})
			rec.MemberTemplates = append(rec.MemberTemplates, e.synthesizeMember(child, rec, substitution{}))
		}
		return visitContinue
	})
}

// synthesizeSpecialization reads a specialization through its pattern, because
// libclang does not expose the instantiated definition's children. The pattern's
// fields and members are copied with the template arguments substituted, and the
// specialization type is asked for whatever layout it can provide.
func (e *recordExtractor) synthesizeSpecialization(rec *Record, pattern CXCursor, specType CXType, facts *ctorFacts, sub substitution) {
	_ = e.l.visit(pattern, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorFieldDecl:
			field := e.collectField(child)
			field.Type = sub.param(field.Type)
			field.OffsetBytes = e.l.typeFieldOffset(specType, field.Name)
			rec.Fields = append(rec.Fields, field)
		case cursorVarDecl:
			field := e.collectStaticField(child)
			field.Type = sub.param(field.Type)
			field.Mangled = ""
			if field.Name != "" {
				rec.StaticFields = append(rec.StaticFields, field)
			}
		case cursorClassDecl, cursorStructDecl, cursorUnionDecl:
			e.synthesizeNestedRecord(rec, child, sub)
		case cursorEnumDecl:
			e.synthesizeNestedEnum(rec, child, sub)
		case cursorBaseSpecifier:
			base := e.collectBase(pattern, child)
			base.QualifiedName = e.specializedBaseName(base.QualifiedName, child, sub)
			base.OffsetBytes = -1
			rec.Bases = append(rec.Bases, base)
		case cursorCXXMethod, cursorConversionFunction, cursorConstructor, cursorDestructor:
			kind := e.l.kind(child)
			if kind == Method || kind == Destructor {
				if e.l.f.methodIsVirtual(child) != 0 {
					rec.HasVirtualFunction = true
					rec.IsPolymorphic = true
				}
			}
			if kind == Constructor {
				facts.add(e.l, child)
			}
			rec.Members = append(rec.Members, e.synthesizeMember(child, rec, sub))
		case cursorFunctionTemplate:
			e.registerMemberTemplate(rec.QualifiedName, child, sub)
			member := e.synthesizeMember(child, rec, sub)
			member.IsTemplate = true
			rec.MemberTemplates = append(rec.MemberTemplates, member)
		}
		return visitContinue
	})
}

// specializedBaseName names a base of a specialization. The pattern's base is
// spelled "Box<T>" relative to its namespace; qualifying it after substitution
// is what gives the generator the concrete base type.
func (e *recordExtractor) specializedBaseName(qualified string, base CXCursor, sub substitution) string {
	name := sub.text(e.l.str(e.l.f.getTypeSpelling(e.l.f.getCursorType(base))))
	name = strings.TrimPrefix(name, "class ")
	name = strings.TrimPrefix(name, "struct ")
	if !strings.Contains(name, "::") {
		if i := strings.LastIndex(qualified, "::"); i >= 0 {
			name = qualified[:i+2] + name
		}
	}
	return name
}

// synthesizeMember turns a pattern's member into the specialization's member:
// the same shape, with the template arguments substituted and no mangled name
// (a template instantiation is carried by a thunk, never by a direct symbol).
func (e *recordExtractor) synthesizeMember(cursor CXCursor, rec *Record, sub substitution) Symbol {
	kind := e.l.kind(cursor)
	name := e.l.str(e.l.f.getCursorSpelling(cursor))
	if (kind == Constructor || kind == Destructor) && rec.TemplateName != "" {
		// The pattern's spelling carries the parameters ("Pair2<T>"); the
		// specialization's constructor is named after the template itself.
		simple := rec.TemplateName
		if i := strings.LastIndex(simple, "::"); i >= 0 {
			simple = simple[i+2:]
		}
		if kind == Constructor {
			name = simple
		} else {
			name = "~" + simple
		}
	}
	member := Symbol{
		QualifiedName: rec.QualifiedName + "::" + name,
		Kind:          kind,
		Parent:        rec.QualifiedName,
		Access:        e.l.access(cursor),
		IsPureVirtual: e.l.isPureVirtual(cursor),
		IsDeleted:     e.l.isDeleted(cursor, kind),
		IsDefaulted:   e.l.isDefaulted(cursor, kind),
		IsVariadic:    e.l.f.cursorIsVariadic(cursor) != 0,
		ExceptionSpec: e.l.exceptionSpec(cursor),
	}
	member.IsConst, member.IsVirtual, member.IsStatic = e.l.qualifiers(cursor, kind)
	member.IsNoExcept = !member.ExceptionSpec.CanThrow()
	member.DeclFile, member.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(cursor))
	member.Comment = e.l.comment(cursor)

	funcType := e.l.f.getCursorType(cursor)
	if kind == Constructor || kind == Destructor {
		member.Result = Param{Kind: TypeVoid, ClangKind: "Void", Spelling: "void", SizeBytes: 0}
	} else {
		member.Result = sub.param(e.l.param(e.l.f.getResultType(funcType)))
	}
	if n := e.l.f.getNumArgTypes(funcType); n > 0 {
		member.Params = make([]Param, 0, n)
		for i := uint32(0); i < uint32(n); i++ {
			member.Params = append(member.Params, sub.param(e.l.param(e.l.f.getArgType(funcType, i))))
		}
		e.l.paramNames(cursor, member.Params)
		e.l.paramDefaults(cursor, member.Params)
	}
	if kind == Constructor {
		member.IsCopyConstructor = e.l.f.constructorIsCopyConstructor(cursor) != 0
		member.IsMoveConstructor = e.l.f.constructorIsMoveConstructor(cursor) != 0
	}
	member.IsOperator, member.Operator, member.IsConversion = operatorInfo(e.l.str(e.l.f.getCursorSpelling(cursor)), member.Result)
	if member.IsConversion && member.Operator != "" {
		// libclang spells a dependent conversion "operator type-parameter-0-0";
		// the instantiation's name is the substituted target type.
		member.QualifiedName = rec.QualifiedName + "::operator " + member.Operator
	}
	return member
}

// collectStaticField reads one static data member.
func (e *recordExtractor) collectStaticField(cursor CXCursor) StaticField {
	typ := e.l.f.getCursorType(cursor)
	return StaticField{
		Name:    e.l.str(e.l.f.getCursorSpelling(cursor)),
		Type:    e.l.param(typ),
		Access:  e.l.access(cursor),
		IsConst: e.l.f.isConstQualifiedType(typ) != 0,
		Mangled: e.l.str(e.l.f.cursorMangling(cursor)),
		Comment: e.l.comment(cursor),
	}
}

// registerMemberTemplate keeps a member function template for on-demand
// instantiation. Only a concrete instantiation has a signature the generator
// can bind; the pattern alone is an input the manifest completes.
func (e *recordExtractor) registerMemberTemplate(record string, cursor CXCursor, class substitution) {
	if e.tu == nil {
		return
	}
	member := e.l.str(e.l.f.getCursorSpelling(cursor))
	if member == "" {
		return
	}
	key := record + "::" + member
	if _, exists := e.tu.memberTemplates[key]; exists {
		return
	}
	e.tu.memberTemplates[key] = memberTemplate{cursor: cursor, class: class, record: record, member: member}
}

// synthesizeNestedRecord builds the record for a nested type of a template
// specialization, with the outer class arguments substituted.
func (e *recordExtractor) synthesizeNestedRecord(outer *Record, cursor CXCursor, sub substitution) {
	if e.l.f.isCursorDefinition(cursor) == 0 {
		return
	}
	full, ok := e.l.qualifiedName(cursor)
	if !ok {
		return
	}
	simple := full
	if i := strings.LastIndex(simple, "::"); i >= 0 {
		simple = simple[i+2:]
	}
	nested := Record{
		QualifiedName: outer.QualifiedName + "::" + simple,
		Kind:          e.recordKind(cursor),
		SizeBytes:     -1,
		AlignBytes:    -1,
	}
	nested.DeclFile, nested.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(cursor))
	if _, exists := e.byName[nested.QualifiedName]; exists {
		return
	}
	facts := ctorFacts{}
	e.synthesizeSpecialization(&nested, cursor, e.l.f.getCursorType(cursor), &facts, sub)
	e.finishRecord(&nested, facts)
	e.addRecord(nested)
}

// synthesizeNestedEnum turns a nested enum of a pattern into the
// specialization's enum: same enumerators, name and underlying type taken from
// the instantiation.
func (e *recordExtractor) synthesizeNestedEnum(outer *Record, cursor CXCursor, sub substitution) {
	if e.l.f.isCursorDefinition(cursor) == 0 {
		return
	}
	enum := e.collectEnum(cursor)
	if enum.QualifiedName == "" {
		return
	}
	simple := enum.QualifiedName
	if i := strings.LastIndex(simple, "::"); i >= 0 {
		simple = simple[i+2:]
	}
	enum.QualifiedName = outer.QualifiedName + "::" + simple
	enum.Underlying = sub.param(enum.Underlying)
	e.evaluateNestedEnum(&enum, cursor, sub)
	if _, exists := e.byEnum[enum.QualifiedName]; exists {
		return
	}
	e.addEnum(enum)
}

// evaluateNestedEnum re-reads a nested enum's values from its source
// tokens, because libclang evaluates an initializer in a template pattern
// against the pattern rather than the instantiation (B = 3 reads as 0).
func (e *recordExtractor) evaluateNestedEnum(enum *Enum, cursor CXCursor, sub substitution) {
	if e.l == nil {
		enum.ConstantsKnown = false
		return
	}
	var constants []CXCursor
	_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
		if e.l.f.getCursorKind(child) == cursorEnumConstantDecl {
			constants = append(constants, child)
		}
		return visitContinue
	})
	if len(constants) != len(enum.Constants) {
		enum.ConstantsKnown = false
		return
	}
	previous := int64(-1)
	for i, constant := range constants {
		value, ok := e.enumeratorValue(constant, sub, previous)
		if !ok {
			enum.ConstantsKnown = false
			continue
		}
		enum.Constants[i].Value = value
		enum.Constants[i].UnsignedValue = uint64(value)
		enum.Constants[i].IsUnsigned = enum.Underlying.Unsigned
		previous = value
	}
}

// enumeratorValue reads one enumerator's initializer from its source text. An
// enumerator with no initializer follows the previous one; an initializer may
// be an integer literal or a non-type template parameter. libclang evaluates
// an initializer in a template pattern against the pattern (B = 3 reads as 0),
// so the source is the honest reading.
func (e *recordExtractor) enumeratorValue(cursor CXCursor, sub substitution, previous int64) (int64, bool) {
	text, ok := e.sourceText(cursor)
	if !ok {
		return 0, false
	}
	equals := strings.Index(text, "=")
	if equals < 0 {
		return previous + 1, true
	}
	expression := strings.TrimSpace(text[equals+1:])
	negative := strings.HasPrefix(expression, "-")
	expression = strings.TrimSpace(strings.TrimPrefix(expression, "-"))
	if arg, ok := sub.names[expression]; ok && arg.Kind == TemplateArgumentIntegral {
		value := arg.Value
		if negative {
			value = -value
		}
		return value, true
	}
	literal := strings.TrimRight(expression, "uUlL")
	if value, err := strconv.ParseInt(literal, 0, 64); err == nil {
		if negative {
			value = -value
		}
		return value, true
	}
	if value, err := strconv.ParseUint(literal, 0, 64); err == nil {
		return int64(value), true
	}
	return 0, false
}

// sourceText returns the source a cursor covers, read from the file it is in.
func (e *recordExtractor) sourceText(cursor CXCursor) (string, bool) {
	if e.tu == nil || e.tu.l == nil {
		// A record synthesised without a translation unit has no source to read;
		// the caller treats that as values it cannot see, not as a failure.
		return "", false
	}
	span := e.l.f.getCursorExtent(cursor)
	path, start, ok := e.offsetOf(e.l.f.getRangeStart(span))
	if !ok {
		return "", false
	}
	if endPath, end, ok := e.offsetOf(e.l.f.getRangeEnd(span)); !ok || endPath != path || end < start {
		return "", false
	} else {
		content, ok := e.sources[path]
		if !ok {
			data, err := os.ReadFile(path)
			if err != nil || uint64(len(data)) < uint64(end) {
				return "", false
			}
			if e.sources == nil {
				e.sources = make(map[string][]byte)
			}
			e.sources[path] = data
			content = data
		}
		return string(content[start:end]), true
	}
}

// offsetOf turns a source location into its file and byte offset.
func (e *recordExtractor) offsetOf(loc CXSourceLocation) (string, uint32, bool) {
	var file uintptr
	var line, column, offset uint32
	e.l.f.getFileLocation(loc, &file, &line, &column, &offset)
	path := e.l.str(e.l.f.getFileName(file))
	if path == "" {
		return "", 0, false
	}
	return path, offset, true
}

// InstantiateMember returns the concrete instantiation of a member function
// template. It carries the pattern's signature with both the class and the
// member arguments substituted. libclang exposes no mangled name for the
// instantiation (it has no cursor in the TU). Mangled therefore stays empty,
// and the emitter carries the call through a thunk.
func (tu *TU) InstantiateMember(record, member string, args []TemplateArgument) (Symbol, bool) {
	tu.once.Do(tu.extract)
	pattern, ok := tu.memberTemplates[record+"::"+member]
	if !ok {
		return Symbol{}, false
	}
	if countTemplateParameters(tu.l, pattern.cursor) != len(args) {
		return Symbol{}, false
	}
	own := tu.l.substitution(pattern.cursor, args)
	combined := pattern.class.then(own)
	extractor := &recordExtractor{l: tu.l, tu: tu}
	sym := extractor.synthesizeMember(pattern.cursor, &Record{QualifiedName: record}, combined)
	sym.IsTemplate = false
	sym.QualifiedName = record + "::" + member
	return sym, true
}

// countTemplateParameters counts the template parameters declared by a
// function-template cursor.
func countTemplateParameters(l *Libclang, cursor CXCursor) int {
	count := 0
	_ = l.visit(cursor, func(child, parent CXCursor) int32 {
		switch l.f.getCursorKind(child) {
		case cursorTemplateTypeParameter, cursorNonTypeTemplateParameter, cursorTemplateTemplateParameter:
			count++
		}
		return visitContinue
	})
	return count
}

// typeFieldOffset asks a record type for one field's byte offset. It is the
// only way to an offset when the specialization's definition cursor is not
// reachable, and it answers -1 when libclang has not completed the type.
func (l *Libclang) typeFieldOffset(typ CXType, name string) int64 {
	if name == "" {
		return -1
	}
	buf := append([]byte(name), 0)
	bits := l.f.typeGetOffsetOf(typ, &buf[0])
	runtime.KeepAlive(buf)
	if bits < 0 {
		return -1
	}
	return bits / 8
}

// ctorFacts is what the model needs to know about a record's constructors to
// promise New<Type>() and Clone().
type ctorFacts struct {
	userDeclared bool
	zeroArg      bool
	zeroDeleted  bool
	copyCtor     bool
	copyDeleted  bool
	moveCtor     bool
	moveDeleted  bool
}

// add records one constructor cursor.
func (f *ctorFacts) add(l *Libclang, cursor CXCursor) {
	f.userDeclared = true
	deleted := l.f.methodIsDeleted(cursor) != 0
	switch {
	case l.f.constructorIsCopyConstructor(cursor) != 0:
		f.copyCtor = true
		f.copyDeleted = deleted
	case l.f.constructorIsMoveConstructor(cursor) != 0:
		f.moveCtor = true
		f.moveDeleted = deleted
	case l.f.getNumArgTypes(l.f.getCursorType(cursor)) == 0:
		f.zeroArg = true
		f.zeroDeleted = deleted
	}
}

// constructible turns the facts and the fields into the two promises the
// generated API makes. It is conservative: a field whose type the model cannot
// classify is not assumed constructible, and the generated shim's compile is
// the final check for the cases it cannot see.
func (e *recordExtractor) constructible(f ctorFacts, fields []Field) (defaultConstructible, copyConstructible bool) {
	if f.userDeclared {
		defaultConstructible = f.zeroArg && !f.zeroDeleted
	} else {
		defaultConstructible = e.trivialFields(fields)
	}
	switch {
	case f.copyCtor:
		copyConstructible = !f.copyDeleted
	case f.moveCtor:
		// A user-declared move constructor deletes the implicit copy one.
		copyConstructible = false
	default:
		copyConstructible = e.trivialFields(fields)
	}
	return defaultConstructible, copyConstructible
}

// trivialFields reports whether every field is a type the model can vouch for
// without asking a constructor: a scalar, pointer or enum, an array of those,
// or a record it has already classified as trivially copyable and (for default
// construction) either POD or default-constructible.
func (e *recordExtractor) trivialFields(fields []Field) bool {
	for _, field := range fields {
		if !e.trivialType(field.Type, 0) {
			return false
		}
	}
	return true
}

// trivialType is the recursive answer for one field type.
func (e *recordExtractor) trivialType(p Param, depth int) bool {
	if depth > maxTrivialityDepth {
		return false
	}
	switch p.Kind {
	case TypeInteger, TypeFloat, TypePointer, TypeEnum:
		return true
	case TypeArray:
		return p.Element != nil && e.trivialType(*p.Element, depth+1)
	case TypeRecord:
		if p.RecordName == "" {
			return false
		}
		i, ok := e.byName[p.RecordName]
		if !ok {
			return false
		}
		record := e.records[i]
		// The layout is not needed to know that a constructor exists: a
		// trivially copyable record that is POD or default-constructible
		// qualifies even when libclang deferred its layout. The generated
		// shim's compile is the final check.
		return record.IsTrivial && (record.IsPOD || record.IsDefaultConstructible)
	default:
		return false
	}
}

// substitution maps template parameter names to the concrete arguments of a
// specialization, so a pattern's member can be read as the instantiation's.
type substitution struct {
	names map[string]TemplateArgument
	// indexed is the class arguments in declaration order. libclang spells a
	// dependent type used inside another type as "type-parameter-0-N" rather
	// than by the parameter's name, so the index is the only handle.
	indexed []TemplateArgument
}

// substitution builds the map from the pattern's template parameters, in
// declaration order, to the specialization's arguments.
func (l *Libclang) substitution(pattern CXCursor, args []TemplateArgument) substitution {
	sub := substitution{names: make(map[string]TemplateArgument, len(args)), indexed: make([]TemplateArgument, 0, len(args))}
	index := 0
	_ = l.visit(pattern, func(child, parent CXCursor) int32 {
		switch l.f.getCursorKind(child) {
		case cursorTemplateTypeParameter, cursorNonTypeTemplateParameter, cursorTemplateTemplateParameter:
			if index < len(args) {
				if name := l.str(l.f.getCursorSpelling(child)); name != "" {
					sub.names[name] = args[index]
				}
				sub.indexed = append(sub.indexed, args[index])
			}
			index++
		}
		return visitContinue
	})
	return sub
}

// then composes two substitutions: the class arguments first, then the
// member's own (which win on a name collision).
func (s substitution) then(next substitution) substitution {
	combined := substitution{
		names:   make(map[string]TemplateArgument, len(s.names)+len(next.names)),
		indexed: append([]TemplateArgument(nil), s.indexed...),
	}
	for name, arg := range s.names {
		combined.names[name] = arg
	}
	for name, arg := range next.names {
		combined.names[name] = arg
	}
	return combined
}

// param substitutes the parameters of the pattern in one type.
func (s substitution) param(p Param) Param {
	original := p.Spelling
	if arg, ok := s.argumentFor(p.Spelling); ok && arg.Kind == TemplateArgumentType {
		replaced := arg.Type
		replaced.IsConst = replaced.IsConst || p.IsConst
		replaced.Spelling = s.text(p.Spelling)
		return replaced
	}
	p.Spelling = s.text(p.Spelling)
	if p.RecordName != "" {
		p.RecordName = s.text(p.RecordName)
	}
	if p.Pointee != nil {
		pointee := s.param(*p.Pointee)
		p.Pointee = &pointee
	}
	if p.Kind == TypeArray && p.Element != nil {
		element := s.param(*p.Element)
		p.Element = &element
		if p.ArraySize < 0 {
			p.ArraySize = s.arraySize(original)
		}
	}
	return p
}

// argumentFor finds the argument a dependent type refers to: by name ("T") or
// by libclang's canonical spelling of a template parameter ("type-parameter-0-1").
func (s substitution) argumentFor(spelling string) (TemplateArgument, bool) {
	if arg, ok := s.names[baseTypeName(spelling)]; ok {
		return arg, true
	}
	if depth, index, ok := templateParameterIndex(spelling); ok && depth == 0 && index < len(s.indexed) {
		return s.indexed[index], true
	}
	return TemplateArgument{}, false
}

// templateParameterIndex parses libclang's "type-parameter-<depth>-<index>"
// spelling.
func templateParameterIndex(spelling string) (depth, index int, ok bool) {
	const prefix = "type-parameter-"
	if !strings.HasPrefix(spelling, prefix) {
		return 0, 0, false
	}
	parts := strings.Split(spelling[len(prefix):], "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	depth, errDepth := strconv.Atoi(parts[0])
	index, errIndex := strconv.Atoi(parts[1])
	if errDepth != nil || errIndex != nil {
		return 0, 0, false
	}
	return depth, index, true
}

// arraySize resolves a dependent array bound from the spelling: the first
// template parameter name in it that has an integral argument.
func (s substitution) arraySize(spelling string) int64 {
	for i := 0; i < len(spelling); i++ {
		if !isIdentStart(spelling[i]) {
			continue
		}
		j := i + 1
		for j < len(spelling) && isIdentPart(spelling[j]) {
			j++
		}
		if arg, ok := s.names[spelling[i:j]]; ok && arg.Kind == TemplateArgumentIntegral {
			return arg.Value
		}
		i = j
	}
	return -1
}

// text substitutes parameter names in a spelling, token by token, so "T" in
// "std::vector<T>" becomes the argument while the "T" of an unrelated
// identifier does not.
func (s substitution) text(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if isIdentStart(value[i]) {
			j := i + 1
			for j < len(value) && isIdentPart(value[j]) {
				j++
			}
			token := value[i:j]
			if arg, ok := s.names[token]; ok && arg.String() != "" {
				out.WriteString(arg.String())
			} else {
				out.WriteString(token)
			}
			i = j
			continue
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// baseTypeName strips the qualifiers a type parameter can carry, so
// "const T" and "T" both answer "T".
func baseTypeName(spelling string) string {
	s := strings.TrimSpace(spelling)
	for {
		switch {
		case strings.HasPrefix(s, "const "):
			s = strings.TrimSpace(s[len("const "):])
		case strings.HasPrefix(s, "volatile "):
			s = strings.TrimSpace(s[len("volatile "):])
		case strings.HasPrefix(s, "struct "):
			s = strings.TrimSpace(s[len("struct "):])
		case strings.HasPrefix(s, "class "):
			s = strings.TrimSpace(s[len("class "):])
		default:
			return s
		}
	}
}

// recordKind maps the cursor kind to the declared keyword. A class template is
// classified through clang_getTemplateCursorKind, because ClassTemplate does
// not say whether it was declared `class` or `struct`.
func (e *recordExtractor) recordKind(cursor CXCursor) RecordKind {
	kind := e.l.f.getCursorKind(cursor)
	if isTemplateParent(kind) {
		kind = e.l.f.getTemplateCursorKind(cursor)
	}
	switch kind {
	case cursorUnionDecl:
		return Union
	case cursorStructDecl:
		return Struct
	default:
		return Class
	}
}

// collectField reads one data member.
func (e *recordExtractor) collectField(cursor CXCursor) Field {
	field := Field{
		Name:        e.l.str(e.l.f.getCursorSpelling(cursor)),
		Type:        e.l.param(e.l.f.getCursorType(cursor)),
		Access:      e.l.access(cursor),
		IsBitField:  e.l.f.cursorIsBitField(cursor) != 0,
		IsAnonymous: e.l.f.cursorIsAnonymous(cursor) != 0,
		OffsetBytes: -1,
		Comment:     e.l.comment(cursor),
	}
	if bits := e.l.f.cursorOffsetOfField(cursor); bits >= 0 {
		field.OffsetBytes = bits / 8
	}
	if field.IsBitField {
		field.BitWidth = int(e.l.f.fieldDeclBitWidth(cursor))
	}
	return field
}

// collectBase reads one base class specifier.
func (e *recordExtractor) collectBase(parent, cursor CXCursor) Base {
	base := Base{
		Access:      e.l.access(cursor),
		IsVirtual:   e.l.f.isVirtualBase(cursor) != 0,
		OffsetBytes: -1,
	}
	if name, ok := e.l.qualifiedName(e.l.f.getTypeDeclaration(e.l.f.getCursorType(cursor))); ok {
		base.QualifiedName = name
	} else if spelling := e.l.str(e.l.f.getTypeSpelling(e.l.f.getCursorType(cursor))); !strings.HasPrefix(spelling, "(") {
		// A base named through a typedef, or one from an anonymous namespace,
		// still has a deterministic spelling. A synthesized "(unnamed at …)"
		// does not, and is left empty.
		base.QualifiedName = spelling
	}
	if bits := e.l.f.getOffsetOfBase(parent, cursor); bits >= 0 {
		base.OffsetBytes = bits / 8
	}
	return base
}

// collectEnum reads one enum definition and its values.
func (e *recordExtractor) collectEnum(cursor CXCursor) Enum {
	name, ok := e.l.qualifiedName(cursor)
	if !ok {
		return Enum{}
	}
	enum := Enum{
		QualifiedName:  name,
		IsScoped:       e.l.f.enumDeclIsScoped(cursor) != 0,
		Underlying:     e.l.param(e.l.f.getEnumDeclIntegerType(cursor)),
		ConstantsKnown: true,
		Comment:        e.l.comment(cursor),
	}
	enum.DeclFile, enum.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(cursor))
	_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
		if e.l.f.getCursorKind(child) == cursorEnumConstantDecl {
			enum.Constants = append(enum.Constants, Enumerator{
				Name:          e.l.str(e.l.f.getCursorSpelling(child)),
				Value:         e.l.f.getEnumConstantValue(child),
				UnsignedValue: e.l.f.getEnumConstantUnsignedValue(child),
				IsUnsigned:    enum.Underlying.Unsigned,
				Comment:       e.l.comment(child),
			})
		}
		return visitContinue
	})
	return enum
}

// addRecord records a record definition. An unnamed record is dropped: there
// is no name for the generator to bind it under.
func (e *recordExtractor) addRecord(rec Record) {
	if rec.QualifiedName == "" {
		return
	}
	if i, ok := e.byName[rec.QualifiedName]; ok {
		existing := e.records[i]
		if existing.Synthesized && !rec.Synthesized {
			// A full explicit specialization's own body replaces a record that
			// was synthesized before its definition cursor was reached.
			e.records[i] = rec
			return
		}
		mergeRecord(&e.records[i], rec)
		return
	}
	e.byName[rec.QualifiedName] = len(e.records)
	e.records = append(e.records, rec)
}

// addEnum records an enum definition. An unnamed enum is dropped for the same
// reason an unnamed record is.
func (e *recordExtractor) addEnum(enum Enum) {
	if enum.QualifiedName == "" {
		return
	}
	e.byEnum[enum.QualifiedName] = len(e.enums)
	e.enums = append(e.enums, enum)
}

// mergeRecord folds a second sighting into the first. A layout-bearing sighting
// wins over a template pattern, and the first definition wins over another
// definition of the same name.
func mergeRecord(into *Record, from Record) {
	if into.SizeBytes < 0 && from.SizeBytes >= 0 {
		into.SizeBytes = from.SizeBytes
		into.AlignBytes = from.AlignBytes
		into.LayoutKnown = from.LayoutKnown
		into.IsPOD = from.IsPOD
		into.IsTrivial = from.IsTrivial
		into.IsPolymorphic = from.IsPolymorphic
		into.IsAbstract = from.IsAbstract
		into.IsTemplate = from.IsTemplate
		if len(into.Fields) == 0 {
			into.Fields = from.Fields
		}
		if len(into.Bases) == 0 {
			into.Bases = from.Bases
		}
	}
	if into.DeclFile == "" {
		into.DeclFile, into.DeclLine = from.DeclFile, from.DeclLine
	}
	if into.Comment == "" {
		into.Comment = from.Comment
	}
	into.IsTemplateSpecialization = into.IsTemplateSpecialization || from.IsTemplateSpecialization
	into.HasVirtualFunction = into.HasVirtualFunction || from.HasVirtualFunction
	into.HasVirtualBase = into.HasVirtualBase || from.HasVirtualBase
	into.IsPolymorphic = into.IsPolymorphic || from.IsPolymorphic
	if into.TemplateName == "" {
		into.TemplateName = from.TemplateName
	}
	if len(into.TemplateArguments) == 0 {
		into.TemplateArguments = from.TemplateArguments
	}
	if len(into.TemplateParameters) == 0 {
		into.TemplateParameters = from.TemplateParameters
	}
}
