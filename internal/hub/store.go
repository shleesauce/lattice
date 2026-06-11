package hub

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/shleesauce/lattice/internal/proto"
)

// Store is the SQLite-backed persistence layer for agents and command history.
type Store struct {
	db *sql.DB
}

// AgentRecord is a persisted agent row plus its last-known metrics.
type AgentRecord struct {
	ID           string
	Name         string
	Hostname     string
	OS           string
	Arch         string
	AgentVersion string
	FirstSeen    time.Time
	LastSeen     time.Time
	Metrics      proto.HeartbeatPayload
}

// SessionRecord is a persisted long-lived session row (Phase 3 / D18). The hub
// owns the row; the agent owns the live process. claude_session_id mirrors id
// for claude sessions (the hub assigns it as --session-id) and is empty for
// terminals.
type SessionRecord struct {
	ID              string
	ProjectPath     string
	Kind            string
	AgentID         string
	ClaudeSessionID string
	Title           string
	Status          string
	Pinned          bool
	// Scope is "project" (a synced projects-root worktree, auto-placeable) or
	// "device" (machine-local work pinned to one box, cwd = that box's home).
	Scope    string
	Archived bool
	// DeletedAt is the trash timestamp. Zero ⇒ not trashed. A trashed session is
	// hidden from the workspace (lives in Trash) and auto-purged after 30 days.
	DeletedAt time.Time
	// NotifyOnIdle opts this session into fire-and-forget phone notifications:
	// when it goes quiet (waiting on input) or exits, the hub pushes to ntfy. Set
	// at creation; preserved across re-adopt/resume (never reset by a reconnect).
	NotifyOnIdle bool
	// Model is the claude --model this session launched with (full model id, e.g.
	// claude-opus-4-8[1m]). Empty ⇒ claude's own default. Persisted so --resume
	// relaunches the same model and keeps one logical identity (D20).
	Model string
	// PRURL is the GitHub pull-request URL the PR-detection enricher (D) found in
	// this session's transcript. Empty until detected; set once (structural dedupe
	// for the one-shot "PR opened" push), surfaced as a card link.
	PRURL        string
	CreatedAt    time.Time
	LastActiveAt time.Time
}

const schema = `
CREATE TABLE IF NOT EXISTS agents (
	id            TEXT PRIMARY KEY,
	name          TEXT,
	hostname      TEXT,
	os            TEXT,
	arch          TEXT,
	agent_version TEXT,
	first_seen    TEXT,
	last_seen     TEXT,
	metrics_json  TEXT
);
CREATE TABLE IF NOT EXISTS command_history (
	cmd_id      TEXT PRIMARY KEY,
	agent_id    TEXT,
	command     TEXT,
	started_at  TEXT,
	finished_at TEXT,
	exit_code   INTEGER,
	error       TEXT
);
CREATE TABLE IF NOT EXISTS sessions (
	id                TEXT PRIMARY KEY,
	project_path      TEXT,
	kind              TEXT,
	agent_id          TEXT,
	claude_session_id TEXT,
	title             TEXT,
	status            TEXT,
	pinned            INTEGER DEFAULT 0,
	scope             TEXT DEFAULT 'project',
	archived          INTEGER DEFAULT 0,
	deleted_at        TEXT DEFAULT '',
	notify_on_idle    INTEGER DEFAULT 0,
	model             TEXT DEFAULT '',
	pr_url            TEXT DEFAULT '',
	created_at        TEXT,
	last_active_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_agent_id ON sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status_active ON sessions(status, archived, deleted_at);
CREATE TABLE IF NOT EXISTS audit_log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	session_id  TEXT,
	agent_id    TEXT,
	event_type  TEXT,
	tool_name   TEXT,
	detail_json TEXT,
	at          TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_session_id ON audit_log(session_id);
CREATE TABLE IF NOT EXISTS settings (
	key   TEXT PRIMARY KEY,
	value TEXT
);
CREATE TABLE IF NOT EXISTS agent_labels (
	agent_id TEXT PRIMARY KEY,
	label    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS enroll_tokens (
	token        TEXT PRIMARY KEY,
	label        TEXT,
	created_at   INTEGER,
	revoked_at   INTEGER,
	last_used_at INTEGER,
	agent_id     TEXT
);
`

