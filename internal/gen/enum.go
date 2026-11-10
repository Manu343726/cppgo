package gen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// enumModel is one bound C++ enum: a Go named integer type with one typed
// constant per enumerator.
type enumModel struct {
	// GoName is the exported Go type name, prefixed by the outer record when
	// the enum is nested (Circle::Kind is CircleKind).
	GoName string
	// CxxName is the qualified C++ name.
	CxxName string
	// Go and C are the underlying integer type on each side: int32 and
	// int32_t, uint8 and uint8_t, ...
	Go       string
	C        string
	Unsigned bool
	// Constants are the enumerators in declaration order.
	Constants []*enumConstant
	// Doc is the definition's comment, normalised for Go.
	Doc string
	// selected is set by the binding manifest.
	selected bool
}

// enumConstant is one enumerator as a Go constant.
type enumConstant struct {
	// GoName is the constant's Go name: <EnumGoName><Enumerator>.
	GoName string
	// CxxName is the qualified C++ enumerator name.
	CxxName string
	// Doc is the enumerator's own comment, when the header gives one.
	Doc string
	// Value is the value, read as signed and unsigned.
	Value         int64
	UnsignedValue uint64
}

// literal renders the enumerator's value as a Go untyped constant.
func (c *enumConstant) literal(unsigned bool) string {
	if unsigned {
		return strconv.FormatUint(c.UnsignedValue, 10)
	}
	return strconv.FormatInt(c.Value, 10)
}

// assignTypeNames computes the Go type name of every record and enum before any
// model is built. A Go package cannot nest a type inside a type, so a type
// declared inside a record carries the record's Go name as a prefix:
// mathx::Circle::Tag is CircleTag and mathx::Circle::Kind is CircleKind. The
// namespaces leave the name unchanged, as DESIGN.md states.
func (b *builder) assignTypeNames() {
	records := append([]cxx.Record(nil), b.rawRecords...)
	sort.Slice(records, func(i, j int) bool { return records[i].QualifiedName < records[j].QualifiedName })
	b.recordGoNames = make(map[string]string, len(records))
	for _, rec := range records {
		if rec.IsTemplate || rec.QualifiedName == "" {
			// A template pattern is the generic API's input, not a Go type.
			continue
		}
		if _, done := b.recordGoNames[rec.QualifiedName]; done {
			continue
		}
		b.recordGoNames[rec.QualifiedName] = b.nestPrefix(rec.QualifiedName, b.recordGoNames) + goRecordName(rec.QualifiedName)
	}
	b.enumGoNames = make(map[string]string, len(b.rawEnums))
	for _, enum := range b.rawEnums {
		if enum.QualifiedName == "" {
			continue
		}
		if _, done := b.enumGoNames[enum.QualifiedName]; done {
			continue
		}
		b.enumGoNames[enum.QualifiedName] = b.nestPrefix(enum.QualifiedName, b.recordGoNames) + exportName(simpleName(enum.QualifiedName))
	}
}

// nestPrefix returns the Go name of the innermost record that encloses a
// qualified name, or "" when the type is at namespace scope. Every prefix of the
// name at a "::" boundary is checked against the records already named, which is
// what keeps a template argument's own "::" from being read as nesting.
func (b *builder) nestPrefix(qualified string, records map[string]string) string {
	best, bestLen := "", 0
	for i := 0; i+1 < len(qualified); i++ {
		if qualified[i] != ':' || qualified[i+1] != ':' {
			continue
		}
		prefix := qualified[:i]
		if name, ok := records[prefix]; ok && len(prefix) > bestLen {
			best, bestLen = name, len(prefix)
		}
	}
	return best
}

// buildEnums turns every enum definition into an enumModel.
func (b *builder) buildEnums() {
	seen := map[string]bool{}
	for _, enum := range b.rawEnums {
		if enum.QualifiedName == "" || seen[enum.QualifiedName] {
			continue
		}
		seen[enum.QualifiedName] = true
		if !enum.ConstantsKnown {
			b.warnings = append(b.warnings, fmt.Sprintf(
				"%s is not bound: the enumerator values of a template specialization's enum cannot be read from the pattern; give them literal initialisers",
				enum.QualifiedName))
			continue
		}
		model, err := b.enumModelFor(enum)
		if err != nil {
			// One enum of a record the caller asked for as a whole: the record
			// stays usable, the enum is reported, and a signature that names it
			// fails on its own.
			b.warnings = append(b.warnings, fmt.Sprintf("%s is not bound: %v", enum.QualifiedName, err))
			seen[enum.QualifiedName] = false
			continue
		}
		b.enums[enum.QualifiedName] = model
		b.enumOrder = append(b.enumOrder, enum.QualifiedName)
	}
	sort.Strings(b.enumOrder)
}

