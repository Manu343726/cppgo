package gen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// This file is the direct C++ ABI mode: it resolves every binding to a
// cxx.Symbol, validates that the symbol can be called from a generated
// trampoline, and reports how it was resolved. When libclang and the header are
// available a binding that cannot work is a build error, not a runtime crash.

// abiShape is how a binding's value comes back from the C++ call.
type abiShape uint8

const (
	// abiVoid calls a void function and returns nothing.
	abiVoid abiShape = iota
	// abiObject is a constructor: the Go side gets the address of the object
	// it allocated.
	abiObject
	// abiValue returns the C++ value as the Go value.
	abiValue
	// abiValueError returns the C++ value and a nil error, because a direct
	// call has no status channel.
	abiValueError
	// abiErrorOnly calls a void function and always returns a nil error.
	abiErrorOnly
	// abiStatus reads an integer status: zero means success.
	abiStatus
)

// abiBinding is the resolved C++ side of one binding.
type abiBinding struct {
	sym cxx.Symbol
	// shape is how the value comes back.
	shape abiShape
	// statusWidth is the width, in bytes, of the integer status read when
	// shape is abiStatus.
	statusWidth int64
	// notes are the assumptions this binding was emitted under. They are
	// printed by the report and repeated in the generated file.
	notes []string
	// forced lists the validation problems -force downgraded to warnings.
	forced []string
}

// abiPackage is the resolved C++ side of an ABI-mode package.
type abiPackage struct {
	// source names the tier that answered, e.g. "compile_commands.json".
	source string
	// assumed is true when the compilation flags are the generator's fallback
	// rather than something the project declared.
	assumed bool
	// detail explains where the flags came from, for the report.
	detail string
}

// problem is one reason a binding cannot be emitted. A fatal problem cannot be
// downgraded by -force: the code could not be generated at all.
type problem struct {
	msg   string
	fatal bool
}

// abiResolver resolves a package's bindings against its header.
type abiResolver struct {
	pkg     *packageInfo
	project *cxx.Project
}

// resolveABI opens the package's header, resolves and validates every binding,
// and rewrites each Func's Symbol to its mangled name. It closes libclang
// before returning: everything the emitters need is copied out.
func resolveABI(pkg *packageInfo, cfg Config) error {
	project, err := cxx.Open(cxx.Options{Header: pkg.HeaderPath, CompileCommands: cfg.CompileCommands})
	if err != nil {
		return fmt.Errorf("cppgo: %s: %w", pkg.Header, err)
	}
	defer func() { _ = project.Close() }()

	pkg.abi = &abiPackage{
		source:  project.Flags.Source.String(),
		assumed: project.Flags.Assumed(),
		detail:  project.Flags.Detail,
	}
	r := &abiResolver{pkg: pkg, project: project}
	if err := r.probeSizes(); err != nil {
		return err
	}

	var failures []string
	for _, f := range pkg.Funcs {
		binding, err := r.resolve(f, cfg.Force)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		f.abi = binding
		f.Symbol = binding.sym.Mangled
	}
	if len(failures) > 0 {
		return fmt.Errorf("%s", strings.Join(failures, "\n"))
	}
	return checkUniqueSymbols(pkg)
}

// checkUniqueSymbols rejects two bindings that resolve to the same mangled
// name: the second would overwrite the first's trampoline.
func checkUniqueSymbols(pkg *packageInfo) error {
	seen := map[string]*Func{}
	for _, f := range pkg.Funcs {
		if prev, ok := seen[f.Symbol]; ok {
			return posErr(f.Pos, "%s and %s both bind %s", f.Name, prev.Name, f.Symbol)
		}
		seen[f.Symbol] = f
	}
	return nil
}

