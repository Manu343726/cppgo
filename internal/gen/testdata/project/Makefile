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

$(LIBRARY): src/analytic.cpp include/analytic/analytic.hpp
	@mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared src/analytic.cpp -o $@

clean:
	rm -rf $(BUILD)
