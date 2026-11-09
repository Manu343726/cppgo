package main

import (
	"os"
	osexec "os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMainExitsWithTheCommandsCode runs the real entry point in a child process,
// because main calls os.Exit: the exit code the shell sees is the one the
// command returned.
func TestMainExitsWithTheCommandsCode(t *testing.T) {
	if os.Getenv("CPPGO_MAIN_HELPER") == "1" {
		os.Args = []string{"cppgo", "version"}
		main()
		return
	}
	command := osexec.Command(os.Args[0], "-test.run=TestMainExitsWithTheCommandsCode")
	command.Env = append(os.Environ(), "CPPGO_MAIN_HELPER=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "the version command exits 0: %s", output)
	assert.Contains(t, string(output), "cppgo")
}
