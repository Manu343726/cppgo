// The Go end of the library's log sink.
//
// The C++ side stores the function pointer `SetLogCallback(fn)` hands it and
// calls it for every record it writes, from inside any call made from Go — the
// registering call does not have to be the delivering one, and the records do
// not wait for a later call to be collected. Nothing here buffers, pumps or
// drains: a record is formatted by the C++ sink and delivered to slog where it
// is written.
//
// The lifetime rules the generated binding states, and this file follows:
//
//   - the registration keeps the Go function alive; it is released by Close,
//     which waits for the calls already inside it;
//   - the C++ side holds a raw pointer, so the sink is cleared
//     (`SetLogCallback(nil)`) *before* the registration is closed. A delivery
//     after Close without clearing is the released slot's panic in Go — which
//     is what the closure below avoids by clearing first;
//   - the callback runs on the goroutine that made the call, and only while it
//     is inside it, so a worker goroutine's calls deliver to the same
//     registration on the worker's own stack;
//   - it must not log: it runs while the C++ sink holds its mutex, and a record
//     written from inside would re-enter it and deadlock.

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"

	fingerprint "github.com/Manu343726/cppgo/examples/fingerprint/gen"
)

// bindingLog is the callback the library delivers to, and the per-level count
// the CLI prints beside the run's own numbers.
type bindingLog struct {
	logger *slog.Logger

	mu      sync.Mutex
	records int64
	levels  [6]int64
}

// levelNames are spdlog's levels, in its order: the C++ side passes the value
// of its own enum, so the names are the library's and not this program's.
var levelNames = [6]string{"trace", "debug", "info", "warn", "error", "critical"}

// newBindingLog writes the records to out through slog. A verbose run adds the
// levels slog would otherwise drop: the sink writes trace and debug, and a
// `-v` is the caller asking to see them.
func newBindingLog(out io.Writer, verbose bool) *bindingLog {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug - 8 // below trace
	}
	return &bindingLog{logger: slog.New(slog.NewTextHandler(out, &slog.HandlerOptions{Level: level}))}
}

// record is the callback. It counts the record and hands it to slog; it does
// nothing else, and in particular it logs nothing of its own.
func (l *bindingLog) record(level int32, message string) {
	l.mu.Lock()
	if level >= 0 && level < int32(len(l.levels)) {
		l.levels[level]++
	}
	l.records++
	l.mu.Unlock()

	l.logger.Log(context.Background(), slogLevel(level), message, "source", "cpp")
}

// slogLevel maps spdlog's ordering onto slog's. spdlog has two levels below
// debug and slog has one, and the two above error share slog's error.
func slogLevel(level int32) slog.Level {
	switch level {
	case 0: // trace
		return slog.LevelDebug - 4
	case 1: // debug
		return slog.LevelDebug
	case 2: // info
		return slog.LevelInfo
	case 3: // warn
		return slog.LevelWarn
	default: // error, critical
		return slog.LevelError
	}
}

// summary is the per-level count of the records the callback received.
func (l *bindingLog) summary() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := ""
	for index, count := range l.levels {
		if count == 0 {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += fmt.Sprintf("%s %d", levelNames[index], count)
	}
	if out == "" {
		return "no records"
	}
	return out
}

// install registers the callback and returns the function that uninstalls it,
// in the order the lifetime requires: clear the C++ sink, then release the
// registration.
func (l *bindingLog) install() (func() error, error) {
	registration, err := fingerprint.SetLogCallback(l.record)
	if err != nil {
		return nil, err
	}
	return func() error {
		if _, err := fingerprint.SetLogCallback(nil); err != nil {
			return err
		}
		return registration.Close()
	}, nil
}
