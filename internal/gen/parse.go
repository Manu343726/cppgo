package gen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// generatedMarker matches the conventional "generated file" banner. Files
// carrying it are never used as generator input, so a package can be
// regenerated into its own source directory.
var generatedMarker = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

const (
	libraryDirective  = "library"
	externalDirective = "external"
	modeDirective     = "mode"
	headerDirective   = "header"
	classDirective    = "class"
	ctorDirective     = "ctor"
	dtorDirective     = "dtor"
	methodDirective   = "method"
	functionDirective = "function"
)

// ModeShim is the extern "C" calling convention: every call goes through an
// entry point declared in cppgo_shim.h.
const ModeShim = "shim"

// ModeABI calls the C++ symbol directly: the mangled name is resolved with
// dlsym and `this` travels in the first integer argument slot. It is the
// default when //cppgo:header is present.
const ModeABI = "abi"

// bindingKind is the ABI-mode form a declaration was written with. It decides
// whether the call carries an implicit `this`.
type bindingKind uint8

const (
	// bindShim is the //cppgo:external form: an extern "C" entry point.
	bindShim bindingKind = iota
	// bindCtor is //cppgo:ctor: allocates the object and calls C1.
	bindCtor
	// bindDtor is //cppgo:dtor: calls D1 and releases the storage.
	bindDtor
	// bindMethod is //cppgo:method: a non-static member function.
	bindMethod
	// bindFunction is //cppgo:function: a free function or a static method.
	bindFunction
)

// hasThis reports whether the call takes an implicit `this` argument.
func (k bindingKind) hasThis() bool {
	return k == bindCtor || k == bindDtor || k == bindMethod
}

// packageInfo is everything the generator needs to know about the input
// package.
type packageInfo struct {
	Name    string
	Library string
	// Mode is the calling convention of the bindings: ModeShim or ModeABI.
	Mode string
	// Header is the //cppgo:header value as written, and HeaderPath is the
	// absolute path handed to libclang.
	Header     string
	HeaderPath string
	Dir        string
	Funcs      []*Func
	Files      []*sourceFile
	// Classes are the //cppgo:class declarations, indexed by Go type name.
	Classes map[string]*classInfo
	// abi is the resolved C++ side of the package, nil in shim mode.
	abi  *abiPackage
	fset *token.FileSet
	// byDecl maps a bound function's declaration back to its model, so the
	// source rewriter knows which bodies to replace.
	byDecl map[*ast.FuncDecl]*Func
}

// classInfo is one //cppgo:class declaration: the Go type that mirrors a C++
// class, and the C++ class it mirrors.
type classInfo struct {
	// GoName is the Go type declared over cppgo.Handle, e.g. "Vector".
	GoName string
	// CxxName is the qualified C++ name, e.g. "mathx::Vector".
	CxxName string
	// Pos locates the //cppgo:class directive.
	Pos token.Position
	// Size and Align are sizeof and alignof of the C++ class, learned from the
	// probe translation unit. They are only computed when the class is
	// constructed by a bound //cppgo:ctor.
	Size  int64
	Align int64
}

// simple is the class name without its namespace, which is what a constructor
// or destructor is spelled with inside the class.
func (c *classInfo) simple() string {
	name := c.CxxName
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	return strings.TrimPrefix(name, "~")
}

// isHandle mirrors reports whether a type expression is `cppgo.Handle`.
func isHandle(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "cppgo" && sel.Sel.Name == "Handle"
}

// sourceFile is one input file.
type sourceFile struct {
	// Path is the path the file was read from, as the user spelled it.
	Path string
	// Abs is the absolute path, used to decide whether regenerating means
	// rewriting the file in place.
	Abs  string
	File *ast.File
	// src is the file's bytes as read from disk, used for in-place edits.
	src []byte
}

// Func is one bound C++ function.
type Func struct {
	Name    string   // Go function name
	Symbol  string   // dynamic symbol the trampoline calls
	Doc     []string // doc comment lines, with the cppgo directives removed
	Params  []*Param
	Results []*Param
	Pos     token.Position
	// Kind is the form the declaration was written with.
	Kind bindingKind
	// Recv is the receiver of an ABI-mode method or destructor.
	Recv *Param
	// Class is the C++ class the binding belongs to, for methods,
	// constructors and destructors.
	Class *classInfo
	// Member is the C++ member or qualified function name the directive
	// named, i.e. the argument of //cppgo:method or //cppgo:function.
	Member string
	// abi is the resolved and validated C++ side. Nil in shim mode.
	abi *abiBinding
	// renamed is set when a parameter had no name (or was blank), in which
	// case the source rewrite has to spell the names out.
	renamed bool
	decl    *ast.FuncDecl
	file    *sourceFile
}

