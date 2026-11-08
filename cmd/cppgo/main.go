// Command cppgo binds a C++ library to Go from its headers, and builds the C++
// shim the generated package loads.
//
// There are three commands:
//
//   - gen writes the Go package, the assembly trampolines and the shim sources,
//     then compiles the shim.
//   - check writes nothing, and exits non-zero when the files on disk differ
//     from what gen would write.
//   - build is the second phase of the template flow: it adds the template
//     instantiations a Go program uses to the binding, before the shim is
//     compiled.
//
// All three accept the same manifest flags, which usage prints. Exit codes: 0
// when the command succeeded, 1 when generation or verification failed, 2 when
// the command line itself was wrong.
package main

import "os"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
