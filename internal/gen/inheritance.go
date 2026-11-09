package gen

import (
	"fmt"
	"sort"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// baseModel is one base class of a bound record.
type baseModel struct {
	// GoName and CxxName name the base type.
	GoName  string
	CxxName string
	// Virtual is true for a virtual base.
	Virtual bool
	// Record is the base's model.
	Record *recordModel
}

// buildInheritance gives a derived record the three things the contract promises
// for a base class:
//
//   - the base's public methods, on the derived Go type, called through the
//     derived pointer so C++ applies the `this` adjustment (a non-zero base
//     offset and a virtual base both land here);
//   - As<Base>() for every public base, a view of the same object at the
//     address the C++ cast produced;
//   - As<Derived>() on a polymorphic base, the dynamic_cast that reports whether
//     the object really is that derived type.
//
// It runs after selection so only the kept records take part, and before naming
// so the inherited names are checked for collisions like any other.
func (b *builder) buildInheritance() error {
	for _, name := range b.order {
		record := b.records[name]
		record.Bases = b.basesOf(record)
	}
	// Inherited methods, innermost base first so an override in a nearer base
	// wins over the one further away.
	for _, name := range b.order {
		record := b.records[name]
		if b.rawRecord(record.CxxName).Synthesized {
			// A specialization synthesized from a pattern inherits the
			// pattern's base list, and a base member's signature read there is
			// the pattern's. Copying it can instantiate a member the C++
			// type system rejects for this instantiation (Eigen's vector-only
			// accessors on a 3x3 matrix), which the shim's compile finds and Go
			// cannot undo. The base views are casts and stay.
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s inherits %d base class(es) and its inherited members are not copied: a specialization synthesized from a pattern cannot prove that a base member is valid for this instantiation; bind the members through As<Base>()",
				record.CxxName, len(record.Bases)))
			continue
		}
		taken := map[string]bool{}
		for _, own := range record.Methods {
			taken[methodKey(own.sym)] = true
		}
		for _, base := range record.Bases {
			for _, inherited := range b.inheritedMethods(base.Record) {
				key := methodKey(inherited.sym)
				if taken[key] {
					continue
				}
				taken[key] = true
				copy := b.copyInherited(record, base, inherited)
				record.Methods = append(record.Methods, copy)
				b.bindings = append(b.bindings, copy)
			}
		}
	}
	// The views: As<Base> on the derived, As<Derived> on a polymorphic base.
	for _, name := range b.order {
		record := b.records[name]
		for _, base := range record.Bases {
			up, err := b.newBaseView(record, base.Record, false)
			if err != nil {
				b.warnings = append(b.warnings, fmt.Sprintf("%s: As%s is not generated: %v", record.CxxName, base.Record.GoName, err))
				continue
			}
			record.Methods = append(record.Methods, up)
			b.bindings = append(b.bindings, up)
		}
	}
	for _, name := range b.order {
		base := b.records[name]
		derived := b.derivedFrom(base)
		sort.Slice(derived, func(i, j int) bool { return derived[i].CxxName < derived[j].CxxName })
		for _, child := range derived {
			if !b.rawRecord(base.CxxName).IsPolymorphic {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s is not polymorphic, so %s is not generated: a dynamic_cast needs a vtable (build the library with RTTI and give the base a virtual member)",
					base.CxxName, "As"+child.GoName))
				continue
			}
			down, err := b.newBaseView(base, child, true)
			if err != nil {
				b.warnings = append(b.warnings, fmt.Sprintf("%s: As%s is not generated: %v", base.CxxName, child.GoName, err))
				continue
			}
			base.Methods = append(base.Methods, down)
			b.bindings = append(b.bindings, down)
		}
	}
	return nil
}

// basesOf resolves a record's public bases, transitively, into models.
func (b *builder) basesOf(record *recordModel) []*baseModel {
	var out []*baseModel
	seen := map[string]bool{}
	var walk func(raw cxx.Record, depth int)
	walk = func(raw cxx.Record, depth int) {
		if depth > maxHierarchyDepthForBases {
			return
		}
		for _, base := range raw.Bases {
			if base.Access != cxx.AccessPublic || seen[base.QualifiedName] {
				continue
			}
			model, ok := b.records[base.QualifiedName]
			if !ok {
				continue
			}
			seen[base.QualifiedName] = true
			out = append(out, &baseModel{
				GoName:  model.GoName,
				CxxName: model.CxxName,
				Virtual: base.IsVirtual,
				Record:  model,
			})
			walk(b.rawRecord(base.QualifiedName), depth+1)
		}
	}
	walk(b.rawRecord(record.CxxName), 0)
	return out
}