// Param is one Go parameter or result.
type Param struct {
	Name string
	Info *typeInfo
}

// wrappername is the unexported Go function in the generated file that holds
// the idiomatic wrapper for f.
func (f *Func) wrapperName() string { return "cppgoCall" + f.Name }

// trampName is the assembly trampoline implementing the call.
func (f *Func) trampName() string { return "cppgo_" + f.Symbol }

// symVar is the variable holding the resolved symbol address.
func (f *Func) symVar() string { return "cppgoSym" + f.Name }

// cName is the C++ shim entry point.
func (f *Func) cName() string { return "cppgo_" + f.Symbol }

// resultName is the Go spelling of the function's result list.
func (f *Func) resultName() string {
	switch len(f.Results) {
	case 0:
		return ""
	case 1:
		return f.Results[0].Info.goType
	}
	parts := make([]string, len(f.Results))
	for i, r := range f.Results {
		parts[i] = r.Info.goType
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// signature is the Go spelling of the function, used in comments.
func (f *Func) signature() string {
	params := make([]string, len(f.Params))
	for i, p := range f.Params {
		params[i] = p.Name + " " + p.Info.goType
	}
	sig := "func "
	if f.Recv != nil {
		sig += "(" + f.Recv.Name + " " + f.Recv.Info.goType + ") "
	}
	sig += f.Name + "(" + strings.Join(params, ", ") + ")"
	if r := f.resultName(); r != "" {
		sig += " " + r
	}
	return sig
}

// parsePackage reads and validates the package in dir.
func parsePackage(dir string) (*packageInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading package directory %s: %w", dir, err)
	}
	pkg := &packageInfo{
		Mode:    ModeShim,
		Dir:     dir,
		fset:    token.NewFileSet(),
		byDecl:  map[*ast.FuncDecl]*Func{},
		Classes: map[string]*classInfo{},
	}

	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		if strings.HasPrefix(n, "_") || strings.HasPrefix(n, ".") {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	skipped := 0
	for _, n := range names {
		path := filepath.Join(dir, n)
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		if generatedMarker.Match(src) {
			skipped++
			continue
		}
		f, err := parser.ParseFile(pkg.fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if pkg.Name == "" {
			pkg.Name = f.Name.Name
		} else if pkg.Name != f.Name.Name {
			return nil, posErr(pkg.fset.Position(f.Package), "found package %q, but the rest of the directory is package %q", f.Name.Name, pkg.Name)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", path, err)
		}
		pkg.Files = append(pkg.Files, &sourceFile{Path: path, Abs: abs, File: f, src: src})
	}
	if len(pkg.Files) == 0 {
		if skipped > 0 {
			return nil, fmt.Errorf("no Go files in %s: %d file(s) carry a \"Code generated\" banner and were ignored", dir, skipped)
		}
		return nil, fmt.Errorf("no Go files in %s", dir)
	}

	if err := scanPackageDirectives(pkg); err != nil {
		return nil, err
	}
	if err := scanClasses(pkg); err != nil {
		return nil, err
	}
	if err := scanBindings(pkg); err != nil {
		return nil, err
	}
	sortFuncs(pkg.Funcs)
	return pkg, nil
}

// scanPackageDirectives collects the package-level library, header and mode
// directives, and rejects bind directives that are not on a declaration.
func scanPackageDirectives(pkg *packageInfo) error {
	docs := map[*ast.CommentGroup]bool{}
	for _, sf := range pkg.Files {
		for _, d := range sf.File.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
				docs[fd.Doc] = true
			}
		}
	}

	var libraryPos, modePos, headerPos token.Position
	mode := ""
	for _, sf := range pkg.Files {
		for _, cg := range sf.File.Comments {
			for _, d := range directivesIn(pkg.fset, cg) {
				switch d.name {
				case libraryDirective:
					if d.arg == "" {
						return posErr(d.pos, "//cppgo:library needs a library name")
					}
					if strings.ContainsAny(d.arg, " \t") {
						return posErr(d.pos, "//cppgo:library takes a single library name, got %q", d.arg)
					}
					if pkg.Library != "" {
						return posErr(d.pos, "duplicate //cppgo:library %q (already declared at %s)", d.arg, libraryPos)
					}
					pkg.Library = d.arg
					libraryPos = d.pos
				case modeDirective:
					if modePos.Line != 0 {
						return posErr(d.pos, "duplicate //cppgo:mode (already declared at %s)", modePos)
					}
					switch d.arg {
					case ModeShim, ModeABI:
					default:
						return posErr(d.pos, "unsupported //cppgo:mode %q: use %q (direct C++ calls) or %q (the extern \"C\" shim)", d.arg, ModeABI, ModeShim)
					}
					mode = d.arg
					modePos = d.pos
				case headerDirective:
					if d.arg == "" {
						return posErr(d.pos, "//cppgo:header needs the path of the C++ header to bind")
					}
					if strings.ContainsAny(d.arg, " \t") {
						return posErr(d.pos, "//cppgo:header takes a single path, got %q", d.arg)
					}
					if pkg.Header != "" {
						return posErr(d.pos, "duplicate //cppgo:header %q (already declared at %s)", d.arg, headerPos)
					}
					pkg.Header = d.arg
					headerPos = d.pos
				case externalDirective, ctorDirective, dtorDirective, methodDirective, functionDirective:
					if !docs[cg] {
						return posErr(d.pos, "//cppgo:%s must be in the doc comment of a function declaration", d.name)
					}
				}
			}
		}
	}

	// The mode is decided once, from what the package declared: a header means
	// the direct ABI mode unless the package opted out explicitly.
	switch {
	case mode == ModeShim && pkg.Header != "":
		return posErr(modePos, "//cppgo:header selects the direct ABI mode, which //cppgo:mode shim opts out of: remove one of the two")
	case mode != "":
		pkg.Mode = mode
	case pkg.Header != "":
		pkg.Mode = ModeABI
	}
	if pkg.Mode == ModeABI && pkg.Header == "" {
		return posErr(modePos, "//cppgo:mode abi needs //cppgo:header: the direct ABI mode resolves and validates every binding against the C++ declarations")
	}
	if pkg.Library == "" {
		return fmt.Errorf("%s: missing //cppgo:library <name> directive, which declares the shared library to open", pkg.Dir)
	}
	if pkg.Header != "" {
		path := pkg.Header
		if !filepath.IsAbs(path) {
			path = filepath.Join(pkg.Dir, path)
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return posErr(headerPos, "resolving //cppgo:header %q: %v", pkg.Header, err)
		}
		pkg.HeaderPath = filepath.Clean(abs)
	}
	return nil
}

// scanClasses collects the //cppgo:class declarations.
func scanClasses(pkg *packageInfo) error {
	byCxx := map[string]token.Position{}
	for _, sf := range pkg.Files {
		for _, d := range sf.File.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok {
				continue
			}
			var dir *directive
			for _, candidate := range directivesIn(pkg.fset, gd.Doc) {
				if candidate.name != classDirective {
					continue
				}
				if dir != nil {
					return posErr(candidate.pos, "duplicate //cppgo:class on %s", dir.arg)
				}
				cp := candidate
				dir = &cp
			}
			if dir == nil {
				continue
			}
			if gd.Tok != token.TYPE {
				return posErr(dir.pos, "//cppgo:class must be on a type declaration")
			}
			if pkg.Mode != ModeABI {
				return posErr(dir.pos, "//cppgo:class is a directive of the direct ABI mode: add //cppgo:header, or drop //cppgo:mode shim")
			}
			if dir.arg == "" {
				return posErr(dir.pos, "//cppgo:class needs the qualified C++ class name, e.g. mathx::Vector")
			}
			if len(gd.Specs) != 1 {
				return posErr(dir.pos, "//cppgo:class must be on a single type declaration")
			}
			ts, ok := gd.Specs[0].(*ast.TypeSpec)
			if !ok || ts.TypeParams != nil {
				return posErr(dir.pos, "//cppgo:class must be on a single non-generic type declaration")
			}
			if !isHandle(ts.Type) {
				return posErr(dir.pos, "type %s must be declared over cppgo.Handle (type %s cppgo.Handle), so the value is the object pointer", ts.Name.Name, ts.Name.Name)
			}
			if prev, ok := pkg.Classes[ts.Name.Name]; ok {
				return posErr(dir.pos, "duplicate //cppgo:class for %s (already declared at %s)", ts.Name.Name, prev.Pos)
			}
			if prev, ok := byCxx[dir.arg]; ok {
				return posErr(dir.pos, "%s is already bound to a Go type at %s", dir.arg, prev)
			}
			byCxx[dir.arg] = dir.pos
			pkg.Classes[ts.Name.Name] = &classInfo{GoName: ts.Name.Name, CxxName: dir.arg, Pos: dir.pos}
		}
	}
	return nil
}