// OpenStore opens (creating if needed) the SQLite database and ensures schema.
func OpenStore(path string) (*Store, error) {
	// WAL + busy_timeout: modernc.org/sqlite serializes writes, and every agent
	// heartbeat is a write. Under fleet load this avoids SQLITE_BUSY. We also pin
	// the pool to a single connection (SetMaxOpenConns(1)): writes are serialized
	// anyway, and with a multi-connection pool the WAL auto-checkpoint never runs
	// reliably (a separate idle conn holds a read lock), so the WAL grew without
	// bound. One connection lets wal_autocheckpoint(1000) actually fire;
	// synchronous(NORMAL) is the recommended durability tradeoff under WAL.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=wal_autocheckpoint(1000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	// Idempotent column migrations for DBs created before a column was added.
	// SQLite has no "ADD COLUMN IF NOT EXISTS"; a duplicate-column error is benign.
	for _, mig := range []string{
		`ALTER TABLE sessions ADD COLUMN scope TEXT DEFAULT 'project'`,
		`ALTER TABLE sessions ADD COLUMN archived INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN deleted_at TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN notify_on_idle INTEGER DEFAULT 0`,
		`ALTER TABLE sessions ADD COLUMN model TEXT DEFAULT ''`,
		`ALTER TABLE sessions ADD COLUMN pr_url TEXT DEFAULT ''`,
	} {
		if _, err := db.Exec(mig); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, err
		}
	}
	// Collapse any WAL that grew under the old multi-conn pool back into the main
	// db file at startup. Best-effort: a checkpoint failure is non-fatal.
	if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		log.Printf("store: startup wal_checkpoint failed: %v", err)
	}
	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// UpsertAgent inserts or updates an agent identity on (re)register. first_seen
// is preserved on conflict; last_seen and identity fields are refreshed.
func (s *Store) UpsertAgent(rec AgentRecord) error {
	now := rec.LastSeen.UTC().Format(time.RFC3339)
	first := rec.FirstSeen.UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`
		INSERT INTO agents (id, name, hostname, os, arch, agent_version, first_seen, last_seen, metrics_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '')
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			hostname=excluded.hostname,
			os=excluded.os,
			arch=excluded.arch,
			agent_version=excluded.agent_version,
			last_seen=excluded.last_seen
	`, rec.ID, rec.Name, rec.Hostname, rec.OS, rec.Arch, rec.AgentVersion, first, now)
	return err
}

// UpdateMetrics stores the latest heartbeat metrics + last_seen for an agent.
func (s *Store) UpdateMetrics(id string, m proto.HeartbeatPayload, seen time.Time) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE agents SET metrics_json=?, last_seen=? WHERE id=?`,
		string(raw), seen.UTC().Format(time.RFC3339), id)
	return err
}

// ListAgents returns every persisted agent with its last-known metrics parsed
// from metrics_json. A malformed/empty metrics blob yields zero-value metrics
// rather than failing the whole listing. Used by the hub to keep offline
// machines visible in the fleet view (and woken via WoL).
func (s *Store) ListAgents() ([]AgentRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, name, hostname, os, arch, agent_version, first_seen, last_seen, metrics_json
		FROM agents
		ORDER BY id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentRecord
	for rows.Next() {
		var (
			rec         AgentRecord
			firstSeen   string
			lastSeen    string
			metricsJSON sql.NullString
		)
		if err := rows.Scan(&rec.ID, &rec.Name, &rec.Hostname, &rec.OS, &rec.Arch,
			&rec.AgentVersion, &firstSeen, &lastSeen, &metricsJSON); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, firstSeen); err == nil {
			rec.FirstSeen = t
		}
		if t, err := time.Parse(time.RFC3339, lastSeen); err == nil {
			rec.LastSeen = t
		}
		if metricsJSON.Valid && metricsJSON.String != "" {
			var m proto.HeartbeatPayload
			if err := json.Unmarshal([]byte(metricsJSON.String), &m); err == nil {
				rec.Metrics = m
			}
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// --- Phase 4: agent labels (display-name overrides) ---

// SetAgentLabel upserts a human display-name override for an agent. Survives the
// agent's re-register (which UPSERTs Name back to the hostname), so a rename
// sticks across reconnects. fleet() overlays the label onto Agent.Name.
func (s *Store) SetAgentLabel(id, label string) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_labels (agent_id, label) VALUES (?, ?)
		ON CONFLICT(agent_id) DO UPDATE SET label=excluded.label
	`, id, label)
	return err
}

