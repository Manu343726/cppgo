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
)

// ModeShim is the calling convention this generator emits: every call goes
// through an extern "C" entry point declared in cppgo_shim.h.
const ModeShim = "shim"

// packageInfo is everything the generator needs to know about the input
// package.
type packageInfo struct {
	Name    string
	Library string
	// Mode is the calling convention of the bindings. Only ModeShim is
	// implemented; //cppgo:mode exists so the direct C++ ABI mode can be added
	// without changing the DSL.
	Mode  string
	Dir   string
	Funcs []*Func
	Files []*sourceFile
	fset  *token.FileSet
	// byDecl maps a bound function's declaration back to its model, so the
	// source rewriter knows which bodies to replace.
	byDecl map[*ast.FuncDecl]*Func
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

// Func is one bound C function.
type Func struct {
	Name    string   // Go function name
	Symbol  string   // dynamic symbol the trampoline calls
	Doc     []string // doc comment lines, with the cppgo directives removed
	Params  []*Param
	Results []*Param
	Pos     token.Position
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
	sig := "func " + f.Name + "(" + strings.Join(params, ", ") + ")"
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
	pkg := &packageInfo{Mode: ModeShim, Dir: dir, fset: token.NewFileSet(), byDecl: map[*ast.FuncDecl]*Func{}}

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

	if err := scanDirectives(pkg); err != nil {
		return nil, err
	}
	sortFuncs(pkg.Funcs)
	return pkg, nil
}

// scanDirectives collects the package-level library directive and one Func per
// //cppgo:external function declaration.
func scanDirectives(pkg *packageInfo) error {
	docs := map[*ast.CommentGroup]*ast.FuncDecl{}
	for _, sf := range pkg.Files {
		for _, d := range sf.File.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Doc != nil {
				docs[fd.Doc] = fd
			}
		}
	}

	var libraryPos, modePos token.Position
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
					if d.arg != ModeShim {
						return posErr(d.pos, "unsupported //cppgo:mode %q: this generator emits the extern \"C\" shim (%q); the direct C++ ABI mode is not implemented yet", d.arg, ModeShim)
					}
					modePos = d.pos
				case externalDirective:
					if _, ok := docs[cg]; !ok {
						return posErr(d.pos, "//cppgo:external must be in the doc comment of a function declaration")
					}
				}
			}
		}
	}
	if pkg.Library == "" {
		return fmt.Errorf("%s: missing //cppgo:library <name> directive, which declares the shared library to open", pkg.Dir)
	}

	for _, sf := range pkg.Files {
		for _, d := range sf.File.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Doc == nil {
				continue
			}
			var ext *directive
			for _, d := range directivesIn(pkg.fset, fd.Doc) {
				if d.name != externalDirective {
					continue
				}
				if ext != nil {
					return posErr(d.pos, "duplicate //cppgo:external on %s", fd.Name.Name)
				}
				dc := d
				ext = &dc
			}
			if ext == nil {
				continue
			}
			f, err := newFunc(pkg, sf, fd, *ext)
			if err != nil {
				return err
			}
			pkg.Funcs = append(pkg.Funcs, f)
			pkg.byDecl[fd] = f
		}
	}
	if len(pkg.Funcs) == 0 {
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
		if prev, ok := bySymbol[f.Symbol]; ok {
			return posErr(f.Pos, "duplicate symbol %q: already bound by %s at %s", f.Symbol, prev.Name, prev.Pos)
		}
		bySymbol[f.Symbol] = f
	}
	return nil
}

var symbolPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// newFunc validates one //cppgo:external declaration.
func newFunc(pkg *packageInfo, sf *sourceFile, fd *ast.FuncDecl, d directive) (*Func, error) {
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
		Pos:    d.pos,
		decl:   fd,
		file:   sf,
	}

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
			info, err := resolveType(field.Type)
			if err != nil {
				return nil, posErr(d.pos, "%s: %v", fd.Name.Name, err)
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
				info, err := resolveType(field.Type)
				if err != nil {
					return nil, posErr(d.pos, "%s: %v", fd.Name.Name, err)
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
	return f, nil
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