// scanBindings builds one Func per bound declaration.
func scanBindings(pkg *packageInfo) error {
	for _, sf := range pkg.Files {
		for _, d := range sf.File.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Doc == nil {
				continue
			}
			var bind *directive
			for _, candidate := range directivesIn(pkg.fset, fd.Doc) {
				switch candidate.name {
				case externalDirective, ctorDirective, dtorDirective, methodDirective, functionDirective:
					if bind != nil {
						return posErr(candidate.pos, "duplicate //cppgo:%s on %s (already bound by //cppgo:%s at %s)", candidate.name, fd.Name.Name, bind.name, bind.pos)
					}
					cp := candidate
					bind = &cp
				}
			}
			if bind == nil {
				continue
			}
			f, err := newFunc(pkg, sf, fd, *bind)
			if err != nil {
				return err
			}
			pkg.Funcs = append(pkg.Funcs, f)
			pkg.byDecl[fd] = f
		}
	}
	if len(pkg.Funcs) == 0 {
		if pkg.Mode == ModeABI {
			return fmt.Errorf("%s: no //cppgo:ctor, //cppgo:method, //cppgo:dtor or //cppgo:function declarations found", pkg.Dir)
		}
		return fmt.Errorf("%s: no //cppgo:external functions found", pkg.Dir)
	}
	if err := checkUnique(pkg); err != nil {
		return err
	}
	return nil
}

