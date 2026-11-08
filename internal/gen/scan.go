package gen

import (
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// scanInstantiations is the second phase's collection step: it loads the Go
// program named by patterns, and returns every instantiation of the generic
// types and functions this package generated. The type checker is the evidence:
// only an instantiation the program actually type-checks is collected, and the
// argument spellings are the ones the runtime's reflect.Type.String() produces,
// so the Go side and the shim agree on the instantiation key.
//
// outDir is the directory the generic package was written to. A generic object
// declared there belongs to this generated package; the same name from another
// package is not a false positive, because the declaration's position is what
// the match uses.
func scanInstantiations(dir string, patterns []string, outDir string, names map[string]string) ([]Instantiation, error) {
	if len(patterns) == 0 {
		return nil, nil
	}
	if dir == "" {
		dir = "."
	}
	// Abs fails only when the process has no working directory, which no Go
	// program can recover from; the scan's own errors name the pattern.
	absolute, _ := filepath.Abs(dir)
	if outDir != "" {
		outDir, _ = filepath.Abs(outDir)
	}
	environment := append(os.Environ(), "GOFLAGS=-mod=mod", "GOWORK=off")
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		Dir:   absolute,
		Env:   environment,
		Tests: true,
	}, patterns...)
	if err != nil {
		return nil, fmt.Errorf("cppgo: scanning %s: %w", strings.Join(patterns, " "), err)
	}
	if len(loaded) == 0 {
		return nil, fmt.Errorf("cppgo: scanning %s: the pattern matches no Go package", strings.Join(patterns, " "))
	}
	var failures []string
	for _, pkg := range loaded {
		for _, failure := range pkg.Errors {
			failures = append(failures, failure.Error())
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return nil, fmt.Errorf("cppgo: scanning %s: %s", strings.Join(patterns, " "), strings.Join(failures, "; "))
	}
	seen := map[string]bool{}
	var out []Instantiation
	for _, pkg := range loaded {
		for ident, instance := range pkg.TypesInfo.Instances {
			if instance.TypeArgs == nil || instance.TypeArgs.Len() == 0 {
				continue
			}
			object := pkg.TypesInfo.Uses[ident]
			if object == nil {
				object = pkg.TypesInfo.Defs[ident]
			}
			template, ok := generatedTemplate(object, pkg.Fset, outDir, names)
			if !ok {
				continue
			}
			args := make([]string, instance.TypeArgs.Len())
			for i := range args {
				args[i] = types.TypeString(instance.TypeArgs.At(i), func(pkg *types.Package) string {
					if pkg == nil {
						return ""
					}
					return pkg.Name()
				})
			}
			key := template + "|" + strings.Join(args, ",")
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Instantiation{Template: template, Args: args})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Template != out[j].Template {
			return out[i].Template < out[j].Template
		}
		return strings.Join(out[i].Args, ",") < strings.Join(out[j].Args, ",")
	})
	return out, nil
}

// generatedTemplate reports which generated template an instantiated object
// is, and whether it is one at all. The object's declaration has to be inside
// the directory the generic API was written to: two packages may use the same
// Go type name, and the type checker is the only thing that tells them apart.
func generatedTemplate(object types.Object, fset *token.FileSet, outDir string, names map[string]string) (string, bool) {
	if object == nil || object.Pkg() == nil {
		return "", false
	}
	template, ok := names[object.Name()]
	if !ok {
		return "", false
	}
	if outDir != "" {
		file := fset.Position(object.Pos()).Filename
		if file == "" || !withinDir(file, outDir) {
			return "", false
		}
	}
	return template, true
}

// withinDir reports whether file is inside dir. Both are absolute paths from
// the type checker and from filepath.Abs, so the comparison is the check.
func withinDir(file, dir string) bool {
	return strings.HasPrefix(filepath.Clean(file), filepath.Clean(dir)+string(filepath.Separator))
}
