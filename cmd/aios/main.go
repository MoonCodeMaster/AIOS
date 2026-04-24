package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Solaxis/aios/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		fmt.Fprintln(os.Stderr, "aios: interrupt received, cancelling…")
	}()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "aios:", err)
		if ctx.Err() != nil {
			os.Exit(130)
		}
		os.Exit(1)
	}
	_ = signal.Ignore // retained for clarity
}
