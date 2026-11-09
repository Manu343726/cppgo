package gen

import (
	"fmt"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// buildFieldAccessors gives every public field of an opaque record it has
// selected a getter and, when the field is not const, a setter: Field() and
// SetField(v), carried by thunks that read and write the member as C++.
//
// A mirrored record needs none: its Go struct *is* the C layout, so its fields
// are fields. An opaque one has no layout Go may copy, so every public member
// would otherwise be unreachable, which is what the contract's "every construct
// stays reachable" rules out.
//
// A field whose type is a record the Go side cannot hold by value is reached as
// a view of the field inside the object: the thunk returns the member's
// address, and the Go value shares the object exactly like a borrowed result
// does.
func (b *builder) buildFieldAccessors() {
	for _, name := range b.order {
		owner := b.records[name]
		if owner.Mirrored {
			continue
		}
		for _, field := range b.rawRecord(owner.CxxName).Fields {
			if field.Access != cxx.AccessPublic || field.IsAnonymous {
				continue
			}
			getter, err := b.newFieldBinding(owner, field, false)
			if err != nil {
				// One member of a record the caller asked for as a whole: the
				// record stays usable and the omission is reported, because a
				// pointer nobody owns has no Go value to be.
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s::%s is not bound: %v", owner.CxxName, field.Name, err))
				continue
			}
			owner.Methods = append(owner.Methods, getter)
			b.bindings = append(b.bindings, getter)
			if field.Type.IsConst {
				continue
			}
			if !b.fieldAssignable(field) {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s::%s has no setter: its type's copy assignment is not public",
					owner.CxxName, field.Name))
				continue
			}
			setter, err := b.newFieldBinding(owner, field, true)
			if err != nil {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s::%s has no setter: %v", owner.CxxName, field.Name, err))
				continue
			}
			owner.Methods = append(owner.Methods, setter)
			b.bindings = append(b.bindings, setter)
		}
	}
}

// fieldAssignable reports whether a setter can copy into the member: a record's
// copy assignment may be private or protected, which a field setter cannot call.
// A scalar or a pointer is always assignable.
func (b *builder) fieldAssignable(field cxx.Field) bool {
	if field.Type.Kind != cxx.TypeRecord {
		return true
	}
	return b.copyAssignmentPublic(field.Type.RecordName)
}

// copyAssignmentPublic reports whether a record's copy assignment is callable by
// someone outside it: an implicit one is public, and a declared one has an
// access specifier the model carries on its symbol. A record can carry both a
// public implicit one (libclang reports it) and a protected declared one, and
// the declared one is what a field setter would call, so any non-public
// declaration makes the setter impossible.
func (b *builder) copyAssignmentPublic(record string) bool {
	public := true
	for _, sym := range b.symbols {
		if sym.QualifiedName != record+"::operator=" || !sym.IsOperator || sym.Operator != "=" {
			continue
		}
		if sym.Access != cxx.AccessPublic {
			return false
		}
		public = true
	}
	return public
}

// noBorrowedRecordPointer refuses a field that is a raw pointer to a record: the
// address belongs to C++ and nothing on the Go side owns it, so the value cannot
// be a Go pointer to a record — a view needs a receiver to share the object with,
// and a field has none.
func noBorrowedRecordPointer(typ cxx.Param) error {
	if typ.Kind != cxx.TypePointer || typ.Pointee == nil || typ.Pointee.Kind != cxx.TypeRecord {
		return nil
	}
	return pending(
		fmt.Sprintf("%q is a pointer to a record, and nothing on the Go side owns it", typ.Spelling),
		"expose the pointed-to object through a method, or take the address only inside C++")
}

// newFieldBinding builds one half of a public field accessor.
func (b *builder) newFieldBinding(owner *recordModel, field cxx.Field, write bool) (*binding, error) {
	if err := noBorrowedRecordPointer(field.Type); err != nil {
		return nil, err
	}
	// The member's type as a parameter: a record the Go side cannot hold by
	// value is reached through its address, which is also the view the getter
	// hands back.
	typ := field.Type
	byValue := false
	if typ.Kind == cxx.TypeRecord {
		if record, ok := b.records[typ.RecordName]; !ok || !record.Mirrored {
			byValue = true
			pointee := typ
			typ = cxx.Param{
				Kind:      cxx.TypePointer,
				Spelling:  pointee.Spelling + " *",
				ClangKind: "Pointer",
				SizeBytes: 8,
				Ptr:       true,
				Pointee:   &pointee,
			}
		}
	}
	sym := cxx.Symbol{
		QualifiedName: owner.CxxName + "::" + field.Name,
		Parent:        owner.CxxName,
		Kind:          cxx.Method,
		Access:        cxx.AccessPublic,
		IsConst:       !write,
		IsNoExcept:    true,
		ExceptionSpec: cxx.ExceptionNoexcept,
		Comment:       field.Comment,
	}
	if write {
		value := typ
		value.Name = field.Name
		_ = value
		sym.Params = []cxx.Param{typ}
		sym.Params[0].Name = field.Name
		sym.Result = cxx.Param{Kind: cxx.TypeVoid, ClangKind: "Void", Spelling: "void"}
	} else {
		sym.Result = typ
	}
	built, err := b.newBinding(sym, owner)
	if err != nil {
		return nil, err
	}
	built.memberField = &field
	built.fieldWrite = write
	built.fieldByValue = byValue
	// A field accessor reads or writes the member's storage; it is never a
	// symbol the library exports.
	b.forceThunk(built, "a field accessor: the thunk reads or writes the member")
	name := exportName(field.Name)
	if write {
		name = "Set" + name
	}
	built.fieldName = name
	return built, nil
}

// castTarget is the C++ expression a base view returns: a static_cast for
// As<Base>, which the compiler resolves to the adjusted subobject (or through the
// vtable for a virtual base), and a dynamic_cast for As<Derived>, which is the
// only cast that can report that the object is not the type asked for.
func (m *packageModel) castTarget(binding *binding) (string, error) {
	from := "static_cast<::" + binding.Owner.CxxName + " *>(self)"
	to := "::" + binding.castTo.CxxName
	if binding.dynamicCast {
		return "dynamic_cast<" + to + " *>(" + from + ")", nil
	}
	return "static_cast<" + to + " *>(" + from + ")", nil
}

// fieldTarget is the C++ expression a field accessor reads or writes, and the
// value expression a setter assigns.
func (m *packageModel) fieldTarget(binding *binding, args []string) (string, error) {
	field := binding.memberField
	qualifier := ""
	if binding.sym.IsConst {
		// A getter is a const member: the receiver arrives as const void *.
		qualifier = "const "
	}
	member := "static_cast<" + qualifier + "::" + binding.Owner.CxxName + " *>(self)->" + field.Name
	if !binding.fieldWrite {
		return member, nil
	}
	if len(args) != 1 {
		return "", pending("a field setter takes exactly one value", "fix the declaration")
	}
	value := args[0]
	if binding.fieldByValue {
		// The Go value is a pointer to the record the member holds by value.
		value = "*" + value
	}
	return member + " = " + value, nil
}
