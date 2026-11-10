package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// coverageEntry is one declaration's place in the coverage report.
type coverageEntry struct {
	// Declaration is the C++ signature a reader can look up.
	Declaration string
	// Module is the library module the declaring header belongs to, read from
	// the include path ("opencv2/core/..." is core).
	Module string
	// Kind is bound, refused, template or not-selected.
	Kind string
	// Reason is the generator's own sentence for a refused declaration.
	Reason string
	// Class is the refusal's classification: the category a fix or a decision
	// belongs to.
	Class string
}

// coverageSummary is one module's counts.
type coverageSummary struct {
	Module      string
	Selected    int
	Bound       int
	Refused     int
	Skipped     int
	Templates   int
	NotSelected int
}

// coverageReport renders the machine-readable summary of what one run bound,
// refused and left alone. It is what makes "the whole module" a measurement
// instead of a claim: the ratio is over the declarations the manifest selected,
// and every refused one carries its classification, so a module cannot drop
// from bound to unbound unnoticed.
//
// The file is written by -coverage and compared by check like any other output.
func (b *builder) coverageReport() string {
	entries := b.coverageEntries()
	summaries := map[string]*coverageSummary{}
	order := make([]string, 0, 4)
	for _, entry := range entries {
		summary := summaries[entry.Module]
		if summary == nil {
			summary = &coverageSummary{Module: entry.Module}
			summaries[entry.Module] = summary
			order = append(order, entry.Module)
		}
		switch entry.Kind {
		case "bound":
			summary.Bound++
		case "refused":
			summary.Refused++
		case "skipped":
			summary.Skipped++
		case "template":
			summary.Templates++
		case "not-selected":
			summary.NotSelected++
		}
		if entry.Kind != "not-selected" {
			summary.Selected++
		}
	}
	sort.Strings(order)

	var out strings.Builder
	fmt.Fprintf(&out, "# cppgo coverage: package %s\n", b.cfg.Package)
	fmt.Fprintf(&out, "# percentage: bound / (bound + refused + template), over the declarations the manifest selected\n")
	for _, header := range b.cfg.Headers {
		fmt.Fprintf(&out, "# header: %s\n", header)
	}
	out.WriteString("#\n# module\tselected\tbound\trefused\tskipped\ttemplates\tnot_selected\tbound_ratio\n")
	for _, module := range order {
		summary := summaries[module]
		ratio := 0.0
		if denominator := summary.Bound + summary.Refused + summary.Templates; denominator > 0 {
			ratio = float64(summary.Bound) / float64(denominator)
		}
		fmt.Fprintf(&out, "%s\t%d\t%d\t%d\t%d\t%d\t%d\t%.4f\n",
			module, summary.Selected, summary.Bound, summary.Refused, summary.Skipped, summary.Templates, summary.NotSelected, ratio)
	}
	out.WriteString("#\n# classification counts (refused declarations)\n#\n# class\tcount\n")
	classes := map[string]int{}
	classOrder := make([]string, 0, 8)
	for _, entry := range entries {
		if entry.Kind != "refused" {
			continue
		}
		if classes[entry.Class] == 0 {
			classOrder = append(classOrder, entry.Class)
		}
		classes[entry.Class]++
	}
	sort.Strings(classOrder)
	for _, class := range classOrder {
		fmt.Fprintf(&out, "%s\t%d\n", class, classes[class])
	}
	out.WriteString("#\n# declarations\tkind\tmodule\tclass\treason\n")
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Declaration < entries[j].Declaration
	})
	for _, entry := range entries {
		if entry.Kind == "bound" {
			continue
		}
		fmt.Fprintf(&out, "%s\t%s\t%s\t%s\t%s\n",
			entry.Declaration, entry.Kind, entry.Module, entry.Class, entry.Reason)
	}
	return out.String()
}

