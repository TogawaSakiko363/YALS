package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"YALS/internal/config"

	_ "modernc.org/sqlite"
)

// CommandRecord represents a persisted command definition for an agent.
type CommandRecord struct {
	Name         string `json:"name"`
	Template     string `json:"template"`
	UsePlugin    string `json:"use_plugin"`
	IgnoreTarget bool   `json:"ignore_target"`
	MaximumQueue int    `json:"maxmium_queue"`
	OrderIndex   int    `json:"order_index"`
}

// AgentRecord represents a stored agent registration and runtime config.
type AgentRecord struct {
	UUID      string              `json:"uuid"`
	Token     string              `json:"token"`
	Name      string              `json:"name"`
	Group     string              `json:"group"`
	Details   config.AgentDetails `json:"details"`
	Commands  []CommandRecord     `json:"commands"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
	SortOrder int                 `json:"sort_order"`
}

// AgentUpsertInput is used for create/update requests.
type AgentUpsertInput struct {
	UUID     string              `json:"uuid"`
	Token    string              `json:"token"`
	Name     string              `json:"name"`
	Group    string              `json:"group"`
	Details  config.AgentDetails `json:"details"`
	Commands []CommandRecord     `json:"commands"`
}

// Store provides SQLite-backed persistence for control-plane data.
//
// It keeps two connection pools over the same WAL-mode database: dbW is a single
// writer connection (SQLite allows only one writer), and dbR is a pool of reader
// connections. Under WAL, readers never block the writer and vice versa, so the
// heavy probe aggregate/series reads run concurrently with the high-frequency
// agent metric/probe writes instead of serializing behind them. Writes/DDL use
// dbW; pure SELECTs use dbR. Auto-committed writes are visible to dbR immediately.
type Store struct {
	dbW *sql.DB
	dbR *sql.DB
}

// NewStore opens the SQLite database and initializes schema.
func NewStore(dbPath string) (*Store, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, errors.New("database path is required")
	}

	cleanPath := filepath.Clean(dbPath)
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// PRAGMAs are encoded in the DSN so every pooled connection applies them.
	// WAL + synchronous=NORMAL removes the per-commit full fsync that dominated
	// write cost; busy_timeout absorbs brief contention; the rest reduce I/O.
	pragmas := strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=temp_store(MEMORY)",
		"_pragma=cache_size(-16000)", // ~16MB page cache per connection
		"_pragma=foreign_keys(ON)",
		"_pragma=mmap_size(268435456)", // 256MB
	}, "&")
	dsn := cleanPath + "?" + pragmas

	dbW, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite writer: %w", err)
	}
	// SQLite permits a single writer; pin the writer pool to one connection.
	dbW.SetMaxOpenConns(1)
	dbW.SetMaxIdleConns(1)
	dbW.SetConnMaxLifetime(0)

	dbR, err := sql.Open("sqlite", dsn)
	if err != nil {
		_ = dbW.Close()
		return nil, fmt.Errorf("open sqlite reader: %w", err)
	}
	readConns := runtime.NumCPU()
	if readConns < 4 {
		readConns = 4
	}
	dbR.SetMaxOpenConns(readConns)
	dbR.SetMaxIdleConns(readConns)
	dbR.SetConnMaxLifetime(0)

	store := &Store{dbW: dbW, dbR: dbR}
	if err := store.initSchema(); err != nil {
		_ = dbW.Close()
		_ = dbR.Close()
		return nil, err
	}

	return store, nil
}

// Close closes both connection pools.
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	var firstErr error
	if s.dbR != nil {
		if err := s.dbR.Close(); err != nil {
			firstErr = err
		}
	}
	if s.dbW != nil {
		if err := s.dbW.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *Store) initSchema() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agents (
			uuid TEXT PRIMARY KEY,
			token TEXT NOT NULL DEFAULT '',
			name TEXT NOT NULL,
			group_name TEXT NOT NULL,
			details_json TEXT NOT NULL,
			commands_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_agents_group_name ON agents(group_name);`,
		`CREATE INDEX IF NOT EXISTS idx_agents_name ON agents(name);`,
		`ALTER TABLE agents ADD COLUMN token TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE agents ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0;`,
		`CREATE TABLE IF NOT EXISTS runtime_settings (
			key TEXT PRIMARY KEY,
			value_json TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS agent_metrics (
			agent_uuid TEXT PRIMARY KEY,
			updated_at TEXT NOT NULL,
			cpu_percent REAL NOT NULL DEFAULT 0,
			mem_used INTEGER NOT NULL DEFAULT 0,
			mem_total INTEGER NOT NULL DEFAULT 0,
			disk_used INTEGER NOT NULL DEFAULT 0,
			disk_total INTEGER NOT NULL DEFAULT 0,
			net_up_rate REAL NOT NULL DEFAULT 0,
			net_down_rate REAL NOT NULL DEFAULT 0,
			net_up_total INTEGER NOT NULL DEFAULT 0,
			net_down_total INTEGER NOT NULL DEFAULT 0,
			uptime_sec INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS probe_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent_uuid TEXT NOT NULL,
			agent_name TEXT NOT NULL,
			target_name TEXT NOT NULL,
			ts INTEGER NOT NULL,
			latency_ms REAL NOT NULL DEFAULT 0,
			sent INTEGER NOT NULL DEFAULT 0,
			recv INTEGER NOT NULL DEFAULT 0
		);`,
		// (agent_name, target_name, ts) serves the series query (exact prefix) and
		// the aggregate query (agent_name prefix, already grouped/ordered the way
		// the window functions partition, avoiding a sort). (ts) lets the retention
		// pruner range-delete instead of full-scanning. The old single-purpose
		// indexes are dropped to cut per-insert index maintenance on the hot table.
		`CREATE INDEX IF NOT EXISTS idx_probe_results_atts ON probe_results(agent_name, target_name, ts);`,
		`CREATE INDEX IF NOT EXISTS idx_probe_results_ts ON probe_results(ts);`,
		`DROP INDEX IF EXISTS idx_probe_results_query;`,
		`DROP INDEX IF EXISTS idx_probe_results_target;`,
	}

	for _, stmt := range statements {
		if _, err := s.dbW.Exec(stmt); err != nil {
			lowerErr := strings.ToLower(err.Error())
			if strings.Contains(lowerErr, "duplicate column name") || strings.Contains(lowerErr, "already exists") {
				continue
			}
			return fmt.Errorf("initialize sqlite schema: %w", err)
		}
	}

	return nil
}