// report renders the per-binding report lines described by DESIGN.md: which
// declaration each binding resolved to, its mangled name, and what was assumed.
// The lines are ordered by the C++ name, so an overload set reads together and
// the output does not depend on the order the Go files declare things in.
func (p *abiPackage) report(funcs []*Func) []string {
	source := "abi from " + p.source
	if p.assumed {
		source = "abi from " + p.source + " flags (assumed)"
	}
	ordered := make([]*Func, len(funcs))
	copy(ordered, funcs)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].abi.sym.QualifiedName != ordered[j].abi.sym.QualifiedName {
			return ordered[i].abi.sym.QualifiedName < ordered[j].abi.sym.QualifiedName
		}
		return ordered[i].abi.sym.Mangled < ordered[j].abi.sym.Mangled
	})
	lines := make([]string, 0, len(ordered))
	for _, f := range ordered {
		var b strings.Builder
		fmt.Fprintf(&b, "%-34s mangled %-46s %s", f.abi.sym.QualifiedName, f.abi.sym.Mangled, source)
		for _, note := range f.abi.notes {
			fmt.Fprintf(&b, "; note: %s", note)
		}
		for _, forced := range f.abi.forced {
			fmt.Fprintf(&b, "; forced with -force, NOT validated: %s", forced)
		}
		lines = append(lines, b.String())
	}
	return lines
}

// resolve resolves and validates one binding.
func (r *abiResolver) resolve(f *Func, force bool) (*abiBinding, error) {
	name := f.cxxName()
	candidates := r.project.Lookups(name)
	if len(candidates) == 0 {
		return nil, posErr(f.Pos, "%s: %s is not declared in %s as a bindable function (a declaration in a system header, or one with internal linkage, is invisible to the generator)", f.Name, name, r.pkg.Header)
	}
	binding := &abiBinding{}
	if len(candidates) == 1 {
		// A single declaration: there is no overload to choose, so problems
		// reports what does not fit one parameter at a time, which is the
		// message a user can act on.
		binding.sym = candidates[0]
	} else {
		sym, sameArity, err := pickOverload(f, candidates)
		switch {
		case err == nil:
			binding.sym = sym
		case force && len(sameArity) == 1:
			// -force: the user accepted this binding, so the one overload that
			// takes the right number of arguments is emitted as written.
			binding.sym = sameArity[0]
			binding.forced = append(binding.forced, err.Error())
		default:
			return nil, posErr(f.Pos, "%s: %v", name, err)
		}
	}

	problems, shape := r.problems(f, binding.sym)
	var fatal []string
	for _, p := range problems {
		switch {
		case p.fatal:
			fatal = append(fatal, p.msg)
		case force:
			binding.forced = append(binding.forced, p.msg)
		default:
			return nil, posErr(f.Pos, "%s: %s", name, p.msg)
		}
	}
	if len(fatal) > 0 {
		return nil, posErr(f.Pos, "%s: %s", name, strings.Join(fatal, "; and "))
	}
	binding.shape = shape
	if shape == abiStatus {
		binding.statusWidth = binding.sym.Result.SizeBytes
	}
	switch shape {
	case abiObject:
		binding.notes = append(binding.notes, fmt.Sprintf("the object is allocated in Go storage owned by this package (%d bytes, alignment %d) and released by //cppgo:dtor", f.Class.Size, f.Class.Align))
	case abiValueError, abiErrorOnly:
		binding.notes = append(binding.notes, "the trailing error is always nil: a direct C++ call has no status channel (return an int32 status and declare only error to get one)")
	}
	return binding, nil
}

// pickOverload chooses the overload the Go declaration describes.
//
// Overloads share a qualified name, so the Go signature is the only thing that
// can disambiguate them: the candidate must take the same number of arguments
// and each C++ parameter must accept the corresponding Go type. It also returns
// the candidates that at least take the right number of arguments, which -force
// needs to emit a binding the user accepted anyway.
func pickOverload(f *Func, candidates []cxx.Symbol) (cxx.Symbol, []cxx.Symbol, error) {
	var sameArity, fitting []cxx.Symbol
	want := cxxParamCount(f)
	for _, cand := range candidates {
		if len(cand.Params) != want {
			continue
		}
		sameArity = append(sameArity, cand)
		if len(paramReasons(f, cand)) == 0 {
			fitting = append(fitting, cand)
		}
	}
	switch {
	case len(fitting) == 1:
		return fitting[0], sameArity, nil
	case len(fitting) > 1:
		return cxx.Symbol{}, sameArity, fmt.Errorf("%d overloads accept the Go argument types (%s); rename the Go function or drop the binding so the call is unambiguous", len(fitting), signatures(fitting))
	case len(sameArity) == 0:
		return cxx.Symbol{}, nil, fmt.Errorf("no overload takes %s; the header declares %s", argumentCount(want), signatures(candidates))
	default:
		return cxx.Symbol{}, sameArity, fmt.Errorf("no overload accepts the Go argument types; the header declares %s", signatures(sameArity))
	}
}

