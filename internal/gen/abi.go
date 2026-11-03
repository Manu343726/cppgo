package gen

import (
	"fmt"
	"sort"
	"strings"
)

// class is a SysV argument class.
type class uint8

const (
	classInt class = iota
	classFloat
)

// The SysV AMD64 argument registers, in assignment order. The C++ side is
// generated from the same table, so both halves of the boundary agree.
var (
	intRegs   = []string{"DI", "SI", "DX", "CX", "R8", "R9"}
	floatRegs = []string{"X0", "X1", "X2", "X3", "X4", "X5", "X6", "X7"}

	// cIntRegs is the human-readable form used in diagnostics.
	cIntRegs = "RDI, RSI, RDX, RCX, R8, R9"
)

const (
	maxIntRegs   = 6
	maxFloatRegs = 8
)

// slot is one mirror parameter of the trampoline. It is what actually lands
// in an argument register: strings and slices expand into two slots each.
type slot struct {
	label  string // asm FP label: "fn", "a0", "a1", ...
	goType string // Go type of the slot
	class  class
	size   int
	reg    string
	offset int
}

func (s *slot) align() int { return s.size }

// resultKind is the shape of a bound function's result.
type resultKind uint8

const (
	resNone        resultKind = iota // void
	resValue                         // single value in AX/X0
	resStatus                        // int32_t status in AX
	resStatusValue                   // int32_t status in AX + value through an out-pointer
)

// retSlot describes the trampoline's result slot.
type retSlot struct {
	kind resultKind
	// slot is the asm-visible result slot (nil for resNone and resStatusValue,
	// which deliver the value through an out-pointer instead).
	slot *slot
	// goType is the Go type of the bound function's value result.
	goType string
	// tmpType is the Go type of the local an out-parameter points at; it has
	// the same width as the C type so the C++ side can write it directly.
	tmpType string
	// info is the type of the value result (nil for resStatus).
	info *typeInfo
}

// cParam is one parameter of the generated C++ shim prototype.
type cParam struct {
	Type string
	Name string
}

// signature is the fully computed calling convention of one bound function.
type signature struct {
	fn      *Func
	slots   []*slot // slots[0] is the hidden fn pointer
	total   int     // frame size for the TEXT $0-N directive
	ret     *retSlot
	cParams []cParam
	// abi is true for a direct C++ ABI binding.
	abi bool
	// sret is true when the trampoline reserves the hidden first INTEGER slot
	// for a struct returned by value. It is reserved so `this` lands in the
	// slot the Itanium ABI puts it in, and never emitted: the validator rejects
	// by-value class results in v1. See the TODO in buildSignature.
	sret bool
	// shape is how a direct ABI binding returns its value.
	shape abiShape
}

// usesUnsafe reports whether the generated Go wrapper (or trampoline
// declaration) mentions unsafe.Pointer.
func (s *signature) usesUnsafe() bool {
	for _, sl := range s.slots {
		if sl.goType == "unsafe.Pointer" {
			return true
		}
	}
	return s.ret != nil && s.ret.slot != nil && s.ret.slot.goType == "unsafe.Pointer"
}

