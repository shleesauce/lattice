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

// TestInstanceIDStability asserts the per-process nonce is non-empty and stable
// within a process (every reconnect must carry the same value), which is what lets
// the hub tell a benign reconnect from a rival process.
func TestInstanceIDStability(t *testing.T) {
	if processInstanceID == "" {
		t.Fatal("process instance id must be non-empty")
	}
	if randomHex(16) == randomHex(16) {
		t.Fatal("randomHex must produce distinct values")
	}
	if newIdentity().instance != processInstanceID {
		t.Fatal("newIdentity must carry the stable process instance id")
	}
}
