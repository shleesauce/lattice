package hub

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/hashicorp/yamux"

	"github.com/shleesauce/lattice/internal/proto"
)

// Agent is the JSON shape served over REST and inside fleet WS events. Keys
// match the dashboard contract exactly.
type Agent struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Hostname     string  `json:"hostname"`
	OS           string  `json:"os"`
	Arch         string  `json:"arch"`
	AgentVersion string  `json:"agentVersion"`
	Online       bool    `json:"online"`
	LastSeen     string  `json:"lastSeen"`
	UptimeSec    uint64  `json:"uptimeSec"`
	DiskTotal    uint64  `json:"diskTotal"`
	DiskFree     uint64  `json:"diskFree"`
	MemTotal     uint64  `json:"memTotal"`
	DiskUsedPct  float64 `json:"diskUsedPct"`
	MemUsedPct   float64 `json:"memUsedPct"`
	LoadAvg1     float64 `json:"loadAvg1"`
	CPUCount     int     `json:"cpuCount"`
	// MACs are the agent's last-known physical-interface hardware addresses,
	// surfaced so an OFFLINE machine can still be woken (WoL) by a peer on its
	// LAN. Filled from the most recent heartbeat (live or persisted).
	MACs []string `json:"macs"`
	// Capabilities is what the agent can run (Phase 3, D19). Drives placement's
	// hard filter and is shown in the fleet view.
	Capabilities proto.Capabilities `json:"capabilities"`
	// Local is true when the agent's WebSocket connects from loopback — i.e. it
	// runs on the hub host itself. Project scaffolding (POST /api/projects) prefers
	// the local agent so the freshly written files are already on disk at the exact
	// projDir path, avoiding the Syncthing propagation delay and the D23 home-path
	// divergence. Always false for persisted/offline agents.
	Local bool `json:"local"`
}

// agentConn is a live agent WebSocket. gorilla connections are not safe for
// concurrent writes, so every write goes through writeMu.
type agentConn struct {
	id       string
	name     string
	hostname string
	os       string
	arch     string
	version  string
	conn     *websocket.Conn
	local    bool // WS connected from loopback ⇒ co-located with the hub host
	writeMu  sync.Mutex

	mu       sync.Mutex
	metrics  proto.HeartbeatPayload
	caps     proto.Capabilities
	lastSeen time.Time
	online   bool
}

// wsSend is the one place a gorilla WebSocket text frame is written: it takes the
// write lock, optionally honors a closed flag (skip the write on an already-closed
// bridge), sets the per-call write deadline, and writes the frame. b is the
// already-marshaled payload. closed may be nil for conns that have no close flag.
func wsSend(conn *websocket.Conn, mu *sync.Mutex, closed *bool, deadline time.Duration, b []byte) error {
	mu.Lock()
	defer mu.Unlock()
	if closed != nil && *closed {
		return nil
	}
	conn.SetWriteDeadline(time.Now().Add(deadline))
	return conn.WriteMessage(websocket.TextMessage, b)
}

// send marshals an envelope and writes it as a WS text frame under the lock.
func (a *agentConn) send(t proto.MessageType, payload any) error {
	b, err := proto.Encode(t, payload)
	if err != nil {
		return err
	}
	return wsSend(a.conn, &a.writeMu, nil, agentWriteTimeout, b)
}

// isLive reports whether the agent has heartbeated within the window — used to
// reject command dispatch to an agent the sweep is about to drop.
func (a *agentConn) isLive(window time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.online && time.Since(a.lastSeen) <= window
}

func (a *agentConn) updateHeartbeat(m proto.HeartbeatPayload, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.metrics = m
	a.lastSeen = now
	a.online = true
	// Heartbeats refresh capabilities so placement scores fresh can-run state.
	// Only overwrite when the heartbeat carried them (zero-value claude+node
	// could be a probe miss; keep the last good value if so).
	if m.Capabilities.ClaudeInstalled || m.Capabilities.NodeInstalled {
		a.caps = m.Capabilities
	}
}

// view builds the dashboard Agent shape, computing online liveness live.
func (a *agentConn) view(window time.Duration, now time.Time) Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Agent{
		ID:           a.id,
		Name:         a.name,
		Hostname:     a.hostname,
		OS:           a.os,
		Arch:         a.arch,
		AgentVersion: a.version,
		Online:       a.online && now.Sub(a.lastSeen) <= window,
		LastSeen:     a.lastSeen.UTC().Format(time.RFC3339),
		UptimeSec:    a.metrics.UptimeSec,
		DiskTotal:    a.metrics.DiskTotal,
		DiskFree:     a.metrics.DiskFree,
		MemTotal:     a.metrics.MemTotal,
		DiskUsedPct:  a.metrics.DiskUsedPct,
		MemUsedPct:   a.metrics.MemUsedPct,
		LoadAvg1:     a.metrics.LoadAvg1,
		CPUCount:     a.metrics.CPUCount,
		MACs:         copyMACs(a.metrics.MACs),
		Capabilities: a.caps,
		Local:        a.local,
	}
}

