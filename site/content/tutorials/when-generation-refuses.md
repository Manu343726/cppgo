+++
title = "When the generator refuses"
description = "Read the error, find the cause in the C++ declaration, and choose between moving a definition out of line, changing the signature, or falling back to shim mode. Three worked refusals."
weight = 40
+++

The direct ABI mode is a validator before it is a code generator. When it
refuses a binding, the refusal is the feature: the alternative is a trampoline
that fails at load time or, worse, calls the wrong address. The skill is reading
the message and picking the cheapest real fix.

There are only three fixes:

1. **Move the definition out of line** — the binding was possible, the symbol was
   not exported.
2. **Change the signature** — the Go declaration described something the C++ side
   does not say.
3. **Use shim mode** — the construct is outside what the direct ABI can express
   (virtual dispatch, a by-value class, a `std::` type, a throw).

Everything below is one of those three.

## Case 1: the accessor defined in the class

```cpp
class Vector {
public:
    double norm() const;              // declared
    double x() const { return x_; }   // defined inline
private:
    double x_, y_;
};
```

```go
//cppgo:method x
func (v Vector) X() float64
```

```
mathx.go:31: mathx::Vector::x: it is defined inside its class definition or has
internal linkage, so it is implicitly inline and the library exports no symbol
for it: move the definition out into a source file and rebuild, or bind it
through the shim mode
```

**What it means.** A member defined inside its class definition is implicitly
inline. The compiler is not required to emit an out-of-line body, and in practice
does not, so the library's dynamic symbol table has no entry for it. `dlsym`
would fail at run time.

**Option A — move it out of line.** This is the fix, and it costs one line in
each file:

```cpp
// header
double x() const;

// source
double Vector::x() const { return x_; }
```

Rebuild the `.so`, regenerate, and the report now shows
`mathx::Vector::x mangled _ZNK5mathx6Vector1xEv`.

**Option B — an out-of-line factory.** If the header is a third-party SDK you
cannot edit, wrap it:

```cpp
// your header
double vector_x(const mathx::Vector &v);
// your source
double vector_x(const mathx::Vector &v) { return v.x(); }
```

Bind the factory with `//cppgo:function vector_x`. The inline call happens
inside your translation unit, where the definition is visible.

**Option C — shim mode.** The same idea with an `extern "C"` entry point:
`extern "C" double mathx_vector_x(void *v) { return static_cast<Vector*>(v)->x(); }`.
Choose this only if you are already in shim mode for other reasons; a factory is
less machinery.

**Do not `-force` this one.** The parse is right: there is no symbol. `-force`
would emit a trampoline that panics on the first call.

## Case 2: a virtual function

```cpp
class Shape {
public:
    virtual ~Shape();
    virtual double area() const = 0;
    int id() const;
};
```

```go
//cppgo:method area
func (s Shape) Area() float64
```

```
mathx.go:44: mathx::Shape::area: it is virtual: the call has to go through the
vtable to reach an override, so a fixed symbol cannot be called directly; use
the shim mode, or bind a non-virtual member
```

**What it means.** A mangled symbol names one function body. A virtual call reads
the object's vtable and dispatches to the override for the dynamic type. Calling
the base's symbol directly would silently ignore the override.

**The fix is shim mode.** There is no signature change that makes a virtual call
non-virtual, and the definition is already out of line. The shim calls
`s->area()` normally; the C++ compiler emits the dispatch:

```cpp
extern "C" int32_t mathx_shape_area(void *obj, double *out) {
    *out = static_cast<mathx::Shape *>(obj)->area();
    return 0;
}
```

If you only need the base implementation, a non-virtual member or a free
function that calls `Base::area()` explicitly is also correct — but it is a
different function, and the Go name should say so.

## Case 3: a `std::` type by value

```cpp
std::string Vector::describe() const;
```

```go
//cppgo:method describe
func (v Vector) Describe() string
```

```
mathx.go:52: mathx::Vector::describe: it returns "std::string" by value: a
standard-library type: use the shim mode
```

**What it means.** `std::string` is not one pointer. It is an object with its own
layout and lifetime, and the direct mode has no shim to construct or destroy it.
A Go `string` also cannot be returned: the C++ side cannot allocate Go memory.

**Option A — change the signature.** Return the two things that cross cleanly:

