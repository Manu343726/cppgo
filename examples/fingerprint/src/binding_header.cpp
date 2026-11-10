// The bound header, compiled on its own.
//
// Nothing else is included here on purpose: a public header that needs another
// file to be included before it is a header that lies about its own
// dependencies, and this translation unit is where that fails first.
//
// The include is spelled without its directory for a second reason: `cppgo gen`
// finds the flags a header is parsed with by looking in the compilation
// database for a translation unit that includes it, matched by base name. With
// the namespaced spelling the header would have no matching unit, the generator
// would fall back to assumed flags, and it would say so on every run.

#include "fingerprint.hpp"
