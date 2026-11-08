// Command cppgo binds a C++ library to Go from its headers, and builds the C++
// shim the generated package loads.
//
// The commands are gen, check and build. gen writes the Go package, the
// assembly trampolines and the shim sources, then compiles the shim; check writes
// nothing and exits non-zero when the files on disk differ from what gen would
// write; build is the second phase of the template flow, which loads a program
// with go/packages, collects the instantiations it uses and adds their thunks to
// the shim. All three accept the same manifest flags, printed by usage.
//
// Exit codes: 0 when the command succeeded, 1 when generation or verification
// failed, 2 when the command line itself was wrong.
package main

import "os"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
