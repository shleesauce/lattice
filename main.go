// Command lattice is a single binary with two roles selected by subcommand:
//
//	lattice hub init [--mesh NAME] [--projects-root DIR] [--addr :7400]
//	lattice hub set-password [--password PW]
//	lattice hub     [--addr :7400] [--db lattice.db] [--token CODE]
//	lattice agent   --hub HOST:PORT --token CODE [--name NAME]
//	lattice update  [--base URL] [--restart]
//	lattice doctor  [--json]
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

	"github.com/shleesauce/lattice/internal/agent"
	"github.com/shleesauce/lattice/internal/doctor"
	"github.com/shleesauce/lattice/internal/hub"
	"github.com/shleesauce/lattice/internal/uninstall"
	"github.com/shleesauce/lattice/internal/update"
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
		if len(os.Args) >= 3 && os.Args[2] == "init" {
			if err := hub.Init(ctx, os.Args[3:], Version); err != nil {
				fatal(err)
			}
		} else if len(os.Args) >= 3 && os.Args[2] == "set-password" {
			if err := hub.SetPassword(ctx, os.Args[3:], Version); err != nil {
				fatal(err)
			}
		} else if err := hub.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "agent", "join":
		if err := agent.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "update":
		if err := update.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "doctor":
		if err := doctor.Run(ctx, os.Args[2:], Version); err != nil {
			fatal(err)
		}
	case "uninstall":
		if err := uninstall.Run(ctx, os.Args[2:], Version); err != nil {
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
  lattice hub init [--mesh NAME] [--projects-root DIR] [--addr :7400]  write config + token, pick a free port
  lattice hub set-password [--password PW]                    set/rotate the dashboard admin password (or LATTICE_ADMIN_PASSWORD)
  lattice hub     [--addr :7400] [--db PATH] [--token CODE]   run the controller + dashboard
  lattice agent   --hub HOST:PORT --token CODE [--name NAME]  run a leaf agent
  lattice update  [--base URL] [--restart]                    self-update: swap in the latest release binary
  lattice doctor  [--json]                                    diagnose this machine: config, hub, capabilities, integrations
  lattice uninstall [--dry-run] [--yes]                       completely remove Lattice from this machine (services + ~/.lattice)
  lattice version

`)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "lattice: %v\n", err)
	os.Exit(1)
}
