package cli

// spinnerOn reports whether to show a progress spinner. It is off when the
// stream is not a terminal, TERM is "dumb" or debug logs are on, since log
// lines would tear through the animation.
func spinnerOn(terminal, debug bool, getenv func(string) string) bool {
	return terminal && !debug && getenv("TERM") != "dumb"
}
