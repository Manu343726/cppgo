package gen

import (
	"fmt"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// expandDefaults turns a declaration with trailing default arguments into one
// binding per prefix of its parameter list, from the shortest prefix that is
// allowed up to the full signature.
//
// The default is applied by the caller's compiler, so a prefix cannot be a
// transparent trampoline: the call is written in C++ with the omitted
// parameters left out, and the shim's compiler inserts the default. The full
// signature keeps the bare Go name and every shorter prefix takes the overload
// suffix, because the shortest prefix is not the declaration a reader called.
func (b *builder) expandDefaults() {
	var expanded []*binding
	for _, binding := range b.bindings {
		expanded = append(expanded, b.defaultPrefixes(binding)...)
	}
	b.bindings = expanded
}

// defaultPrefixes is the binding itself when it declares no default arguments,
// and one binding per allowed prefix otherwise.
func (b *builder) defaultPrefixes(variant *binding) []*binding {
	sym := variant.sym
	first := len(sym.Params)
	for i := len(sym.Params) - 1; i >= 0; i-- {
		if !sym.Params[i].HasDefault {
			break
		}
		first = i
	}
	if first == len(sym.Params) {
		// No default, or no trailing run of them: one binding, unchanged.
		return []*binding{variant}
	}
	out := make([]*binding, 0, len(sym.Params)-first+1)
	for arity := first; arity <= len(sym.Params); arity++ {
		if arity == len(sym.Params) {
			out = append(out, variant)
			continue
		}
		if arity == 0 {
			// The bare name belongs to the full signature, and a zero-argument
			// prefix has no parameter type to take a suffix from. The contract's
			// rule is to drop it and say so.
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s: the zero-argument spelling is dropped, because it would take the name of the full signature; call it with every argument",
				displaySignature(sym)))
			continue
		}
		prefix := sym
		prefix.Params = append([]cxx.Param(nil), sym.Params[:arity]...)
		// A prefix is a subset of a signature the caller already lowered, so
		// this cannot fail; a prefix that does is dropped and named rather than
		// taking the process down.
		built, err := b.newBinding(prefix, variant.Owner)
		if err != nil {
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s with %d argument(s) is not bound: %v", displaySignature(sym), arity, err))
			continue
		}
		// The default lives in C++, so the call is written there: a prefix can
		// never be a transparent trampoline.
		b.forceThunk(built, "default arguments: the caller's compiler applies them, so the call is written in C++")
		if err := b.checkRegisters(built); err != nil {
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s with %d argument(s) is not bound: %v", displaySignature(sym), arity, err))
			continue
		}
		built.defaultPrefix = true
		salt := "free"
		if variant.Owner != nil {
			salt = variant.Owner.CxxName
		}
		b.resalt(built, salt+"#"+itoa(arity))
		out = append(out, built)
	}
	return out
}
