// Package hub implements the Lattice controller: it accepts agent WebSocket
// connections, persists fleet state in SQLite, serves the REST API + embedded
// dashboard, and fans live events out to connected browsers.
package hub

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Registration errors.
var (
	errFirstFrame = errors.New("hub: first frame must be register")
	errBadToken   = errors.New("hub: invalid token")
)

// offlineAfter is how long without a heartbeat before an agent is considered
// offline. The agent heartbeat interval is 5s, so 15s tolerates two misses.
const offlineAfter = 15 * time.Second

// sweepInterval is how often the hub re-evaluates agent liveness.
const sweepInterval = 5 * time.Second

// trashTTL is how long a trashed (soft-deleted) session lives before it is
// permanently purged. trashSweepInterval is how often the purge runs.
const trashTTL = 30 * 24 * time.Hour
const trashSweepInterval = 1 * time.Hour

// reapGrace is how long an exited session may sit in the Active view before the
// reaper auto-archives it (F18). The grace lets you resume a just-exited claude;
// past it, dead sessions stop cluttering Active. reapInterval is the sweep cadence.
const reapGrace = 10 * time.Minute
const reapInterval = 2 * time.Minute

// cmdHistoryKeep caps the unbounded command_history table to its most recent N
// rows, reaped on the same cadence as the session reaper (reapLoop).
const cmdHistoryKeep = 1000

// auditLogKeep caps the unbounded audit_log table to its most recent N rows,
// reaped on the same cadence as command_history. Audit rows accrue per session
// tool-use and are otherwise only purged on hard session delete.
const auditLogKeep = 50000

// revokedTokenTTL is how long a revoked per-machine enroll token row is retained
// before the reaper drops it. The row is dead the moment it's revoked
// (EnrollTokenValid rejects it); the grace just keeps it visible in the token
// list for a while so an operator can still see a recent revocation.
const revokedTokenTTL = 30 * 24 * time.Hour

// agentReadTimeout bounds how long the hub waits for the next frame from an
// agent. Agents heartbeat every 5s, so a healthy link refreshes this on every
// read; a half-open socket (sleeping laptop, network partition) trips it and
// the read loop unwinds instead of leaking the goroutine + connection.
const agentReadTimeout = 20 * time.Second

// agentWriteTimeout bounds a hub→agent write so a dead/slow agent socket cannot
// block the dispatching HTTP handler or the per-heartbeat fleet broadcast.
const agentWriteTimeout = 10 * time.Second

// terminalWriteTimeout bounds a hub→browser terminal write so a stalled browser
// socket cannot block the agentws read loop forwarding PTY output.
const terminalWriteTimeout = 10 * time.Second

// terminalReadTimeout bounds how long the hub waits for the next frame from a
// browser terminal. An idle interactive shell still pings via gorilla control
// frames; a dead browser trips this and the bridge unwinds.
const terminalReadTimeout = 5 * time.Minute

// pendingTimeout bounds a file/wake round-trip to the agent before the hub
// gives up and returns an error to the HTTP caller.
const pendingTimeout = 10 * time.Second

// dashboardReadTimeout bounds how long the hub waits for the next frame (or pong)
// from a browser dashboard before tearing the connection down. The dashboard is
// read-mostly — the server pushes, the browser rarely sends — so without a
// deadline a half-open/silent socket (closed laptop lid, dropped Wi-Fi) leaks the
// read goroutine + conn forever. dashboardPingInterval is how often the hub sends
// a keepalive ping; each pong refreshes the read deadline, so a HEALTHY idle
// dashboard is never disconnected. Ping interval is comfortably under the timeout
// so two missed pongs are tolerated before the link is declared dead.
const dashboardReadTimeout = 60 * time.Second
const dashboardPingInterval = 25 * time.Second

