package hub

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
)

// Init implements `lattice hub init`: it writes ~/.lattice/config.json with
// setupComplete:false, persists a stable enrollment token, and picks a free
// listen address — everything the bootstrap installer needs to start the hub
// and hand the operator a "finish setup in the browser" URL. It is idempotent:
// re-running on an already-initialized hub is a no-op success unless --force.
func Init(ctx context.Context, args []string, version string) error {
	_ = ctx

	fs := flag.NewFlagSet("hub init", flag.ContinueOnError)
	projectsRoot := fs.String("projects-root", defaultProjectsRoot(), "workspace projects root")
	mesh := fs.String("mesh", "lattice", "mesh name")
	addr := fs.String("addr", "", "listen address (auto-pick a free port if empty)")
	force := fs.Bool("force", false, "re-initialize even if a config already exists")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("hub init: cannot determine home directory")
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return fmt.Errorf("hub init: %w", err)
	}

	existing := LoadConfig()
	// Idempotent: a config already on disk means this host was initialized (by a
	// prior `hub init`, whether or not the browser wizard has been finished). Don't
	// re-mint the token or re-pick the port — that would churn the listen address
	// out from under a running service. --force re-initializes from a clean slate.
	if configExists() && !*force {
		chosen := existing.Addr
		if chosen == "" {
			chosen = ":7400"
		}
		fmt.Println("lattice: already initialized (run with --force to re-init)")
		if NeedsSetup(existing) {
			fmt.Printf("lattice: open %s to finish setup\n", dashboardURL(chosen))
		} else {
			fmt.Printf("lattice: open %s to manage the hub\n", dashboardURL(chosen))
		}
		return nil
	}

	// Stable token: reuse the persisted one if present, else mint + persist.
	token := LoadPersistedToken()
	if token == "" {
		token = randomToken()
		if err := PersistToken(token); err != nil {
			return fmt.Errorf("hub init: persist token: %w", err)
		}
	}

	chosenAddr := *addr
	if chosenAddr == "" {
		chosenAddr = pickFreeAddr()
	}

	// Build the config to save. Start from the loaded config (preserve fields)
	// unless --force, which resets to a clean generic baseline.
	cfg := existing
	if *force {
		cfg = defaultConfig()
	}
	cfg.ProjectsRoot = *projectsRoot
	cfg.MeshName = *mesh
	cfg.Addr = chosenAddr
	cfg.SetupComplete = boolPtr(false)
	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("hub init: %w", err)
	}

	fmt.Println("lattice: initialized hub config")
	fmt.Printf("  config:   %s\n", configPath())
	fmt.Printf("  projects: %s\n", cfg.ProjectsRoot)
	fmt.Printf("  mesh:     %s\n", cfg.MeshName)
	fmt.Printf("  listen:   %s\n", cfg.Addr)
	fmt.Println()
	fmt.Printf("lattice: start the hub (the installer does this for you), then open %s to finish setup.\n", dashboardURL(chosenAddr))
	return nil
}

// configDir returns ~/.lattice (the parent of config.json).
func configDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".lattice"
	}
	return home + string(os.PathSeparator) + ".lattice"
}

// dashboardURL renders the finish-setup URL from the hostname and the chosen
// addr (which is ":PORT"), e.g. http://my-mac:7400/.
func dashboardURL(addr string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("http://%s%s/", host, addr)
}

// pickFreeAddr returns a ":PORT" listen address. It prefers the conventional
// :7400; if that is taken it asks the OS for any free port on loopback and
// returns that. Falls back to ":7400" if even that fails (Run will surface the
// bind error).
func pickFreeAddr() string {
	if ln, err := net.Listen("tcp", ":7400"); err == nil {
		ln.Close()
		return ":7400"
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return ":7400"
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	return fmt.Sprintf(":%d", port)
}