// checkUnique rejects duplicate Go names and duplicate C symbols.
func checkUnique(pkg *packageInfo) error {
	byName := map[string]*Func{}
	bySymbol := map[string]*Func{}
	for _, f := range pkg.Funcs {
		if prev, ok := byName[f.Name]; ok {
			return posErr(f.Pos, "duplicate function %s (already bound at %s)", f.Name, prev.Pos)
		}
		byName[f.Name] = f
		if f.Symbol == "" {
			continue
		}
		if prev, ok := bySymbol[f.Symbol]; ok {
			return posErr(f.Pos, "duplicate symbol %q: already bound by %s at %s", f.Symbol, prev.Name, prev.Pos)
		}
		bySymbol[f.Symbol] = f
	}
	return nil
}

var symbolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// newFunc validates one bound declaration and turns it into a Func.
func newFunc(pkg *packageInfo, sf *sourceFile, fd *ast.FuncDecl, d directive) (*Func, error) {
	switch d.name {
	case externalDirective:
		if pkg.Mode != ModeShim {
			return nil, posErr(d.pos, "//cppgo:external selects the shim mode; in abi mode use //cppgo:ctor, //cppgo:method, //cppgo:dtor or //cppgo:function")
		}
		return newShimFunc(pkg, sf, fd, d)
	default:
		if pkg.Mode != ModeABI {
			return nil, posErr(d.pos, "%s is a directive of the direct ABI mode: add //cppgo:header, or drop //cppgo:mode shim", d)
		}
		return newABIFunc(pkg, sf, fd, d)
	}
}

// newShimFunc validates one //cppgo:external declaration.
func newShimFunc(pkg *packageInfo, sf *sourceFile, fd *ast.FuncDecl, d directive) (*Func, error) {
	if fd.Recv != nil {
		return nil, posErr(d.pos, "//cppgo:external cannot be used on a method; use a plain function")
	}
	if fd.Type.TypeParams != nil {
		return nil, posErr(d.pos, "//cppgo:external cannot be used on a generic function")
	}
	if d.arg == "" {
		return nil, posErr(d.pos, "//cppgo:external needs the C symbol name")
	}
	if !symbolPattern.MatchString(d.arg) {
		return nil, posErr(d.pos, "invalid symbol name %q: it must be a valid C identifier", d.arg)
	}
	f := &Func{
		Name:   fd.Name.Name,
		Symbol: d.arg,
		Kind:   bindShim,
		Pos:    d.pos,
		decl:   fd,
		file:   sf,
	}
	if err := collectSignature(pkg, f, fd, d, &resolver{}); err != nil {
		return nil, err
	}
	return f, nil
}