// fleetBroadcastInterval is the max cadence at which per-heartbeat fleet churn is
// flushed to dashboards. Heartbeats arrive every 5s per agent, each triggering a
// full ListAgents + JSON marshal to every client (O(agents²) per tick); coalescing
// to at most one broadcast/sec collapses that without making the UI feel laggy.
// Membership changes (register/disconnect) still broadcast immediately.
const fleetBroadcastInterval = 1 * time.Second

// Hub holds the shared runtime state for a running controller.
type Hub struct {
	version      string
	token        string
	distDir      string
	projectsRoot string
	store        *Store
	registry     *Registry

	// Config-driven, de-personalized settings (LoadConfig → defaults).
	excludedDevices     []string
	projectRegistry     bool
	projectRegistryPath string
	previewPortMin      int
	previewPortMax      int
	// hubURL is the operator-configured canonical base URL (no trailing slash).
	// Empty ⇒ derive from the request Host (legacy behavior). Used by the OPEN
	// installer endpoints to avoid trusting a spoofed Host. See Config.HubURL.
	hubURL string

	// cfgMu guards the mutable config the first-run wizard can rewrite at runtime
	// (Phase 2): meshName, projectsRoot, adminPasswordHash, setupComplete. Read
	// these via the accessor methods (ProjectsRoot, needsSetup, …), never directly.
	cfgMu             sync.RWMutex
	meshName          string
	adminPasswordHash string
	setupComplete     bool

	// Auth (Phase 3): live login sessions + per-IP login rate limiter. Enforced
	// only when adminPasswordHash != "" (see auth.go).
	sessions     *sessionStore
	loginLimiter *loginLimiter

	// approvals holds in-memory phone approve/deny capabilities (fire-and-forget,
	// v0.1.5): armed when a session goes idle, consumed by the ntfy action link.
	approvals *approvalStore

	// releases memoizes the GitHub release list (release-notes panel + update check).
	releases *releaseCache

	// fleetDirty is set when a heartbeat changed fleet metrics; the coalescing
	// flushFleetLoop broadcasts at most once per fleetBroadcastInterval when set.
	fleetMu    sync.Mutex
	fleetDirty bool
}

