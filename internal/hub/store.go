package hub

import (
	"database/sql"
	"encoding/json"
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
