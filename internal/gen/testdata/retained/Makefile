# The project's own build: its compiler flags, its sources, its shared object.
# cppgo is not part of this file, and no generated code is compiled into the
# library. `make library` produces build/libretained.so.
CXX      ?= g++
CXXFLAGS ?= -std=c++17 -O1 -fPIC
INCLUDES  = -Iinclude

BUILD   = build
LIBRARY = $(BUILD)/libretained.so

.PHONY: library clean

library: $(LIBRARY)

$(LIBRARY): src/retained.cpp include/retained/retained.hpp
	@mkdir -p $(BUILD)
	$(CXX) $(CXXFLAGS) $(INCLUDES) -shared -Wl,-soname,libretained.so src/retained.cpp -o $@

clean:
	rm -rf $(BUILD)