// cxxParamCount is how many C++ parameters the Go declaration describes: a Go
// string or slice crosses as a (pointer, length) pair, so it spends two.
func cxxParamCount(f *Func) int {
	n := 0
	for _, p := range f.Params {
		switch p.Info.kind {
		case kindSlice, kindString:
			n += 2
		default:
			n++
		}
	}
	return n
}

// argumentCount renders a parameter count for a diagnostic.
func argumentCount(n int) string {
	if n == 1 {
		return "1 argument"
	}
	return fmt.Sprintf("%d arguments", n)
}

// paramReasons lists every parameter that does not fit the C++ signature. A
// Go string or slice consumes two C++ parameters, the pointer and the length.
func paramReasons(f *Func, sym cxx.Symbol) []string {
	if len(sym.Params) != cxxParamCount(f) {
		return []string{fmt.Sprintf("the binding describes %s but the C++ signature takes %s", argumentCount(cxxParamCount(f)), argumentCount(len(sym.Params)))}
	}
	var out []string
	i := 0
	for n, p := range f.Params {
		if why := matchParam(p.Info, sym.Params[i]); why != "" {
			out = append(out, fmt.Sprintf("parameter %d (%s %s): %s", n+1, p.Name, p.Info.goType, why))
		}
		i++
		if p.Info.kind == kindSlice || p.Info.kind == kindString {
			if why := matchLength(sym.Params[i]); why != "" {
				out = append(out, fmt.Sprintf("parameter %d (%s %s): %s", n+1, p.Name, p.Info.goType, why))
			}
			i++
		}
	}
	return out
}

// signatures renders a set of overloads for a diagnostic.
func signatures(syms []cxx.Symbol) string {
	parts := make([]string, len(syms))
	for i, sym := range syms {
		params := make([]string, len(sym.Params))
		for j, p := range sym.Params {
			params[j] = p.Spelling
		}
		parts[i] = sym.QualifiedName + "(" + strings.Join(params, ", ") + ")"
	}
	return strings.Join(parts, ", ")
}

// problems lists every reason a resolved symbol cannot be emitted, together
// with the shape the binding will use if only the escapable reasons are found.
func (r *abiResolver) problems(f *Func, sym cxx.Symbol) ([]problem, abiShape) {
	out := kindProblems(f, sym)
	if sym.Mangled == "" {
		out = append(out, problem{fatal: true, msg: "it has no mangled name, so it is a template (or an entity the ABI does not name): only an explicit instantiation emits a symbol, instantiate it in a source file or use the shim mode"})
	}
	if !sym.HasExportedSymbol() {
		out = append(out, problem{msg: "it is defined inside its class definition or has internal linkage, so it is implicitly inline and the library exports no symbol for it: move the definition out into a source file and rebuild, or bind it through the shim mode"})
	}
	if sym.IsVirtual {
		out = append(out, problem{msg: "it is virtual: the call has to go through the vtable to reach an override, so a fixed symbol cannot be called directly; use the shim mode, or bind a non-virtual member"})
	}
	for _, why := range paramReasons(f, sym) {
		out = append(out, problem{msg: why})
	}
	shape, why := resultShape(f, sym)
	if why != "" {
		fatal := shape == shapeFatal
		if fatal {
			shape = abiVoid
		}
		out = append(out, problem{msg: why, fatal: fatal})
	}
	return out, shape
}

// shapeFatal is the shape sentinel a fatal result problem carries.
const shapeFatal abiShape = 0xff