```cpp
std::size_t Vector::describe(char *out, std::size_t cap) const;
```

```go
//cppgo:method describe
func (v Vector) Describe(out []byte) int64
```

The caller allocates, the callee writes at most `cap` bytes and returns the
length. That is the same caller-owned-buffer shape as [bulk
data]({{< ref "tutorials/bulk-data.md" >}}), and it avoids the allocation
entirely.

**Option B — shim mode.** The shim owns the `std::string`, copies it into a
caller buffer (or returns a `malloc`'d buffer and a free function), and converts
the failure. This is the honest choice when the C++ API is `std::string` all the
way down and you cannot change it.

**What not to do:** declare the Go result as `string` and `-force` it. The
generated trampoline would read a `std::string` object as the two words of a Go
string header, which is not a pointer/length pair at all.

## Case 4: a class returned by value

```cpp
Vector Vector::scaled(double k) const;   // returns Vector by value
```

```
mathx.go:60: mathx::Vector::scaled: it returns Vector by value: the Itanium ABI
brings a class back through a hidden sret pointer, which the direct ABI mode
does not emit in v1 (the trampoline layout reserves the slot ...) — use the shim
mode for this binding
```

**What it means.** A class result is built in caller-provided storage and passed
back through a hidden first pointer. Direct-ABI v1 reserves the slot in the
layout so `this` still lands where the ABI expects, but it does not emit the
call. This is a v1 limitation, not a mistake in your declaration (see
[limitations]({{< ref "project/limitations.md" >}})).

**The fixes:**

- **Change the signature** so nothing is returned by value: write into storage
  the caller already owns. A member declared
  `void scaled_into(double k, void *out) const` can be bound as
  `func (v Vector) ScaledInto(k float64, out cppgo.Handle)`, with `out` coming
  from a constructor. The C++ side placement-constructs into it.
- **Shim mode**, where the shim owns the destination and the generated wrapper
  hands it the storage Go allocated.

## The decision procedure

When the generator refuses, ask in this order:

1. **Does the message say "no symbol" / "inline"?** Move the definition out of
   line. If you cannot edit the header, add a factory. If neither, shim.
2. **Does it say "virtual", "std::", "by value", or "template"?** The construct is
   outside the mode. Shim mode, unless you can change the C++ signature to
   something that crosses (pointer + length, out-parameter, integer status).
3. **Does it say "width", "register class", "arity" or "register"?** The Go
   declaration is wrong. Fix it: match the width, match the register class,
   expand slices into pointer + length, group arguments.
4. **Does it say "not declared"?** The header/libclang did not see it. Check the
   compilation flags first — a fallback parse is the usual cause — then the
   namespace and the linkage.
5. **Does it say "-force"?** It does not; that is your decision, and it is only
   right when the *parse* is wrong, never when the code is.

| message contains | cause | answer |
|---|---|---|
| inline / internal linkage / no symbol | no dynamic symbol | out of line, factory, or shim |
| virtual | vtable dispatch | shim |
| `std::` | non-POD by value | change signature or shim |
| struct/class by value (param or result) | aggregate ABI | pointer/out-param, or shim |
| no mangled name | template, never instantiated | explicit instantiation, or shim |
| width / register class | Go type is wrong | change the Go type |
| arity | slice/string expansion miscounted | one slice = pointer + length |
| register exhaustion | more than 6 integer / 8 XMM | group into a struct pointer |
| not declared | flags, namespace, linkage | fix `compile_commands.json` |

## Reading the error's shape

Every refusal is prefixed with `file:line:` and the qualified C++ name:

```
./bindings/mathx/mathx.go:44: mathx::Shape::area: it is virtual: ...
```

- The **file and line** point at the Go directive, not at C++.
- The **qualified name** is what the generator resolved *to*; if it is not the
  entity you meant, the directive is naming the wrong thing.
- The **rest** is one sentence of cause and one of remedy. The remedy is written
  to be actionable, not to be exhaustive.

If the message names a type whose spelling surprises you (`"long"` where you
expected `"int"`), the parse may have used the wrong flags. Regenerate with
`-compile-commands` before changing any code.

Back to the [manual]({{< ref "manual/_index.md" >}}), or read the same failures
by symptom in [troubleshooting]({{< ref "manual/troubleshooting.md" >}}).
