// Package hub implements the Lattice controller: it accepts agent WebSocket
// connections, persists fleet state in SQLite, serves the REST API + embedded
// dashboard, and fans live events out to connected browsers.
package hub

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"log"
	"net/http"
	"sort"
	"time"
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

// Hub holds the shared runtime state for a running controller.
type Hub struct {
	version  string
	token    string
	store    *Store
	registry *Registry
}

// Run parses flags, opens the store, and serves until ctx is cancelled.
func Run(ctx context.Context, args []string, version string) error {
	fs := flag.NewFlagSet("hub", flag.ContinueOnError)
	addr := fs.String("addr", ":7400", "listen address")
	dbPath := fs.String("db", "lattice.db", "sqlite database path")
	token := fs.String("token", "", "enrollment token (random 8-char if empty)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *token == "" {
		*token = randomToken()
	}

	store, err := OpenStore(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	h := &Hub{
		version:  version,
		token:    *token,
		store:    store,
		registry: NewRegistry(),
	}

	mux := h.routes()
	srv := &http.Server{Addr: *addr, Handler: mux}

	go h.sweepLoop(ctx)

	log.Printf("lattice hub %s starting", version)
	log.Printf("  listen: %s", *addr)
	log.Printf("  db:     %s", *dbPath)
	log.Printf("  token:  %s   (enroll agents with --token %s)", *token, *token)

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

// broadcastFleet sends a full fleet snapshot to every dashboard client.
func (h *Hub) broadcastFleet() {
	h.registry.broadcast(map[string]any{
		"type":   "fleet",
		"agents": h.fleet(),
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

// randomToken returns an 8-char hex enrollment token.
func randomToken() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "lattice0"
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

// randomID returns a random 16-char hex id with a fallback prefix.
func randomID(prefix string) string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return prefix + time.Now().Format("150405.000")
	}
	return hex.EncodeToString(b)
}
