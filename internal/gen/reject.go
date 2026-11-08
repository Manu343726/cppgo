package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// pending is the inner half of the one-line error the contract specifies for a
// construct this build does not emit yet: what is wrong, then what the user
// supplies to make it representable today.
func pending(what, fix string) error {
	return fmt.Errorf("%s; %s", what, fix)
}

// declError renders the contract's one-line error:
//
//	cppgo: <file>:<line>: <symbol>: <what is wrong>; <fix>
func declError(file string, line int, symbol string, err error) error {
	where := file
	switch {
	case where != "" && line > 0:
		where = fmt.Sprintf("%s:%d", where, line)
	case where == "" && line > 0:
		where = fmt.Sprintf("line %d", line)
	case where == "":
		where = "<unknown>"
	}
	return fmt.Errorf("cppgo: %s: %s: %w", where, symbol, err)
}

// recordError is declError for a record.
func recordError(rec cxx.Record, err error) error {
	return declError(rec.DeclFile, rec.DeclLine, rec.QualifiedName, err)
}

// symbolError is declError for a function, method, constructor or destructor.
func symbolError(sym cxx.Symbol, err error) error {
	return declError(sym.DeclFile, sym.DeclLine, displaySignature(sym), err)
}

// nestedRecords reports which record definitions are types nested inside
// another record: their qualified name's parent is itself a record.
func nestedRecords(records []cxx.Record) map[string]bool {
	names := make(map[string]bool, len(records))
	for _, rec := range records {
		names[rec.QualifiedName] = true
	}
	nested := map[string]bool{}
	for _, rec := range records {
		if i := strings.LastIndex(rec.QualifiedName, "::"); i > 0 && names[rec.QualifiedName[:i]] {
			nested[rec.QualifiedName] = true
		}
	}
	return nested
}

// rejectUnimplemented is the single list of constructs this build does not
// emit yet. It runs before anything is classified, so a construct that would
// otherwise be omitted silently, or emitted as the wrong type because
// libclang's canonical form of it is wrong, is a generation error naming the
// declaration and the fix.
//
// Adding a construct means implementing it and removing its entry here; a
// construct must never fall through to a guess.
func (b *builder) rejectUnimplemented() error {
	nested := nestedRecords(b.rawRecords)
	for _, rec := range b.rawRecords {
		switch {
		case rec.IsTemplate:
			// A class template is the generic API's input, not a record: the
			// two-phase model in template.go decides what it can emit, and it
			// names the construct it cannot.
		case nested[rec.QualifiedName]:
			return recordError(rec, pending(
				"a type nested inside a record is not emitted by this build yet",
				"move the nested type to namespace scope, or bind the outer type through a C++ wrapper"))
		case len(rec.Bases) > 0:
			return recordError(rec, pending(
				"inherited members and base views are not emitted by this build yet",
				"bind the base class directly, or expose the derived members through a C++ wrapper"))
		case len(rec.StaticFields) > 0:
			return recordError(rec, pending(
				fmt.Sprintf("the static data member %s is not emitted by this build yet", rec.StaticFields[0].Name),
				"expose it through a static member function or a free function"))
		case len(rec.MemberTemplates) > 0:
			return recordError(rec, pending(
				fmt.Sprintf("the member function template %s is not emitted by this build yet", rec.MemberTemplates[0].QualifiedName),
				"expose the concrete instantiation as a non-template member"))
		}
	}

	for _, sym := range b.symbols {
		switch {
		case sym.IsTemplate:
			// A template is the generic API's input. A member of a class
			// template pattern is read from the pattern's own symbols, and a
			// member function template of a concrete record is named by
			// Record.MemberTemplates above.
			continue
		case sym.IsOperator || sym.IsConversion:
			return symbolError(sym, pending(
				"operators and conversion operators are not emitted by this build yet",
				"give it a named member function or a named free function"))
		case sym.IsVariadic:
			return symbolError(sym, pending(
				"a variadic function has no fixed argument list to lay out",
				"replace the ellipsis with a fixed parameter list or a span"))
		}
	}
	return nil
}

// instantiationNamed reports whether an -instantiate entry names exactly this
// specialization. It marks the entry as matched when it does.
func (b *builder) instantiationNamed(name string) bool {
	for i, entry := range b.cfg.Instantiate {
		if entry == name {
			b.instantiateMatched[i] = true
			return true
		}
	}
	return false
}
