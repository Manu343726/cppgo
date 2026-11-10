// The watch command's flags, kept apart so the loop and the command are
// readable on their own.

package main

import (
	"errors"
	"flag"
	"io"
)

// errSourceAbsent says a source the user named is not there. A camera that is
// not plugged in is not a failure of the program: there is nothing to watch,
// and the command says so and stops.
var errSourceAbsent = errors.New("the source is not present")

type watchFlags struct {
	flagSet *flag.FlagSet
	source  string
	outDir  string
	limit   int
	verbose bool
}

func newWatchFlags(stderr io.Writer) *watchFlags {
	flags := &watchFlags{flagSet: flag.NewFlagSet("watch", flag.ContinueOnError)}
	flags.flagSet.SetOutput(stderr)
	flags.flagSet.StringVar(&flags.source, "source", "", "dir:<path> or camera:<index>")
	flags.flagSet.StringVar(&flags.outDir, "out", "", "write one template per processed frame into this directory")
	flags.flagSet.IntVar(&flags.limit, "limit", 0, "stop after N frames (0: the whole stream)")
	flags.flagSet.BoolVar(&flags.verbose, "v", false, "print the C++ library's log records and the per-frame detail")
	return flags
}
