package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/MoonCodeMaster/AIOS/internal/cli"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Signal handling. Ctrl+C must exit the CLI even when the REPL is
	// blocked on a stdin read — bufio.Scanner.Scan does not respond to
	// context cancellation, so we close os.Stdin to wake it. A second
	// signal during shutdown is a hard exit.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	signaled := make(chan struct{})
	go func() {
		select {
		case <-sigCh:
			close(signaled)
			cancel()
			// Closing stdin makes any blocked Scan() return false.
			// Best-effort: ignore errors (already closed, no tty, etc.).
			_ = os.Stdin.Close()
		case <-ctx.Done():
			return
		}
		// Second signal — escape hatch if cleanup is wedged.
		<-sigCh
		fmt.Fprintln(os.Stderr, "aios: forced exit")
		os.Exit(130)
	}()

	err := cli.NewRootCmd().ExecuteContext(ctx)

	// If we were signaled, exit 130 quietly (matches the bash convention
	// for SIGINT). Pipeline errors emitted on the way out are already
	// printed by their owning command; we don't need to reprint them.
	select {
	case <-signaled:
		os.Exit(130)
	default:
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "aios:", err)
		os.Exit(1)
	}
}