// kindProblems checks that the declaration form matches what the C++ entity is.
func kindProblems(f *Func, sym cxx.Symbol) []problem {
	switch f.Kind {
	case bindMethod:
		switch sym.Kind {
		case cxx.Method:
			return nil
		case cxx.StaticMethod:
			return []problem{{msg: "it is a static member function, which has no this: bind it with //cppgo:function " + sym.QualifiedName}}
		default:
			return []problem{{msg: fmt.Sprintf("it is a %s, not a member function: use //cppgo:function", sym.Kind)}}
		}
	case bindCtor:
		if sym.Kind != cxx.Constructor {
			return []problem{{msg: fmt.Sprintf("it is a %s, not a constructor: the constructor is %s::%s", sym.Kind, f.Class.CxxName, f.Class.simple())}}
		}
	case bindDtor:
		if sym.Kind != cxx.Destructor {
			return []problem{{msg: fmt.Sprintf("it is a %s, not a destructor: the destructor is %s::~%s", sym.Kind, f.Class.CxxName, f.Class.simple())}}
		}
	case bindFunction:
		switch sym.Kind {
		case cxx.FreeFunc, cxx.StaticMethod:
			return nil
		default:
			return []problem{{msg: "it is a non-static member function, which needs this: bind it with //cppgo:method on the class"}}
		}
	}
	return nil
}

// resultShape describes how the C++ result reaches the Go side, and reports the
// reason it cannot.
func resultShape(f *Func, sym cxx.Symbol) (abiShape, string) {
	rec := sym.Result.Spelling
	if f.Kind == bindCtor {
		// Constructors return void; the handle is the storage the Go side
		// allocated for this.
		return abiObject, ""
	}
	if rec != "" && sym.Result.Kind == cxx.TypeRecord {
		return shapeFatal, fmt.Sprintf("it returns %s by value: the Itanium ABI brings a class back through a hidden sret pointer, which the direct ABI mode does not emit in v1 (the trampoline layout reserves the slot, see the TODO in abi.go) — use the shim mode for this binding", rec)
	}
	value := valueResult(f)
	switch {
	case value != nil:
		if why := matchResult(value.Info, sym.Result); why != "" {
			return abiValue, why
		}
		if hasErrorResult(f) {
			return abiValueError, ""
		}
		return abiValue, ""
	case hasErrorResult(f):
		if sym.Result.Kind == cxx.TypeInteger || sym.Result.Kind == cxx.TypeEnum {
			return abiStatus, ""
		}
		if sym.Result.Kind == cxx.TypeVoid {
			return abiErrorOnly, ""
		}
		return abiErrorOnly, fmt.Sprintf("the binding declares only an error, but the C++ function returns %s: the status convention needs an integer return type, so the error would always be nil", rec)
	default:
		// Nothing to return: any scalar result can be discarded, and a call
		// that discards its result is still correct at the ABI level.
		return abiVoid, ""
	}
}

// valueResult is the binding's value result, or nil when it has none.
func valueResult(f *Func) *Param {
	if len(f.Results) == 0 {
		return nil
	}
	if v := f.Results[0]; v.Info.kind != kindError {
		return v
	}
	return nil
}

// hasErrorResult reports whether the binding declares a trailing error.
func hasErrorResult(f *Func) bool {
	n := len(f.Results)
	return n > 0 && f.Results[n-1].Info.kind == kindError
}

