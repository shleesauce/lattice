// Package uninstall implements `lattice uninstall`: it removes Lattice from the
// machine it runs on — stopping and unregistering the hub and/or agent service
// and deleting the ~/.lattice data directory — and nothing else.
//
// It is the offline, built-in twin of install/uninstall.sh: same labels, same
// paths, same "touch only Lattice's own footprint" guarantee, but discoverable
// from `lattice --help` and runnable with no network. It removes BOTH roles
// because a single box may run the hub, an agent, or both.
package uninstall

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// service is one persistence registration to tear down (a launchd agent, a
// systemd --user unit, or a Windows scheduled task).
type service struct {
	kind  string // "launchd" | "systemd" | "schtask"
	label string // sh.lattice.hub | lattice-hub | LatticeHub | …
	file  string // plist/unit path on disk ("" for a scheduled task)
}

func (s service) describe() string {
	if s.file != "" {
		return fmt.Sprintf("%s (%s)", s.label, s.file)
	}
	return s.label
}

// plan is the full, OS-specific set of things `lattice uninstall` will remove.
// It is produced by planFor (pure) so it can be unit-tested without touching the
// machine.
type plan struct {
	goos     string
	uid      int
	services []service
	dataDirs []string
}

// planFor computes the removal plan for an OS + home dir. localAppData is only
// used on Windows (the binary install dir). Keeping this pure makes the path/
// label contract testable; execution lives in (plan).execute.
func planFor(goos, home, localAppData string, uid int) plan {
	p := plan{goos: goos, uid: uid}
	switch goos {
	case "darwin":
		la := filepath.Join(home, "Library", "LaunchAgents")
		p.services = []service{
			{kind: "launchd", label: "sh.lattice.hub", file: filepath.Join(la, "sh.lattice.hub.plist")},
			{kind: "launchd", label: "sh.lattice.agent", file: filepath.Join(la, "sh.lattice.agent.plist")},
		}
		p.dataDirs = []string{filepath.Join(home, ".lattice")}
	case "windows":
		p.services = []service{
			{kind: "schtask", label: "LatticeHub"},
			{kind: "schtask", label: "LatticeAgent"},
		}
		p.dataDirs = []string{filepath.Join(home, ".lattice")}
		if localAppData != "" {
			p.dataDirs = append(p.dataDirs, filepath.Join(localAppData, "Lattice"))
		}
	default: // linux + other unixes use systemd --user
		ud := filepath.Join(home, ".config", "systemd", "user")
		p.services = []service{
			{kind: "systemd", label: "lattice-hub", file: filepath.Join(ud, "lattice-hub.service")},
			{kind: "systemd", label: "lattice-agent", file: filepath.Join(ud, "lattice-agent.service")},
		}
		p.dataDirs = []string{filepath.Join(home, ".lattice")}
	}
	return p
}

// Run is the `lattice uninstall` entry point.
func Run(ctx context.Context, args []string, version string) error {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "show exactly what would be removed, then exit without changing anything")
	yes := fs.Bool("yes", false, "skip the confirmation prompt (for scripts)")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "usage: lattice uninstall [--dry-run] [--yes]\n\n"+
			"Completely removes Lattice from this machine: stops + unregisters the hub\n"+
			"and/or agent service, then deletes ~/.lattice. Touches nothing else.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return fmt.Errorf("uninstall: cannot determine home directory: %w", err)
	}
	p := planFor(runtime.GOOS, home, os.Getenv("LOCALAPPDATA"), os.Getuid())

	fmt.Println("lattice uninstall — this will remove Lattice from this machine:")
	for _, s := range p.services {
		fmt.Printf("  • stop + remove service:  %s\n", s.describe())
	}
	for _, d := range p.dataDirs {
		fmt.Printf("  • delete data directory:  %s\n", d)
	}
	fmt.Println("\nIt will NOT touch your projects, your files, your shell config, Claude/~/.claude,")
	fmt.Println("or your Tailscale / SSH / Syncthing setup. Removing Lattice leaves the rest as-is.")

	if *dryRun {
		fmt.Println("\n(dry run — nothing was changed)")
		return nil
	}
	if !*yes && !confirm() {
		fmt.Println("aborted — nothing was changed.")
		return nil
	}

	// Stop/unregister services first so nothing is writing into the data dir as
	// we remove it. Every teardown step is best-effort: a service that isn't
	// installed is a no-op, not an error.
	for _, s := range p.services {
		stopService(ctx, s)
	}
	if p.goos == "linux" {
		_ = exec.CommandContext(ctx, "systemctl", "--user", "daemon-reload").Run()
	}
	for _, d := range p.dataDirs {
		stopPidfiles(ctx, d)
	}

	var failures []string
	for _, d := range p.dataDirs {
		if err := os.RemoveAll(d); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", d, err))
		}
	}

	if len(failures) > 0 {
		// On Windows the running lattice.exe locks its own install dir; the data
		// (config/db/token) is still gone, only the binary folder lingers.
		fmt.Println("\nlattice: removed services + data, but could not delete:")
		for _, f := range failures {
			fmt.Printf("  - %s\n", f)
		}
		if runtime.GOOS == "windows" {
			fmt.Println("lattice: this is the running binary's folder — delete it after this process exits,")
			fmt.Println("         or it will be removable on next reboot.")
		}
		return nil
	}

	fmt.Println("\nlattice: done — Lattice has been completely removed from this machine.")
	return nil
}

// stopService tears down one registration. Best-effort: errors are ignored
// because "not installed" is the common, expected case.
func stopService(ctx context.Context, s service) {
	switch s.kind {
	case "launchd":
		_ = exec.CommandContext(ctx, "launchctl", "bootout",
			"gui/"+strconv.Itoa(os.Getuid())+"/"+s.label).Run()
		_ = exec.CommandContext(ctx, "launchctl", "unload", s.file).Run()
		_ = os.Remove(s.file)
	case "systemd":
		_ = exec.CommandContext(ctx, "systemctl", "--user", "disable", "--now", s.label+".service").Run()
		_ = os.Remove(s.file)
	case "schtask":
		_ = exec.CommandContext(ctx, "schtasks", "/end", "/tn", s.label).Run()
		_ = exec.CommandContext(ctx, "schtasks", "/delete", "/tn", s.label, "/f").Run()
	}
}

// stopPidfiles kills any nohup-fallback process recorded under a ~/.lattice dir,
// so a non-systemd Linux agent/hub is stopped before its dir is removed.
func stopPidfiles(ctx context.Context, dir string) {
	for _, name := range []string{"hub.pid", "agent.pid"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		pid := strings.TrimSpace(string(b))
		if pid == "" {
			continue
		}
		if runtime.GOOS == "windows" {
			_ = exec.CommandContext(ctx, "taskkill", "/PID", pid, "/F").Run()
		} else {
			_ = exec.CommandContext(ctx, "kill", pid).Run()
		}
	}
}

// confirm reads a single y/N line from stdin. Anything not starting with y/Y is a
// "no", so an accidental Enter is safe.
func confirm() bool {
	fmt.Print("\nRemove Lattice now? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
