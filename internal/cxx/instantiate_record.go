package cxx

import (
	"strconv"
	"strings"
)

// InstantiateRecord builds the model of a concrete specialization of a class
// template from the pattern the header declares.
//
// libclang reports a specialization only when the translation unit happens to
// declare one: an implicit instantiation has no definition cursor, and the
// explicit `template class ...;` line a wrapper adds is not part of a library's
// own headers. The generator still has to bind the types a library's own
// functions use — cv::Scalar, cv::Point, cv::Size, cv::Rect — so this reads them
// out of the pattern instead: the fields and members with the arguments
// substituted, and the layout computed the way the C++ standard lays out a
// standard-layout aggregate.
//
// The result is honest about its limits: a pattern whose layout cannot be
// computed from its fields (a dependent base, a bitfield, a member the model
// cannot size) yields a record with SizeBytes -1, and the generator refuses to
// allocate it rather than guessing.
func (p *Project) InstantiateRecord(templateName string, args []TemplateArgument) (Record, bool) {
	p.mu.Lock()
	tus := append([]*TU(nil), p.tus...)
	p.mu.Unlock()
	for _, tu := range tus {
		if rec, ok := tu.instantiateRecord(templateName, args, 0); ok {
			return rec, true
		}
	}
	return Record{}, false
}

// instantiateRecord specializes one pattern.
func (tu *TU) instantiateRecord(templateName string, args []TemplateArgument, depth int) (Record, bool) {
	if depth > 16 {
		return Record{}, false
	}
	pattern, ok := tu.recordBySpelling(templateName)
	if !ok || !pattern.IsTemplate || len(pattern.TemplateParameters) != len(args) {
		return Record{}, false
	}
	templateName = pattern.QualifiedName
	name := specializationName(templateName, args)
	if tu.instantiating != nil && tu.instantiating[name] {
		return Record{}, false
	}
	if tu.instantiating == nil {
		tu.instantiating = make(map[string]bool)
	}
	tu.instantiating[name] = true
	defer delete(tu.instantiating, name)
	if tu.builtRecords != nil && tu.builtRecords[name] {
		if existing, ok := tu.Record(name); ok {
			return existing, true
		}
	}
	sub := substitution{
		names:   make(map[string]TemplateArgument, len(args)),
		indexed: append([]TemplateArgument(nil), args...),
	}
	for i, param := range pattern.TemplateParameters {
		if param.Name != "" {
			sub.names[param.Name] = args[i]
		}
	}
	rec := Record{
		QualifiedName:            name,
		Kind:                     pattern.Kind,
		IsTemplateSpecialization: true,
		Synthesized:              true,
		TemplateName:             templateName,
		TemplateArguments:        append([]TemplateArgument(nil), args...),
		DeclFile:                 pattern.DeclFile,
		DeclLine:                 pattern.DeclLine,
		SizeBytes:                -1,
		AlignBytes:               -1,
	}
	for _, field := range pattern.Fields {
		field.Type = tu.normalizeParam(sub.param(field.Type), depth)
		rec.Fields = append(rec.Fields, field)
	}
	for _, base := range pattern.Bases {
		if base.Spelling != "" {
			base.QualifiedName = qualifyBaseName(sub.text(base.Spelling), templateName)
		} else {
			base.QualifiedName = sub.text(base.QualifiedName)
		}
		rec.Bases = append(rec.Bases, base)
	}
	// The layout is computed from the fields and bases alone, and the record is
	// registered before its members are lowered: a member's own signature names
	// the specialization ("const Point_<int> &"), and the substitution has to
	// find the type this run is building.
	tu.layoutInstantiation(&rec, depth)
	tu.registerInstantiated(rec)
	members := pattern.Members
	if len(members) == 0 {
		// A class template's members are collected by the symbol walk with
		// IsTemplate set, not into the pattern record: read them from the
		// translation unit's symbol list, which is where the walk put them.
		for _, sym := range tu.symbols {
			if sym.Parent != templateName || !sym.IsTemplate {
				continue
			}
			if len(sym.TemplateParameters) > 0 {
				// A member template of a class template is a second parameter
				// list the manifest would have to name; the specialization's
				// callers cannot reach it here.
				continue
			}
			members = append(members, sym)
		}
	}
	for _, member := range members {
		m := member
		m.Parent = name
		m.QualifiedName = name + "::" + simpleMemberName(member.QualifiedName)
		m.IsTemplate = false
		// The specialization has no declaration cursor in the translation unit,
		// so it has no mangled name: the generator writes the call in C++.
		m.Mangled = ""
		m.HasOutOfLineBody = false
		m.Result = tu.normalizeParam(sub.param(member.Result), depth)
		params := make([]Param, len(member.Params))
		for i, param := range member.Params {
			params[i] = tu.normalizeParam(sub.param(param), depth)
		}
		m.Params = params
		rec.Members = append(rec.Members, m)
	}
	tu.applyConstructibility(&rec)
	tu.registerInstantiated(rec)
	return rec, true
}

