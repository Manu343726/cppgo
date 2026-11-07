package cxx

import (
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
	// SizeBytes is sizeof, or -1 for an incomplete or template type.
	SizeBytes int64
	// AlignBytes is alignof, or -1 for an incomplete or template type.
	AlignBytes int64
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
	// IsAbstract is true when the record has a pure virtual function and
	// cannot be instantiated.
	IsAbstract bool
	// Fields are the non-static data members in declaration order.
	Fields []Field
	// Bases are the base classes in declaration order.
	Bases []Base
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
	// The caller has already established that the handle is open and that the
	// first walk answered; a second failure here means the same thing, so the
	// model stays empty rather than reporting a half-read translation unit.
	if err := tu.l.visit(tu.l.f.getTranslationUnitCursor(tu.tu), ex.visit); err != nil {
		return
	}
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
		if e.l.f.isCursorDefinition(cursor) != 0 {
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

	if isTemplate {
		// The pattern's layout and field types depend on the parameter: there
		// is nothing to read out of it, and pretending otherwise would put a
		// T-sized field in a Go struct.
		return rec
	}

	typ := e.l.f.getCursorType(cursor)
	rec.SizeBytes = e.l.f.typeGetSizeOf(typ)
	rec.AlignBytes = e.l.f.typeGetAlignOf(typ)
	rec.IsPOD = e.l.f.isPODType(typ) != 0
	rec.IsAbstract = e.l.f.cxxRecordIsAbstract(cursor) != 0
	rec.IsTrivial = !e.l.nonTrivialRecord(cursor, 0)

	// A record's immediate children are its fields, its base classes and its
	// member functions. The walk does not recurse: nested records are visited
	// by the translation-unit walk, which reaches every one exactly once.
	_ = e.l.visit(cursor, func(child, parent CXCursor) int32 {
		switch e.l.f.getCursorKind(child) {
		case cursorFieldDecl:
			rec.Fields = append(rec.Fields, e.collectField(child))
		case cursorBaseSpecifier:
			rec.Bases = append(rec.Bases, e.collectBase(cursor, child))
		case cursorCXXMethod, cursorConstructor, cursorDestructor:
			if e.l.f.methodIsVirtual(child) != 0 {
				rec.IsPolymorphic = true
			}
		}
		return visitContinue
	})
	if len(rec.Bases) > 0 {
		for _, base := range rec.Bases {
			if base.IsVirtual {
				rec.IsPolymorphic = true
			}
		}
	}
	return rec
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
}
