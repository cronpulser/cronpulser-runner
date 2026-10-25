//go:build !windows

package runservice

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

func Run(run func(context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return run(ctx)
}