// copyMACs returns a non-nil copy so the JSON view always serializes "macs" as
// [] rather than null.
func copyMACs(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// dashboardConn is a connected browser. Each has its own write mutex.
type dashboardConn struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

func (d *dashboardConn) send(obj any) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return wsSend(d.conn, &d.writeMu, nil, agentWriteTimeout, b)
}

// ping writes a WebSocket ping control frame under the same write lock the data
// pushes use, so the keepalive can never interleave with a broadcast on the same
// gorilla conn (which is not safe for concurrent writes). The read loop's pong
// handler refreshes the read deadline when the browser answers.
func (d *dashboardConn) ping() error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	d.conn.SetWriteDeadline(time.Now().Add(agentWriteTimeout))
	return d.conn.WriteMessage(websocket.PingMessage, nil)
}

// terminalConn is a live browser↔hub terminal WebSocket bridge. gorilla
// connections are not safe for concurrent writes, so every hub→browser write
// goes through writeMu.
type terminalConn struct {
	conn    *websocket.Conn
	agentID string
	writeMu sync.Mutex
	closed  bool
}

// send writes a JSON object to the browser terminal conn under its write lock,
// skipping the write if the bridge has already been closed.
func (t *terminalConn) send(obj any) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return wsSend(t.conn, &t.writeMu, &t.closed, terminalWriteTimeout, b)
}

// close marks the bridge closed and closes the underlying connection once.
func (t *terminalConn) close() {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	t.conn.Close()
}

// Registry tracks live agent connections, dashboard subscribers, browser
// terminal bridges (keyed by termId), and in-flight file/wake round-trips
// (keyed by reqId).
type Registry struct {
	mu         sync.RWMutex
	agents     map[string]*agentConn
	dashboards map[*dashboardConn]struct{}

	termMu sync.Mutex
	terms  map[string]*terminalConn

	pendMu  sync.Mutex
	pending map[string]chan proto.Envelope

	// tunnels holds the live yamux session per agent for the editor tunnel (D27),
	// keyed by agentId. The hub OPENS streams over it to reach an agent's
	// loopback-bound code-server; the agent never opens streams.
	tunMu   sync.Mutex
	tunnels map[string]*yamux.Session
}

// NewRegistry builds an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		agents:     make(map[string]*agentConn),
		dashboards: make(map[*dashboardConn]struct{}),
		terms:      make(map[string]*terminalConn),
		pending:    make(map[string]chan proto.Envelope),
		tunnels:    make(map[string]*yamux.Session),
	}
}

// putTunnel registers an agent's editor tunnel session, closing any prior one so
// a reconnecting agent's stale session can't linger and serve dead streams.
func (r *Registry) putTunnel(agentID string, s *yamux.Session) {
	r.tunMu.Lock()
	old := r.tunnels[agentID]
	r.tunnels[agentID] = s
	r.tunMu.Unlock()
	if old != nil && old != s {
		_ = old.Close()
	}
}

// getTunnel returns the live editor tunnel for an agent, if any (and not closed).
func (r *Registry) getTunnel(agentID string) (*yamux.Session, bool) {
	r.tunMu.Lock()
	defer r.tunMu.Unlock()
	s, ok := r.tunnels[agentID]
	if !ok || s.IsClosed() {
		return nil, false
	}
	return s, true
}

// removeTunnel drops an agent's tunnel mapping only if it is still the current one.
func (r *Registry) removeTunnel(agentID string, s *yamux.Session) {
	r.tunMu.Lock()
	defer r.tunMu.Unlock()
	if cur, ok := r.tunnels[agentID]; ok && cur == s {
		delete(r.tunnels, agentID)
	}
}

// putTerminal maps a termId to its browser bridge.
// putTerminal stores or replaces a browser bridge by termId/sessionId. When it
// displaces an existing bridge for the same key (a re-attach to a session),
// it closes the old bridge so its read loop errors out and the now-orphaned
// browser socket doesn't linger half-open until its read deadline.
func (r *Registry) putTerminal(termID string, t *terminalConn) {
	r.termMu.Lock()
	old := r.terms[termID]
	r.terms[termID] = t
	r.termMu.Unlock()
	if old != nil && old != t {
		old.close()
	}
}