// buildSignature validates a function's signature and computes its ABI.
func buildSignature(f *Func) (*signature, error) {
	sig := &signature{fn: f, slots: []*slot{{label: "fn", goType: "uintptr", class: classInt, size: 8}}}
	if f.abi != nil {
		sig.abi = true
		sig.shape = f.abi.shape
		sig.sret = f.abi.sym.HasSret
		if sig.sret {
			// TODO(abi): a class returned by value comes back through this
			// hidden pointer, which the caller has to own and pass in the first
			// INTEGER slot (Itanium C++ ABI §3.2.3). The validator rejects such
			// bindings in v1 (see resultShape), so the slot is only reserved:
			// `this` must already sit in the slot the ABI expects, but nothing
			// writes the destination address here yet.
			sig.slots = append(sig.slots, &slot{label: "sret", goType: "unsafe.Pointer", class: classInt, size: 8})
		}
		if f.Kind.hasThis() {
			// `this` is an ordinary pointer argument for the ABI: it takes the
			// first INTEGER slot after the reserved sret pointer, and the float
			// registers are not affected by it at all.
			sig.slots = append(sig.slots, &slot{label: "this", goType: "uintptr", class: classInt, size: 8})
		}
	}

	for i, p := range f.Params {
		if p.Info.kind == kindError {
			return nil, fmt.Errorf("unsupported parameter %q: error is only supported as the last result", p.Name)
		}
		label := fmt.Sprintf("a%d", i)
		switch p.Info.kind {
		case kindString:
			sig.slots = append(sig.slots,
				&slot{label: label, goType: "unsafe.Pointer", class: classInt, size: 8},
				&slot{label: label + "_len", goType: "uintptr", class: classInt, size: 8})
		case kindSlice:
			sig.slots = append(sig.slots,
				&slot{label: label, goType: "unsafe.Pointer", class: classInt, size: 8},
				&slot{label: label + "_len", goType: "uintptr", class: classInt, size: 8})
		default:
			sl := &slot{label: label, goType: p.Info.slot, class: classInt, size: p.Info.size}
			if p.Info.kind == kindFloat {
				sl.class = classFloat
			}
			sig.slots = append(sig.slots, sl)
		}
	}

	if sig.abi {
		sig.ret = buildABIResult(f)
	} else {
		ret, err := buildResult(f)
		if err != nil {
			return nil, err
		}
		if ret.kind == resStatusValue {
			// The value is written through a trailing out-pointer.
			sig.slots = append(sig.slots, &slot{label: "out", goType: "unsafe.Pointer", class: classInt, size: 8})
		}
		if ret.kind == resValue {
			ret.slot.label = "ret"
		}
		if ret.kind == resStatus || ret.kind == resStatusValue {
			ret.slot = &slot{label: "ret", goType: "int32", class: classInt, size: 4}
		}
		sig.ret = ret
	}

	if err := layout(sig); err != nil {
		return nil, err
	}
	if !sig.abi {
		sig.cParams = buildCParams(f, sig.ret)
	}
	return sig, nil
}

// buildABIResult describes how a direct ABI binding's value comes back. The
// shape was decided by the validator, which is the only place that knows both
// the Go and the C++ types.
func buildABIResult(f *Func) *retSlot {
	switch f.abi.shape {
	case abiStatus:
		// The status is whatever integer the C++ function returns: read it at
		// its own width, so a non-zero high word is not lost.
		sl := &slot{label: "ret", goType: "int32", class: classInt, size: 4}
		if f.abi.statusWidth > 4 {
			sl = &slot{label: "ret", goType: "int64", class: classInt, size: 8}
		}
		return &retSlot{kind: resStatus, slot: sl}
	case abiValue, abiValueError:
		value := f.Results[0]
		sl := resultSlot(value.Info)
		sl.label = "ret"
		return &retSlot{kind: resValue, info: value.Info, goType: value.Info.goType, slot: sl}
	default:
		// abiVoid, abiObject and abiErrorOnly return nothing from the call: a
		// constructor's handle is the storage Go allocated for it.
		return &retSlot{kind: resNone}
	}
}

// buildResult validates the result list and describes how it comes back.
func buildResult(f *Func) (*retSlot, error) {
	if len(f.Results) == 0 {
		return &retSlot{kind: resNone}, nil
	}
	if len(f.Results) > 2 {
		return nil, fmt.Errorf("too many results: cppgo supports at most one value result plus a trailing error")
	}
	value := f.Results[0]
	if len(f.Results) == 2 {
		if f.Results[1].Info.kind != kindError {
			return nil, fmt.Errorf("second result must be error, found %s", f.Results[1].Info.goType)
		}
		if value.Info.kind == kindError {
			return nil, fmt.Errorf("a function can only have one error result")
		}
	}
	if value.Info.kind == kindError {
		return &retSlot{kind: resStatus, info: value.Info, goType: "error"}, nil
	}
	switch value.Info.kind {
	case kindString, kindSlice:
		return nil, fmt.Errorf("unsupported result type %s: a %s cannot be returned from C; return it through a pointer argument", value.Info.goType, value.Info.goType)
	}
	if len(f.Results) == 1 {
		return &retSlot{kind: resValue, info: value.Info, goType: value.Info.goType, slot: resultSlot(value.Info)}, nil
	}
	return &retSlot{
		kind:    resStatusValue,
		info:    value.Info,
		goType:  value.Info.goType,
		tmpType: outParamType(value.Info),
	}, nil
}

// resultSlot is the trampoline slot a single value result comes back in.
func resultSlot(info *typeInfo) *slot {
	if info.kind == kindFloat {
		return &slot{goType: info.slot, class: classFloat, size: info.size}
	}
	if info.kind == kindBool {
		// C returns int32_t, so only the low 32 bits are meaningful.
		return &slot{goType: "int32", class: classInt, size: 4}
	}
	return &slot{goType: info.slot, class: classInt, size: 8}
}

