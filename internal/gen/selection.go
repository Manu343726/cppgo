package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// applySelection filters the bindings and records named by -bind and -exclude.
// With no -bind every declaration is a root. A type named by a kept binding is
// kept even when it is not a root: that is the type closure.
func (b *builder) applySelection() error {
	// The pre-selection already recorded the entries that named a declaration,
	// including the declarations it did not lower.
	bindMatched := b.bindMatched
	excludeMatched := b.excludeMatched

	for _, binding := range b.bindings {
		keep := len(b.cfg.Bind) == 0
		if binding.Owner != nil && b.instantiated[binding.Owner.CxxName] {
			// The manifest named the specialization with -instantiate, which
			// selects the type and with it the members the type declares.
			keep = true
		}
		if b.requestFor(binding.sym) != nil {
			// A function instantiation is a root of its own: the entry asked for
			// that declaration by name, and there is no record to carry it.
			keep = true
		}
		for i, entry := range b.cfg.Bind {
			if bindingMatches(entry, binding) {
				bindMatched[i] = true
				keep = true
			}
		}
		for i, entry := range b.cfg.Exclude {
			if bindingMatches(entry, binding) {
				excludeMatched[i] = true
				keep = false
			}
		}
		binding.selected = keep
	}

	// A record is part of the API when it is a root of its own, when one of its
	// members survived, or when a kept signature names it.
	needed := map[*recordModel]bool{}
	for _, name := range b.order {
		record := b.records[name]
		root := len(b.cfg.Bind) == 0
		if b.neededRecords[record.CxxName] || b.instantiated[record.CxxName] {
			// A record the manifest named with -instantiate, or the receiver of
			// an instantiated member, is part of the API whether or not it was
			// also named with -bind.
			root = true
		}
		for i, entry := range b.cfg.Bind {
			if matchesName(entry, record.CxxName) {
				bindMatched[i] = true
				root = true
			}
		}
		for i, entry := range b.cfg.Exclude {
			if matchesName(entry, record.CxxName) {
				excludeMatched[i] = true
				root = false
			}
		}
		if root {
			needed[record] = true
		}
	}
	// The enums are selected here, before the unmatched-entry check, because an
	// entry may name an enum and nothing else.
	b.selectEnums(bindMatched)
	for i, entry := range b.cfg.Bind {
		if !bindMatched[i] {
			return fmt.Errorf("cppgo: -bind %s: it names nothing this header declares", entry)
		}
	}
	for i, entry := range b.cfg.Exclude {
		if !excludeMatched[i] {
			return fmt.Errorf("cppgo: -exclude %s: it names nothing this header declares", entry)
		}
	}
	for _, binding := range b.bindings {
		if !binding.selected {
			continue
		}
		if binding.Owner != nil {
			// A selected member needs its record's type even when the record
			// itself was not a root.
			needed[binding.Owner] = true
		}
		addReferenced(binding, needed)
	}
	keptRecords := make([]string, 0, len(b.order))
	for _, name := range b.order {
		if needed[b.records[name]] {
			keptRecords = append(keptRecords, name)
		}
	}
	b.order = keptRecords
	b.selectedRecords = make(map[string]bool, len(keptRecords))
	for _, name := range keptRecords {
		b.selectedRecords[name] = true
	}

	kept := b.bindings[:0]
	for _, binding := range b.bindings {
		if binding.Owner != nil && !needed[binding.Owner] {
			continue
		}
		if !binding.selected {
			continue
		}
		kept = append(kept, binding)
	}
	b.bindings = kept
	b.reattach()
	return nil
}

// bindingMatches reports whether a manifest entry names the binding: the
// declaration itself — by qualified name or by full signature — or the record
// it is a member of.
func bindingMatches(entry string, binding *binding) bool {
	if matchesSignature(entry, binding.sym) {
		return true
	}
	if matchesName(entry, binding.sym.QualifiedName) {
		return true
	}
	return binding.Owner != nil && matchesName(entry, binding.Owner.CxxName)
}

