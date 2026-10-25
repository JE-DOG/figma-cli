package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"example.com/figma-cli/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, "figma-rest:", err)
		os.Exit(1)
	}
}