// matchParam reports why a Go parameter cannot be passed to a C++ parameter,
// or "" when it can.
func matchParam(g *typeInfo, c cxx.Param) string {
	if strings.Contains(c.Spelling, "std::") {
		return fmt.Sprintf("the C++ parameter is %q, a standard-library type: its ABI (and its lifetime) is the shim mode's business", c.Spelling)
	}
	if c.Kind == cxx.TypeRecord && !c.Ptr {
		return fmt.Sprintf("the C++ parameter is %q, a struct passed by value, which the direct ABI mode does not support: take it by const reference or use the shim mode", c.Spelling)
	}
	if c.Kind == cxx.TypeOther {
		return fmt.Sprintf("the C++ parameter is %q, which the direct ABI mode cannot classify (a member pointer, an array, a function pointer or an unknown type)", c.Spelling)
	}
	switch g.kind {
	case kindInt:
		switch c.Kind {
		case cxx.TypeInteger:
			if c.SizeBytes != int64(g.width) {
				return widthProblem(g, c)
			}
		case cxx.TypeEnum:
			// An enum travels as its underlying integer, which is never wider
			// than the slot the value is passed in.
			if c.SizeBytes <= 0 || c.SizeBytes > int64(g.width) {
				return widthProblem(g, c)
			}
		default:
			return classProblem(g, c)
		}
	case kindFloat:
		if c.Kind != cxx.TypeFloat {
			return classProblem(g, c)
		}
		if c.SizeBytes != int64(g.width) {
			return widthProblem(g, c)
		}
	case kindBool:
		if c.ClangKind != "Bool" {
			return fmt.Sprintf("the Go type is bool but the C++ parameter is %q: bool crosses as a C++ bool only", c.Spelling)
		}
	case kindHandle, kindPointer:
		if !c.Ptr {
			return fmt.Sprintf("the Go type is %s, a pointer, but the C++ parameter is %q, passed by value", g.goType, c.Spelling)
		}
	case kindSlice:
		if !c.Ptr {
			return fmt.Sprintf("the Go type is %s, which crosses as (pointer, length), but the C++ parameter is %q, passed by value", g.goType, c.Spelling)
		}
	case kindString:
		if !c.Ptr {
			return fmt.Sprintf("the Go type is string, which crosses as (pointer, length), but the C++ parameter is %q, passed by value", c.Spelling)
		}
	case kindObject:
		return fmt.Sprintf("a %s handle is the object pointer, not an argument: the C++ parameter is %q", g.goType, c.Spelling)
	}
	return ""
}

// matchResult reports why a Go value result cannot describe a C++ result.
func matchResult(g *typeInfo, c cxx.Param) string {
	if c.Kind == cxx.TypeVoid {
		return fmt.Sprintf("the C++ function returns void, but the binding declares a %s result", g.goType)
	}
	if strings.Contains(c.Spelling, "std::") {
		return fmt.Sprintf("the C++ function returns %q, a standard-library type: use the shim mode", c.Spelling)
	}
	if c.Kind == cxx.TypeOther {
		return fmt.Sprintf("the C++ returns %q, which the direct ABI mode cannot classify", c.Spelling)
	}
	switch g.kind {
	case kindInt:
		switch c.Kind {
		case cxx.TypeInteger:
			if c.SizeBytes != int64(g.width) {
				return widthProblem(g, c)
			}
		case cxx.TypeEnum:
			if c.SizeBytes <= 0 || c.SizeBytes > int64(g.width) {
				return widthProblem(g, c)
			}
		default:
			return classProblem(g, c)
		}
	case kindFloat:
		if c.Kind != cxx.TypeFloat || c.SizeBytes != int64(g.width) {
			return classProblem(g, c)
		}
	case kindBool:
		if c.ClangKind != "Bool" {
			return fmt.Sprintf("the Go result is bool but the C++ function returns %q", c.Spelling)
		}
	case kindPointer, kindHandle:
		if !c.Ptr {
			return fmt.Sprintf("the Go result is %s, a pointer, but the C++ function returns %q by value", g.goType, c.Spelling)
		}
	case kindObject:
		return fmt.Sprintf("a %s handle cannot be returned by value: the C++ function returns %q", g.goType, c.Spelling)
	}
	return ""
}

// matchLength checks the second half of a Go string or slice, which crosses as
// a size_t after the pointer.
func matchLength(c cxx.Param) string {
	if c.Kind != cxx.TypeInteger || c.SizeBytes != 8 {
		return fmt.Sprintf("a string or slice passes its length as a size_t (an 8-byte unsigned integer), but the C++ parameter after the pointer is %q", c.Spelling)
	}
	return ""
}

// widthProblem explains a scalar width mismatch.
func widthProblem(g *typeInfo, c cxx.Param) string {
	return fmt.Sprintf("the Go type is %s (%d-byte) but the C++ type is %q (%d-byte): the two must have the same width, because the value is passed in one register", g.goType, g.width, c.Spelling, c.SizeBytes)
}

