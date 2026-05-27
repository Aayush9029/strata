package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/Aayush9029/strata/internal/runner"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := runner.Run(ctx, os.Args[1:], version, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
