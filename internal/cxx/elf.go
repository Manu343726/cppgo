package cxx

import (
	"debug/elf"
	"fmt"
	"path/filepath"
	"sort"
)

// DefinedDynamicSymbols returns the names in the dynamic symbol table of an ELF
// shared object that are defined by it. That is exactly the list
// `nm -D --defined-only <file>` prints.
//
// This is the ground truth for Symbol.HasOutOfLineBody, because libclang says
// a declaration has a body outside its class definition and this says whether
// the linker actually kept a symbol for it. A test that compares the two is the
// only way to tell a correct rule from a plausible one.
func DefinedDynamicSymbols(path string) ([]string, error) {
	file, err := elf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cxx: reading dynamic symbols: %w", err)
	}
	defer func() { _ = file.Close() }()

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

// Soname returns the DT_SONAME of an ELF shared object, or its base name when
// the object declares none.
//
// It is what the generated shim is linked against and what the generated
// package records. A shared object's DT_NEEDED entry takes the soname so a
// build directory never reaches the binary. A deployment that keeps the
// library and the shim together resolves it through $ORIGIN.
func Soname(path string) (string, error) {
	file, err := elf.Open(path)
	if err != nil {
		return "", fmt.Errorf("cxx: reading the soname: %w", err)
	}
	defer func() { _ = file.Close() }()

	values, err := file.DynString(elf.DT_SONAME)
	if err != nil {
		return "", fmt.Errorf("cxx: reading the dynamic section of %s: %w", path, err)
	}
	for _, value := range values {
		if value != "" {
			return value, nil
		}
	}
	return filepath.Base(path), nil
}
