package gen

import (
	"fmt"
	"strings"

	"github.com/Manu343726/cppgo/internal/cxx"
)

// containerKind is how a std::vector's elements cross the boundary.
//
// The wire has a fixed shape — a handful of words in registers, or one pointer
// to an argument block — and a container of containers does not. The three
// shapes below are what a std::vector can be: the plain element buffer, a
// buffer plus the per-inner-vector bounds, and the element addresses of a
// vector of records the Go side owns one by one.
type containerKind uint8

const (
	// containerFlat is the plain form: an element buffer and its length.
	containerFlat containerKind = iota
	// containerNested is a vector of vectors: one flat element buffer plus the
	// bounds of every inner vector, because a nested vector is not a shape the
	// register file can describe. The last bound is the total element count.
	containerNested
	// containerOpaque is a vector of records the Go side does not hold as plain
	// values: an array of element addresses in, and a block of objects the Go
	// side adopts out, one view per element.
	containerOpaque
)

// elementShape classifies a container's element.
func elementShape(elem *goType) containerKind {
	switch {
	case elem == nil:
		return containerFlat
	case elem.Kind == goSlice:
		return containerNested
	case elem.Record != nil && !elem.Record.Mirrored:
		return containerOpaque
	default:
		return containerFlat
	}
}

// containerWord builds one wire word and appends it. It is the extra words a
// container needs (its bounds and its length) that it builds: the primary word
// comes from marshalContainer, and the caller marks it.
func containerWord(binding *binding, p cxx.Param, add func(*wireArg), slot string, expr, keep, cType, cName string) *wireArg {
	arg := &wireArg{
		slotType: slot, class: classInt, size: 8,
		expr: expr, keep: keep, cType: cType, cName: cName, param: p,
		label:   "a" + itoa(len(binding.Wire)),
		stdRole: stdRoleExtra,
	}
	add(arg)
	return arg
}

// stdVectorParamWire lowers a std::vector parameter the callee only reads.
//
//	flat      (T * data, size_t n)
//	nested    (E * data, const size_t * bounds, size_t n)
//	opaque    (void * const * elems, size_t n)
func (b *builder) stdVectorParamWire(binding *binding, p cxx.Param, mapped *goType, goName string) {
	base := cIdent(p.Name)
	elem := mapped.Elem
	primary := b.marshalContainer(binding, p, mapped, goName, goName, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) })
	switch elementShape(elem) {
	case containerNested:
		bounds := base + "_bounds"
		count := base + "_size"
		containerWord(binding, p, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) },
			"unsafe.Pointer", "cppgo.Ptr("+bounds+")", bounds, "const size_t *", bounds)
		containerWord(binding, p, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) },
			"uint64", "uint64(len("+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_data", bounds, count}
		primary.stdOffsets = bounds
	case containerOpaque:
		count := base + "_size"
		containerWord(binding, p, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) },
			"uint64", "uint64(len("+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_elems", count}
	default:
		count := base + "_size"
		containerWord(binding, p, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) },
			"uint64", "uint64(len("+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_data", count}
	}
	primary.stdRole = stdRolePrimary
	primary.stdKind = mapped.Std
	primary.stdLocal = "cppgo_" + base
	primary.stdElem = elem
	primary.stdType = mapped
}

