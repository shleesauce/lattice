package hub

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/dylanstoryyy/lattice/internal/proto"
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
	// Scope is "project" (a synced ~/AI-Hub/projects/* worktree, auto-placeable)
	// or "device" (machine-local work pinned to one box, cwd = that box's home).
	Scope    string
	Archived bool
	// DeletedAt is the trash timestamp. Zero ⇒ not trashed. A trashed session is
	// hidden from the workspace (lives in Trash) and auto-purged after 30 days.
	DeletedAt    time.Time
	CreatedAt    time.Time
	LastActiveAt time.Time
}

// AuditEntry is one logged Claude tool event (D21). detail_json is a capped
// slice of the verbatim stream-json event for after-the-fact review.
type AuditEntry struct {
	ID         int64  `json:"id"`
	SessionID  string `json:"sessionId"`
	AgentID    string `json:"agentId"`
	EventType  string `json:"eventType"`
	ToolName   string `json:"toolName"`
	DetailJSON string `json:"detailJson"`
	At         string `json:"at"`
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
	created_at        TEXT,
	last_active_at    TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_agent_id ON sessions(agent_id);
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
`

// OpenStore opens (creating if needed) the SQLite database and ensures schema.
func OpenStore(path string) (*Store, error) {
	// WAL + busy_timeout: modernc.org/sqlite serializes writes, and every agent
	// heartbeat is a write. Under fleet load this avoids SQLITE_BUSY.
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
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
	} {
		if _, err := db.Exec(mig); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			db.Close()
			return nil, err
		}
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
	_, err := s.db.Exec(`
		INSERT INTO sessions (id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, created_at, last_active_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		rec.Title, rec.Status, pinned, scope, archived, deletedAt, created, active)
	return err
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

// PurgeDeletedBefore permanently removes trashed sessions whose deleted_at is
// older than cutoff (the trash TTL), with their audit rows. Returns the count.
func (s *Store) PurgeDeletedBefore(cutoff time.Time) (int, error) {
	c := cutoff.UTC().Format(time.RFC3339)
	rows, err := s.db.Query(`SELECT id FROM sessions WHERE deleted_at != '' AND deleted_at < ?`, c)
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
		SELECT id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, created_at, last_active_at
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
		SELECT id, project_path, kind, agent_id, claude_session_id, title, status, pinned, scope, archived, deleted_at, created_at, last_active_at
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
		createdAt string
		activeAt  string
	)
	if err := r.Scan(&rec.ID, &rec.ProjectPath, &rec.Kind, &rec.AgentID,
		&rec.ClaudeSessionID, &rec.Title, &rec.Status, &pinned, &rec.Scope, &archived, &deletedAt, &createdAt, &activeAt); err != nil {
		return SessionRecord{}, err
	}
	rec.Pinned = pinned != 0
	rec.Archived = archived != 0
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

// InsertAudit records one Claude tool event for after-the-fact review (D21).
func (s *Store) InsertAudit(sessionID, agentID, eventType, toolName, detailJSON string, at time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (session_id, agent_id, event_type, tool_name, detail_json, at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, sessionID, agentID, eventType, toolName, detailJSON, at.UTC().Format(time.RFC3339))
	return err
}

// ListAudit returns the audit trail for a session, oldest first.
func (s *Store) ListAudit(sessionID string) ([]AuditEntry, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, agent_id, event_type, tool_name, detail_json, at
		FROM audit_log WHERE session_id=? ORDER BY id ASC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.SessionID, &e.AgentID, &e.EventType,
			&e.ToolName, &e.DetailJSON, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
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
