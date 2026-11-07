package cxx

import (
	"runtime"
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
// place it in a Go mirror: its type, its offset, its visibility, and whether it
// is a bitfield (which no Go field can represent).
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

// Record is one class, struct or union definition: the layout the ABI needs
// plus the visibility the generator needs.
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
	// TemplateName is the qualified name of the template a specialization
	// instantiates.
	TemplateName string
	// TemplateArguments are the concrete arguments of a specialization.
	TemplateArguments []TemplateArgument
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
	// Members are the constructors, destructor and methods of a template
	// specialisation, synthesised from its pattern. libclang does not expose
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
}

// Enum is one enum definition: its underlying integer type and its values.
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
	// DeclFile and DeclLine locate the definition.
	DeclFile string
	DeclLine int
}

// recordExtractor walks one translation unit and turns record and enum
// definitions into the model. It is a second pass over the same cursors the
// symbol extractor sees: a function cursor and a record cursor need different
// questions asked of them, and keeping the walks apart keeps each one readable.
type recordExtractor struct {
	l       *Libclang
	records []Record
	byName  map[string]int
	enums   []Enum
	byEnum  map[string]int
}

// extractRecords fills tu.records and tu.enums. It shares the lazy extraction
// with the symbol pass: TU.Records and TU.Symbols both force the same parse.
func (tu *TU) extractRecords() {
	ex := &recordExtractor{
		l:      tu.l,
		byName: make(map[string]int),
		byEnum: make(map[string]int),
	}
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
	if e.l.f.locationIsInSystemHeader(e.l.f.getCursorLocation(cursor)) != 0 {
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
	case cursorClassTemplate, cursorClassTemplatePartialSpecialization:
		e.addRecord(e.collectRecord(cursor, true))
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
		// T-sized field in a Go struct.
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
	rec.SizeBytes = e.l.f.typeGetSizeOf(typ)
	rec.AlignBytes = e.l.f.typeGetAlignOf(typ)
	rec.IsPOD = e.l.f.isPODType(typ) != 0
	rec.IsAbstract = e.l.f.cxxRecordIsAbstract(decl) != 0
	rec.IsTrivial = !e.l.nonTrivialRecord(decl, 0)
	rec.IsPolymorphic = e.l.polymorphic(decl, 0)

	facts := ctorFacts{}
	if isSpecialization {
		e.synthesizeSpecialization(&rec, pattern, typ, &facts)
	} else {
		e.collectChildren(&rec, decl, typ, &facts)
	}

	e.finishRecord(&rec, facts)
	return rec
}

// finishRecord applies the rules that are the same whether a record came from
// its own definition cursor or was synthesised from a pattern.
func (e *recordExtractor) finishRecord(rec *Record, facts ctorFacts) {
	for _, base := range rec.Bases {
		if base.IsVirtual {
			rec.HasVirtualBase = true
			rec.IsPolymorphic = true
		}
	}
	rec.IsDefaultConstructible, rec.IsCopyConstructible = facts.constructible(rec.Fields)
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

// addTypeSpecialization records a specialisation that is reachable through a
// field type, even when libclang emits no cursor for the instantiation itself
// (it does not for a template with a non-type or template-template parameter).
func (e *recordExtractor) addTypeSpecialization(field CXCursor) {
	typ := e.l.f.getTypeDeclaration(e.l.f.getCursorType(field))
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

// recordFromSpecialization builds the record for a specialisation declaration
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
	facts := ctorFacts{}
	e.synthesizeSpecialization(&rec, pattern, typ, &facts)
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
		case cursorCXXMethod, cursorConstructor, cursorDestructor:
			if e.l.f.methodIsVirtual(child) != 0 {
				rec.HasVirtualFunction = true
				rec.IsPolymorphic = true
			}
			if e.l.f.getCursorKind(child) == cursorConstructor {
				facts.add(e.l, child)
			}
		}
		return visitContinue
	})
}