// matchesSignature reports whether a manifest entry spells one declaration's
// full signature, which is how a single overload of a set is named:
//
//	-exclude 'cv::Mat::Mat(int, int, int, void *)'
//
// Whitespace is ignored on both sides, because a reader writes the signature
// with or without the spaces libclang puts around a pointer's star, and the
// trailing qualifiers are optional: an entry matches the declaration as it is
// written (without `const` or `noexcept`) as well as the full signature the
// report shows. Only an entry that looks like a signature (it names a parameter
// list) is compared this way; a plain qualified name stays a prefix selection.
func matchesSignature(entry string, sym cxx.Symbol) bool {
	if !strings.Contains(entry, "(") {
		return false
	}
	want := normalizeSignature(entry)
	return want == normalizeSignature(displaySignature(sym)) ||
		want == normalizeSignature(bareSignature(sym))
}

// bareSignature is the declaration without the trailing const and noexcept
// qualifiers: the way a reader writes it in a manifest entry.
func bareSignature(sym cxx.Symbol) string {
	params := make([]string, len(sym.Params))
	for i, param := range sym.Params {
		params[i] = param.Spelling
	}
	return sym.QualifiedName + "(" + strings.Join(params, ", ") + ")"
}

// normalizeSignature removes the whitespace libclang and a reader spell
// differently.
func normalizeSignature(signature string) string {
	return strings.Join(strings.Fields(signature), "")
}

// matchesName reports whether qualified is entry or lives inside it. Template
// argument lists are compared with their spaces removed, because libclang spells
// the same instantiation with and without them depending on where it read it,
// and a manifest entry is written the way a reader writes it.
func matchesName(entry, qualified string) bool {
	e := strings.ReplaceAll(entry, " ", "")
	q := strings.ReplaceAll(qualified, " ", "")
	return q == e || strings.HasPrefix(q, e+"::")
}

// addReferenced records every record a binding's signature names.
func addReferenced(binding *binding, needed map[*recordModel]bool) {
	var add func(*goType)
	add = func(t *goType) {
		if t == nil {
			return
		}
		if t.Record != nil {
			needed[t.Record] = true
		}
		add(t.Elem)
	}
	for _, param := range binding.Params {
		add(param.Type)
	}
	add(binding.Result)
}

// reattach rebuilds each record's member lists from the kept bindings.
func (b *builder) reattach() {
	for _, name := range b.order {
		record := b.records[name]
		record.Ctors = nil
		record.Clone = nil
		record.Dtor = nil
		record.Methods = nil
		record.Statics = nil
	}
	for _, binding := range b.bindings {
		owner := binding.Owner
		if owner == nil {
			continue
		}
		switch {
		case binding.IsConstructor:
			owner.Ctors = append(owner.Ctors, binding)
		case binding.IsDestructor:
			owner.Dtor = binding
		case binding.sym.IsCopyConstructor:
			owner.Clone = binding
		case binding.IsStatic:
			owner.Statics = append(owner.Statics, binding)
		default:
			owner.Methods = append(owner.Methods, binding)
		}
	}
}

// assignNames gives every binding its Go identifier: the base name, the
// overload suffix when a name has several declarations, then the manifest's
// -rename, then the collision check.
func (b *builder) assignNames() error {
	for _, binding := range b.bindings {
		name, err := b.baseName(binding)
		if err != nil {
			return symbolError(binding.sym, err)
		}
		binding.GoName = name
	}
	b.applyOverloadSuffixes()
	b.resolveCollisions()
	if err := b.applyRenames(); err != nil {
		return err
	}
	return b.checkCollisions()
}