// coverageEntries classifies every declaration the parse reported.
func (b *builder) coverageEntries() []coverageEntry {
	bound := make(map[string]*binding, len(b.bindings))
	for _, binding := range b.bindings {
		bound[symbolKey(binding.sym)] = binding
	}
	symbols := append(append([]cxx.Symbol(nil), b.symbols...), b.synthMembers...)
	entries := make([]coverageEntry, 0, len(symbols))
	for _, sym := range symbols {
		if sym.Access == cxx.AccessPrivate || sym.Access == cxx.AccessProtected {
			// A private or protected member is not part of the library's API.
			continue
		}
		declaration := displaySignature(sym)
		entry := coverageEntry{
			Declaration: declaration,
			Module:      moduleOf(sym.DeclFile),
		}
		key := symbolKey(sym)
		switch {
		case sym.IsTemplate:
			if !b.templateEntrySelected(sym) {
				entry.Kind = "not-selected"
				entry.Class = "not-selected"
				entry.Reason = "the manifest did not name this declaration"
				break
			}
			entry.Kind = "template"
			entry.Class = "template"
			entry.Reason = "a template pattern is bound through -instantiate or the two-phase scan"
		case bound[key] != nil:
			entry.Kind = "bound"
		case b.skipReasons[key] != "":
			entry.Kind = "skipped"
			entry.Class = b.skipClasses[key]
			entry.Reason = b.skipReasons[key]
		case b.selectedSymbols[key]:
			entry.Kind = "refused"
			entry.Reason = b.refusalReasons[key]
			if entry.Reason == "" {
				entry.Reason = "not lowered: the declaration was selected and produced no binding"
			}
			entry.Class = classifyRefusal(declaration, entry.Reason)
		default:
			entry.Kind = "not-selected"
			entry.Class = "not-selected"
			entry.Reason = "the manifest did not name this declaration"
		}
		entries = append(entries, entry)
	}
	return entries
}

// templateEntrySelected reports whether the manifest named this template
// declaration (or everything around it). A template outside the selection is
// not part of the surface the manifest asked for, and the report says so.
func (b *builder) templateEntrySelected(sym cxx.Symbol) bool {
	if len(b.cfg.Bind) == 0 {
		return true
	}
	for _, entry := range b.cfg.Bind {
		if matchesName(entry, sym.QualifiedName) || (sym.Parent != "" && matchesName(entry, sym.Parent)) {
			return true
		}
		if strings.HasPrefix(entry, sym.QualifiedName+"<") {
			return true
		}
	}
	return false
}

// moduleOf reads the library module a declaration belongs to from its header:
// the directory immediately under the library's include root, e.g. core for
// /usr/include/opencv4/opencv2/core/mat.hpp.
func moduleOf(file string) string {
	if file == "" {
		return "unknown"
	}
	parts := strings.Split(strings.ReplaceAll(file, "\\", "/"), "/")
	for i, part := range parts {
		if part == "opencv2" && i+1 < len(parts) {
			return moduleName(parts[i+1])
		}
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "af" {
			return "arrayfire"
		}
		if parts[i] == "Eigen" {
			return "eigen"
		}
	}
	return moduleName(parts[len(parts)-1])
}

// moduleName is one path component made a module name: the named header's own
// file is the module's public header (core.hpp is core), and a directory is
// already the module.
func moduleName(part string) string {
	if part == "" || part == "." || part == ".." {
		return "unknown"
	}
	for _, suffix := range []string{".hpp", ".h", ".hxx"} {
		if strings.HasSuffix(part, suffix) {
			return strings.TrimSuffix(part, suffix)
		}
	}
	return part
}

// classifyRefusal turns the generator's sentence into the category a fix or a
// decision belongs to. The categories are the ones the coverage report names:
// a construct the emitter must learn, an untyped pointer, a GPU-only API, a
// variadic C function, a record carried by value, an unsupported standard
// type, an array, or something the operator has to decide.
func classifyRefusal(declaration, reason string) string {
	switch {
	case strings.Contains(declaration, "cv::cuda::"), strings.Contains(declaration, "cv::ogl::"),
		strings.Contains(declaration, "cv::cudev::"), strings.Contains(declaration, "cv::ocl::"):
		return "gpu-only"
	case strings.Contains(reason, "variadic"):
		return "variadic-c"
	case strings.Contains(reason, "member function template"):
		return "template-needs-instantiation"
	case strings.Contains(reason, "void pointer"), strings.Contains(reason, "af_array"):
		return "untyped-pointer"
	case strings.Contains(reason, "C array"):
		return "c-array"
	case strings.Contains(reason, "standard-library"):
		return "unsupported-std"
	case strings.Contains(reason, "not a POD"), strings.Contains(reason, "layout is unknown"),
		strings.Contains(reason, "passed by value"), strings.Contains(reason, "is not a type this build classifies"):
		return "by-value-record"
	case strings.Contains(reason, "not part of the generated API"),
		strings.Contains(reason, "not a scalar or a known record"),
		strings.Contains(reason, "enum the generated API does not hold"),
		strings.Contains(reason, "points at a record that is not part of"):
		return "unmodeled-type"
	case strings.Contains(reason, "pointer to a record"), strings.Contains(reason, "pointer to an opaque record"):
		return "unowned-pointer"
	case strings.Contains(reason, "operator"):
		return "operator"
	case strings.Contains(reason, "callback"):
		return "callback"
	default:
		return "other"
	}
}