// stdVectorOutParamWire lowers a std::vector parameter the callee replaces: the
// caller's current value in, so an in-out call sees it, and the new value out
// through the words the shape needs.
//
//	flat      (T * data, size_t n, void ** out, size_t * out_n)
//	nested    (E * data, const size_t * bounds, size_t n,
//	           void ** out, void ** out_bounds, size_t * out_n)
//	opaque    (void * const * elems, size_t n, void ** out, size_t * out_n)
func (b *builder) stdVectorOutParamWire(binding *binding, p cxx.Param, mapped *goType, goName string) {
	base := cIdent(p.Name)
	elem := mapped.Elem
	primary := b.marshalContainer(binding, p, mapped, goName, "*"+goName, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) })
	addWord := func(slot, expr, keep, cType, cName string) *wireArg {
		return containerWord(binding, p, func(arg *wireArg) { binding.Wire = append(binding.Wire, arg) }, slot, expr, keep, cType, cName)
	}
	// The callee's current value first: an out-parameter is in-out in C++, and a
	// call that appends to what it was given must see what it was given.
	switch elementShape(elem) {
	case containerNested:
		bounds := base + "_bounds"
		count := base + "_size"
		addWord("unsafe.Pointer", "cppgo.Ptr("+bounds+")", bounds, "const size_t *", bounds)
		addWord("uint64", "uint64(len(*"+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_data", bounds, count}
		primary.stdOffsets = bounds
	case containerOpaque:
		count := base + "_size"
		addWord("uint64", "uint64(len(*"+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_elems", count}
	default:
		count := base + "_size"
		addWord("uint64", "uint64(len(*"+goName+"))", "", "size_t", count)
		primary.stdArgs = []string{base + "_data", count}
	}
	outData := base + "_out_data"
	outSize := base + "_out_size"
	outDataArg := addWord("unsafe.Pointer", "unsafe.Pointer(&"+outData+")", "&"+outData, "void **", outData)
	outDataArg.isOut = true
	outDataArg.stdRole = stdRoleNone
	if elementShape(elem) == containerNested {
		outBounds := base + "_out_bounds"
		outBoundsArg := addWord("unsafe.Pointer", "unsafe.Pointer(&"+outBounds+")", "&"+outBounds, "void **", outBounds)
		outBoundsArg.isOut = true
		outBoundsArg.stdRole = stdRoleNone
		primary.stdOutOffsets = outBounds
		binding.stdDecls = append(binding.stdDecls, "var "+outBounds+" unsafe.Pointer")
	}
	outSizeArg := addWord("unsafe.Pointer", "unsafe.Pointer(&"+outSize+")", "&"+outSize, "size_t *", outSize)
	outSizeArg.isOut = true
	outSizeArg.stdRole = stdRoleNone
	primary.stdRole = stdRolePrimary
	primary.stdKind = mapped.Std
	primary.stdLocal = "cppgo_" + base
	primary.stdElem = elem
	primary.stdType = mapped
	primary.stdOutCopy = true
	primary.stdOutData = outData
	primary.stdOutSize = outSize
	primary.stdGoName = goName
	binding.stdDecls = append(binding.stdDecls,
		"var "+outData+" unsafe.Pointer",
		"var "+outSize+" uint64")
	binding.usesFree = true
	b.usesFree = true
}

// stdVectorResultWire appends the out-parameters a std::vector result crosses
// through, and the Go locals they aim at.
func (b *builder) stdVectorResultWire(binding *binding, result *goType) {
	out := func(expr, keep, cType, cName string) *wireArg {
		arg := &wireArg{
			label:    "a" + itoa(len(binding.Wire)),
			slotType: "unsafe.Pointer",
			class:    classInt,
			size:     8,
			expr:     expr,
			keep:     keep,
			cType:    cType,
			cName:    cName,
			isOut:    true,
		}
		binding.Wire = append(binding.Wire, arg)
		return arg
	}
	binding.stdDecls = []string{"var cppgoData unsafe.Pointer", "var cppgoSize uint64"}
	if elementShape(result.Elem) == containerNested {
		binding.stdDecls = append(binding.stdDecls, "var cppgoBounds unsafe.Pointer")
		out("unsafe.Pointer(&cppgoData)", "&cppgoData", "void **", "cppgo_out_data")
		out("unsafe.Pointer(&cppgoBounds)", "&cppgoBounds", "void **", "cppgo_out_bounds")
		out("unsafe.Pointer(&cppgoSize)", "&cppgoSize", "size_t *", "cppgo_out_size")
	} else {
		out("unsafe.Pointer(&cppgoData)", "&cppgoData", "void **", "cppgo_out_data")
		out("unsafe.Pointer(&cppgoSize)", "&cppgoSize", "size_t *", "cppgo_out_size")
	}
	binding.usesFree = true
	b.usesFree = true
}

// marshalContainer appends the Go declarations that build one container's wire
// value from the caller's Go slice, and appends the primary wire word. It is
// shared by the read-only and the in-out forms: both hand the callee the same
// value, and the in-out form simply also asks for the new one back.
func (b *builder) marshalContainer(binding *binding, p cxx.Param, mapped *goType, goName, value string, add func(*wireArg)) *wireArg {
	base := cIdent(p.Name)
	elem := mapped.Elem
	primary := func(expr, keep, cType, cName string) *wireArg {
		return containerWord(binding, p, add, "unsafe.Pointer", expr, keep, cType, cName)
	}
	switch elementShape(elem) {
	case containerNested:
		data := base + "_data"
		bounds := base + "_bounds"
		inner := elem.Elem
		binding.stdDecls = append(binding.stdDecls,
			"var "+data+" []"+inner.Go,
			"var "+bounds+" []uint64",
			"{",
			"\tcppgo_total := 0",
			"\tfor _, cppgo_inner := range "+value+" { cppgo_total += len(cppgo_inner) }",
			"\t"+data+" = make([]"+inner.Go+", cppgo_total)",
			"\t"+bounds+" = make([]uint64, len("+value+")+1)",
			"\tcppgo_at := 0",
			"\tfor cppgo_i, cppgo_inner := range "+value+" {",
			"\t\tcopy("+data+"[cppgo_at:], cppgo_inner)",
			"\t\tcppgo_at += len(cppgo_inner)",
			"\t\t"+bounds+"[cppgo_i+1] = uint64(cppgo_at)",
			"\t}",
			"}",
		)
		return primary("cppgo.Ptr("+data+")", data+", "+bounds, containerCTypes(inner), data)
	case containerOpaque:
		elems := base + "_elems"
		binding.stdDecls = append(binding.stdDecls,
			"var "+elems+" []unsafe.Pointer",
			"{",
			"\t"+elems+" = make([]unsafe.Pointer, len("+value+"))",
			"\tfor cppgo_i, cppgo_item := range "+value+" {",
			"\t\t"+b.nilElementCheck(binding, p.Name),
			"\t\t"+elems+"[cppgo_i] = unsafe.Pointer(cppgo_item.cppgoAddr())",
			"\t}",
			"}",
		)
		return primary("cppgo.Ptr("+elems+")", elems, "void * const *", elems)
	}
	data := base + "_data"
	// The caller's value is a slice for a read-only parameter and a pointer to
	// one for an out-parameter: cppgo.Ptr takes either the slice or what the
	// pointer names, and the length comes from the same place.
	keep := value
	if keep == "" {
		keep = goName
	}
	if keep != goName {
		return primary("cppgo.Ptr(*"+goName+")", "*"+goName, containerCTypes(elem), data)
	}
	return primary("cppgo.Ptr("+goName+")", goName, containerCTypes(elem), data)
}

// containerCTypes is the C type of an element buffer in the thunk prototype. A
// record crosses as void *, the way every other record parameter does, so the
// shim header stays standalone and the cast happens in the translation unit that
// includes the library's header.
func containerCTypes(elem *goType) string {
	if elem.Kind == goRecord {
		return "void *"
	}
	cType := elem.Cxx + " *"
	if elem.Const {
		cType = "const " + cType
	}
	return cType
}

// nilElementCheck is the statement a container of records emits for a nil or
// closed element: C++ would dereference the address, and a nil record has none.
func (b *builder) nilElementCheck(binding *binding, param string) string {
	if binding.ErrorResult {
		return "if cppgo_item == nil || cppgo_item.obj == nil { return " + errorZeroPrefix(binding) + "cppgo.ErrClosed }"
	}
	return fmt.Sprintf("if cppgo_item == nil || cppgo_item.obj == nil { panic(%q) }",
		"cppgo: "+displaySignature(binding.sym)+": a nil element in "+param)
}

// containerLocal is the C++ value the shim builds for one container parameter.
// It is rendered inside the thunk's try block, so an allocation failure becomes
// an ordinary error.
func (m *packageModel) containerLocal(arg *wireArg) string {
	elem := arg.stdElem
	var b strings.Builder
	switch elementShape(elem) {
	case containerNested:
		inner := elem.Elem
		count := arg.stdArgs[2]
		fmt.Fprintf(&b, "\t\tstd::vector<std::vector<%s>> %s;\n", inner.Cxx, arg.stdLocal)
		fmt.Fprintf(&b, "\t\tif (%s != 0) {\n", count)
		fmt.Fprintf(&b, "\t\t\t%s.reserve(%s);\n", arg.stdLocal, count)
		fmt.Fprintf(&b, "\t\t\tfor (size_t cppgo_i = 0; cppgo_i < %s; ++cppgo_i) {\n", count)
		fmt.Fprintf(&b, "\t\t\t\t%s.emplace_back(static_cast<const %s *>(%s) + %s[cppgo_i], static_cast<const %s *>(%s) + %s[cppgo_i + 1]);\n",
			arg.stdLocal, inner.Cxx, arg.stdArgs[0], arg.stdOffsets, inner.Cxx, arg.stdArgs[0], arg.stdOffsets)
		b.WriteString("\t\t\t}\n\t\t}\n")
	case containerOpaque:
		count := arg.stdArgs[1]
		fmt.Fprintf(&b, "\t\tstd::vector<%s> %s;\n", elem.Cxx, arg.stdLocal)
		fmt.Fprintf(&b, "\t\tif (%s != 0) {\n", count)
		fmt.Fprintf(&b, "\t\t\t%s.reserve(%s);\n", arg.stdLocal, count)
		fmt.Fprintf(&b, "\t\t\tfor (size_t cppgo_i = 0; cppgo_i < %s; ++cppgo_i) {\n", count)
		fmt.Fprintf(&b, "\t\t\t\t%s.push_back(*static_cast<const %s *>(static_cast<void * const *>(%s)[cppgo_i]));\n",
			arg.stdLocal, elem.Cxx, arg.stdArgs[0])
		b.WriteString("\t\t\t}\n\t\t}\n")
	default:
		fmt.Fprintf(&b, "\t\tstd::vector<%s> %s;\n", elem.Cxx, arg.stdLocal)
		if elem.Kind == goRecord {
			fmt.Fprintf(&b, "\t\tconst %s *cppgo_elems = static_cast<const %s *>(%s);\n", elem.Cxx, elem.Cxx, arg.stdArgs[0])
			fmt.Fprintf(&b, "\t\tif (%s != 0) { %s.assign(cppgo_elems, cppgo_elems + %s); }\n",
				arg.stdArgs[1], arg.stdLocal, arg.stdArgs[1])
		} else {
			fmt.Fprintf(&b, "\t\tif (%s != 0) { %s.assign(%s, %s + %s); }\n",
				arg.stdArgs[1], arg.stdLocal, arg.stdArgs[0], arg.stdArgs[0], arg.stdArgs[1])
		}
	}
	return b.String()
}

// containerOutCopy renders the copy-back of one container value into the blocks
// the thunk's out-parameters describe. It is used for an out-parameter (from
// the C++ local) and for a by-value result (from the call's own copy); the C++
// value is destroyed at the end of the thunk, and what crosses is a plain block
// the Go side owns and frees.
func containerOutCopy(local string, elem *goType, outData, outSize, outBounds string) string {
	var b strings.Builder
	switch elementShape(elem) {
	case containerNested:
		inner := elem.Elem
		fmt.Fprintf(&b, "\t\tconst size_t cppgo_n = %s.size();\n", local)
		b.WriteString("\t\tsize_t cppgo_total = 0;\n")
		b.WriteString("\t\tfor (size_t cppgo_i = 0; cppgo_i < cppgo_n; ++cppgo_i) { cppgo_total += ")
		fmt.Fprintf(&b, "%s[cppgo_i].size(); }\n", local)
		fmt.Fprintf(&b, "\t\t%s *cppgo_flat = static_cast<%s *>(::operator new[]((cppgo_total == 0 ? 1 : cppgo_total) * sizeof(%s)));\n",
			inner.Cxx, inner.Cxx, inner.Cxx)
		b.WriteString("\t\tsize_t *cppgo_bounds = static_cast<size_t *>(::operator new[]((cppgo_n + 1) * sizeof(size_t)));\n")
		b.WriteString("\t\tcppgo_bounds[0] = 0;\n")
		b.WriteString("\t\tsize_t cppgo_at = 0;\n")
		b.WriteString("\t\tfor (size_t cppgo_i = 0; cppgo_i < cppgo_n; ++cppgo_i) {\n")
		fmt.Fprintf(&b, "\t\t\tfor (size_t cppgo_j = 0; cppgo_j < %s[cppgo_i].size(); ++cppgo_j) { cppgo_flat[cppgo_at++] = %s[cppgo_i][cppgo_j]; }\n", local, local)
		b.WriteString("\t\t\tcppgo_bounds[cppgo_i + 1] = cppgo_at;\n\t\t}\n")
		fmt.Fprintf(&b, "\t\t*%s = cppgo_flat;\n\t\t*%s = cppgo_bounds;\n\t\t*%s = cppgo_n;\n", outData, outBounds, outSize)
	case containerOpaque:
		fmt.Fprintf(&b, "\t\tconst size_t cppgo_n = %s.size();\n", local)
		fmt.Fprintf(&b, "\t\t%s *cppgo_block = static_cast<%s *>(::operator new[]((cppgo_n == 0 ? 1 : cppgo_n) * sizeof(%s)));\n",
			elem.Cxx, elem.Cxx, elem.Cxx)
		b.WriteString("\t\tfor (size_t cppgo_i = 0; cppgo_i < cppgo_n; ++cppgo_i) {\n")
		fmt.Fprintf(&b, "\t\t\tnew (cppgo_block + cppgo_i) %s(%s[cppgo_i]);\n\t\t}\n", elem.Cxx, local)
		fmt.Fprintf(&b, "\t\t*%s = cppgo_block;\n\t\t*%s = cppgo_n;\n", outData, outSize)
	default:
		fmt.Fprintf(&b, "\t\tconst size_t cppgo_n = %s.size();\n", local)
		fmt.Fprintf(&b, "\t\t%s *cppgo_copy = static_cast<%s *>(::operator new[]((cppgo_n == 0 ? 1 : cppgo_n) * sizeof(%s)));\n",
			elem.Cxx, elem.Cxx, elem.Cxx)
		fmt.Fprintf(&b, "\t\tfor (size_t cppgo_i = 0; cppgo_i < cppgo_n; ++cppgo_i) { cppgo_copy[cppgo_i] = %s[cppgo_i]; }\n", local)
		fmt.Fprintf(&b, "\t\t*%s = cppgo_copy;\n\t\t*%s = cppgo_n;\n", outData, outSize)
	}
	return b.String()
}