// newABIFunc validates one abi-mode declaration: which C++ entity it binds,
// and which class it belongs to.
func newABIFunc(pkg *packageInfo, sf *sourceFile, fd *ast.FuncDecl, d directive) (*Func, error) {
	if fd.Type.TypeParams != nil {
		return nil, posErr(d.pos, "//cppgo:%s cannot be used on a generic function", d.name)
	}
	f := &Func{
		Name: fd.Name.Name,
		Kind: bindCtor,
		Pos:  d.pos,
		decl: fd,
		file: sf,
	}
	switch d.name {
	case ctorDirective:
		f.Kind = bindCtor
	case dtorDirective:
		f.Kind = bindDtor
	case methodDirective:
		f.Kind = bindMethod
	case functionDirective:
		f.Kind = bindFunction
	}

	r := &resolver{classes: pkg.Classes}
	if fd.Recv != nil {
		if f.Kind == bindCtor || f.Kind == bindFunction {
			return nil, posErr(d.pos, "//cppgo:%s cannot be used on a method; the receiver is only meaningful for //cppgo:method and //cppgo:dtor", d.name)
		}
		if len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) == 0 || fd.Recv.List[0].Names[0].Name == "_" {
			return nil, posErr(d.pos, "the receiver of a bound method must be named, so the generated call can pass it as this")
		}
		info, err := r.resolve(fd.Recv.List[0].Type)
		if err != nil {
			return nil, posErr(d.pos, "receiver of %s: %v", fd.Name.Name, err)
		}
		if info.kind != kindObject {
			return nil, posErr(d.pos, "the receiver of %s is %s: declare the class with //cppgo:class and use that type (type Vector cppgo.Handle)", fd.Name.Name, info.goType)
		}
		f.Recv = &Param{Name: fd.Recv.List[0].Names[0].Name, Info: info}
		f.Class = info.class
	}
	if f.Kind != bindCtor && f.Kind != bindFunction && f.Recv == nil {
		return nil, posErr(d.pos, "//cppgo:%s must be declared on a method of the class it binds", d.name)
	}
	if f.Kind == bindMethod {
		if d.arg == "" {
			return nil, posErr(d.pos, "//cppgo:method needs the C++ member name, e.g. norm")
		}
		if strings.ContainsAny(d.arg, " \t:") || strings.Contains(d.arg, "(") {
			return nil, posErr(d.pos, "//cppgo:method takes a single member name, got %q (qualify it with //cppgo:class, not inline)", d.arg)
		}
		f.Member = d.arg
	}
	if f.Kind == bindFunction {
		if d.arg == "" {
			return nil, posErr(d.pos, "//cppgo:function needs the qualified C++ name, e.g. mathx::saxpy")
		}
		if strings.ContainsAny(d.arg, " \t") || strings.Contains(d.arg, "(") {
			return nil, posErr(d.pos, "//cppgo:function takes a single qualified name, got %q", d.arg)
		}
		f.Member = d.arg
	}
	if err := collectSignature(pkg, f, fd, d, r); err != nil {
		return nil, err
	}
	if err := checkABIShape(f); err != nil {
		return nil, err
	}
	return f, nil
}

// checkABIShape validates the result list against what the ABI mode can
// express, before the C++ side is known.
func checkABIShape(f *Func) error {
	switch f.Kind {
	case bindCtor:
		if len(f.Results) == 0 {
			return posErr(f.Pos, "//cppgo:ctor must return the object handle, e.g. func %s(...) (Vector, error), with Vector declared by //cppgo:class", f.Name)
		}
		if f.Results[0].Info.kind != kindObject {
			return posErr(f.Pos, "//cppgo:ctor result 0 must be a //cppgo:class handle type, found %s", f.Results[0].Info.goType)
		}
		// The class a constructor builds is the class its result names: a
		// constructor has no receiver to take it from.
		f.Class = f.Results[0].Info.class
	case bindDtor:
		if len(f.Params) != 0 {
			return posErr(f.Pos, "//cppgo:dtor takes no arguments, found %d", len(f.Params))
		}
		if len(f.Results) != 0 {
			return posErr(f.Pos, "//cppgo:dtor returns nothing, found %s", f.resultName())
		}
	}
	if len(f.Results) > 2 {
		return posErr(f.Pos, "too many results: a binding returns at most one value plus a trailing error")
	}
	if len(f.Results) == 2 && f.Results[1].Info.kind != kindError {
		return posErr(f.Pos, "second result must be error, found %s", f.Results[1].Info.goType)
	}
	return nil
}

