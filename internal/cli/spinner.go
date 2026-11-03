package cli

import (
	"io"
	"log/slog"
	"os"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/output"
)

// spinnerOn reports whether to show a progress spinner. It is off when the
// stream is not a terminal, TERM is "dumb" or debug logs are on, since log
// lines would tear through the animation.
func spinnerOn(terminal, debug bool, getenv func(string) string) bool {
	return terminal && !debug && getenv("TERM") != "dumb"
}

// startSpinner starts a spinner on stderr with message, or returns nil when it
// should not be shown. Stop on nil does nothing.
func startSpinner(stderr io.Writer, application *app.App, message string) *output.Spinner {
	debug := application.LogLevel.Level() <= slog.LevelDebug

	if !spinnerOn(isTerminal(stderr), debug, os.Getenv) {
		return nil
	}

	return output.StartSpinner(stderr, application.Stderr, message)
}