// Run parses flags, opens the store, and serves until ctx is cancelled.
func Run(ctx context.Context, args []string, version string) error {
	// Config file (~/.lattice/config.json) supplies the de-personalized defaults;
	// an explicitly-passed flag still overrides because cfg values are the flag
	// defaults.
	cfg := LoadConfig()

	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	addr := fs.String("addr", cfg.Addr, "listen address")
	// Default the db into the ~/.lattice data dir (alongside config.json + the
	// token), NOT a bare relative "lattice.db". The persistent services run
	// `lattice hub` with no --db and an unpredictable cwd (launchd → /, systemd
	// --user → $HOME, nohup → wherever curl|sh ran), so a relative default lands
	// the db in the wrong place or fails to open at the fs root. pm2 (live hub)
	// passes --db explicitly, so it overrides this and is unaffected.
	dbPath := fs.String("db", filepath.Join(configDir(), "lattice.db"), "sqlite database path")
	token := fs.String("token", "", "enrollment token (random 128-bit if empty)")
	distDir := fs.String("dist", "dist", "directory of cross-compiled agent binaries served at /dl/")
	projectsRoot := fs.String("projects-root", cfg.ProjectsRoot, "directory scanned by /api/projects for workspace projects")
	insecure := fs.Bool("insecure-no-auth", false, "allow listening on a public address with no admin password (trusted-network opt-in)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Token resolution (only the EMPTY-flag path changes, so pm2's explicit
	// --token is untouched): flag → LATTICE_TOKEN env → persisted token file →
	// freshly generated (and persisted best-effort).
	if *token == "" {
		if env := os.Getenv("LATTICE_TOKEN"); env != "" {
			*token = env
		} else if persisted := LoadPersistedToken(); persisted != "" {
			*token = persisted
		} else {
			// randomToken panics if crypto/rand is unavailable — refuse to start
			// rather than mint a guessable master credential.
			*token = randomToken()
			if err := PersistToken(*token); err != nil {
				log.Printf("token: could not persist generated token: %v (continuing)", err)
			}
		}
	}

	store, err := OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	h := &Hub{
		version:             version,
		token:               *token,
		distDir:             *distDir,
		projectsRoot:        *projectsRoot,
		store:               store,
		registry:            NewRegistry(),
		meshName:            cfg.MeshName,
		excludedDevices:     cfg.ExcludedDevices,
		projectRegistry:     cfg.ProjectRegistry,
		projectRegistryPath: cfg.ProjectRegistryPath,
		previewPortMin:      cfg.PreviewPortMin,
		previewPortMax:      cfg.PreviewPortMax,
		hubURL:              strings.TrimRight(cfg.HubURL, "/"),
		adminPasswordHash:   cfg.AdminPasswordHash,
		// setupComplete is true unless the config EXPLICITLY says otherwise
		// (legacy/hand-written configs with no field are already-configured).
		setupComplete: !NeedsSetup(cfg),
		sessions:      newSessionStore(),
		loginLimiter:  newLoginLimiter(),
		approvals:     newApprovalStore(),
		releases:      newReleaseCache(),
	}

	// Secure-by-default: a fully-configured hub (setup done) with NO admin password
	// must not listen on a public interface unless the operator explicitly opts in.
	// On a passwordless hub requireAuth is a pass-through, so a public bind would
	// expose every admin route — including session-create (RCE on agents) and the
	// editor/preview proxies — to anyone who can reach the port. The first-run
	// window (setup not yet complete) is exempt so the wizard is reachable over the
	// network to set the password in the first place. LATTICE_INSECURE_NO_AUTH=1 is
	// an env-equivalent of --insecure-no-auth for service managers.
	insecureOptIn := *insecure || os.Getenv("LATTICE_INSECURE_NO_AUTH") == "1"
	if !h.authEnabled() && h.setupComplete && isPubliclyBound(*addr) && !insecureOptIn {
		return fmt.Errorf("refusing to listen on public address %q without an admin password: "+
			"set one with `lattice hub set-password`, or pass --insecure-no-auth "+
			"(or LATTICE_INSECURE_NO_AUTH=1) to run open on a trusted network", *addr)
	}

	mux := h.routes()
	// Only bound the header read — NOT ReadTimeout/WriteTimeout/IdleTimeout, which
	// would sever the long-lived WebSockets (/ws/agent, /ws/session, /ws/tunnel,
	// /ws/dashboard) and the editor/preview proxies. ReadHeaderTimeout mitigates
	// slow-header (Slowloris) without touching established connections.
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 15 * time.Second}

	go h.sweepLoop(ctx)
	go h.trashSweepLoop(ctx)
	go h.reapLoop(ctx)
	go h.flushFleetLoop(ctx)
	go h.sessionCleanupLoop(ctx)

	log.Printf("lattice hub %s starting", version)
	log.Printf("  mesh:   %s   (config: %s)", cfg.MeshName, configSource())
	log.Printf("  listen: %s", *addr)
	log.Printf("  db:     %s", *dbPath)
	log.Printf("  dist:   %s   (binaries served at /dl/)", *distDir)
	log.Printf("  projects: %s   (scanned by /api/projects)", *projectsRoot)
	log.Printf("  token:  %s   (full token in %s)", tokenHint(*token), func() string {
		if p := tokenFilePath(); p != "" {
			return p
		}
		return "~/.lattice/.lattice-token"
	}())

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		log.Printf("lattice hub shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

// isPubliclyBound reports whether addr listens on anything other than loopback.
// A wildcard host (":7400", "0.0.0.0", "::") binds every interface; a specific
// non-loopback IP or a hostname (e.g. a tailnet name) is routable. Only the
// loopback addresses (127.0.0.1, ::1, localhost) count as private.
func isPubliclyBound(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		return true
	}
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}

// sweepLoop periodically marks stale agents offline and broadcasts on change.
func (h *Hub) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if h.registry.sweepOffline(offlineAfter) {
				h.broadcastFleet()
			}
		}
	}
}