// AgentLabels returns every label override keyed by agent id. Loaded once per
// fleet() call so the overlay is a single query, not one per agent.
func (s *Store) AgentLabels() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT agent_id, label FROM agent_labels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, label string
		if err := rows.Scan(&id, &label); err != nil {
			return nil, err
		}
		out[id] = label
	}
	return out, rows.Err()
}

// DeleteAgentLabel removes an agent's display-name override (e.g. on remove).
func (s *Store) DeleteAgentLabel(id string) error {
	_, err := s.db.Exec(`DELETE FROM agent_labels WHERE agent_id=?`, id)
	return err
}

// DeleteAgent removes an agent identity row. The agent's sessions are left to
// MarkAgentSessionsOrphaned; a removed box re-enrolling with the MASTER token
// reappears (only its per-machine enroll token, if any, is revoked).
func (s *Store) DeleteAgent(id string) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id=?`, id)
	return err
}

// --- Phase 4: per-machine revocable enrollment tokens ---

// EnrollTokenRecord is a persisted per-machine enrollment token. Zero-value
// times mean unset (created_at is always set; revoked_at/last_used_at start 0).
type EnrollTokenRecord struct {
	Token      string
	Label      string
	CreatedAt  time.Time
	RevokedAt  time.Time
	LastUsedAt time.Time
	AgentID    string
}

// CreateEnrollToken mints a new per-machine enrollment token row (created_at=now,
// not revoked, never used, unbound). The master token h.token is NOT stored here —
// it is always valid and never revocable (tokenValid handles it separately).
func (s *Store) CreateEnrollToken(token, label string) error {
	_, err := s.db.Exec(`
		INSERT INTO enroll_tokens (token, label, created_at, revoked_at, last_used_at, agent_id)
		VALUES (?, ?, ?, 0, 0, '')
	`, token, label, time.Now().Unix())
	return err
}

// ListEnrollTokens returns every per-machine enrollment token, newest first.
func (s *Store) ListEnrollTokens() ([]EnrollTokenRecord, error) {
	rows, err := s.db.Query(`
		SELECT token, label, created_at, revoked_at, last_used_at, agent_id
		FROM enroll_tokens
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EnrollTokenRecord
	for rows.Next() {
		var (
			rec      EnrollTokenRecord
			label    sql.NullString
			agentID  sql.NullString
			created  int64
			revoked  int64
			lastUsed int64
		)
		if err := rows.Scan(&rec.Token, &label, &created, &revoked, &lastUsed, &agentID); err != nil {
			return nil, err
		}
		rec.Label = label.String
		rec.AgentID = agentID.String
		if created > 0 {
			rec.CreatedAt = time.Unix(created, 0)
		}
		if revoked > 0 {
			rec.RevokedAt = time.Unix(revoked, 0)
		}
		if lastUsed > 0 {
			rec.LastUsedAt = time.Unix(lastUsed, 0)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// errNoEnrollToken is returned by RevokeEnrollToken when no matching, not-already-
// revoked per-machine token row was affected (unknown or already-revoked token).
// The handler maps it to a 404 instead of reporting {"ok":true} for a no-op.
var errNoEnrollToken = errors.New("hub: no such enroll token")

// RevokeEnrollToken marks a per-machine token revoked (revoked_at=now). A revoked
// token fails EnrollTokenValid, so the box can no longer enroll with it (the
// master token still works — that's the intended escape hatch). Returns
// errNoEnrollToken when the token is unknown or already revoked (RowsAffected==0)
// so the handler doesn't falsely report success for a token it never touched.
func (s *Store) RevokeEnrollToken(token string) error {
	res, err := s.db.Exec(`UPDATE enroll_tokens SET revoked_at=? WHERE token=? AND revoked_at=0`,
		time.Now().Unix(), token)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errNoEnrollToken
	}
	return nil
}

// RevokeEnrollTokenForAgent revokes whatever per-machine token a removed agent
// enrolled with (no-op if it enrolled with the master token). Used by remove.
func (s *Store) RevokeEnrollTokenForAgent(agentID string) error {
	_, err := s.db.Exec(`UPDATE enroll_tokens SET revoked_at=? WHERE agent_id=? AND revoked_at=0`,
		time.Now().Unix(), agentID)
	return err
}

// EnrollTokenValid reports whether a presented token is a known, non-revoked
// per-machine token. The master token is handled by tokenValid, not here.
func (s *Store) EnrollTokenValid(token string) (bool, error) {
	var revoked int64
	err := s.db.QueryRow(`SELECT revoked_at FROM enroll_tokens WHERE token=?`, token).Scan(&revoked)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return revoked == 0, nil
}

// EnrollTokenAgentID returns the agent a non-revoked per-machine token is bound
// to (set by BindEnrollToken on the agent's first /ws/agent register). ok=false
// when the token is unknown, revoked, or not yet bound to any agent — callers
// treat all of those the same: the token may not act for an arbitrary agent. The
// master token is NOT in this table; resolve it via matchesMasterToken first.
func (s *Store) EnrollTokenAgentID(token string) (agentID string, ok bool, err error) {
	var (
		bound   sql.NullString
		revoked int64
	)
	err = s.db.QueryRow(`SELECT agent_id, revoked_at FROM enroll_tokens WHERE token=?`, token).
		Scan(&bound, &revoked)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if revoked != 0 || !bound.Valid || bound.String == "" {
		return "", false, nil
	}
	return bound.String, true, nil
}

// BindEnrollToken records which agent enrolled with a per-machine token and
// stamps last_used_at. Best-effort on each (re)register so the operator can see
// which machine a token is on and when it last connected.
func (s *Store) BindEnrollToken(token, agentID string) error {
	_, err := s.db.Exec(`UPDATE enroll_tokens SET agent_id=?, last_used_at=? WHERE token=?`,
		agentID, time.Now().Unix(), token)
	return err
}

// InsertCommandStarted records a dispatched command with no exit yet.
func (s *Store) InsertCommandStarted(cmdID, agentID, command string, started time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO command_history (cmd_id, agent_id, command, started_at, finished_at, exit_code, error)
		VALUES (?, ?, ?, ?, '', NULL, '')
	`, cmdID, agentID, command, started.UTC().Format(time.RFC3339))
	return err
}

// FinishCommand records the exit code/error and finish time for a command.
func (s *Store) FinishCommand(cmdID string, exitCode int, errMsg string, finished time.Time) error {
	_, err := s.db.Exec(`
		UPDATE command_history SET finished_at=?, exit_code=?, error=? WHERE cmd_id=?
	`, finished.UTC().Format(time.RFC3339), exitCode, errMsg, cmdID)
	return err
}

// ReapCommandHistory bounds the otherwise-unbounded command_history table: a row
// is inserted on every dispatched command and never deleted. It keeps the most
// recent `keep` rows (by started_at) and drops the rest, so old exec logs don't
// grow the db forever. Returns the count deleted. Mirrors the time/grace-cutoff
// reaps (ArchiveExitedBefore, PurgeDeletedBefore) but caps by row count since the
// table has no natural TTL.
func (s *Store) ReapCommandHistory(keep int) (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM command_history
		WHERE cmd_id NOT IN (
			SELECT cmd_id FROM command_history ORDER BY started_at DESC LIMIT ?
		)
	`, keep)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// ReapAuditLog bounds the otherwise-unbounded audit_log table: a row is appended
// on every session tool-use/event and is only ever deleted when its session is
// hard-deleted. On a long-lived hub with retained sessions it grows without
// limit. This keeps the most recent `keep` rows (by auto-increment id, which is
// monotonic insert order) and drops the rest. It deliberately caps by row count
// rather than age — the same conservative shape as ReapCommandHistory — so a
// quiet fleet never loses recent history. Returns the count deleted.
// LogAudit appends one row to audit_log — the lifecycle/approval trail behind
// fire-and-forget (v0.1.5). detailJSON is an opaque JSON string ("" ⇒ NULL).
// Best-effort by design: the caller logs the error and continues, since losing an
// audit row must never break a session lifecycle transition. The reap loop
// (ReapAuditLog) caps total rows, so this is unbounded-safe.
func (s *Store) LogAudit(sessionID, agentID, eventType, toolName, detailJSON string, at time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (session_id, agent_id, event_type, tool_name, detail_json, at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sessionID, agentID, eventType, toolName, detailJSON, at.UTC().Format(time.RFC3339))
	return err
}

func (s *Store) ReapAuditLog(keep int) (int, error) {
	// Keep the newest `keep` rows. OFFSET ? lands on the (keep+1)-th newest id;
	// delete everything at or below it — a single index range scan on the PK, vs the
	// old `id NOT IN (SELECT …)` anti-join that scanned the whole table every 2 min
	// on the single write conn. If there are ≤ keep rows the subquery is NULL →
	// `id <= NULL` deletes nothing.
	res, err := s.db.Exec(`
		DELETE FROM audit_log
		WHERE id <= (SELECT id FROM audit_log ORDER BY id DESC LIMIT 1 OFFSET ?)
	`, keep)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// ReapRevokedEnrollTokens drops per-machine enroll tokens that were revoked
// before cutoff. A revoked token is dead weight — EnrollTokenValid already
// rejects it — so once it has been revoked long enough that no operator is still
// reviewing the revocation, the row can go. It is conservative on purpose: only
// rows with revoked_at > 0 AND revoked_at < cutoff are touched, so a live/valid
// token (revoked_at == 0) is NEVER deleted, and a recently-revoked one stays
// visible in the token list for a while. The master token is not in this table.
// Returns the count deleted.
func (s *Store) ReapRevokedEnrollTokens(cutoff time.Time) (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM enroll_tokens WHERE revoked_at > 0 AND revoked_at < ?
	`, cutoff.Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// --- Phase 3: sessions, audit, settings ---

// UpsertSession inserts or updates a session row. created_at is preserved on
// conflict; the mutable fields (agent, status, identity, activity) are refreshed
// so this serves both initial create and re-discovery self-heal.
func (s *Store) UpsertSession(rec SessionRecord) error {
	created := rec.CreatedAt.UTC().Format(time.RFC3339)
	active := rec.LastActiveAt.UTC().Format(time.RFC3339)
	pinned := 0
	if rec.Pinned {
		pinned = 1
	}
	scope := rec.Scope
	if scope == "" {
		scope = "project"
	}
	archived := 0
	if rec.Archived {
		archived = 1
	}
	deletedAt := ""
	if !rec.DeletedAt.IsZero() {
		deletedAt = rec.DeletedAt.UTC().Format(time.RFC3339)
	}
	notify := 0
	if rec.NotifyOnIdle {
		notify = 1
	}
	// notify_on_idle and model are set on INSERT only — deliberately absent from the
	// conflict UPDATE so a re-adopt/resume (which rebuilds the record) can't silently
	// turn off an operator's fire-and-forget opt-in or wipe the launched model. The
	// resume path reads the stored model and threads it back in, so the value carries
	// forward intact (D20 — one logical identity, same model on every relaunch).
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, notify_on_idle, model, pr_url, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			project_path=excluded.project_path,
			kind=excluded.kind,
			agent_id=excluded.agent_id,
			claude_session_id=excluded.claude_session_id,
			title=excluded.title,
			status=excluded.status,
			pinned=excluded.pinned,
			scope=excluded.scope,
			archived=excluded.archived,
			deleted_at=excluded.deleted_at,
			last_active_at=excluded.last_active_at
	`, rec.ID, rec.ProjectPath, rec.Kind, rec.AgentID, rec.ClaudeSessionID,
		rec.Title, rec.Status, pinned, scope, archived, deletedAt, notify, rec.Model, rec.PRURL, created, active)
	return err
}

// SetSessionPRURLIfEmpty records a detected PR URL only if the session doesn't
// already have one (atomic compare-and-set in SQL: WHERE pr_url=”). Returns true
// when THIS call set it — the caller fires the one-shot "PR opened" push only then,
// so a racing re-scan from another turn can't double-fire (D).
func (s *Store) SetSessionPRURLIfEmpty(id, prURL string, at time.Time) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE sessions SET pr_url=?, last_active_at=? WHERE id=? AND (pr_url='' OR pr_url IS NULL)`,
		prURL, at.UTC().Format(time.RFC3339), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateSessionStatus sets a session's status and bumps last_active_at.
func (s *Store) UpdateSessionStatus(id, status string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE sessions SET status=?, last_active_at=? WHERE id=?`,
		status, at.UTC().Format(time.RFC3339), id)
	return err
}

