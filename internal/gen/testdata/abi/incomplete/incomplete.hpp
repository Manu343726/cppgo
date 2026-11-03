#pragma once

// incomplete.hpp is the fixture for the object-size probe: node is declared
// but never defined, so libclang cannot report its size, and a constructor
// binding for it has to fail with an actionable message instead of allocating
// the wrong number of bytes.

namespace incomplete {

struct Node;

// node_size takes the class by value, which is the only way the probe can be
// asked for sizeof.
void node_size(Node n);

} // namespace incomplete
