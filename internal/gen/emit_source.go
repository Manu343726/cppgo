package gen

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// emitSource rewrites one input file so that every bound function delegates to
// its wrapper.
//
// The rewrite is done on the file's text: every //cppgo:external body is
// replaced by a one-line delegation and the result is parsed again before it
// is printed. Building the new body as an AST instead would leave synthetic
// nodes without positions, and go/printer interleaves comments by position, so
// a later declaration's doc comment ends up inside the argument list.
//
// When banner is set the result is written under a new name and gets the
// "DO NOT EDIT" banner; otherwise the file is regenerated in place, where a
// banner would be a lie (it still holds the user's own code) and would make the
// next run skip it.
func emitSource(pkg *packageInfo, sf *sourceFile, banner bool) ([]byte, error) {
	patched := stripBannerText(rewriteBodies(pkg, sf))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, sf.Path, patched, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("rewriting %s: %w", sf.Path, err)
	}
	pruneImports(file)

	var buf bytes.Buffer
	cfg := printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(&buf, fset, file); err != nil {
		return nil, fmt.Errorf("printing %s: %w", sf.Path, err)
	}
	out, err := gofmtSource(buf.String())
	if err != nil {
		return nil, fmt.Errorf("formatting %s: %w", sf.Path, err)
	}
	if banner {
		out = append([]byte(GeneratedBanner+"\n\n"), out...)
	}
	return out, nil
}

// rewriteBodies replaces every bound function's body with a delegation to its
// wrapper, working on byte ranges so the rest of the file is untouched.
func rewriteBodies(pkg *packageInfo, sf *sourceFile) []byte {
	type replacement struct {
		start, end int
		text       string
	}
	var edits []replacement
	for _, decl := range sf.File.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		f := pkg.byDecl[fd]
		if f == nil {
			continue
		}
		text := delegationText(f)
		if f.renamed {
			// The body refers to the parameter names, so a declaration that
			// used blanks (or no names at all) has to spell them out.
			names := make([]string, len(f.Params))
			for i, p := range f.Params {
				names[i] = p.Name + " " + p.Info.goType
			}
			start := pkg.fset.Position(fd.Type.Params.Opening).Offset + 1
			end := pkg.fset.Position(fd.Type.Params.Closing).Offset
			edits = append(edits, replacement{start: start, end: end, text: strings.Join(names, ", ")})
		}
		if fd.Body != nil {
			start := pkg.fset.Position(fd.Body.Lbrace).Offset
			end := pkg.fset.Position(fd.Body.Rbrace).Offset + 1
			edits = append(edits, replacement{start: start, end: end, text: text})
			continue
		}
		// A declaration without a body: append one after the signature.
		at := pkg.fset.Position(fd.Type.End()).Offset
		edits = append(edits, replacement{start: at, end: at, text: " " + text})
	}
	// Apply back to front so earlier offsets stay valid.
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })

	out := sf.src
	for _, e := range edits {
		next := make([]byte, 0, len(out)-(e.end-e.start)+len(e.text))
		next = append(next, out[:e.start]...)
		next = append(next, e.text...)
		next = append(next, out[e.end:]...)
		out = next
	}
	return out
}

// delegationText is the body that hands the call over to the wrapper.
func delegationText(f *Func) string {
	args := make([]string, len(f.Params))
	for i, p := range f.Params {
		args[i] = p.Name
	}
	call := f.wrapperName() + "(" + strings.Join(args, ", ") + ")"
	if len(f.Results) > 0 {
		return "{ return " + call + " }"
	}
	return "{ " + call + " }"
}

// stripBannerText drops a generated-file banner left by an earlier run.
func stripBannerText(src []byte) []byte {
	first, rest, found := strings.Cut(string(src), "\n")
	if !found || !generatedMarker.MatchString(first) {
		return src
	}
	// Also swallow the blank line the banner was written with.
	if after, ok := strings.CutPrefix(rest, "\n"); ok {
		return []byte(after)
	}
	return []byte(rest)
}

// pruneImports removes the imports the rewritten file no longer refers to,
// which is what keeps it compiling when a removed body was the only user of a
// package. Anything the generator cannot resolve is kept: an unused import is
// a loud compile error, a missing one is a confusing one.
func pruneImports(f *ast.File) {
	used := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})

	var decls []ast.Decl
	var imports []*ast.ImportSpec
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			decls = append(decls, decl)
			continue
		}
		var specs []ast.Spec
		for _, spec := range gd.Specs {
			// An import declaration only holds *ast.ImportSpec nodes.
			is := spec.(*ast.ImportSpec)
			name := ""
			if is.Name != nil {
				name = is.Name.Name
			} else {
				name = importIdent(is.Path.Value)
			}
			if name == "" || name == "_" || name == "." || used[name] {
				specs = append(specs, spec)
				imports = append(imports, is)
			}
		}
		if len(specs) == 0 {
			continue
		}
		gd.Specs = specs
		decls = append(decls, gd)
	}
	f.Decls = decls
	f.Imports = imports
}

// importIdent guesses the name an import is bound to. It returns "" when the
// path does not follow a convention the generator recognises, in which case
// the import is kept.
func importIdent(pathLit string) string {
	p, err := strconv.Unquote(pathLit)
	if err != nil {
		return ""
	}
	base := path.Base(p)
	// Semantic import versioning: gopkg.in/yaml.v3 -> yaml, foo/v3 -> v3.
	if i := strings.LastIndex(base, "."); i > 0 && isVersionSuffix(base[i+1:]) {
		base = base[:i]
	}
	if isVersionSuffix(base) {
		return ""
	}
	// Conventional prefixes/suffixes that the package name omits.
	base = strings.TrimPrefix(base, "go-")
	base = strings.TrimSuffix(base, "-go")
	if !isGoIdent(base) {
		return ""
	}
	return base
}

// isVersionSuffix reports whether s looks like the "v3" of a module path.
func isVersionSuffix(s string) bool {
	if len(s) < 2 || (s[0] != 'v' && s[0] != 'V') {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isGoIdent reports whether s is a valid Go identifier.
func isGoIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