// maxHierarchyDepthForBases bounds the walk through a base chain.
const maxHierarchyDepthForBases = 16

// inheritedMethods is a record's methods together with those of its own bases,
// so a two-level hierarchy reaches the grandparent's members.
func (b *builder) inheritedMethods(record *recordModel) []*binding {
	out := append([]*binding(nil), record.Methods...)
	for _, base := range record.Bases {
		out = append(out, b.inheritedMethods(base.Record)...)
	}
	return out
}

// methodKey is the identity an override changes: the simple name, the parameter
// spellings and constness. The qualified name is deliberately absent, because an
// override in the derived record has a different one.
func methodKey(sym cxx.Symbol) string {
	spellings := make([]string, len(sym.Params))
	for i, param := range sym.Params {
		spellings[i] = param.Spelling
	}
	key := simpleName(sym.QualifiedName) + "(" + joinSpellings(spellings) + ")"
	if sym.IsConst {
		key += " const"
	}
	return key
}

// joinSpellings joins parameter spellings for a key.
func joinSpellings(spellings []string) string {
	out := ""
	for i, spelling := range spellings {
		if i > 0 {
			out += ","
		}
		out += spelling
	}
	return out
}

// copyInherited re-targets a base method at the derived record: the C++ call is
// written against the derived pointer, so the compiler applies the adjustment,
// and the symbol names are salted with the derived type so two receivers of one
// inherited method are two thunks.
func (b *builder) copyInherited(record *recordModel, base *baseModel, inherited *binding) *binding {
	copy := *inherited
	copy.Owner = record
	copy.base = base.Record
	// The derived pointer is not the base pointer when the base sits at a
	// non-zero offset, and a transparent trampoline cannot adjust it: the shim's
	// static_cast is the only place the offset exists.
	b.forceThunk(&copy, "an inherited method: the shim's cast applies the base offset")
	b.resalt(&copy, record.CxxName)
	return &copy
}

// resalt recomputes the symbol variable, trampoline and thunk names of a binding
// from a salted signature, so the same C++ declaration bound twice produces two
// distinct machine symbols.
func (b *builder) resalt(binding *binding, salt string) {
	binding.ID = shortHash(displaySignature(binding.sym) + "|" + salt)
	binding.SymbolVar = "cppgoSym_" + binding.ID
	binding.Trampoline = "cppgoCall_" + binding.ID
	if binding.Carrier.shimCarried() {
		binding.Thunk = "cppgo_thunk_" + binding.ID
		binding.Symbol = binding.Thunk
	}
}

// newBaseView builds As<Base>() (up) or As<Derived>() (down): a view of the same
// object at the address the C++ cast produces.
func (b *builder) newBaseView(from, to *recordModel, dynamic bool) (*binding, error) {
	pointee := cxx.Param{
		Kind:       cxx.TypeRecord,
		Spelling:   to.CxxName,
		ClangKind:  "Record",
		SizeBytes:  to.Size,
		RecordName: to.CxxName,
	}
	result := cxx.Param{
		Kind:      cxx.TypePointer,
		Spelling:  to.CxxName + " *",
		ClangKind: "Pointer",
		SizeBytes: 8,
		Ptr:       true,
		Pointee:   &pointee,
	}
	sym := cxx.Symbol{
		QualifiedName: from.CxxName + "::As" + to.GoName,
		Parent:        from.CxxName,
		Kind:          cxx.Method,
		Access:        cxx.AccessPublic,
		// A dynamic_cast of a pointer reports failure as null and never throws;
		// the Go signature is the (T, bool) pair, not an error.
		IsNoExcept:    true,
		ExceptionSpec: cxx.ExceptionNoexcept,
		Result:        result,
	}
	view, err := b.newBinding(sym, from)
	if err != nil {
		return nil, err
	}
	view.castTo = to
	view.dynamicCast = dynamic
	view.Carrier = thunk
	view.Reason = "a base view: the shim performs the cast, so Go never computes an offset"
	view.fieldName = "As" + to.GoName
	if dynamic {
		view.Reason = "a derived view: the shim performs the dynamic_cast and reports whether it succeeded"
	}
	view.Thunk = "cppgo_thunk_" + view.ID
	view.Symbol = view.Thunk
	return view, nil
}

// derivedFrom returns the records that name base as a public base, transitively.
func (b *builder) derivedFrom(base *recordModel) []*recordModel {
	var out []*recordModel
	for _, name := range b.order {
		record := b.records[name]
		if record == base {
			continue
		}
		for _, candidate := range record.Bases {
			if candidate.Record == base {
				out = append(out, record)
				break
			}
		}
	}
	return out
}