// trashSweepLoop permanently purges trashed sessions older than trashTTL (30d).
// Runs once at startup, then hourly. Broadcasts only when something was purged.
func (h *Hub) trashSweepLoop(ctx context.Context) {
	purge := func() {
		n, err := h.store.PurgeDeletedBefore(time.Now().Add(-trashTTL))
		if err != nil {
			log.Printf("trash sweep: %v", err)
			return
		}
		if n > 0 {
			log.Printf("trash sweep: purged %d session(s) older than 30d", n)
			h.broadcastSessions()
		}
	}
	purge()
	ticker := time.NewTicker(trashSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			purge()
		}
	}
}

// reapLoop auto-archives exited sessions older than reapGrace (F18), so dead
// sessions don't linger in Active forever. Runs once at startup, then every
// reapInterval. Broadcasts only when something was reaped.
func (h *Hub) reapLoop(ctx context.Context) {
	reap := func() {
		n, err := h.store.ArchiveExitedBefore(time.Now().Add(-reapGrace))
		if err != nil {
			log.Printf("reap sweep: %v", err)
		} else if n > 0 {
			log.Printf("reap sweep: archived %d exited session(s)", n)
			h.broadcastSessions()
		}
		// Bound the command_history table on the same cadence (no session
		// broadcast: it's not part of the workspace view).
		if c, err := h.store.ReapCommandHistory(cmdHistoryKeep); err != nil {
			log.Printf("reap sweep: command history: %v", err)
		} else if c > 0 {
			log.Printf("reap sweep: pruned %d old command-history row(s)", c)
		}
		// Bound the audit_log table (row-count cap) and drop long-revoked
		// per-machine enroll tokens (age cutoff). Neither affects the workspace
		// view, so no broadcast.
		if c, err := h.store.ReapAuditLog(auditLogKeep); err != nil {
			log.Printf("reap sweep: audit log: %v", err)
		} else if c > 0 {
			log.Printf("reap sweep: pruned %d old audit-log row(s)", c)
		}
		if c, err := h.store.ReapRevokedEnrollTokens(time.Now().Add(-revokedTokenTTL)); err != nil {
			log.Printf("reap sweep: revoked enroll tokens: %v", err)
		} else if c > 0 {
			log.Printf("reap sweep: pruned %d long-revoked enroll token(s)", c)
		}
		// Drop expired fire-and-forget approval nonces + stale expected-exit markers.
		h.approvals.sweep(time.Now())
	}
	reap()
	ticker := time.NewTicker(reapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reap()
		}
	}
}

// broadcastFleet sends a full fleet snapshot to every dashboard client. Used for
// membership changes (register/disconnect) and sweep transitions where the UI
// should update instantly. Per-heartbeat metric churn goes through markFleetDirty
// instead, which the flush loop coalesces to ≤1 broadcast/sec.
func (h *Hub) broadcastFleet() {
	h.registry.broadcast(map[string]any{
		"type":   "fleet",
		"agents": h.fleet(),
	})
}

// markFleetDirty records that fleet metrics changed (a heartbeat) without
// broadcasting synchronously. flushFleetLoop fans the snapshot out on its next
// tick. This collapses N agents × every-5s heartbeats from O(agents²) marshals
// per tick down to one snapshot per fleetBroadcastInterval.
func (h *Hub) markFleetDirty() {
	h.fleetMu.Lock()
	h.fleetDirty = true
	h.fleetMu.Unlock()
}

// flushFleetLoop broadcasts a coalesced fleet snapshot at most once per
// fleetBroadcastInterval, but only when a heartbeat marked the fleet dirty.
func (h *Hub) flushFleetLoop(ctx context.Context) {
	ticker := time.NewTicker(fleetBroadcastInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.fleetMu.Lock()
			dirty := h.fleetDirty
			h.fleetDirty = false
			h.fleetMu.Unlock()
			if dirty {
				h.broadcastFleet()
			}
		}
	}
}