// synthesizeSpecialization reads a specialisation through its pattern, because
// libclang does not expose the instantiated definition's children. The pattern's
// fields and members are copied with the template arguments substituted, and the
// specialisation type is asked for whatever layout it can provide.
func (e *recordExtractor) synthesizeSpecialization(rec *Record, pattern CXCursor, specType CXType, facts *ctorFacts) {
	sub := e.l.substitution(pattern, rec.TemplateArguments)
	_ = e.l.visit(pattern, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorFieldDecl:
			field := e.collectField(child)
			field.Type = sub.param(field.Type)
			field.OffsetBytes = e.l.typeFieldOffset(specType, field.Name)
			rec.Fields = append(rec.Fields, field)
		case cursorBaseSpecifier:
			base := e.collectBase(pattern, child)
			base.QualifiedName = e.specializedBaseName(base.QualifiedName, child, sub)
			base.OffsetBytes = -1
			rec.Bases = append(rec.Bases, base)
		case cursorCXXMethod, cursorConstructor, cursorDestructor:
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
		}
		return visitContinue
	})
}

// specializedBaseName names a base of a specialisation. The pattern's base is
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

// synthesizeMember turns a pattern's member into the specialisation's member:
// the same shape, with the template arguments substituted and no mangled name
// (a template instantiation is carried by a thunk, never by a direct symbol).
func (e *recordExtractor) synthesizeMember(cursor CXCursor, rec *Record, sub substitution) Symbol {
	kind := e.l.kind(cursor)
	name := e.l.str(e.l.f.getCursorSpelling(cursor))
	if (kind == Constructor || kind == Destructor) && rec.TemplateName != "" {
		// The pattern's spelling carries the parameters ("Pair2<T>"); the
		// specialisation's constructor is named after the template itself.
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
	}
	return member
}

// typeFieldOffset asks a record type for one field's byte offset. It is the
// only way to an offset when the specialisation's definition cursor is not
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
func (f ctorFacts) constructible(fields []Field) (defaultConstructible, copyConstructible bool) {
	if f.userDeclared {
		defaultConstructible = f.zeroArg && !f.zeroDeleted
	} else {
		defaultConstructible = trivialFields(fields)
	}
	switch {
	case f.copyCtor:
		copyConstructible = !f.copyDeleted
	case f.moveCtor:
		// A user-declared move constructor deletes the implicit copy one.
		copyConstructible = false
	default:
		copyConstructible = trivialFields(fields)
	}
	return defaultConstructible, copyConstructible
}

// trivialFields reports whether every field is a scalar, pointer or enum: the
// types the model can vouch for without asking a constructor.
func trivialFields(fields []Field) bool {
	for _, field := range fields {
		switch field.Type.Kind {
		case TypeInteger, TypeFloat, TypePointer, TypeEnum:
		default:
			return false
		}
	}
	return true
}

// substitution maps template parameter names to the concrete arguments of a
// specialisation, so a pattern's member can be read as the instantiation's.
type substitution struct {
	names map[string]TemplateArgument
}

// substitution builds the map from the pattern's template parameters, in
// declaration order, to the specialisation's arguments.
func (l *Libclang) substitution(pattern CXCursor, args []TemplateArgument) substitution {
	sub := substitution{names: make(map[string]TemplateArgument, len(args))}
	index := 0
	_ = l.visit(pattern, func(child, parent CXCursor) int32 {
		switch l.f.getCursorKind(child) {
		case cursorTemplateTypeParameter, cursorNonTypeTemplateParameter, cursorTemplateTemplateParameter:
			if index < len(args) {
				if name := l.str(l.f.getCursorSpelling(child)); name != "" {
					sub.names[name] = args[index]
				}
			}
			index++
		}
		return visitContinue
	})
	return sub
}

// param substitutes the parameters of the pattern in one type.
func (s substitution) param(p Param) Param {
	if arg, ok := s.names[baseTypeName(p.Spelling)]; ok && arg.Kind == TemplateArgumentType {
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
	return p
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
		// still has a deterministic spelling. A synthesised "(unnamed at …)"
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
		QualifiedName: name,
		IsScoped:      e.l.f.enumDeclIsScoped(cursor) != 0,
		Underlying:    e.l.param(e.l.f.getEnumDeclIntegerType(cursor)),
	}
	enum.DeclFile, enum.DeclLine = e.l.locationValues(e.l.f.getCursorLocation(cursor))
	_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
		if e.l.f.getCursorKind(child) == cursorEnumConstantDecl {
			enum.Constants = append(enum.Constants, Enumerator{
				Name:          e.l.str(e.l.f.getCursorSpelling(child)),
				Value:         e.l.f.getEnumConstantValue(child),
				UnsignedValue: e.l.f.getEnumConstantUnsignedValue(child),
				IsUnsigned:    enum.Underlying.Unsigned,
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
}
