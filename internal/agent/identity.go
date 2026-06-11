package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Agent identity (v0.2.0). Two ids, two lifetimes:
//
//   - The PERSISTENT agent id lives at ~/.lattice/agent-id. It is this machine's
//     stable identity across restarts, reinstalls, and even hostname changes. The
//     hub keys its registry on it, so it is what decouples "which machine" from
//     "what is the hostname" and what makes two same-hostname boxes distinct. On a
//     brand-new agent it is empty until the hub ASSIGNS one on first register (the
//     hub may reuse a legacy hostname+os id for an already-enrolled box so sessions
//     don't orphan, or mint a fresh UUID); the agent then persists what it's told.
//
//   - The per-process INSTANCE id (processInstanceID) is minted ONCE at process
//     start and never persisted. It lets the hub tell a normal network reconnect
//     (same process re-dialing, same instance) from two RIVAL processes claiming
//     one agent id (different instances) — the reconnect-storm class. See the hub's
//     duel detector.

// processInstanceID is this process's instance nonce, minted once at package load.
// Stable for the lifetime of the process (so every reconnect carries the same
// value), fresh on every (re)start (so a restarted agent is a distinct instance).
var processInstanceID = randomHex(16)

// identity is the shared, mutable holder for this agent's persistent id. Both the
// main /ws/agent link and the editor tunnel read it, and the main link WRITES it
// once the hub assigns an id on first enrollment. Guarded so the tunnel goroutine
// can read it concurrently with the register path setting it.
type identity struct {
	mu       sync.Mutex
	id       string // persistent agent id; "" until known (hub-assigned on first run)
	instance string // per-process instance nonce
}

// newIdentity loads any persisted agent id from ~/.lattice/agent-id and pairs it
// with this process's instance nonce. An absent/empty file ⇒ id "", which the hub
// fills in on first register.
func newIdentity() *identity {
	return &identity{id: loadPersistedAgentID(), instance: processInstanceID}
}

// get returns the current persistent id ("" until the hub has assigned one).
func (i *identity) get() string {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.id
}

// adopt records the hub-assigned id and persists it, but only the FIRST time (an
// already-known id is never overwritten — the persisted file is the source of
// truth once it exists). Best-effort persistence: a write failure is logged by the
// caller's path; the in-memory value is still set so the tunnel can proceed.
func (i *identity) adopt(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	i.mu.Lock()
	already := i.id != ""
	if !already {
		i.id = id
	}
	i.mu.Unlock()
	if !already {
		_ = persistAgentID(id)
	}
}

// agentIDPath is ~/.lattice/agent-id (the persistent id file).
func agentIDPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("agent: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".lattice", "agent-id"), nil
}

// loadPersistedAgentID returns the trimmed contents of ~/.lattice/agent-id, or ""
// if it is absent/unreadable/empty (in which case the hub assigns one).
func loadPersistedAgentID() string {
	p, err := agentIDPath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// persistAgentID writes the agent id under ~/.lattice (0700 dir, 0600 file) so it
// survives restarts. Mirrors the hub's token-file permissions — the id isn't a
// secret, but it shares the private data dir.
func persistAgentID(id string) error {
	p, err := agentIDPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("agent: mkdir lattice dir: %w", err)
	}
	if err := os.WriteFile(p, []byte(strings.TrimSpace(id)+"\n"), 0o600); err != nil {
		return fmt.Errorf("agent: write agent-id: %w", err)
	}
	return nil
}

// randomHex returns n random bytes as a lowercase hex string. Used for the
// per-process instance nonce. crypto/rand never fails on supported platforms; on
// the impossible error we panic rather than emit a constant nonce (which would
// make every process look like the same instance and defeat duel detection).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("agent: crypto/rand unavailable for instance id: %v", err))
	}
	return hex.EncodeToString(b)
}