// broadcastSessions pushes a fresh session snapshot to every dashboard client
// so session mutations (archive/restore/delete) reflect instantly, ahead of the
// workspace's periodic poll. Best-effort: a list error just skips the push.
func (h *Hub) broadcastSessions() {
	recs, err := h.store.ListSessions()
	if err != nil {
		return
	}
	out := make([]sessionView, 0, len(recs))
	for _, rec := range recs {
		out = append(out, toSessionView(rec))
	}
	h.registry.broadcast(map[string]any{
		"type":     "sessions",
		"sessions": out,
	})
}

// fleet returns the dashboard view of every known machine: the UNION of agents
// persisted in the store (shown offline with last-known metrics + MACs) and the
// live registry (online + fresh metrics override). Offline/disconnected
// machines therefore remain visible — required so a sleeping host can still be
// woken via WoL. Stable-sorted by id.
func (h *Hub) fleet() []Agent {
	live := h.registry.snapshot(offlineAfter)
	byID := make(map[string]Agent, len(live))
	for _, a := range live {
		byID[a.ID] = a
	}

	persisted, err := h.store.ListAgents()
	if err != nil {
		log.Printf("fleet: list persisted agents failed: %v", err)
	} else {
		for _, rec := range persisted {
			if _, ok := byID[rec.ID]; ok {
				// Live registry entry wins (fresh metrics / online state).
				continue
			}
			byID[rec.ID] = agentFromRecord(rec)
		}
	}

	// Overlay operator-set display-name labels (Phase 4 rename). Loaded once here
	// so a rename survives the agent's re-register (which UPSERTs Name=hostname).
	if labels, err := h.store.AgentLabels(); err != nil {
		log.Printf("fleet: load agent labels failed: %v", err)
	} else if len(labels) > 0 {
		for id, a := range byID {
			if label, ok := labels[id]; ok && label != "" {
				a.Name = label
				byID[id] = a
			}
		}
	}

	out := make([]Agent, 0, len(byID))
	for _, a := range byID {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// agentFromRecord builds an offline dashboard Agent from a persisted row and
// its last-known heartbeat metrics.
func agentFromRecord(rec AgentRecord) Agent {
	return Agent{
		ID:           rec.ID,
		Name:         rec.Name,
		Hostname:     rec.Hostname,
		OS:           rec.OS,
		Arch:         rec.Arch,
		AgentVersion: rec.AgentVersion,
		Online:       false,
		LastSeen:     rec.LastSeen.UTC().Format(time.RFC3339),
		UptimeSec:    rec.Metrics.UptimeSec,
		DiskTotal:    rec.Metrics.DiskTotal,
		DiskFree:     rec.Metrics.DiskFree,
		MemTotal:     rec.Metrics.MemTotal,
		DiskUsedPct:  rec.Metrics.DiskUsedPct,
		MemUsedPct:   rec.Metrics.MemUsedPct,
		LoadAvg1:     rec.Metrics.LoadAvg1,
		CPUCount:     rec.Metrics.CPUCount,
		MACs:         copyMACs(rec.Metrics.MACs),
	}
}

// matchesMasterToken is the single constant-time comparison of a presented secret
// against the master token (h.token). Every master-token check — agent/tunnel
// enrollment, the admin API Bearer, and isMasterToken — routes through here so the
// comparison and its locking are defined in exactly one place. No lock is taken:
// h.token is set once in Run() and never mutated at runtime (unlike
// adminPasswordHash, which set-password/setup rewrite under cfgMu), so a read is
// race-free. An empty presented value never matches (the master token is always
// non-empty), which keeps the auth fallback from treating a missing header as the
// master credential.
func (h *Hub) matchesMasterToken(presented string) bool {
	if presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(h.token)) == 1
}

// tokenValid reports whether a presented enrollment token is acceptable. The
// MASTER token (h.token) is ALWAYS valid and never revocable — the whole live
// fleet enrolls with it (baked into every service's launchd/systemd/schtask
// args), so rejecting it would drop every agent on reconnect. A per-machine
// token (Phase 4) is valid only while it exists and is not revoked. Constant-time
// compare on the master path avoids a token-length/value timing oracle.
func (h *Hub) tokenValid(presented string) bool {
	if h.matchesMasterToken(presented) {
		return true
	}
	ok, err := h.store.EnrollTokenValid(presented)
	if err != nil {
		log.Printf("token: enroll-token lookup failed: %v", err)
		return false
	}
	return ok
}

// isMasterToken reports whether a presented token IS the master token (so the
// caller can skip per-machine bookkeeping like BindEnrollToken for master enrolls).
func (h *Hub) isMasterToken(presented string) bool {
	return h.matchesMasterToken(presented)
}

// tokenMayActForAgent reports whether a presented enrollment token is allowed to
// register/act as agentID. This binds an action to the token's IDENTITY, not just
// its validity, so a holder of one machine's per-machine token cannot impersonate
// another machine (FIX 2: /ws/tunnel hijack). The rules mirror enrollment:
//   - the MASTER token is the trusted root and may act for ANY agentID;
//   - a per-machine token may act ONLY for the agentID it is bound to (the box that
//     enrolled with it via /ws/agent, recorded by BindEnrollToken);
//   - anything else (unknown/revoked/unbound token, or a mismatched agentID) is
//     rejected.
//
// A per-machine token is "unbound" until its agent's first /ws/agent register
// stamps agent_id; until then it cannot open a tunnel, which is correct — the
// agent's normal startup registers before it dials the tunnel.
func (h *Hub) tokenMayActForAgent(presented, agentID string) bool {
	if h.matchesMasterToken(presented) {
		return true
	}
	bound, ok, err := h.store.EnrollTokenAgentID(presented)
	if err != nil {
		log.Printf("token: enroll-token agent lookup failed: %v", err)
		return false
	}
	return ok && bound == agentID
}

// randomToken returns a 32-char hex enrollment token (16 bytes / 128 bits). This
// is the hub's long-lived master credential — it's both the agent-enrollment
// token and the admin API Bearer (D37) — so it gets full 128-bit entropy rather
// than a short code. It PANICS if crypto/rand is unavailable rather than emitting
// a predictable secret: a guessable master token (the old literal fallback) is far
// worse than refusing to start, and rand.Read never fails on supported platforms.
// Both call sites are startup mint paths (hub Run / hub init), so a panic is a
// loud refusal-to-start, and keeping the string signature avoids rippling into the
// init.go caller.
func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("hub: crypto/rand unavailable generating master token: %v", err))
	}
	return hex.EncodeToString(b)
}