// resolveCollisions gives every binding of one scope its own Go name when two
// declarations genuinely share one. The overload rule (applyOverloadSuffixes)
// already tells apart the parameter lists of an overload set; what is left is
// the case a header makes common and the suffix cannot: a member whose own name
// collides with a generated one — cv::Mat's copy constructor and its clone()
// both want Clone — and a const/non-const pair, which has the same parameter
// list twice.
//
// The declared method keeps the name, because that is the name the C++ header
// wrote; the copy constructor becomes Copy (it is cppgo's own notation for the
// implicit copy); a const overload that loses the name takes a Const suffix; and
// anything still colliding after that is told apart by a short hash of its
// declaration. The manifest's -rename still wins, and checkCollisions still
// refuses a name that collides across kinds (a function and a type) or that the
// generated API reserves.
func (b *builder) resolveCollisions() {
	groups := map[string][]*binding{}
	keys := make([]string, 0, len(b.bindings))
	for _, binding := range b.bindings {
		key := nameScope(binding) + "\x00" + binding.GoName
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], binding)
	}
	sort.Strings(keys)
	// Every name a group starts from is in use in its scope before any group
	// is resolved: a suffix this pass invents may not land on the name another
	// group already holds (cv::Mat's const ptr(int) and its ptr(int, int) are
	// exactly that case).
	taken := map[string]map[string]bool{}
	for _, key := range keys {
		group := groups[key]
		scope := nameScope(group[0])
		if taken[scope] == nil {
			taken[scope] = map[string]bool{}
		}
		taken[scope][group[0].GoName] = true
	}
	for _, key := range keys {
		group := groups[key]
		if len(group) < 2 {
			continue
		}
		// The loser is decided by what a reader expects to find: a declared
		// member before a generated copy constructor, a non-const spelling
		// before the const one, and the declaration order of the header after
		// that. Sorting on the signature keeps it independent of file order.
		sort.SliceStable(group, func(i, j int) bool {
			left, right := group[i], group[j]
			if left.IsClone != right.IsClone {
				return !left.IsClone
			}
			if left.sym.IsConst != right.sym.IsConst {
				return !left.sym.IsConst
			}
			return displaySignature(left.sym) < displaySignature(right.sym)
		})
		scope := nameScope(group[0])
		for _, binding := range group[1:] {
			binding.GoName = b.collisionName(binding, taken[scope])
			taken[scope][binding.GoName] = true
		}
	}
}

// collisionName is the name a binding takes when the one it computed is
// already in use in its scope. It is deterministic: the parameter-type suffix
// first (the same rule the overload set uses), then Copy for a copy
// constructor, then Const for a const overload, and a short hash of the
// declaration as the last resort.
func (b *builder) collisionName(binding *binding, taken map[string]bool) string {
	base := binding.GoName
	suffix := paramSuffix(binding.Params)
	if binding.IsClone {
		candidate := "Copy" + suffix
		if !taken[candidate] {
			return candidate
		}
		return candidate + shortHash(displaySignature(binding.sym))[:6]
	}
	candidate := base + suffix
	if !taken[candidate] && candidate != base {
		return candidate
	}
	if binding.sym.IsConst {
		candidate = base + suffix + "Const"
		if !taken[candidate] {
			return candidate
		}
		return candidate + shortHash(displaySignature(binding.sym))[:6]
	}
	return base + suffix + shortHash(displaySignature(binding.sym))[:6]
}

// baseName is the name a binding gets before overloads are told apart.
func (b *builder) baseName(binding *binding) (string, error) {
	if binding.fieldName != "" {
		return binding.fieldName, nil
	}
	if binding.IsDestructor {
		return "Close", nil
	}
	if binding.IsClone {
		return "Clone", nil
	}
	if binding.IsConstructor {
		return "New" + binding.Owner.GoName + b.templateSuffix(binding), nil
	}
	name := exportName(simpleName(binding.sym.QualifiedName))
	if binding.sym.IsOperator {
		operator, err := b.operatorGoName(binding)
		if err != nil {
			return "", err
		}
		name = operator
	}
	if binding.IsStatic {
		return binding.Owner.GoName + name + b.templateSuffix(binding), nil
	}
	return name + b.templateSuffix(binding), nil
}