// applyConstructibility records the two facts the generator's factories depend
// on: whether a zero-argument constructor exists and whether the copy
// constructor can be called.
func (tu *TU) applyConstructibility(rec *Record) {
	for _, member := range rec.Members {
		if member.Kind != Constructor {
			continue
		}
		if len(member.Params) == 0 && !member.IsDeleted {
			rec.IsDefaultConstructible = true
		}
		if member.IsCopyConstructor && !member.IsDeleted {
			rec.IsCopyConstructible = true
		}
	}
	if rec.IsTrivial {
		rec.IsCopyConstructible = true
	}
}

// registerInstantiated stores the record this run built, so a later lookup by
// name (a member's own signature, a base, another specialization) finds it.
func (tu *TU) registerInstantiated(rec Record) {
	if tu.builtRecords == nil {
		tu.builtRecords = make(map[string]bool)
	}
	tu.builtRecords[rec.QualifiedName] = true
	if _, exists := tu.byRecord[rec.QualifiedName]; !exists {
		tu.byRecord[rec.QualifiedName] = len(tu.records)
		tu.records = append(tu.records, rec)
		return
	}
	if i := tu.byRecord[rec.QualifiedName]; i >= 0 && i < len(tu.records) {
		tu.records[i] = rec
	}
}

// simpleMemberName is the member's own name: the last "::" component, kept
// whole for a destructor or a conversion operator.
func simpleMemberName(qualified string) string {
	if i := strings.LastIndex(qualified, "::"); i >= 0 {
		return qualified[i+2:]
	}
	return qualified
}

// layoutInstantiation computes the size, alignment and field offsets of a
// standard-layout specialization from its substituted fields, and records
// whether the result can be a Go struct.
func (tu *TU) layoutInstantiation(rec *Record, depth int) bool {
	offset := int64(0)
	align := int64(1)
	pod := true
	// A base subobject comes first: Scalar_ derives from Vec, whose own base
	// carries the four coordinates, and the derived record adds nothing. The
	// base's fields are flattened into the derived record's list with their
	// offsets shifted, so the Go struct is the C layout whether a field was
	// declared here or in a base.
	own := rec.Fields
	rec.Fields = nil
	for i := range rec.Bases {
		base := &rec.Bases[i]
		if base.IsVirtual {
			return false
		}
		size, baseAlign, ok := tu.typeSizeFor(Param{Kind: TypeRecord, ClangKind: "Record", Spelling: base.QualifiedName, RecordName: base.QualifiedName}, depth+1)
		if !ok {
			return false
		}
		baseOffset := alignUp64(offset, baseAlign)
		base.OffsetBytes = baseOffset
		offset = baseOffset + size
		if baseAlign > align {
			align = baseAlign
		}
		inner, ok := tu.instantiatedRecordByName(base.QualifiedName, depth+1)
		if !ok {
			pod = false
			continue
		}
		if !inner.IsPOD {
			pod = false
		}
		for _, field := range inner.Fields {
			if field.Access != AccessPublic {
				pod = false
				continue
			}
			shifted := field
			if shifted.OffsetBytes >= 0 {
				shifted.OffsetBytes += baseOffset
			}
			rec.Fields = append(rec.Fields, shifted)
		}
	}
	rec.Fields = append(rec.Fields, own...)
	for i := range rec.Fields {
		field := &rec.Fields[i]
		if field.OffsetBytes >= 0 {
			// A base field already carries its offset; the layout walk leaves
			// it where the base put it.
			if size, _, ok := tu.typeSizeFor(field.Type, depth+1); ok {
				if end := field.OffsetBytes + size; end > offset {
					offset = end
				}
			}
			continue
		}
		size, fieldAlign, ok := tu.typeSizeFor(field.Type, depth+1)
		if !ok {
			return false
		}
		offset = alignUp64(offset, fieldAlign)
		field.OffsetBytes = offset
		offset += size
		if fieldAlign > align {
			align = fieldAlign
		}
	}
	for i := range rec.Fields {
		switch rec.Fields[i].Type.Kind {
		case TypeInteger, TypeFloat, TypePointer, TypeArray:
		case TypeRecord:
			if inner, ok := tu.Record(rec.Fields[i].Type.RecordName); !ok || !inner.IsPOD {
				pod = false
			}
		default:
			pod = false
		}
	}
	rec.SizeBytes = alignUp64(offset, align)
	rec.AlignBytes = align
	rec.LayoutKnown = true
	rec.IsPOD = pod
	rec.IsTrivial = pod
	rec.IsPolymorphic = false
	for _, member := range rec.Members {
		if member.Kind == Destructor && !member.IsDefaulted {
			rec.IsTrivial = false
			rec.IsPOD = false
		}
		if member.Kind == Constructor && len(member.Params) == 0 && !member.IsDeleted {
			rec.IsDefaultConstructible = true
		}
		if member.Kind == Constructor && member.IsCopyConstructor && !member.IsDeleted {
			rec.IsCopyConstructible = true
		}
	}
	if rec.IsTrivial {
		rec.IsCopyConstructible = true
	}
	return true
}

