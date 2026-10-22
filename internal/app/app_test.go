package app

import (
	"log/slog"
	"testing"

	"github.com/spf13/viper"
)

func TestNew(t *testing.T) {
	t.Parallel()

	a := New("1.2.3")

	if a.Logger == nil || a.LogLevel == nil || a.Viper == nil {
		t.Fatalf("New() = Logger %v, LogLevel %v, Viper %v, want all set", a.Logger, a.LogLevel, a.Viper)
	}
	if a.Version != "1.2.3" {
		t.Errorf("New().Version = %q, want %q", a.Version, "1.2.3")
	}
	if got := a.LogLevel.Level(); got != slog.LevelInfo {
		t.Errorf("New().LogLevel.Level() = %v, want %v", got, slog.LevelInfo)
	}
	if a.Stdout.Colour() {
		t.Errorf("New().Stdout.Colour() = true, want false")
	}
	if a.Stderr.Colour() {
		t.Errorf("New().Stderr.Colour() = true, want false")
	}
}

func TestNew_logLevelControlsLogger(t *testing.T) {
	t.Parallel()

	a := New("")
	ctx := t.Context()

	if a.Logger.Enabled(ctx, slog.LevelDebug) {
		t.Errorf("Logger.Enabled(debug) = true before LogLevel.Set, want false")
	}
	if !a.Logger.Enabled(ctx, slog.LevelInfo) {
		t.Errorf("Logger.Enabled(info) = false, want true")
	}

	derived := a.Logger.With("component", "cli")
	a.LogLevel.Set(slog.LevelDebug)

	if !a.Logger.Enabled(ctx, slog.LevelDebug) {
		t.Errorf("Logger.Enabled(debug) = false after LogLevel.Set(debug), want true")
	}
	if !derived.Enabled(ctx, slog.LevelDebug) {
		t.Errorf("derived Logger.Enabled(debug) = false after LogLevel.Set(debug), want true")
	}
}

func TestNew_independentViper(t *testing.T) {
	t.Parallel()

	a, b := New(""), New("")

	if a.Viper == b.Viper {
		t.Errorf("two New() calls share a Viper, want one each")
	}
	if a.Viper == viper.GetViper() {
		t.Errorf("New().Viper is the global viper, want its own")
	}

	a.Viper.Set("location", "uksouth")
	if b.Viper.IsSet("location") {
		t.Errorf("setting a key on one App's Viper set it on another's")
	}

	a.LogLevel.Set(slog.LevelDebug)
	if b.Logger.Enabled(t.Context(), slog.LevelDebug) {
		t.Errorf("setting one App's LogLevel enabled debug on another's Logger")
	}
}
