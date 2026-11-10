# fingerprint — the C++ side of the example.
#
# cppgo is not part of this file, as in every example: the generator reads a
# header and compiles its own shim against the library this file builds. What
# cppgo does need is the compilation database below, so that the shim compiles
# with the same flags and the same include roots the library does.
#
# The dependencies are Conan's: `make` runs `conan install` first and points
# CMake at the generated toolchain, which is what makes `find_package(spdlog)`
# resolve to the pinned package and not to whatever the machine happens to have.
# OpenCV is the image's own installation (its component packages ship no
# OpenCVConfig.cmake), so it is found by path and both paths are variables.
cmake_minimum_required(VERSION 3.16)

project(fingerprint LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

# The compilation database cppgo is pointed at with -compile-commands.
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# --- OpenCV, by path -------------------------------------------------------

find_path(FINGERPRINT_OPENCV_INCLUDE_DIR NAMES opencv2/core.hpp
          HINTS ${OPENCV_INCLUDE} /usr/include/opencv4)
if(NOT FINGERPRINT_OPENCV_INCLUDE_DIR)
    message(FATAL_ERROR "opencv2/core.hpp was not found; pass -DOPENCV_INCLUDE=<the include root>")
endif()

foreach(component core imgproc imgcodecs)
    find_library(FINGERPRINT_OPENCV_${component} NAMES opencv_${component} HINTS ${OPENCV_LIB_DIR})
    if(NOT FINGERPRINT_OPENCV_${component})
        message(FATAL_ERROR "libopencv_${component} was not found; pass -DOPENCV_LIB_DIR=<the library directory>")
    endif()
endforeach()

# --- spdlog, from Conan ----------------------------------------------------

find_package(spdlog REQUIRED)

# --- the library -----------------------------------------------------------

add_library(fingerprint SHARED
    src/ansi378.cpp
    src/binding_header.cpp
    src/log.cpp
    src/pipeline.cpp
    src/session.cpp
    src/trigger.cpp
)

# `include/fingerprint` is on the private search path so that src/binding_header.cpp
# can include the public header by its bare name, which is the spelling cppgo's
# flag discovery matches a header against (see that file).
target_include_directories(fingerprint
    PUBLIC ${CMAKE_CURRENT_SOURCE_DIR}/include
    PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/src ${CMAKE_CURRENT_SOURCE_DIR}/include/fingerprint
            ${FINGERPRINT_OPENCV_INCLUDE_DIR}
)

target_link_libraries(fingerprint
    PRIVATE
        ${FINGERPRINT_OPENCV_core}
        ${FINGERPRINT_OPENCV_imgproc}
        ${FINGERPRINT_OPENCV_imgcodecs}
)

# The recipe's spdlog is the header-only one: the package's CMakeDeps file
# names both targets and only one of them exists in a given configuration.
if(TARGET spdlog::spdlog_header_only)
    target_link_libraries(fingerprint PRIVATE spdlog::spdlog_header_only)
else()
    target_link_libraries(fingerprint PRIVATE spdlog::spdlog)
endif()

target_compile_options(fingerprint PRIVATE -Wall -Wextra -Wpedantic)

# A soname, as a shared library meant to be deployed declares: the shim cppgo
# builds records it in its DT_NEEDED entry.
set_target_properties(fingerprint PROPERTIES VERSION 0.1.0 SOVERSION 0)

# --- the tests and the fixture generator -----------------------------------

enable_testing()

add_executable(fingerprint_tests
    tests/ansi378_test.cpp
    tests/log_test.cpp
    tests/main.cpp
    tests/pipeline_test.cpp
    tests/trigger_test.cpp
)
target_link_libraries(fingerprint_tests PRIVATE fingerprint
                                                ${FINGERPRINT_OPENCV_core} ${FINGERPRINT_OPENCV_imgproc}
                                                ${FINGERPRINT_OPENCV_imgcodecs})
target_include_directories(fingerprint_tests PRIVATE ${CMAKE_CURRENT_SOURCE_DIR}/include
                                                    ${FINGERPRINT_OPENCV_INCLUDE_DIR})
target_compile_options(fingerprint_tests PRIVATE -Wall -Wextra -Wpedantic)
add_test(NAME fingerprint COMMAND fingerprint_tests ${CMAKE_CURRENT_SOURCE_DIR}/testdata)

# Regenerates testdata/ from source; the output is committed, so this is not
# part of the build.
add_executable(fingerprint_fixtures EXCLUDE_FROM_ALL tools/make_fixtures.cpp)
target_include_directories(fingerprint_fixtures PRIVATE ${FINGERPRINT_OPENCV_INCLUDE_DIR})
target_link_libraries(fingerprint_fixtures PRIVATE ${FINGERPRINT_OPENCV_core} ${FINGERPRINT_OPENCV_imgproc}
                                                ${FINGERPRINT_OPENCV_imgcodecs})
set_target_properties(fingerprint_fixtures PROPERTIES RUNTIME_OUTPUT_DIRECTORY ${CMAKE_BINARY_DIR})
