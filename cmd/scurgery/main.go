// Command scurgery makes surgical, reversible edits to a Tailscale tailnet
// policy file.
package main

import (
	"context"
	"os"

	"github.com/nopoz/scurgery/internal/cli"
)

func main() {
	os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Stdin))
}
