package gen

import (
	"fmt"
	"sort"
	"strings"
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
	b.selectEnums(bindMatched)
	return nil
}

// bindingMatches reports whether a manifest entry names the binding: the
// declaration itself, or the record it is a member of.
func bindingMatches(entry string, binding *binding) bool {
	if matchesName(entry, binding.sym.QualifiedName) {
		return true
	}
	return binding.Owner != nil && matchesName(entry, binding.Owner.CxxName)
}

// matchesName reports whether qualified is entry or lives inside it.
func matchesName(entry, qualified string) bool {
	return qualified == entry || strings.HasPrefix(qualified, entry+"::")
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
	if err := b.applyRenames(); err != nil {
		return err
	}
	return b.checkCollisions()
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
		return "New" + binding.Owner.GoName, nil
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
		return binding.Owner.GoName + name, nil
	}
	return name, nil
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
		primary := ""
		hasPrefix := false
		for _, binding := range group {
			if binding.defaultPrefix {
				hasPrefix = true
			}
		}
		if hasPrefix {
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

// paramSuffix concatenates the Go type suffixes of a parameter list.
func paramSuffix(params []*goParam) string {
	var b strings.Builder
	for _, param := range params {
		b.WriteString(typeSuffix(param.Type.Go))
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