// getTerminal returns the browser bridge for a termId.
func (r *Registry) getTerminal(termID string) (*terminalConn, bool) {
	r.termMu.Lock()
	defer r.termMu.Unlock()
	t, ok := r.terms[termID]
	return t, ok
}

// removeTerminal drops a termId mapping.
func (r *Registry) removeTerminal(termID string) {
	r.termMu.Lock()
	defer r.termMu.Unlock()
	delete(r.terms, termID)
}

// registerPending creates and stores a buffered result channel for a reqId.
func (r *Registry) registerPending(reqID string) chan proto.Envelope {
	ch := make(chan proto.Envelope, 1)
	r.pendMu.Lock()
	r.pending[reqID] = ch
	r.pendMu.Unlock()
	return ch
}

// resolvePending routes an agent result to the waiting channel for its reqId.
func (r *Registry) resolvePending(reqID string, env proto.Envelope) {
	r.pendMu.Lock()
	ch, ok := r.pending[reqID]
	if ok {
		delete(r.pending, reqID)
	}
	r.pendMu.Unlock()
	if ok {
		// Buffered (cap 1) and removed from the map, so this never blocks.
		ch <- env
	}
}

// clearPending removes a reqId channel (called on timeout / completion).
func (r *Registry) clearPending(reqID string) {
	r.pendMu.Lock()
	delete(r.pending, reqID)
	r.pendMu.Unlock()
}

// putAgent stores or replaces an agent connection by id. When it displaces a
// previous connection for the same id (a reconnect under the deterministic
// agent id — laptop wake, NAT rebind), it closes the old socket so its parked
// readLoop errors out immediately instead of lingering until the read deadline.
func (r *Registry) putAgent(a *agentConn) {
	r.mu.Lock()
	old := r.agents[a.id]
	r.agents[a.id] = a
	r.mu.Unlock()
	if old != nil && old != a {
		old.conn.Close()
	}
}

// removeAgent removes a connection only if it is still the registered one. It
// reports whether it actually deleted, so a stale connection's deferred cleanup
// can skip side effects (orphaning sessions, broadcasting) that would clobber
// the live reconnected agent sharing the same id.
func (r *Registry) removeAgent(a *agentConn) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur, ok := r.agents[a.id]; ok && cur == a {
		cur.mu.Lock()
		cur.online = false
		cur.mu.Unlock()
		delete(r.agents, a.id)
		return true
	}
	return false
}

// getAgent returns the live connection for an id.
func (r *Registry) getAgent(id string) (*agentConn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// liveAgent returns the connection for an id only if it is registered AND has
// heartbeated within offlineAfter. It folds the registered+isLive(offlineAfter)
// two-step every request handler needs into one call.
func (r *Registry) liveAgent(id string) (*agentConn, bool) {
	a, ok := r.getAgent(id)
	if !ok || !a.isLive(offlineAfter) {
		return nil, false
	}
	return a, true
}

// liveAgentCount returns how many agents are currently connected, from the
// in-memory registry only — no DB round-trip. Used by the unauthenticated
// /api/health liveness probe so a health-hammer can't starve the single SQLite
// connection by recomputing fleet() (up to 3 DB reads) on every hit.
func (r *Registry) liveAgentCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// snapshot returns the current fleet as dashboard Agent objects.
func (r *Registry) snapshot(window time.Duration) []Agent {
	now := time.Now()
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a.view(window, now))
	}
	// Stable order so the dashboard grid + selector don't reshuffle on every
	// heartbeat (map iteration is random).
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// sweepOffline flips agents past the window to offline; returns true if any
// agent state changed.
func (r *Registry) sweepOffline(window time.Duration) bool {
	now := time.Now()
	changed := false
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, a := range r.agents {
		a.mu.Lock()
		if a.online && now.Sub(a.lastSeen) > window {
			a.online = false
			changed = true
		}
		a.mu.Unlock()
	}
	return changed
}

// addDashboard registers a browser client.
func (r *Registry) addDashboard(d *dashboardConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dashboards[d] = struct{}{}
}

// removeDashboard drops a browser client.
func (r *Registry) removeDashboard(d *dashboardConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.dashboards, d)
}

// broadcast sends a JSON object to every dashboard client, dropping any that
// fail to write.
func (r *Registry) broadcast(obj any) {
	r.mu.RLock()
	clients := make([]*dashboardConn, 0, len(r.dashboards))
	for d := range r.dashboards {
		clients = append(clients, d)
	}
	r.mu.RUnlock()

	for _, d := range clients {
		if err := d.send(obj); err != nil {
			r.removeDashboard(d)
			d.conn.Close()
		}
	}
}