// enumModelFor classifies one enum's underlying type and names its constants.
func (b *builder) enumModelFor(enum cxx.Enum) (*enumModel, error) {
	underlying := enum.Underlying
	if underlying.Kind != cxx.TypeInteger {
		return nil, pending(fmt.Sprintf("the underlying type %q of the enum is not an integer", underlying.Spelling),
			"fix the enum's underlying type in C++")
	}
	goName, cName, ok := integerType(underlying.SizeBytes, underlying.Unsigned)
	if !ok {
		return nil, pending(fmt.Sprintf("the enum's underlying type %q has no Go value type", underlying.Spelling),
			"widen or narrow the enum's underlying type at the C++ boundary")
	}
	model := &enumModel{
		GoName:   b.enumGoNames[enum.QualifiedName],
		CxxName:  enum.QualifiedName,
		Go:       goName,
		C:        cName,
		Unsigned: underlying.Unsigned,
		Doc:      enum.Comment,
		selected: true,
	}
	// Two enumerators can name one value: BORDER_REFLECT_101 and BORDER_DEFAULT
	// are the same constant, and exportName strips the underscore that tells
	// their names apart. The alias is not a second Go constant — it is the same
	// value under another C++ name — so the first spelling keeps the Go name and
	// the alias is dropped. Two different values that fold to one name are a
	// real collision and are reported.
	byName := make(map[string]*enumConstant, len(enum.Constants))
	for _, constant := range enum.Constants {
		goName := model.GoName + exportName(constant.Name)
		if existing, taken := byName[goName]; taken {
			if existing.Value == constant.Value && existing.UnsignedValue == constant.UnsignedValue {
				b.warnings = append(b.warnings, fmt.Sprintf(
					"%s::%s is the same value as %s; the Go constant keeps the first spelling",
					enum.QualifiedName, constant.Name, existing.CxxName))
				continue
			}
			return nil, pending(fmt.Sprintf("the enumerators %s and %s both map to Go name %s", existing.CxxName, constant.Name, goName),
				"rename one with -rename '<signature>=<GoName>'")
		}
		constantModel := &enumConstant{
			GoName:        goName,
			CxxName:       enum.QualifiedName + "::" + constant.Name,
			Doc:           constant.Comment,
			Value:         constant.Value,
			UnsignedValue: constant.UnsignedValue,
		}
		byName[goName] = constantModel
		model.Constants = append(model.Constants, constantModel)
	}
	return model, nil
}

// selectEnums narrows the enums the manifest binds. With no -bind every enum
// is a root; a -bind entry that names an enum keeps it, and an enum a kept
// binding's signature names is kept as part of the type closure.
func (b *builder) selectEnums(bindMatched []bool) {
	needed := map[*enumModel]bool{}
	if len(b.cfg.Bind) == 0 {
		for _, enum := range b.enums {
			needed[enum] = true
		}
	}
	for i, entry := range b.cfg.Bind {
		for _, enum := range b.enums {
			if matchesName(entry, enum.CxxName) {
				bindMatched[i] = true
				needed[enum] = true
			}
		}
	}
	for i, entry := range b.cfg.Exclude {
		for _, enum := range b.enums {
			if matchesName(entry, enum.CxxName) {
				_ = i
				needed[enum] = false
			}
		}
	}
	var add func(*goType)
	add = func(t *goType) {
		if t == nil {
			return
		}
		if t.Enum != nil {
			needed[t.Enum] = true
		}
		add(t.Elem)
	}
	for _, binding := range b.bindings {
		if !binding.selected {
			continue
		}
		for _, param := range binding.Params {
			add(param.Type)
		}
		add(binding.Result)
	}
	for _, name := range b.order {
		for _, field := range b.records[name].Fields {
			if field.Type.Enum != nil {
				needed[field.Type.Enum] = true
			}
		}
	}
	for _, enum := range b.enums {
		enum.selected = needed[enum]
	}
}

// selectedEnums is the enums to emit, in Go name order.
func (b *builder) selectedEnums() []*enumModel {
	out := make([]*enumModel, 0, len(b.enums))
	for _, name := range b.enumOrder {
		if b.enums[name].selected {
			out = append(out, b.enums[name])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GoName < out[j].GoName })
	return out
}

// emitEnum writes the named Go integer type and its typed constants.
func (m *packageModel) emitEnum(b *strings.Builder, enum *enumModel) {
	fmt.Fprintf(b, "\n%s\n", docBlock(enum.GoName, fmt.Sprintf("%s is %s, an enum with underlying type %s.", enum.GoName, enum.CxxName, enum.Go), enum.Doc))
	fmt.Fprintf(b, "type %s %s\n\n", enum.GoName, enum.Go)
	if len(enum.Constants) == 0 {
		return
	}
	b.WriteString("const (\n")
	for _, constant := range enum.Constants {
		if doc := commentLines(constant.Doc); doc != "" {
			fmt.Fprintf(b, "%s", indentLines(doc, "\t"))
		}
		fmt.Fprintf(b, "\t%s %s = %s\n", constant.GoName, enum.GoName, constant.literal(enum.Unsigned))
	}
	b.WriteString(")\n")
}