// SetSessionArchived flips a session's archived flag. Archived sessions are
// hidden from the active workspace tree but kept in the store (and DB), so they
// can be restored or reviewed — unlike DeleteSession, which ends the process.
func (s *Store) SetSessionArchived(id string, archived bool, at time.Time) error {
	v := 0
	if archived {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE sessions SET archived=?, last_active_at=? WHERE id=?`,
		v, at.UTC().Format(time.RFC3339), id)
	return err
}

// SetSessionTitle renames a session (I — session naming, v0.1.5). Used by the
// manual rename (PATCH /api/sessions/{id} {title}) and by the auto-namer. Touch
// last_active_at so the rename also nudges the row to the top of the recent list.
func (s *Store) SetSessionTitle(id, title string, at time.Time) error {
	_, err := s.db.Exec(`UPDATE sessions SET title=?, last_active_at=? WHERE id=?`,
		title, at.UTC().Format(time.RFC3339), id)
	return err
}

// DeleteSessionRow permanently removes a session (and its audit rows) from the
// store. The caller is responsible for ending the live process first.
func (s *Store) DeleteSessionRow(id string) error {
	if _, err := s.db.Exec(`DELETE FROM audit_log WHERE session_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

// SetSessionDeleted moves a session to Trash (deleted=true) or restores it
// (deleted=false). Trashed sessions are hidden from the workspace and auto-purged
// after the trash TTL; the row is kept so a trashed session can be restored.
func (s *Store) SetSessionDeleted(id string, deleted bool, at time.Time) error {
	val := ""
	if deleted {
		val = at.UTC().Format(time.RFC3339)
	}
	_, err := s.db.Exec(`UPDATE sessions SET deleted_at=?, last_active_at=? WHERE id=?`,
		val, at.UTC().Format(time.RFC3339), id)
	return err
}

// PurgeAllDeleted permanently removes every trashed session (and its audit
// rows) immediately — the "Empty Trash" action. Returns the count purged.
func (s *Store) PurgeAllDeleted() (int, error) {
	return s.purgeDeleted(`SELECT id FROM sessions WHERE deleted_at != ''`)
}

// PurgeDeletedBefore permanently removes trashed sessions whose deleted_at is
// older than cutoff (the trash TTL), with their audit rows. Returns the count.
func (s *Store) PurgeDeletedBefore(cutoff time.Time) (int, error) {
	return s.purgeDeleted(`SELECT id FROM sessions WHERE deleted_at != '' AND deleted_at < ?`, cutoff.UTC().Format(time.RFC3339))
}

// purgeDeleted runs the given id-selecting query and hard-deletes each match
// (session row + audit rows). Shared by PurgeAllDeleted / PurgeDeletedBefore.
func (s *Store) purgeDeleted(query string, args ...any) (int, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if err := s.DeleteSessionRow(id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// SetSessionAgent rebinds a session to an agent and records its claude session
// id. Used on (re-)placement and re-discovery so an orphaned session can resume
// on a different machine under the same row id (D20).
func (s *Store) SetSessionAgent(id, agentID, claudeSessionID string) error {
	_, err := s.db.Exec(`UPDATE sessions SET agent_id=?, claude_session_id=? WHERE id=?`,
		agentID, claudeSessionID, id)
	return err
}

// ListSessions returns every session row, newest activity first.
func (s *Store) ListSessions() ([]SessionRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, notify_on_idle, model, pr_url, created_at, last_active_at
		FROM sessions
		ORDER BY last_active_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SessionRecord
	for rows.Next() {
		rec, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// GetSession returns one session row by id. ok=false when no row exists.
func (s *Store) GetSession(id string) (SessionRecord, bool, error) {
	row := s.db.QueryRow(`
		SELECT id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, notify_on_idle, model, pr_url, created_at, last_active_at
		FROM sessions WHERE id=?
	`, id)
	rec, err := scanSession(row)
	if err == sql.ErrNoRows {
		return SessionRecord{}, false, nil
	}
	if err != nil {
		return SessionRecord{}, false, err
	}
	return rec, true, nil
}

// MarkAgentSessionsOrphaned flips every live/starting session on an agent to
// orphaned when that agent disconnects. Rows are kept so the session stays
// visible and resumable elsewhere (D20).
func (s *Store) MarkAgentSessionsOrphaned(agentID string) error {
	_, err := s.db.Exec(`
		UPDATE sessions SET status=? WHERE agent_id=? AND status IN (?, ?, ?)
	`, proto.SessionOrphaned, agentID, proto.SessionStarting, proto.SessionLive, proto.SessionDetached)
	return err
}

// MarkAgentSessionsExitedExcept reaps an agent's zombie session rows (F18). When
// an agent (re-)reports its authoritative live-session list, any of THIS agent's
// rows still marked running (live/detached/orphaned) but absent from that list has
// lost its process — mark it exited. 'starting' is left alone (a create may be
// mid-flight and not yet listed). last_active_at is deliberately NOT bumped so the
// reaper measures its grace from the real last activity (a long-dead session gets
// archived promptly, not after a fresh grace window). Returns the count reaped.
func (s *Store) MarkAgentSessionsExitedExcept(agentID string, keep map[string]bool) (int, error) {
	rows, err := s.db.Query(`
		SELECT id FROM sessions
		WHERE agent_id=? AND archived=0 AND deleted_at='' AND status IN (?, ?, ?)
	`, agentID, proto.SessionLive, proto.SessionDetached, proto.SessionOrphaned)
	if err != nil {
		return 0, err
	}
	var dead []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		if !keep[id] {
			dead = append(dead, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, id := range dead {
		if _, err := s.db.Exec(`UPDATE sessions SET status=? WHERE id=?`, proto.SessionExited, id); err != nil {
			return 0, err
		}
	}
	return len(dead), nil
}

// ArchiveExitedBefore auto-archives sessions whose process has exited and which
// are still sitting in the Active view (not archived, not trashed) past the grace
// cutoff (F18). Dead sessions stop cluttering Active + inflating the count, but stay
// recoverable in Archived (and reviewable once the transcript view lands). The
// grace (cutoff measured against last_active_at) means a just-exited session you
// might want to resume right away isn't yanked out from under you. Returns the
// count archived.
func (s *Store) ArchiveExitedBefore(cutoff time.Time) (int, error) {
	res, err := s.db.Exec(`
		UPDATE sessions SET archived=1
		WHERE status=? AND archived=0 AND deleted_at='' AND last_active_at < ?
	`, proto.SessionExited, cutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

// scanRow is the read interface shared by *sql.Row and *sql.Rows.
type scanRow interface {
	Scan(dest ...any) error
}

// scanSession reads one session row, parsing RFC3339 times leniently.
func scanSession(r scanRow) (SessionRecord, error) {
	var (
		rec       SessionRecord
		pinned    int
		archived  int
		deletedAt string
		notify    int
		createdAt string
		activeAt  string
	)
	if err := r.Scan(&rec.ID, &rec.ProjectPath, &rec.Kind, &rec.AgentID,
		&rec.ClaudeSessionID, &rec.Title, &rec.Status, &pinned, &rec.Scope, &archived, &deletedAt, &notify, &rec.Model, &rec.PRURL, &createdAt, &activeAt); err != nil {
		return SessionRecord{}, err
	}
	rec.Pinned = pinned != 0
	rec.Archived = archived != 0
	rec.NotifyOnIdle = notify != 0
	if deletedAt != "" {
		if t, err := time.Parse(time.RFC3339, deletedAt); err == nil {
			rec.DeletedAt = t
		}
	}
	if rec.Scope == "" {
		rec.Scope = "project"
	}
	if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
		rec.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, activeAt); err == nil {
		rec.LastActiveAt = t
	}
	return rec, nil
}

// GetSetting returns a settings value. ok=false when the key is unset.
func (s *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetSetting upserts a settings key/value.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}
