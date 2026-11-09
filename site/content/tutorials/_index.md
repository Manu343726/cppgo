---
title: Tutorials
summary: Learning-oriented lessons that take you from nothing to a working binding.
weight: 10
description: Lessons that start at zero and end at a running Go program.
---

A tutorial is a lesson: you follow it from beginning to end and it works. It
assumes you write Go and know nothing about a *foreign function interface* — the
mechanism by which one language calls code compiled in another — so it defines
each term the first time it uses one.

Start with [Bind your first C++ class]({{< ref "tutorials/first-binding.md" >}})
if you have not run `cppgo` before. It takes one small C++ class declared in a
header, builds the library with the example's own build commands, generates the
Go package that calls the class, and ends with a program and the example's tests
passing. One class keeps the whole path visible: the declaration, the compiled
library, the generated Go, and a C++ failure arriving as a Go error.

Then take
[Build a complete Go + C++ project]({{< ref "tutorials/full-project.md" >}}): the
same path through a library built by CMake, the C++ build system, with one
declaration of every kind the generator supports — a class that keeps its state
in C++, C++ strings and arrays, a small struct passed by value, a failure that
throws and one reported as an integer status, a C++ call into a Go function, and
templates — with the generated Go quoted and explained line by line.
