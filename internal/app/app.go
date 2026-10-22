// Package app holds the long-lived dependencies shared by every command.
package app

import (
	"log/slog"
	"os"

	"github.com/spf13/viper"

	"github.com/mike-guy/azskufinder/internal/output"
)

// App holds long-lived dependencies shared by every command. API clients and
// services are deliberately absent: they depend on parsed configuration, so
// commands construct them inside RunE.
type App struct {
	Version  string
	Logger   *slog.Logger
	LogLevel *slog.LevelVar
	Viper    *viper.Viper

	// Stdout and Stderr style text for each stream. Colour is decided per
	// stream, since either can be redirected on its own. Both start with
	// colour off; the command sets them before writing anything.
	Stdout output.Styles
	Stderr output.Styles
}

// New returns an App with a logger on stderr and an empty viper.
func New(version string) *App {
	// Log-level not known until flags parsed, so defaults to info.
	// Will not use info/warn logging (will use Cobra outputs), so
	// will only be applicable for debug logging anyway.
	logLevel := &slog.LevelVar{}
	logLevel.Set(slog.LevelInfo)
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(handler)

	v := viper.New()

	return &App{
		Version:  version,
		Logger:   logger,
		LogLevel: logLevel,
		Viper:    v,
	}
}
