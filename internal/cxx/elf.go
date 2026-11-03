package cxx

import (
	"debug/elf"
	"fmt"
	"sort"
)

// DefinedDynamicSymbols returns the names in the dynamic symbol table of an ELF
// shared object that are defined by it — exactly what
// `nm -D --defined-only <file>` lists.
//
// This is the ground truth for Symbol.HasOutOfLineBody: libclang says a
// declaration has a body outside its class definition, and this says whether
// the linker actually kept a symbol for it. A test that compares the two is the
// only way to know the rule is right rather than merely plausible.
func DefinedDynamicSymbols(path string) ([]string, error) {
	file, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cxx: reading dynamic symbols: %w", err)
	}
	defer file.Close()

	symbols, err := file.DynamicSymbols()
	if err != nil {
		return nil, fmt.Errorf("cxx: reading dynamic symbols of %s: %w", path, err)
	}
	names := make([]string, 0, len(symbols))
	for _, sym := range symbols {
		if sym.Section == elf.SHN_UNDEF || sym.Name == "" {
			// Undefined: imported from somewhere else, which is what
			// `nm -D --defined-only` filters out.
			continue
		}
		names = append(names, sym.Name)
	}
	sort.Strings(names)
	return names, nil
}
