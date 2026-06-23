package hub

import (
	"context"
	"flag"
	"fmt"
	"os"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// SetPassword implements `lattice hub set-password`: it sets (or rotates) the
// dashboard admin password on an existing hub config, so a LEGACY hub — one that
// predates the setup wizard and has no password — can opt INTO Phase 3 admin auth
// without re-running the wizard. It also flips SetupComplete to an explicit true,
// so the resulting config is unambiguously "configured + password-protected".
//
// Non-interactive by design (no TTY prompt dependency): the password comes from
// --password or the LATTICE_ADMIN_PASSWORD env var.
//
//	lattice hub set-password --password 'hunter2longenough'
//	LATTICE_ADMIN_PASSWORD='…' lattice hub set-password
func SetPassword(ctx context.Context, args []string, version string) error {
	_ = ctx
	_ = version

	fs := flag.NewFlagSet("hub set-password", flag.ContinueOnError)
	password := fs.String("password", "", "admin password (or set LATTICE_ADMIN_PASSWORD)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pw := *password
	if pw == "" {
		pw = os.Getenv("LATTICE_ADMIN_PASSWORD")
	}
	if pw == "" {
		return fmt.Errorf("hub set-password: provide a password via --password or LATTICE_ADMIN_PASSWORD")
	}
	if utf8.RuneCountInString(pw) < 8 {
		return fmt.Errorf("hub set-password: password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	if err != nil {
		return fmt.Errorf("hub set-password: hash password: %w", err)
	}

	// Load-modify-save so every other field (mesh, projectsRoot, excludedDevices,
	// …) is preserved.
	cfg := LoadConfig()
	cfg.AdminPasswordHash = string(hash)
	cfg.SetupComplete = boolPtr(true)
	if err := SaveConfig(cfg); err != nil {
		return fmt.Errorf("hub set-password: %w", err)
	}

	fmt.Println("lattice: admin password set — restart the hub for it to take effect.")
	return nil
}