// instantiatedRecordByName resolves a base or field record by its substituted
// name: a registered specialization is returned as it was built, and one the
// model has not seen is built on demand.
func (tu *TU) instantiatedRecordByName(name string, depth int) (Record, bool) {
	if rec, ok := tu.recordBySpelling(name); ok && rec.SizeBytes > 0 && len(rec.Fields) > 0 {
		return rec, true
	}
	templateName, args, ok := splitSpecialization(name)
	if !ok {
		return Record{}, false
	}
	return tu.instantiateRecord(templateName, args, depth)
}

// typeSizeFor is the size and alignment one substituted field type occupies in
// the layout: a scalar's width, a pointer's eight bytes, an array's elements, or
// another record's layout (instantiating it on demand when it is a template
// specialization).
func (tu *TU) typeSizeFor(p Param, depth int) (size, align int64, ok bool) {
	if depth > 16 {
		return 0, 0, false
	}
	switch p.Kind {
	case TypeInteger, TypeFloat, TypeEnum:
		if p.SizeBytes <= 0 {
			return 0, 0, false
		}
		align = p.SizeBytes
		if align > 8 && p.ClangKind != "LongDouble" {
			// An integer wider than eight bytes (__int128) still aligns to 16;
			// everything else caps at 8 on x86-64.
			align = 8
		}
		return p.SizeBytes, align, true
	case TypePointer:
		return 8, 8, true
	case TypeArray:
		if p.Element == nil || p.ArraySize <= 0 {
			return 0, 0, false
		}
		elemSize, elemAlign, ok := tu.typeSizeFor(*p.Element, depth+1)
		if !ok {
			return 0, 0, false
		}
		return elemSize * p.ArraySize, elemAlign, true
	case TypeRecord:
		if record, ok := tu.recordBySpelling(p.RecordName); ok && record.SizeBytes > 0 {
			return record.SizeBytes, record.AlignBytes, true
		}
		templateName, args, ok := splitSpecialization(p.RecordName)
		if !ok {
			return 0, 0, false
		}
		rec, ok := tu.instantiateRecord(templateName, args, depth+1)
		if !ok || rec.SizeBytes <= 0 {
			return 0, 0, false
		}
		return rec.SizeBytes, rec.AlignBytes, true
	default:
		return 0, 0, false
	}
}

// qualifyBaseName qualifies an unqualified base spelling with the position of
// the pattern that declared it: "Vec<double, 4>" inside cv is cv::Vec.
func qualifyBaseName(name, pattern string) string {
	name = strings.TrimPrefix(name, "class ")
	name = strings.TrimPrefix(name, "struct ")
	if strings.Contains(name, "::") {
		return name
	}
	if i := strings.LastIndex(pattern, "::"); i >= 0 {
		return pattern[:i+2] + name
	}
	return name
}

// recordBySpelling finds a record by its qualified name, or by its simple name
// when a substitution produced an unqualified spelling ("Vec<double, 4>" inside
// a Vec).
func (tu *TU) recordBySpelling(name string) (Record, bool) {
	if rec, ok := tu.Record(name); ok {
		return rec, true
	}
	wanted := strings.ReplaceAll(name, " ", "")
	var found Record
	seen := false
	for _, rec := range tu.records {
		if strings.ReplaceAll(simpleMemberName(rec.QualifiedName), " ", "") != wanted {
			continue
		}
		if seen {
			return Record{}, false
		}
		found = rec
		seen = true
	}
	return found, seen
}

