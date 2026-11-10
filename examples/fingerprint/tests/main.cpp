// The test runner. `make test` builds this binary and runs it through CTest;
// the fixture directory is the argument, so the tests read the committed frames
// from the source tree and not from wherever the build happened to land.

#include "harness.hpp"

void pipelineTests();
void ansi378Tests();
void triggerTests();
void logTests();

int main(int argc, char **argv) {
    if (argc < 2) {
        std::fprintf(stderr, "usage: %s <testdata directory>\n", argv[0]);
        return 2;
    }
    harness::directory = argv[1];

    pipelineTests();
    ansi378Tests();
    triggerTests();
    logTests();

    std::printf("\n%d checks, %d failed\n", harness::checks, harness::failures);
    return harness::failures == 0 ? 0 : 1;
}
