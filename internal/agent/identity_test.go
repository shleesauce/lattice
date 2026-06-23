package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// TestAgentIDPersistence covers load → persist → load and the adopt() write-once
// rule against a HOME we control so the real ~/.lattice is never touched.
func TestAgentIDPersistence(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)        // unix
	t.Setenv("USERPROFILE", tmp) // windows (os.UserHomeDir on GOOS=windows)

	// Absent file ⇒ empty (hub will assign).
	if got := loadPersistedAgentID(); got != "" {
		t.Fatalf("fresh box should have no persisted id, got %q", got)
	}

	if err := persistAgentID("agent-xyz"); err != nil {
		t.Fatalf("persist: %v", err)
	}
	// Written under ~/.lattice/agent-id with the expected path.
	p := filepath.Join(tmp, ".lattice", "agent-id")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("agent-id not written at %s: %v", p, err)
	}
	if got := loadPersistedAgentID(); got != "agent-xyz" {
		t.Fatalf("load after persist: got %q want agent-xyz", got)
	}

	// adopt() persists the first id and is a no-op once known (the file is the
	// source of truth — a later hub-assigned value must not clobber it).
	id := &identity{instance: processInstanceID} // id starts empty
	id.adopt("first-id")
	if id.get() != "first-id" {
		t.Fatalf("adopt should set first id, got %q", id.get())
	}
	id.adopt("second-id")
	if id.get() != "first-id" {
		t.Fatalf("adopt must not overwrite a known id, got %q", id.get())
	}
}

// TestAdoptRetriesOnPersistFailure asserts adopt() does not give up after a failed
// write: a box whose ~/.lattice is briefly unwritable still persists its id on a
// later register, instead of flapping to a new hub-minted UUID on every restart.
func TestAdoptRetriesOnPersistFailure(t *testing.T) {
	// Point HOME at a regular FILE so MkdirAll(~/.lattice) fails.
	bad := filepath.Join(t.TempDir(), "home-is-a-file")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Setenv("HOME", bad)
	t.Setenv("USERPROFILE", bad)

	id := &identity{instance: processInstanceID}
	id.adopt("agent-1")
	if id.get() != "agent-1" {
		t.Fatalf("adopt must set the in-memory id even when persistence fails, got %q", id.get())
	}
	if id.persisted {
		t.Fatal("persisted must stay false after a failed write so adopt retries")
	}

	// Disk recovers: a later register (same id) must now persist it.
	good := t.TempDir()
	t.Setenv("HOME", good)
	t.Setenv("USERPROFILE", good)
	id.adopt("agent-1")
	if !id.persisted {
		t.Fatal("adopt should persist on retry once the dir is writable")
	}
	if got := loadPersistedAgentID(); got != "agent-1" {
		t.Fatalf("retry persist: got %q want agent-1", got)
	}
}

// TestInstanceIDStability asserts the per-process nonce is non-empty and stable
// within a process (every reconnect must carry the same value), which is what lets
// the hub tell a benign reconnect from a rival process.
func TestInstanceIDStability(t *testing.T) {
	if processInstanceID == "" {
		t.Fatal("process instance id must be non-empty")
	}
	// Two separate calls (held in vars so staticcheck doesn't read it as one
	// identical expression compared to itself) must differ.
	a, b := randomHex(16), randomHex(16)
	if a == b {
		t.Fatal("randomHex must produce distinct values")
	}
	if newIdentity().instance != processInstanceID {
		t.Fatal("newIdentity must carry the stable process instance id")
	}
}
