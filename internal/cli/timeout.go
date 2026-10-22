package cli

import (
	"context"
	"time"
)

// withTimeout bounds a command's run by d, the --timeout setting. Zero or less
// means no limit. Callers must call the returned cancel func.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}

	return context.WithTimeout(ctx, d)
}
