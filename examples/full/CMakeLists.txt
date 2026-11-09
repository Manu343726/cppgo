# metrics — the C++ side of the complete example, built the way a user's own
# project is built.
#
# cppgo is not part of this file. The only thing it asks of the project is the
# compile_commands.json below: `cppgo gen` reads the header with the same flags
# the project compiles it with, and builds its shim with them too.
cmake_minimum_required(VERSION 3.16)

project(metrics LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# The compilation database cppgo is pointed at with -compile-commands. Clang and
# CMake agree on this file, and a project that already builds with CMake has no
# extra work to do for cppgo.
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

add_library(metrics SHARED src/metrics.cpp)

target_include_directories(metrics PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include)

target_compile_options(metrics PRIVATE -Wall -Wextra -Wpedantic)

# A soname, as a shared library meant to be deployed declares. The cppgo-built
# shim records it in its DT_NEEDED entry, so the loader finds whichever copy of
# the library is already open instead of a path from the build tree.
set_target_properties(metrics PROPERTIES VERSION 1.0.0 SOVERSION 1)
