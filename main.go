// Command lattice is a single binary with two roles selected by subcommand:
//
//	lattice hub     [--addr :7400] [--db lattice.db] [--token CODE]
//	lattice agent   --hub HOST:PORT --token CODE [--name NAME]
//	lattice version
//
// The same artifact ships to every machine; the role is a runtime choice. This
// is the core packageability decision (see docs/DECISIONS.md D1).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/dylanstoryyy/lattice/internal/agent"
	"github.com/dylanstoryyy/lattice/internal/hub"
)

// Version is stamped at build time via -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "hub":
		if err := hub.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "agent", "join":
		if err := agent.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "version", "--version", "-v":
		fmt.Printf("lattice %s\n", Version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "lattice: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lattice — packageable cross-platform mesh command center

usage:
  lattice hub     [--addr :7400] [--db PATH] [--token CODE]   run the controller + dashboard
  lattice agent   --hub HOST:PORT --token CODE [--name NAME]  run a leaf agent
  lattice version

`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "lattice: %v\n", err)
	os.Exit(1)
}
