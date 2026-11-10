#pragma once

// Typedefs the generator has to see through: a name for a function pointer, a
// name for a scalar, and a name for a pointer to one. libclang resolves the
// scalar aliases to their canonical kind on the parameter itself, but a
// typedef of a function pointer has no pointee of its own: only the canonical
// type desugars to the prototype the signature is read from.

namespace aliases {

using LogCallback = void (*)(int level, const char *message);
using Scalar = double;

// A typedef of `void *`: unlike a function pointer, the canonical desugar is a
// type with no Go value, so the pointee stays the unreadable one and the mapper
// reports the alias as a void pointer.
using RawHandle = void *;

void set_log_callback(LogCallback fn);
void set_scalar_callback(void (*fn)(Scalar));
void set_value(Scalar value);
void use_handle(RawHandle handle);

} // namespace aliases