// splitSpecialization turns "cv::Point_<int>" into "cv::Point_" and its
// argument list.
func splitSpecialization(name string) (string, []TemplateArgument, bool) {
	open := strings.Index(name, "<")
	if open <= 0 || !strings.HasSuffix(name, ">") {
		return "", nil, false
	}
	templateName := strings.TrimSpace(name[:open])
	parts := splitTemplateArgs(name[open+1 : len(name)-1])
	args := make([]TemplateArgument, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if value, err := strconv.ParseInt(part, 0, 64); err == nil {
			args = append(args, TemplateArgument{Kind: TemplateArgumentIntegral, Spelling: part, Value: value, UnsignedValue: uint64(value)})
			continue
		}
		if param, ok := ScalarType(part); ok {
			args = append(args, TemplateArgument{Kind: TemplateArgumentType, Spelling: param.Spelling, Type: param})
			continue
		}
		args = append(args, TemplateArgument{
			Kind:     TemplateArgumentType,
			Spelling: part,
			Type:     Param{Kind: TypeRecord, ClangKind: "Record", Spelling: part, RecordName: part, SizeBytes: -1},
		})
	}
	return templateName, args, true
}

// alignUp64 rounds offset up to alignment.
func alignUp64(offset, alignment int64) int64 {
	if alignment <= 1 {
		return offset
	}
	remainder := offset % alignment
	if remainder == 0 {
		return offset
	}
	return offset + alignment - remainder
}

// ParseTemplateArgument reads one type argument the manifest wrote, for a
// specialization whose argument is a record ("cv::Point_<int>") rather than a
// scalar. It carries the spelling; the model resolves it by name.
func ParseTemplateArgument(spelling string) (TemplateArgument, bool) {
	spelling = strings.TrimSpace(spelling)
	if param, ok := ScalarType(spelling); ok {
		return TemplateArgument{Kind: TemplateArgumentType, Spelling: param.Spelling, Type: param}, true
	}
	if spelling == "" || strconv.Quote(spelling) == `""` {
		return TemplateArgument{}, false
	}
	return TemplateArgument{
		Kind:     TemplateArgumentType,
		Spelling: spelling,
		Type:     Param{Kind: TypeRecord, ClangKind: "Record", Spelling: spelling, RecordName: spelling, SizeBytes: -1},
	}, true
}

// normalizeParam re-resolves a parameter whose substitution left it
// unclassified. A dependent type libclang spelled "Scalar_<_Tp>" becomes
// TypeOther with a spelling and no kind, and a dependent array
// "double[4 * 1]" keeps no element and no count; both are concrete after the
// substitution, and the type map needs the kind and the size to carry them.
func (tu *TU) normalizeParam(p Param, depth int) Param {
	switch p.Kind {
	case TypePointer:
		if p.Pointee != nil {
			pointee := tu.normalizeParam(*p.Pointee, depth+1)
			p.Pointee = &pointee
			if p.Pointee.Kind == TypePointer && p.Pointee.Pointee != nil {
				// A reference to a reference collapses in C++, but a spelling
				// substitution can produce one; the pointer stays the outer
				// form.
			}
		}
		return p
	case TypeArray:
		if p.ArraySize <= 0 || p.Element == nil || p.Element.Kind == TypeOther || p.Element.SizeBytes <= 0 {
			if element, count, ok := splitArraySpelling(p.Spelling); ok {
				if p.ArraySize <= 0 {
					if value, ok := evalConstant(count); ok {
						p.ArraySize = value
					}
				}
				if resolved, ok := tu.resolveTypeSpelling(element, depth+1); ok {
					p.Element = &resolved
				}
			}
		}
		if p.Element != nil && p.SizeBytes <= 0 && p.ArraySize > 0 {
			if size, _, ok := tu.typeSizeFor(*p.Element, depth+1); ok {
				p.SizeBytes = size * p.ArraySize
				p.ClangKind = "ConstantArray"
			}
		}
		return p
	case TypeOther, "":
		if resolved, ok := tu.resolveTypeSpelling(p.Spelling, depth+1); ok {
			resolved.IsConst = resolved.IsConst || p.IsConst
			resolved.Name = p.Name
			return resolved
		}
		return p
	}
	return p
}

