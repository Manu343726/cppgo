package cxx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Options controls Open.
type Options struct {
	// Header is the C++ header (or source) whose declarations will be bound.
	Header string
	// CompileCommands is the -compile-commands value, passed to Discover.
	CompileCommands string
	// Args, when non-empty, replaces flag discovery entirely.
	Args []string
	// Sources lists extra translation units to parse for out-of-line
	// definitions. When empty they are discovered: the header itself when it is
	// a source file, else every source in the header's directory and in the
	// compilation database that includes it.
	Sources []string
}

// Project is a C++ header plus the translation units needed to answer, for each
// declaration in it, whether a symbol exists in the compiled library.
//
// A header alone is not enough: it declares `Vector(double,double);` and the
// definition lives in a .cpp. So Open parses the header (for the API surface
// and the mangled names) and every source that includes it (for the bodies),
// then merges the sightings by mangled name. The merged Symbol answers both
// questions at once.
type Project struct {
	// Header is the absolute path that was introspected.
	Header string
	// Flags are the compilation flags discovered for the header.
	Flags *Flags
	// MainFiles are the translation units that were parsed, header first.
	MainFiles []string
	// Library is the libclang handle the project holds a reference to.
	Library *Libclang

	mu        sync.Mutex
	tus       []*TU
	symbols   []Symbol
	byName    map[string][]int
	byMangled map[string]int
	records   []Record
	byRecord  map[string]int
	enums     []Enum
	byEnum    map[string]int
	closed    bool
}

// maxSources bounds how many including sources are parsed. A header included by
// hundreds of translation units does not need all of them: any one that
// defines the out-of-line members answers the question.
const maxSources = 8

// Open loads libclang, discovers the compilation flags and parses the
// translation units needed to resolve the header's symbols.
//
// The returned Project holds a reference to libclang; Close releases it.
func Open(opts Options) (*Project, error) {
	if opts.Header == "" {
		return nil, errors.New("cxx: no header to introspect")
	}
	header := opts.Header
	if abs, err := filepath.Abs(header); err == nil {
		header = abs
	}
	header = filepath.Clean(header)
	if _, err := os.Stat(header); err != nil {
		return nil, fmt.Errorf("cxx: introspecting header: %w", err)
	}
	flags, err := Discover(DiscoverOptions{
		File:            header,
		CompileCommands: opts.CompileCommands,
		Args:            opts.Args,
	})
	if err != nil {
		return nil, err
	}
	library, err := Load()
	if err != nil {
		return nil, err
	}
	project := &Project{Header: header, Flags: flags, Library: library}
	if err := project.parse(header, opts); err != nil {
		_ = project.Close()
		return nil, err
	}
	return project, nil
}

// parse opens every translation unit and merges the symbols.
func (p *Project) parse(header string, opts Options) error {
	mainFiles := []string{header}
	for _, extra := range sourcesFor(header, opts.Sources, p.Flags) {
		if extra != header {
			mainFiles = append(mainFiles, extra)
		}
	}

	merged := make(map[string]int)
	for _, file := range mainFiles {
		args := p.Flags.Args
		if file != header {
			// A different translation unit may need different flags (include
			// paths declared per directory, .clangd fragments); asking the same
			// discovery pipeline keeps one rule for every file.
			other, err := Discover(DiscoverOptions{
				File:            file,
				CompileCommands: opts.CompileCommands,
				Args:            opts.Args,
			})
			if err != nil {
				return err
			}
			args = other.Args
		}
		tu, err := p.Library.ParseTranslationUnit(file, args)
		if err != nil {
			return err
		}
		p.tus = append(p.tus, tu)
		p.MainFiles = append(p.MainFiles, file)
		for _, sym := range tu.Symbols() {
			if key := symbolMergeKey(sym); key != "" {
				if i, ok := merged[key]; ok {
					mergeInto(&p.symbols[i], sym)
					continue
				}
				merged[key] = len(p.symbols)
			}
			p.symbols = append(p.symbols, sym)
		}
	}

	p.byName = make(map[string][]int, len(p.symbols))
	p.byMangled = make(map[string]int, len(p.symbols))
	for i, sym := range p.symbols {
		p.byName[sym.QualifiedName] = append(p.byName[sym.QualifiedName], i)
		if sym.Mangled != "" {
			if _, taken := p.byMangled[sym.Mangled]; !taken {
				p.byMangled[sym.Mangled] = i
			}
		}
	}
	p.mergeTypes()
	return nil
}

// mergeTypes folds the records and enums of every parsed translation unit into
// one view. The same header is parsed once per unit, so the sightings are
// identical; the merge exists so a definition that only one unit saw is still
// part of the project.
func (p *Project) mergeTypes() {
	byRecord := make(map[string]int)
	byEnum := make(map[string]int)
	for _, tu := range p.tus {
		for _, rec := range tu.Records() {
			if i, ok := byRecord[rec.QualifiedName]; ok {
				mergeRecord(&p.records[i], rec)
				continue
			}
			byRecord[rec.QualifiedName] = len(p.records)
			p.records = append(p.records, rec)
		}
		for _, enum := range tu.Enums() {
			if _, ok := byEnum[enum.QualifiedName]; ok {
				continue
			}
			byEnum[enum.QualifiedName] = len(p.enums)
			p.enums = append(p.enums, enum)
		}
	}
	p.byRecord = byRecord
	p.byEnum = byEnum
}

