package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Aayush9029/strata/internal/runner"
)

var version = "dev"

func main() {
	if err := runner.Run(context.Background(), os.Args[1:], version, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
