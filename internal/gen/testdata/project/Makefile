# The project's own build: its compiler flags, its sources, its shared object.
# cppgo is not part of this file, and no generated code is compiled into the
# library. `make library` produces build/libanalytic.so.
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O1 -fPIC
INCLUDES  = -Iinclude

BUILD   = build
LIBRARY = $(BUILD)/libanalytic.so

.PHONY: library clean

library: $(LIBRARY)

# -Wl,-soname is what a project's shared-object build normally declares, and it
# is what lets the cppgo-built shim name this library instead of a path: the
# loader resolves the soname to whichever copy is already loaded.
$(LIBRARY): src/analytic.cpp include/analytic/analytic.hpp
	@mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -Wl,-soname,libanalytic.so src/analytic.cpp -o $@

clean:
	rm -rf $(BUILD)
