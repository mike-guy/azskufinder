// Command azskufinder finds Azure SKUs that a subscription can actually deploy.
package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/mike-guy/azskufinder/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return cli.Execute(ctx, buildVersion())
}

// buildVersion returns the version set with -ldflags or, for a build without
// it such as 'go install ...@v1.2.3', the module version Go recorded.
func buildVersion() string {
	if version != "dev" {
		return version
	}

	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	return version
}
