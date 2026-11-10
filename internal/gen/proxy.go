package gen

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// proxyAdaptation is one -proxy entry: a parameter OpenCV declares as an
// InputArray/OutputArray proxy, which the manifest names the container it
// carries instead.
//
// The proxy types are stack objects with no Go shape of their own: an
// _OutputArray holds a pointer to the vector it writes, and a Go value cannot
// keep one alive across the two C++ calls its construction and use would take.
// The shim is the only place where both live at once, so the binding's parameter
// is the container, the shim builds the container, constructs the declared proxy
// from it, calls, and copies the container back — one call, one thunk, no
// wrapper in Go.
type proxyAdaptation struct {
	// Entry is the manifest text, for diagnostics.
	Entry string
	// Selector is the declaration name or signature, matched the way -bind is.
	Selector string
	// Index is the parameter's position in the declaration, zero-based.
	Index int
	// Spelling is the C++ type the parameter is carried as. A trailing `&`
	// makes it an in-out container; without one it is read-only.
	Spelling string
}

// parseProxy reads one -proxy entry, written as
//
//	<declaration>@<index>=<C++ type>
//
// The declaration is a qualified name or a full signature, exactly as -bind
// writes it; the index selects one parameter; the type is a C++ spelling the
// model maps, with a trailing `&` for a parameter the callee replaces.
func parseProxy(entry string) (*proxyAdaptation, error) {
	text := strings.TrimSpace(entry)
	equals := strings.LastIndex(text, "=")
	if equals <= 0 || equals == len(text)-1 {
		return nil, fmt.Errorf("cppgo: -proxy %s: write it as <declaration>@<index>=<C++ type>", entry)
	}
	selector, spelling := strings.TrimSpace(text[:equals]), strings.TrimSpace(text[equals+1:])
	at := strings.LastIndex(selector, "@")
	if at <= 0 || at == len(selector)-1 {
		return nil, fmt.Errorf("cppgo: -proxy %s: the declaration needs an @<index> naming the parameter", entry)
	}
	index, err := strconv.Atoi(strings.TrimSpace(selector[at+1:]))
	if err != nil || index < 0 {
		return nil, fmt.Errorf("cppgo: -proxy %s: %q is not a parameter index", entry, selector[at+1:])
	}
	// The prefix cannot be empty here: it is trimmed, and @ cannot be its first
	// character. The index bounds above are the whole of the grammar.
	return &proxyAdaptation{
		Entry:    entry,
		Selector: selector[:at],
		Index:    index,
		Spelling: spelling,
	}, nil
}

// proxyFor finds the adaptation that names one parameter of a declaration.
func (b *builder) proxyFor(sym cxx.Symbol, index int) (*proxyAdaptation, int, bool) {
	for i, adapted := range b.proxies {
		if adapted.Index != index {
			continue
		}
		if matchesName(adapted.Selector, sym.QualifiedName) || displaySignature(sym) == adapted.Selector {
			return adapted, i, true
		}
	}
	return nil, 0, false
}

// adaptedParam is the parameter the declaration is lowered as: the container the
// manifest named, in place of the proxy type it was declared with. The declared
// parameter still supplies the name and the position; nothing else about it is
// used, because the proxy is constructed in the shim from the container.
func adaptedParam(declared cxx.Param, adapted *proxyAdaptation) cxx.Param {
	spelling := strings.TrimSpace(adapted.Spelling)
	inOut := strings.HasSuffix(spelling, "&")
	base := strings.TrimSpace(strings.TrimSuffix(spelling, "&"))
	pointee := cxx.Param{
		Kind:       cxx.TypeRecord,
		ClangKind:  "Record",
		Spelling:   base,
		RecordName: base,
		SizeBytes:  -1,
	}
	if inOut {
		return cxx.Param{
			Kind:      cxx.TypePointer,
			ClangKind: "LValueReference",
			Spelling:  spelling,
			SizeBytes: 8,
			Ptr:       true,
			Pointee:   &pointee,
			Name:      declared.Name,
		}
	}
	pointee.IsConst = true
	return cxx.Param{
		Kind:      cxx.TypePointer,
		ClangKind: "LValueReference",
		Spelling:  "const " + base + " &",
		SizeBytes: 8,
		Ptr:       true,
		Pointee:   &pointee,
		Name:      declared.Name,
	}
}

// proxyType is the C++ type the shim constructs around the container: the proxy
// the declaration names, qualified, or empty when the parameter is not adapted.
func proxyType(declared cxx.Param) (string, error) {
	if declared.Pointee != nil && declared.Pointee.RecordName != "" {
		return "::" + declared.Pointee.RecordName, nil
	}
	if declared.Kind == cxx.TypeRecord && declared.RecordName != "" {
		return "::" + declared.RecordName, nil
	}
	return "", fmt.Errorf("cppgo: %q is not a record parameter, so a -proxy adaptation cannot construct it", declared.Spelling)
}

// checkProxy reports an adaptation nothing used: a manifest mistake is named,
// with the declaration it was meant for.
func (b *builder) checkProxy() error {
	for i, adapted := range b.proxies {
		if b.proxyMatched[i] {
			continue
		}
		return fmt.Errorf("cppgo: -proxy %s: no selected declaration matches %q with a parameter at index %d",
			adapted.Entry, adapted.Selector, adapted.Index)
	}
	return nil
}