// templateSuffix names the concrete instantiation a manifest request asked for:
// at<unsigned char> is AtUint8, at<double> is AtFloat64. The parameter-type
// suffix cannot tell them apart — one at<uchar>(int, int) and one
// at<double>(int, int) have the same parameters — and the template argument is
// what a reader wrote.
func (b *builder) templateSuffix(binding *binding) string {
	if len(binding.templateArgs) == 0 {
		return ""
	}
	var out strings.Builder
	for _, spelling := range binding.templateArgs {
		// A record argument is named after the Go type the record model has, so
		// _InputArray<cv::Point_<int>> is named PointInt and not the spelling.
		if record, ok := b.mapper.lookupRecord(spelling); ok {
			out.WriteString(typeSuffix(record.GoName))
			continue
		}
		param, ok := cxx.ScalarType(spelling)
		if !ok {
			out.WriteString(typeSuffix(exportName(spelling)))
			continue
		}
		mapped, err := b.mapper.mapType(param)
		if err != nil {
			out.WriteString(typeSuffix(exportName(spelling)))
			continue
		}
		out.WriteString(typeSuffix(mapped.Go))
	}
	return out.String()
}

// nameScope is where a Go name lives: package level for functions,
// constructors and statics; the record's method set for methods.
func nameScope(binding *binding) string {
	if binding.Owner != nil && !binding.IsConstructor && !binding.IsStatic && !binding.IsDestructor {
		return binding.Owner.GoName
	}
	if binding.IsDestructor {
		return binding.Owner.GoName
	}
	return ""
}

// applyOverloadSuffixes appends the parameter-type suffix to every binding of
// an overload set. The zero-parameter overload keeps the bare name.
func (b *builder) applyOverloadSuffixes() {
	groups := map[string][]*binding{}
	keys := make([]string, 0, len(b.bindings))
	for _, binding := range b.bindings {
		key := nameScope(binding) + "\x00" + binding.GoName
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], binding)
	}
	sort.Strings(keys)
	for _, key := range keys {
		group := groups[key]
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			return displaySignature(group[i].sym) < displaySignature(group[j].sym)
		})
		// A group made of one declaration and its default-argument prefixes
		// keeps the bare name for the full signature: that is the declaration a
		// reader called, and each prefix is a shorter spelling of it.
		// A genuine zero-argument declaration keeps the bare name when there is
		// one; otherwise, in a group that holds one declaration and its
		// default-argument prefixes, the full signature keeps it.
		primary := ""
		hasPrefix := false
		zeroArity := false
		for _, binding := range group {
			if binding.defaultPrefix {
				hasPrefix = true
			} else if len(binding.Params) == 0 {
				zeroArity = true
			}
		}
		if hasPrefix && !zeroArity {
			for _, binding := range group {
				if binding.defaultPrefix || len(binding.Params) == 0 {
					continue
				}
				if primary == "" {
					primary = binding.GoName
				} else {
					primary = ""
					break
				}
			}
		}
		for _, binding := range group {
			if len(binding.Params) == 0 {
				// A genuine zero-argument overload keeps the bare name. A
				// collision with the full signature is the collision check's
				// to report.
				continue
			}
			if primary != "" && !binding.defaultPrefix {
				continue
			}
			binding.GoName += paramSuffix(binding.Params)
		}
	}
}

// paramSuffix concatenates the Go type suffixes of a parameter list. A pointer
// and a reference cross as the same Go type, so the C++ distinction is spelled
// in the name: two overloads of one set that differ only there are still two
// declarations, and the Go side can call both.
func paramSuffix(params []*goParam) string {
	var b strings.Builder
	for _, param := range params {
		b.WriteString(typeSuffix(param.Type.Go))
		// A reference crosses as a Go pointer only when its target is one: a
		// record reference is *T and needs the marker, a std::string reference
		// is a string and does not.
		if isReferenceParam(param.Type) && strings.HasPrefix(param.Type.Go, "*") {
			b.WriteString("Ref")
		}
	}
	return b.String()
}