// newCmdID returns a random 16-char hex command id.
func newCmdID() string {
	return randomID("cmd")
}

// newTermID returns a random terminal-session id.
func newTermID() string {
	return randomID("term")
}

// newReqID returns a random request-correlation id for file/wake round-trips.
func newReqID() string {
	return randomID("req")
}

// newSessionID returns a UUIDv4 used as the session row id AND the claude
// --session-id, so the Lattice sessionId IS the claudeSessionId (D17).
func newSessionID() string {
	return uuid.NewString()
}

// defaultProjectsRoot is $HOME/projects — the generic workspace root a stock hub
// scans. Operators who keep their projects elsewhere (e.g. a file-synced folder)
// set projectsRoot explicitly in config.json.
func defaultProjectsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "projects"
	}
	return filepath.Join(home, "projects")
}

// randomID returns a random 16-char hex id. It is used for cmd/term/req
// correlation ids whose unguessability gates which browser/agent can resolve a
// pending round-trip, so a predictable timestamp fallback was a (small) hijack
// surface. It now PANICS if crypto/rand is unavailable rather than emitting a
// guessable id — rand.Read never fails on supported platforms, and a hub with no
// entropy is unsafe to keep serving. The prefix is retained only as panic context.
func randomID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("hub: crypto/rand unavailable generating %s id: %v", prefix, err))
	}
	return hex.EncodeToString(b)
}
