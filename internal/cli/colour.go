package cli

import (
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/mike-guy/azskufinder/internal/app"
	"github.com/mike-guy/azskufinder/internal/output"
)

// setStyles decides colour for stdout and stderr separately, since either can
// be redirected on its own, and stores the result on application.
func setStyles(application *app.App, cmd *cobra.Command, noColour bool, log *slog.Logger) {
	streams := []struct {
		name   string
		w      io.Writer
		styles *output.Styles
	}{
		{"stdout", cmd.OutOrStdout(), &application.Stdout},
		{"stderr", cmd.ErrOrStderr(), &application.Stderr},
	}

	for _, s := range streams {
		on, reason := colourFor(isTerminal(s.w), noColour, os.Getenv)
		*s.styles = output.NewStyles(on)

		if !on {
			log.Debug("colour disabled", "stream", s.name, "reason", reason)
		}
	}
}

// colourFor reports whether a stream should be coloured, and if not, why. It
// is off for --no-colour, when the stream is not a terminal, when NO_COLOR is
// set (https://no-color.org) and when TERM is "dumb".
func colourFor(terminal, noColour bool, getenv func(string) string) (on bool, reason string) {
	switch {
	case noColour:
		return false, "no-colour"
	case !terminal:
		return false, "not a terminal"
	case getenv("NO_COLOR") != "":
		return false, "NO_COLOR"
	case getenv("TERM") == "dumb":
		return false, "TERM is dumb"
	default:
		return true, ""
	}
}