// resolveTypeSpelling classifies a substituted type spelling: a scalar, a
// pointer or reference to one, or a record the model knows (a plain record or a
// specialization it built on demand).
func (tu *TU) resolveTypeSpelling(spelling string, depth int) (Param, bool) {
	s := strings.TrimSpace(spelling)
	isConst := false
	for {
		switch {
		case strings.HasPrefix(s, "const "):
			isConst = true
			s = strings.TrimSpace(s[len("const "):])
		case strings.HasPrefix(s, "volatile "):
			s = strings.TrimSpace(s[len("volatile "):])
		case strings.HasPrefix(s, "class "):
			s = strings.TrimSpace(s[len("class "):])
		case strings.HasPrefix(s, "struct "):
			s = strings.TrimSpace(s[len("struct "):])
		default:
			goto trimmed
		}
	}
trimmed:
	for _, suffix := range []string{"&&", "&", "*"} {
		if strings.HasSuffix(s, suffix) {
			pointee, ok := tu.resolveTypeSpelling(strings.TrimSuffix(s, suffix), depth+1)
			if !ok {
				return Param{}, false
			}
			pointee.IsConst = isConst
			return Param{
				Kind:      TypePointer,
				ClangKind: "LValueReference",
				Spelling:  strings.TrimSpace(spelling),
				SizeBytes: 8,
				Ptr:       true,
				Pointee:   &pointee,
			}, true
		}
	}
	if param, ok := ScalarType(s); ok {
		param.IsConst = isConst
		param.Spelling = s
		return param, true
	}
	if rec, ok := tu.instantiatedRecordByName(s, depth+1); ok && rec.SizeBytes > 0 {
		return Param{
			Kind:       TypeRecord,
			ClangKind:  "Record",
			Spelling:   s,
			RecordName: rec.QualifiedName,
			SizeBytes:  rec.SizeBytes,
			IsConst:    isConst,
		}, true
	}
	if rec, ok := tu.recordBySpelling(s); ok {
		return Param{
			Kind:       TypeRecord,
			ClangKind:  "Record",
			Spelling:   s,
			RecordName: rec.QualifiedName,
			SizeBytes:  rec.SizeBytes,
			IsConst:    isConst,
		}, true
	}
	return Param{}, false
}

// splitArraySpelling splits "double[4]" into "double" and "4".
func splitArraySpelling(spelling string) (element, count string, ok bool) {
	open := strings.Index(spelling, "[")
	if open <= 0 || !strings.HasSuffix(spelling, "]") {
		return "", "", false
	}
	return strings.TrimSpace(spelling[:open]), strings.TrimSpace(spelling[open+1 : len(spelling)-1]), true
}

// evalConstant evaluates the integer expression a dependent array size leaves
// behind once the template arguments are substituted: "4", "4 * 1", "2 + 2".
func evalConstant(expr string) (int64, bool) {
	p := &constParser{text: strings.ReplaceAll(expr, " ", "")}
	value, ok := p.parseSum()
	if !ok || p.pos != len(p.text) {
		return 0, false
	}
	return value, true
}

// constParser is a recursive-descent evaluator for the arithmetic a substituted
// array size uses.
type constParser struct {
	text string
	pos  int
}

func (p *constParser) parseSum() (int64, bool) {
	value, ok := p.parseProduct()
	if !ok {
		return 0, false
	}
	for p.pos < len(p.text) && (p.text[p.pos] == '+' || p.text[p.pos] == '-') {
		op := p.text[p.pos]
		p.pos++
		right, ok := p.parseProduct()
		if !ok {
			return 0, false
		}
		if op == '+' {
			value += right
		} else {
			value -= right
		}
	}
	return value, true
}

func (p *constParser) parseProduct() (int64, bool) {
	value, ok := p.parseAtom()
	if !ok {
		return 0, false
	}
	for p.pos < len(p.text) && (p.text[p.pos] == '*' || p.text[p.pos] == '/') {
		op := p.text[p.pos]
		p.pos++
		right, ok := p.parseAtom()
		if !ok || (op == '/' && right == 0) {
			return 0, false
		}
		if op == '*' {
			value *= right
		} else {
			value /= right
		}
	}
	return value, true
}

func (p *constParser) parseAtom() (int64, bool) {
	if p.pos < len(p.text) && p.text[p.pos] == '(' {
		p.pos++
		value, ok := p.parseSum()
		if !ok || p.pos >= len(p.text) || p.text[p.pos] != ')' {
			return 0, false
		}
		p.pos++
		return value, true
	}
	start := p.pos
	for p.pos < len(p.text) && p.text[p.pos] >= '0' && p.text[p.pos] <= '9' {
		p.pos++
	}
	if start == p.pos {
		// A C suffix ("4u", "4UL") or a name the substitution did not resolve.
		for p.pos < len(p.text) && (p.text[p.pos] == 'u' || p.text[p.pos] == 'U' || p.text[p.pos] == 'l' || p.text[p.pos] == 'L') {
			p.pos++
		}
		if start == p.pos {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(p.text[start:p.pos], 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}