// classProblem explains a register-class mismatch.
func classProblem(g *typeInfo, c cxx.Param) string {
	class := "an integer"
	if g.kind == kindFloat {
		class = "a float (XMM)"
	}
	return fmt.Sprintf("the Go type is %s, %s, but the C++ type is %q, which travels in the other register class", g.goType, class, c.Spelling)
}

// cxxName is the qualified C++ name the binding resolves against.
func (f *Func) cxxName() string {
	switch f.Kind {
	case bindCtor:
		return f.Class.CxxName + "::" + f.Class.simple()
	case bindDtor:
		return f.Class.CxxName + "::~" + f.Class.simple()
	case bindMethod:
		return f.Class.CxxName + "::" + f.Member
	}
	return f.Member
}

// probeSizes asks libclang for sizeof and alignof of every class a constructor
// has to allocate.
//
// cxx.Symbol carries types, not class sizes: the size of a class appears in the
// model only when it is passed by value somewhere. Rather than guess, the
// generator parses one extra translation unit that includes the header and, per
// class, declares
//
//	void size_N(::ns::Class);                       // sizeof, as Param.SizeBytes
//	struct align_N { char c; ::ns::Class v; };
//	void asize_N(align_N);                          // sizeof of the padded struct
//
// so alignof(Class) is sizeof(align_N) - sizeof(Class): the char forces the
// class to its own alignment, and sizeof is always a multiple of it.
func (r *abiResolver) probeSizes() error {
	var classes []*classInfo
	for _, f := range r.pkg.Funcs {
		if f.Kind != bindCtor || f.Class == nil || f.Class.Size != 0 {
			continue
		}
		duplicate := false
		for _, c := range classes {
			duplicate = duplicate || c == f.Class
		}
		if !duplicate {
			classes = append(classes, f.Class)
		}
	}
	if len(classes) == 0 {
		return nil
	}

	var src strings.Builder
	fmt.Fprintf(&src, "// Generated by cppgo gen: object sizes for the direct ABI mode.\n#include %q\n\nnamespace cppgo_probe {\n", r.pkg.HeaderPath)
	for i, c := range classes {
		fmt.Fprintf(&src, "struct align_%d { char cppgo_char; ::%s cppgo_class; };\n", i, c.CxxName)
		fmt.Fprintf(&src, "void size_%d(::%s);\n", i, c.CxxName)
		fmt.Fprintf(&src, "void asize_%d(align_%d);\n", i, i)
	}
	src.WriteString("} // namespace cppgo_probe\n")

	dir, err := os.MkdirTemp("", "cppgo-probe")
	if err != nil {
		return fmt.Errorf("cppgo: creating the size probe: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "cppgo_probe.hpp")
	if err := os.WriteFile(path, []byte(src.String()), 0o644); err != nil {
		return fmt.Errorf("cppgo: writing the size probe: %w", err)
	}

	tu, err := r.project.Library.ParseTranslationUnit(path, r.project.Flags.Args)
	if err != nil {
		return fmt.Errorf("cppgo: parsing the size probe: %w", err)
	}
	defer func() { _ = tu.Close() }()

	for i, c := range classes {
		size, ok := probeParam(tu, fmt.Sprintf("cppgo_probe::size_%d", i))
		asize, ok2 := probeParam(tu, fmt.Sprintf("cppgo_probe::asize_%d", i))
		// sizeof(struct { char; T v; }) - sizeof(T) is alignof(T), because
		// sizeof(T) is always a multiple of alignof(T).
		align := asize - size
		if !ok || !ok2 || size <= 0 || align <= 0 || align&(align-1) != 0 {
			return posErr(c.Pos, "%s: cannot determine the layout of %s from the header: is the class complete there? The direct ABI mode allocates the object and needs its size and alignment", c.GoName, c.CxxName)
		}
		c.Size, c.Align = size, align
	}
	return nil
}

// probeParam reads the size of the single parameter of a probe declaration.
func probeParam(tu *cxx.TU, name string) (int64, bool) {
	sym, ok := tu.Lookup(name)
	if !ok || len(sym.Params) != 1 {
		return 0, false
	}
	return sym.Params[0].SizeBytes, true
}