// symbolMergeKey is the identity of a symbol across translation units. The
// mangled name is the normal one. A template pattern has no mangled name — it
// has no symbol until it is instantiated — so its qualified name and signature
// are the closest thing to an identity it has, and that is what stops the same
// pattern from being reported once per translation unit that includes the
// header.
func symbolMergeKey(sym Symbol) string {
	if sym.Mangled != "" {
		return sym.Mangled
	}
	if sym.IsTemplate {
		return "template:" + sym.QualifiedName + "|" + sym.Signature()
	}
	return ""
}

// sourcesFor decides which translation units carry the definitions.
func sourcesFor(header string, explicit []string, flags *Flags) []string {
	if len(explicit) > 0 {
		out := make([]string, 0, len(explicit))
		for _, file := range explicit {
			resolved := file
			if abs, err := filepath.Abs(resolved); err == nil {
				resolved = filepath.Clean(abs)
			}
			out = append(out, resolved)
		}
		return out
	}
	if isSourceFile(header) {
		return nil
	}
	base := filepath.Base(header)
	candidates := map[string]bool{}

	// Sources next to the header: the common shape of a C++ library
	// (mathx.hpp + mathx.cpp), and the fallback when there is no database.
	dir := filepath.Dir(header)
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !isSourceFile(name) {
				continue
			}
			path := filepath.Join(dir, name)
			if includes(path, base) {
				candidates[path] = true
			}
		}
	}
	// Sources from the compilation database, which may live anywhere.
	if flags != nil && flags.CompileDatabase != "" {
		if entries, err := readDatabase(flags.CompileDatabase); err == nil {
			for _, entry := range entries {
				path := entry.path()
				if !isSourceFile(path) || path == header {
					continue
				}
				if includes(path, base) {
					candidates[path] = true
				}
			}
		}
	}
	out := make([]string, 0, len(candidates))
	for path := range candidates {
		out = append(out, path)
	}
	sort.Strings(out)
	if len(out) > maxSources {
		out = out[:maxSources]
	}
	return out
}

// Symbols returns every symbol the project knows about, in parse order:
// the header's declarations first, then anything only the sources revealed.
func (p *Project) Symbols() []Symbol {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Symbol(nil), p.symbols...)
}

// Lookup finds the first symbol with the given qualified C++ name, e.g.
// "mathx::Vector::norm". Overloads share a name; see Lookups.
func (p *Project) Lookup(name string) (Symbol, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idxs := p.byName[name]
	if len(idxs) == 0 {
		return Symbol{}, false
	}
	return p.symbols[idxs[0]], true
}

// Lookups returns every overload with the given qualified C++ name.
func (p *Project) Lookups(name string) []Symbol {
	p.mu.Lock()
	defer p.mu.Unlock()
	idxs := p.byName[name]
	out := make([]Symbol, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, p.symbols[i])
	}
	return out
}

// LookupMangled finds a symbol by its mangled name, which is how an overload is
// selected unambiguously.
func (p *Project) LookupMangled(mangled string) (Symbol, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i, ok := p.byMangled[mangled]
	if !ok {
		return Symbol{}, false
	}
	return p.symbols[i], true
}

// Members returns every constructor, destructor, method and static member
// whose semantic parent is the given qualified name, e.g. "mathx::Vector".
//
// The result is in parse order; overloads of one name appear together, in the
// order the header declared them. Access control is reported on each symbol and
// not filtered here: the generator decides which members a binding wants.
func (p *Project) Members(parent string) []Symbol {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []Symbol
	for _, sym := range p.symbols {
		if sym.Parent == parent {
			out = append(out, sym)
		}
	}
	// A template specialisation's members are synthesised from its pattern:
	// libclang exposes no cursor for the instantiated definition. They live on
	// the record, not in the symbol list, so the two are joined here.
	for _, record := range p.records {
		if record.QualifiedName == parent {
			out = append(out, record.Members...)
		}
	}
	return out
}

// Records returns every record definition the project knows about, merged
// across translation units and in first-sighting order.
func (p *Project) Records() []Record {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Record(nil), p.records...)
}

// Record finds a record by its qualified C++ name.
func (p *Project) Record(name string) (Record, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i, ok := p.byRecord[name]
	if !ok {
		return Record{}, false
	}
	return p.records[i], true
}

// Enums returns every enum definition the project knows about.
func (p *Project) Enums() []Enum {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Enum(nil), p.enums...)
}

// Enum finds an enum by its qualified C++ name.
func (p *Project) Enum(name string) (Enum, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	i, ok := p.byEnum[name]
	if !ok {
		return Enum{}, false
	}
	return p.enums[i], true
}

// MissingBodies returns the symbols that are declared but export no symbol of
// their own: inline members, pure virtuals, static functions with internal
// linkage, and template patterns. They are not refused — the generator carries
// them through a thunk — but a direct call to one would fail at load time, so
// the list is what a report uses to explain a carrier choice.
func (p *Project) MissingBodies() []Symbol {
	var out []Symbol
	for _, sym := range p.Symbols() {
		if !sym.HasOutOfLineBody {
			out = append(out, sym)
		}
	}
	return out
}

// Close disposes every translation unit and releases the libclang reference.
func (p *Project) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("cxx: project already closed")
	}
	p.closed = true
	tus := p.tus
	p.tus = nil
	library := p.Library
	p.mu.Unlock()

	for _, tu := range tus {
		_ = tu.Close()
	}
	if library != nil {
		return library.Close()
	}
	return nil
}
