// Command ndl is a CLI for the National Diet Library Search (NDLサーチ) APIs.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nightswinger/ndl-cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}
