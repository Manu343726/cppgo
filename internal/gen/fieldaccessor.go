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
func (b *builder) buildFieldAccessors() error {
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
				return recordError(b.rawRecord(owner.CxxName), fmt.Errorf("the public field %s: %w", field.Name, err))
			}
			owner.Methods = append(owner.Methods, getter)
			b.bindings = append(b.bindings, getter)
			if field.Type.IsConst {
				continue
			}
			setter, err := b.newFieldBinding(owner, field, true)
			if err != nil {
				return recordError(b.rawRecord(owner.CxxName), fmt.Errorf("the public field %s: %w", field.Name, err))
			}
			owner.Methods = append(owner.Methods, setter)
			b.bindings = append(b.bindings, setter)
		}
	}
	return nil
}

// newFieldBinding builds one half of a public field accessor.
func (b *builder) newFieldBinding(owner *recordModel, field cxx.Field, write bool) (*binding, error) {
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
	built.Carrier = thunk
	built.Reason = "a field accessor: the thunk reads or writes the member"
	built.Status = built.hasStd || byValue
	built.ErrorResult = built.Status
	if built.Status && built.Result != nil && built.Out == nil && !built.StdResult && built.ViewResult {
		// The view address travels through an out-pointer, so a failing thunk
		// still delivers nothing.
		built.Out = built.Result
		built.OutLocal = outLocalType(built.Result)
		built.Wire = append(built.Wire, &wireArg{
			label:    "out",
			slotType: "unsafe.Pointer",
			class:    classInt,
			size:     8,
			expr:     "unsafe.Pointer(&cppgoResult)",
			keep:     "&cppgoResult",
			cType:    outParamType(built.Result),
			cName:    "cppgo_result",
		})
	}
	built.Thunk = "cppgo_thunk_" + built.ID
	built.Symbol = built.Thunk
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