// UpsertRuntimeSettings saves hot-reloadable runtime settings.
func (s *Store) UpsertRuntimeSettings(settings config.RuntimeSettings) (*config.RuntimeSettings, error) {
	config.NormalizeRuntimeSettings(&settings)
	payload, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("marshal runtime settings: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.dbW.Exec(`
INSERT INTO runtime_settings (key, value_json, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    value_json = excluded.value_json,
    updated_at = excluded.updated_at
`, "server_runtime", string(payload), now)
	if err != nil {
		return nil, fmt.Errorf("upsert runtime settings: %w", err)
	}

	return s.GetRuntimeSettings()
}

// GetRuntimeSettings loads persisted runtime settings.
func (s *Store) GetRuntimeSettings() (*config.RuntimeSettings, error) {
	row := s.dbR.QueryRow(`SELECT value_json FROM runtime_settings WHERE key = ?`, "server_runtime")
	var payload string
	if err := row.Scan(&payload); err != nil {
		return nil, err
	}

	var settings config.RuntimeSettings
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return nil, fmt.Errorf("unmarshal runtime settings: %w", err)
	}
	config.NormalizeRuntimeSettings(&settings)
	return &settings, nil
}

// EnsureRuntimeSettings loads persisted settings or stores defaults on first boot.
func (s *Store) EnsureRuntimeSettings(defaults config.RuntimeSettings) (*config.RuntimeSettings, error) {
	settings, err := s.GetRuntimeSettings()
	if err == nil {
		return settings, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return s.UpsertRuntimeSettings(defaults)
}

// UpsertAgent creates or updates an agent configuration record.
func (s *Store) UpsertAgent(input AgentUpsertInput) (*AgentRecord, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Group = strings.TrimSpace(input.Group)
	input.Token = strings.TrimSpace(input.Token)
	if input.Name == "" {
		return nil, errors.New("agent name is required")
	}
	if input.Group == "" {
		input.Group = "Default"
	}
	if input.Token == "" {
		return nil, errors.New("agent token is required")
	}
	if len(input.Commands) == 0 {
		return nil, errors.New("at least one command is required")
	}

	normalizedCommands := normalizeCommands(input.Commands)
	detailsJSON, err := json.Marshal(input.Details)
	if err != nil {
		return nil, fmt.Errorf("marshal agent details: %w", err)
	}
	commandsJSON, err := json.Marshal(normalizedCommands)
	if err != nil {
		return nil, fmt.Errorf("marshal agent commands: %w", err)
	}

	now := time.Now().UTC()
	createdAt := now
	if existing, err := s.GetAgentByUUID(strings.TrimSpace(input.UUID)); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	uuidValue := strings.TrimSpace(input.UUID)
	if uuidValue == "" {
		return nil, errors.New("agent uuid is required")
	}

	// New agents are appended to the end of the operator-defined order. On a
	// conflict (update) the ON CONFLICT clause leaves sort_order untouched, so an
	// existing agent keeps its position.
	nextOrder := 0
	_ = s.dbR.QueryRow(`SELECT COALESCE(MAX(sort_order), 0) + 1 FROM agents`).Scan(&nextOrder)

	_, err = s.dbW.Exec(`
INSERT INTO agents (uuid, token, name, group_name, details_json, commands_json, created_at, updated_at, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(uuid) DO UPDATE SET
    token = excluded.token,
    name = excluded.name,
    group_name = excluded.group_name,
    details_json = excluded.details_json,
    commands_json = excluded.commands_json,
    updated_at = excluded.updated_at
`, uuidValue, input.Token, input.Name, input.Group, string(detailsJSON), string(commandsJSON), createdAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), nextOrder)
	if err != nil {
		return nil, fmt.Errorf("upsert agent: %w", err)
	}

	return s.GetAgentByUUID(uuidValue)
}

// GetAgentByUUID returns a stored agent by UUID.
func (s *Store) GetAgentByUUID(uuid string) (*AgentRecord, error) {
	row := s.dbR.QueryRow(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at, sort_order
FROM agents
WHERE uuid = ?
`, strings.TrimSpace(uuid))

	return scanAgent(row)
}

// GetAgentByName returns a stored agent by name.
func (s *Store) GetAgentByName(name string) (*AgentRecord, error) {
	row := s.dbR.QueryRow(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at, sort_order
FROM agents
WHERE name = ?
`, strings.TrimSpace(name))

	return scanAgent(row)
}

// ListAgents returns all stored agents in the operator-defined order
// (sort_order), falling back to group/name for ties or un-ordered rows.
func (s *Store) ListAgents() ([]AgentRecord, error) {
	rows, err := s.dbR.Query(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at, sort_order
FROM agents
ORDER BY sort_order ASC, group_name ASC, name ASC
`)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var agents []AgentRecord
	for rows.Next() {
		agentRecord, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agentRecord)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agents: %w", err)
	}

	return agents, nil
}

// DeleteAgent removes an agent from persistence.
func (s *Store) DeleteAgent(uuid string) error {
	result, err := s.dbW.Exec(`DELETE FROM agents WHERE uuid = ?`, strings.TrimSpace(uuid))
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete agent rows affected: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAgentOrder returns agent UUIDs in the operator-defined order. It is a cheap
// query used to order the Status page consistently with the control panel.
func (s *Store) ListAgentOrder() ([]string, error) {
	rows, err := s.dbR.Query(`SELECT uuid FROM agents ORDER BY sort_order ASC, group_name ASC, name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list agent order: %w", err)
	}
	defer rows.Close()
	var uuids []string
	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		uuids = append(uuids, uuid)
	}
	return uuids, rows.Err()
}

// UpdateAgentOrder persists a new operator-defined order: each UUID's sort_order
// is set to its index in the provided slice, in a single transaction.
func (s *Store) UpdateAgentOrder(uuids []string) error {
	tx, err := s.dbW.Begin()
	if err != nil {
		return fmt.Errorf("begin order update: %w", err)
	}
	stmt, err := tx.Prepare(`UPDATE agents SET sort_order = ? WHERE uuid = ?`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare order update: %w", err)
	}
	defer stmt.Close()
	for i, uuid := range uuids {
		if _, err := stmt.Exec(i, strings.TrimSpace(uuid)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update agent order: %w", err)
		}
	}
	return tx.Commit()
}

// BuildRuntimeConfig converts a stored agent record into runtime config for the agent process.
func BuildRuntimeConfig(host string, port int, record AgentRecord, logLevel string) *config.AgentConfig {
	runtimeConfig := &config.AgentConfig{}
	runtimeConfig.Server.Host = host
	runtimeConfig.Server.Port = port
	runtimeConfig.Server.UUID = record.UUID
	runtimeConfig.Agent.Name = record.Name
	runtimeConfig.Agent.Group = record.Group
	runtimeConfig.Agent.Details = record.Details
	runtimeConfig.Log.LogLevel = logLevel
	runtimeConfig.Commands = make(map[string]config.CommandTemplate, len(record.Commands))
	runtimeConfig.OrderedCommands = make([]string, 0, len(record.Commands))

	for _, cmd := range normalizeCommands(record.Commands) {
		runtimeConfig.Commands[cmd.Name] = config.CommandTemplate{
			Template:     cmd.Template,
			UsePlugin:    cmd.UsePlugin,
			IgnoreTarget: cmd.IgnoreTarget,
			MaximumQueue: cmd.MaximumQueue,
		}
		runtimeConfig.OrderedCommands = append(runtimeConfig.OrderedCommands, cmd.Name)
	}

	return config.NormalizeAgentConfig(runtimeConfig, nil)
}

func normalizeCommands(commands []CommandRecord) []CommandRecord {
	result := make([]CommandRecord, 0, len(commands))
	seen := make(map[string]struct{}, len(commands))

	for index, cmd := range commands {
		cmd.Name = strings.TrimSpace(cmd.Name)
		cmd.Template = strings.TrimSpace(cmd.Template)
		cmd.UsePlugin = strings.TrimSpace(cmd.UsePlugin)
		if cmd.Name == "" {
			continue
		}
		if _, exists := seen[cmd.Name]; exists {
			continue
		}
		seen[cmd.Name] = struct{}{}
		cmd.OrderIndex = index
		result = append(result, cmd)
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].OrderIndex < result[j].OrderIndex
	})

	return result
}

func scanAgent(scanner interface{ Scan(dest ...any) error }) (*AgentRecord, error) {
	var (
		record       AgentRecord
		detailsJSON  string
		commandsJSON string
		createdAtRaw string
		updatedAtRaw string
	)

	if err := scanner.Scan(&record.UUID, &record.Token, &record.Name, &record.Group, &detailsJSON, &commandsJSON, &createdAtRaw, &updatedAtRaw, &record.SortOrder); err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(detailsJSON), &record.Details); err != nil {
		return nil, fmt.Errorf("unmarshal agent details: %w", err)
	}
	if err := json.Unmarshal([]byte(commandsJSON), &record.Commands); err != nil {
		return nil, fmt.Errorf("unmarshal agent commands: %w", err)
	}
	if createdAtRaw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, createdAtRaw); err == nil {
			record.CreatedAt = parsed
		}
	}
	if updatedAtRaw != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, updatedAtRaw); err == nil {
			record.UpdatedAt = parsed
		}
	}

	record.Commands = normalizeCommands(record.Commands)
	return &record, nil
}