// collectSignature fills f.Params, f.Results and f.Doc from the declaration.
func collectSignature(pkg *packageInfo, f *Func, fd *ast.FuncDecl, d directive, r *resolver) error {
	// Normalise parameter names so the wrapper and the delegating body can
	// refer to them, even when the declaration used blanks.
	idx := 0
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			field.Names = []*ast.Ident{ast.NewIdent(fmt.Sprintf("a%d", idx))}
			f.renamed = true
		}
		for i, name := range field.Names {
			if name.Name == "_" || name.Name == "" {
				field.Names[i] = ast.NewIdent(fmt.Sprintf("a%d", idx))
				f.renamed = true
			}
			info, err := r.resolve(field.Type)
			if err != nil {
				return posErr(d.pos, "%s: %v", fd.Name.Name, err)
			}
			f.Params = append(f.Params, &Param{Name: field.Names[i].Name, Info: info})
			idx++
		}
	}

	if fd.Type.Results != nil {
		for _, field := range fd.Type.Results.List {
			n := len(field.Names)
			if n == 0 {
				n = 1
			}
			for i := 0; i < n; i++ {
				info, err := r.resolve(field.Type)
				if err != nil {
					return posErr(d.pos, "%s: %v", fd.Name.Name, err)
				}
				name := ""
				if i < len(field.Names) {
					name = field.Names[i].Name
				}
				f.Results = append(f.Results, &Param{Name: name, Info: info})
			}
		}
	}

	// The documentation is normalised to // lines so the generated wrapper
	// never inherits a block comment (or a //go: directive) from the binding.
	for _, c := range fd.Doc.List {
		for _, line := range commentLines(c.Text) {
			if strings.HasPrefix(line, "cppgo:") {
				continue
			}
			f.Doc = append(f.Doc, "// "+line)
		}
	}
	f.Doc = trimBlankComments(f.Doc)
	return nil
}

// trimBlankComments drops the empty //  lines a directive leaves behind when
// it is separated from the function's documentation by a blank comment.
func trimBlankComments(lines []string) []string {
	blank := func(s string) bool {
		return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "//")) == ""
	}
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	for len(lines) > 0 && blank(lines[0]) {
		lines = lines[1:]
	}
	return lines
}

// directive is one //cppgo:<name> <arg> comment.
type directive struct {
	name string
	arg  string
	pos  token.Position
}

// String renders the directive as the user wrote it.
func (d directive) String() string { return "//cppgo:" + d.name }

// isDirectiveComment reports whether a raw comment is a cppgo directive.
func isDirectiveComment(text string) bool {
	for _, line := range commentLines(text) {
		if strings.HasPrefix(line, "cppgo:") {
			return true
		}
	}
	return false
}

// commentLines returns a comment's body split into trimmed lines, with the
// //, /* and * markers removed.
func commentLines(text string) []string {
	var body string
	switch {
	case strings.HasPrefix(text, "//"):
		body = text[2:]
	case strings.HasPrefix(text, "/*"):
		body = strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/")
	default:
		return nil
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "//"))
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "*")))
	}
	// A block comment's closing delimiter leaves an empty last line.
	if n := len(out); n > 0 && out[n-1] == "" {
		out = out[:n-1]
	}
	return out
}

// directivesIn extracts the cppgo directives of a comment group.
func directivesIn(fset *token.FileSet, cg *ast.CommentGroup) []directive {
	if cg == nil {
		return nil
	}
	var out []directive
	for _, c := range cg.List {
		for _, line := range commentLines(c.Text) {
			if !strings.HasPrefix(line, "cppgo:") {
				continue
			}
			rest := line[len("cppgo:"):]
			name, arg, _ := strings.Cut(rest, " ")
			out = append(out, directive{
				name: strings.TrimSpace(name),
				arg:  strings.TrimSpace(arg),
				pos:  fset.Position(c.Pos()),
			})
		}
	}
	return out
}

// posErr formats an error against a source position. The generator reports
// file:line so the message points at the offending directive.
func posErr(pos token.Position, format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if pos.Filename == "" {
		return fmt.Errorf("%s", msg)
	}
	return fmt.Errorf("%s:%d: %s", pos.Filename, pos.Line, msg)
}
