package gen

import (
	"fmt"

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

// manifestError is the invocation's mistake rather than a construct the build
// cannot carry: a -status entry naming a declaration whose return is not a
// status. A declaration the build cannot carry is a warning; a flag that asks
// for something the declaration cannot express is the user's to fix, so it
// stays fatal.
type manifestError struct{ err error }

// Error implements error.
func (e manifestError) Error() string { return e.err.Error() }

// unbound records a declaration the model cannot carry. The record or the
// overload set keeps every declaration it can, and the one it cannot is a
// warning naming the declaration and the reason — never a refusal of the
// declaration's owner. That is the difference between one uncarryable
// constructor and an unbindable class.
//
// A manifestError is not a limitation of the declaration but a mistake in the
// invocation, so it is not a warning: the first one is kept and build stops
// with it once every declaration has been visited.
func (b *builder) unbound(sym cxx.Symbol, err error) {
	if _, ok := err.(manifestError); ok {
		if b.manifestErr == nil {
			b.manifestErr = symbolError(sym, err)
		}
		return
	}
	b.warnings = append(b.warnings, fmt.Sprintf("%s is not bound: %v", displaySignature(sym), err))
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