// applyRenames applies the manifest's -rename entries. An entry names a
// declaration by qualified name or by its full signature; one that matches
// nothing, or matches several declarations by qualified name, is an error.
func (b *builder) applyRenames() error {
	for _, entry := range b.cfg.Rename {
		// The separator is the last "=", because a signature can contain one:
		// operator=, operator== and operator<=> all do.
		cut := strings.LastIndex(entry, "=")
		name, goName := "", ""
		if cut >= 0 {
			name, goName = entry[:cut], entry[cut+1:]
		}
		if cut < 0 || strings.TrimSpace(name) == "" || strings.TrimSpace(goName) == "" {
			return fmt.Errorf("cppgo: -rename %q: it must be <qualified signature>=<GoName>", entry)
		}
		name = strings.TrimSpace(name)
		goName = strings.TrimSpace(goName)
		if !validGoIdentifier(goName) {
			return fmt.Errorf("cppgo: -rename %q: %q is not a valid Go identifier", entry, goName)
		}
		var matches []*binding
		for _, binding := range b.bindings {
			if binding.sym.QualifiedName == name || displaySignature(binding.sym) == name {
				matches = append(matches, binding)
			}
		}
		switch len(matches) {
		case 0:
			return fmt.Errorf("cppgo: -rename %s: it names nothing this header declares", name)
		case 1:
			matches[0].GoName = goName
		default:
			return fmt.Errorf("cppgo: -rename %s: it names %d declarations; use the full signature", name, len(matches))
		}
	}
	return nil
}

// validGoIdentifier reports whether name can be an exported Go identifier.
func validGoIdentifier(name string) bool {
	if name == "" || goKeywords[name] {
		return false
	}
	for i, r := range name {
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

// checkCollisions rejects two bindings that map to one Go name in one scope,
// and the names the generated API reserves.
func (b *builder) checkCollisions() error {
	seen := map[string]*binding{}
	for _, binding := range b.bindings {
		if nameScope(binding) == "" && binding.GoName == "Open" {
			return fmt.Errorf("cppgo: %s maps to Open, which is the package's generated entry point; rename it with -rename '%s=<GoName>'", displaySignature(binding.sym), displaySignature(binding.sym))
		}
		if nameScope(binding) != "" && binding.GoName == "Close" && !binding.IsDestructor {
			return fmt.Errorf("cppgo: %s maps to Close, which is every record's destructor; rename it with -rename '%s=<GoName>'", displaySignature(binding.sym), displaySignature(binding.sym))
		}
		key := nameScope(binding) + "\x00" + binding.GoName
		if previous, ok := seen[key]; ok {
			return fmt.Errorf("cppgo: %s and %s both map to Go name %s; rename one with -rename '<signature>=<GoName>'", displaySignature(previous.sym), displaySignature(binding.sym), binding.GoName)
		}
		seen[key] = binding
	}
	recordNames := map[string]bool{}
	for _, name := range b.order {
		record := b.records[name]
		if recordNames[record.GoName] {
			return fmt.Errorf("cppgo: two records map to Go type %s; rename one with -rename '<signature>=<GoName>'", record.GoName)
		}
		recordNames[record.GoName] = true
		if previous, ok := seen["\x00"+record.GoName]; ok {
			return fmt.Errorf("cppgo: the record %s and %s both map to Go name %s; rename one with -rename '<signature>=<GoName>'",
				record.CxxName, displaySignature(previous.sym), record.GoName)
		}
	}
	for _, enum := range b.enums {
		if !enum.selected {
			continue
		}
		if recordNames[enum.GoName] {
			return fmt.Errorf("cppgo: two types map to Go type %s; rename one with -rename '<signature>=<GoName>'", enum.GoName)
		}
		recordNames[enum.GoName] = true
		for _, constant := range enum.Constants {
			if recordNames[constant.GoName] {
				return fmt.Errorf("cppgo: the enumerator %s maps to Go name %s, which another type or enumerator already uses; rename one with -rename '<signature>=<GoName>'",
					constant.CxxName, constant.GoName)
			}
			recordNames[constant.GoName] = true
		}
	}
	return nil
}
