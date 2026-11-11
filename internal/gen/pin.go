package gen

import "fmt"

// pinShape says which copy keeps a Go value where the C caller can read it.
//
// A call that may run a callback enters C through runtime.cgocall, and the
// callback runs Go code on the goroutine that owns the call. Go code may grow
// that goroutine's stack, and growing it copies the stack to another address: a
// pointer into a Go stack frame that C++ is holding then names bytes that
// belong to nobody. Each shape below is a Go value that can reach C through a
// pointer, and the call copies it into a heap object it pins with
// runtime.Pinner — the supported way to tell the garbage collector not to move
// an object.
type pinShape uint8

const (
	// pinSlice is a []T the callee reads through a (T *, size_t) pair.
	pinSlice pinShape = iota
	// pinString is a string the callee reads through a `const char *`.
	pinString
	// pinValue is a *T the callee reads through the address Go holds.
	pinValue
	// pinOut is one word a thunk writes its result through, on the way back.
	pinOut
	// pinCell is a heap cell the thunk writes a result into. It needs no copy:
	// pinning it is what makes it a heap object the call cannot lose.
	pinCell
)

// pinPlan is one value a callback-capable call hands to C: the heap copy that
// stands in for it, and the write-back that returns what the callee wrote.
type pinPlan struct {
	shape pinShape
	// name is the Go parameter the copy is made from, empty for a word that
	// receives a result.
	name string
	// local is the Go variable that holds the copy, or the pinned heap cell.
	local string
	// elem is the Go type of one element, for the copies that need a type.
	elem string
	// writeBack is true when the callee's writes reach the caller's value: a
	// non-const slice, pointer or reference.
	writeBack bool
}

// pinDecls are the Go declarations the copies need, emitted before anything
// else in the call.
func (binding *binding) pinDecls() []string {
	if !binding.pinned {
		return nil
	}
	decls := []string{"cppgoPin := new(runtime.Pinner)", "defer cppgoPin.Unpin()"}
	for _, plan := range binding.pins {
		switch plan.shape {
		case pinSlice:
			decls = append(decls,
				fmt.Sprintf("%s := cppgoPinSlice(cppgoPin, %s)", plan.local, plan.name),
				fmt.Sprintf("var %sPtr unsafe.Pointer", plan.local),
				fmt.Sprintf("if len(%s) != 0 {\n\t%sPtr = unsafe.Pointer(&%s[0])\n}",
					plan.local, plan.local, plan.local),
			)
		case pinString:
			// The NUL-terminated copy a `const char *` parameter already is.
			// It is heap-allocated because the callee is handed its address;
			// pinning it is what turns "does not move" into a promise.
			decls = append(decls,
				fmt.Sprintf("if len(%s) != 0 {\n\tcppgoPin.Pin(&%s[0])\n}", plan.local, plan.local))
		case pinValue:
			decls = append(decls,
				fmt.Sprintf("var %s *%s", plan.local, plan.elem),
				fmt.Sprintf("if %s != nil {\n\t%s = cppgoPinValue(cppgoPin, *%s)\n}",
					plan.name, plan.local, plan.name),
			)
		case pinOut:
			decls = append(decls, fmt.Sprintf("cppgoPin.Pin(%s)", plan.local))
		case pinCell:
			// The cell is a heap object because cppgoHeapCell returns it: a
			// cell that only ever reaches a uintptr can be stack allocated, and
			// pinning a stack object does not keep the stack from being copied.
			decls = append(decls,
				fmt.Sprintf("%s := cppgoHeapCell[%s]()", plan.local, plan.elem),
				fmt.Sprintf("cppgoPin.Pin(%s)", plan.local),
			)
		}
	}
	return decls
}

// pinAfter is the Go code that runs once the call returned: the callee's writes
// go back into the caller's value, and an out-parameter's pinned cell becomes
// the value the result conversion reads.
func (binding *binding) pinAfter() []string {
	if !binding.pinned {
		return nil
	}
	var after []string
	for _, plan := range binding.pins {
		switch plan.shape {
		case pinSlice:
			if plan.writeBack {
				after = append(after, fmt.Sprintf("copy(%s, %s)", plan.name, plan.local))
			}
		case pinValue:
			if plan.writeBack {
				after = append(after, fmt.Sprintf("if %s != nil {\n\t*%s = *%s\n}", plan.local, plan.name, plan.local))
			}
		case pinOut:
			// The pinned cell is the out-parameter's storage; the binding reads
			// the word out of it once the call has written it.
			after = append(after, fmt.Sprintf("%s := *%s", plan.name, plan.local))
		}
	}
	return after
}

// pinCall replaces every Go pointer the binding hands to C with a pinned heap
// copy and records what has to be copied back afterwards. It runs only for the
// reentrant carrier: on the direct and thunk carriers nothing can re-enter Go
// while the callee holds the pointer, which is what makes the borrowed shape
// safe there.
func (b *builder) pinCall(binding *binding) {
	if binding.Carrier != reentrant {
		return
	}
	index := 0
	for _, arg := range binding.Wire {
		if arg.pin == nil {
			continue
		}
		plan := arg.pin
		if plan.shape != pinString {
			plan.local = fmt.Sprintf("cppgoPinArg%d", index)
			index++
		}
		switch plan.shape {
		case pinSlice:
			arg.pinnedExpr, arg.pinnedKeep = arg.expr, arg.keep
			arg.expr = plan.local + "Ptr"
		case pinValue:
			arg.pinnedExpr, arg.pinnedKeep = arg.expr, arg.keep
			arg.expr = "unsafe.Pointer(" + plan.local + ")"
			arg.keep = plan.local
		}
		binding.pins = append(binding.pins, *plan)
	}
	if len(binding.pins) > 0 {
		binding.pinned = true
	}
}
