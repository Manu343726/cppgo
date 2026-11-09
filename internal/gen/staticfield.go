package gen

import (
	"fmt"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// buildStaticFields binds every public static data member of a record: a getter
// always, and a setter when the member is not const. The thunk reads or writes
// the member's storage as C++, because a static member's exported symbol is data
// and a trampoline that called it would jump into the data.
//
// The manifest narrows this like it narrows a function: an entry that names the
// member or the record selects it.
func (b *builder) buildStaticFields() error {
	for _, name := range b.order {
		owner := b.records[name]
		raw := b.rawRecord(owner.CxxName)
		for i := range raw.StaticFields {
			field := raw.StaticFields[i]
			if field.Access != cxx.AccessPublic {
				continue
			}
			if !b.staticFieldSelected(owner, field) {
				continue
			}
			getter, err := b.newStaticFieldBinding(owner, field, false)
			if err != nil {
				return fmt.Errorf("cppgo: the static data member %s%s: %w", owner.CxxName, "::"+field.Name, err)
			}
			owner.Statics = append(owner.Statics, getter)
			b.bindings = append(b.bindings, getter)
			if field.IsConst {
				continue
			}
			setter, err := b.newStaticFieldBinding(owner, field, true)
			if err != nil {
				return fmt.Errorf("cppgo: the static data member %s%s: %w", owner.CxxName, "::"+field.Name, err)
			}
			owner.Statics = append(owner.Statics, setter)
			b.bindings = append(b.bindings, setter)
		}
	}
	return nil
}

// staticFieldSelected reports whether the manifest asks for a static member.
func (b *builder) staticFieldSelected(owner *recordModel, field cxx.StaticField) bool {
	qualified := owner.CxxName + "::" + field.Name
	keep := len(b.cfg.Bind) == 0
	for i, entry := range b.cfg.Bind {
		if matchesName(entry, qualified) || matchesName(entry, owner.CxxName) {
			b.bindMatched[i] = true
			keep = true
		}
	}
	if !keep {
		return false
	}
	for i, entry := range b.cfg.Exclude {
		if matchesName(entry, qualified) {
			b.excludeMatched[i] = true
			return false
		}
	}
	return true
}

// newStaticFieldBinding builds one half of a static data member: the read when
// write is false, the assignment when it is true.
func (b *builder) newStaticFieldBinding(owner *recordModel, field cxx.StaticField, write bool) (*binding, error) {
	sym := cxx.Symbol{
		QualifiedName: owner.CxxName + "::" + field.Name,
		Parent:        owner.CxxName,
		Kind:          cxx.StaticMethod,
		Access:        cxx.AccessPublic,
		IsStatic:      true,
		IsNoExcept:    true,
		ExceptionSpec: cxx.ExceptionNoexcept,
		Mangled:       field.Mangled,
		Comment:       field.Comment,
	}
	if write {
		value := field.Type
		value.Name = field.Name
		sym.Params = []cxx.Param{value}
		sym.Result = cxx.Param{Kind: cxx.TypeVoid, ClangKind: "Void", Spelling: "void"}
	} else {
		sym.Result = field.Type
	}
	built, err := b.newBinding(sym, owner)
	if err != nil {
		return nil, err
	}
	built.staticField = &field
	built.staticWrite = write
	// A static member is data: there is no symbol to call, so the thunk is the
	// only carrier that can read or write it.
	built.Carrier = thunk
	built.Reason = "a static data member: the thunk reads or writes its storage"
	built.Status = built.hasStd
	built.ErrorResult = built.Status
	if built.Status && built.Result != nil && built.Out == nil && !built.StdResult {
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
	built.fieldName = owner.GoName + name
	return built, nil
}