// outParamType is the Go type of the local an out-parameter points at. It
// must have exactly the width of the C type.
func outParamType(info *typeInfo) string {
	switch info.kind {
	case kindBool:
		return "int32"
	case kindPointer, kindHandle:
		return "unsafe.Pointer"
	}
	if info.kind == kindFloat {
		return info.slot
	}
	switch info.goType {
	case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32", "uint64", "uintptr":
		return info.goType
	}
	// int maps to int64_t.
	if info.goType == "int" {
		return "int64"
	}
	if info.goType == "uint" {
		return "uint64"
	}
	return info.slot
}

// layout assigns stack offsets and argument registers. Go's ABI0 stack frame
// places arguments in declaration order at their natural alignment, with the
// result immediately after the last argument.
func layout(sig *signature) error {
	off := 0
	for _, sl := range sig.slots {
		off = alignUp(off, sl.align())
		sl.offset = off
		off += sl.size
	}
	if sig.ret.slot != nil {
		off = alignUp(off, sig.ret.slot.align())
		sig.ret.slot.offset = off
		off += sig.ret.slot.size
	}
	sig.total = off
	return assignRegisters(sig)
}

// assignRegisters walks the C arguments in declaration order, handing out
// integer and float registers per class.
func assignRegisters(sig *signature) error {
	ints, floats := 0, 0
	for _, sl := range sig.slots[1:] {
		switch sl.class {
		case classInt:
			ints++
			if ints > maxIntRegs {
				return fmt.Errorf("function %s needs %d integer argument registers, but the SysV ABI only provides %d (%s); group or reduce the arguments", sig.fn.Name, ints, maxIntRegs, cIntRegs)
			}
			sl.reg = intRegs[ints-1]
		case classFloat:
			floats++
			if floats > maxFloatRegs {
				return fmt.Errorf("function %s needs %d float argument registers, but the SysV ABI only provides %d (XMM0-XMM7); group or reduce the arguments", sig.fn.Name, floats, maxFloatRegs)
			}
			sl.reg = floatRegs[floats-1]
		}
	}
	return nil
}

// buildCParams renders the C++ shim parameter list.
func buildCParams(f *Func, ret *retSlot) []cParam {
	var out []cParam
	seen := map[string]bool{}
	add := func(typ, name string) {
		n := cIdent(name)
		for i := 2; seen[n]; i++ {
			n = fmt.Sprintf("%s_%d", cIdent(name), i)
		}
		seen[n] = true
		out = append(out, cParam{Type: typ, Name: n})
	}
	for _, p := range f.Params {
		switch p.Info.kind {
		case kindString, kindSlice:
			add(p.Info.cType, p.Name)
			add("size_t", p.Name+"_len")
		default:
			add(p.Info.cType, p.Name)
		}
	}
	if ret.kind == resStatusValue {
		add(ret.info.cType+"*", "ret")
	}
	return out
}

// cKeywords are the C++ keywords a Go parameter name may collide with.
var cKeywords = func() map[string]bool {
	words := strings.Fields(`
		alignas alignof and and_eq asm auto bitand bitor bool break case catch
		char char16_t char32_t class compl const constexpr const_cast continue
		decltype default delete do double dynamic_cast else enum explicit export
		extern false float for friend goto if inline int long mutable namespace
		new noexcept not not_eq nullptr operator or or_eq private protected
		public register reinterpret_cast return short signed sizeof static
		static_assert static_cast struct switch template this thread_local throw
		true try typedef typeid typename union unsigned using virtual void
		volatile wchar_t while xor xor_eq
		size_t int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t`)
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}()

// cIdent turns a Go parameter name into a safe C identifier.
func cIdent(name string) string {
	if name == "" || name == "_" {
		name = "arg"
	}
	var b strings.Builder
	for i, r := range name {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if cKeywords[out] {
		out += "_"
	}
	return out
}

// alignUp rounds n up to a multiple of a. Every supported slot has an
// alignment of 4 or 8, so a guard for a <= 1 would be dead code.
func alignUp(n, a int) int { return (n + a - 1) / a * a }

// sortFuncs orders the bound functions by Go name so the output is
// deterministic no matter how many files declare them.
func sortFuncs(funcs []*Func) {
	sort.Slice(funcs, func(i, j int) bool { return funcs[i].Name < funcs[j].Name })
}
