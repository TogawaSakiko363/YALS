package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
	Description  string `json:"description"`
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
type Store struct {
	db *sql.DB
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

	db, err := sql.Open("sqlite", cleanPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	store := &Store{db: db}
	if err := store.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
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
	}

	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			lowerErr := strings.ToLower(err.Error())
			if strings.Contains(lowerErr, "duplicate column name") || strings.Contains(lowerErr, "already exists") {
				continue
			}
			return fmt.Errorf("initialize sqlite schema: %w", err)
		}
	}

	return nil
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

	_, err = s.db.Exec(`
INSERT INTO agents (uuid, token, name, group_name, details_json, commands_json, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(uuid) DO UPDATE SET
    token = excluded.token,
    name = excluded.name,
    group_name = excluded.group_name,
    details_json = excluded.details_json,
    commands_json = excluded.commands_json,
    updated_at = excluded.updated_at
`, uuidValue, input.Token, input.Name, input.Group, string(detailsJSON), string(commandsJSON), createdAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, fmt.Errorf("upsert agent: %w", err)
	}

	return s.GetAgentByUUID(uuidValue)
}

// GetAgentByUUID returns a stored agent by UUID.
func (s *Store) GetAgentByUUID(uuid string) (*AgentRecord, error) {
	row := s.db.QueryRow(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at
FROM agents
WHERE uuid = ?
`, strings.TrimSpace(uuid))

	return scanAgent(row)
}

// GetAgentByName returns a stored agent by name.
func (s *Store) GetAgentByName(name string) (*AgentRecord, error) {
	row := s.db.QueryRow(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at
FROM agents
WHERE name = ?
`, strings.TrimSpace(name))

	return scanAgent(row)
}

// ListAgents returns all stored agents sorted by group and name.
func (s *Store) ListAgents() ([]AgentRecord, error) {
	rows, err := s.db.Query(`
SELECT uuid, token, name, group_name, details_json, commands_json, created_at, updated_at
FROM agents
ORDER BY group_name ASC, name ASC
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
	result, err := s.db.Exec(`DELETE FROM agents WHERE uuid = ?`, strings.TrimSpace(uuid))
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
			Description:  cmd.Description,
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

	if err := scanner.Scan(&record.UUID, &record.Token, &record.Name, &record.Group, &detailsJSON, &commandsJSON, &createdAtRaw, &updatedAtRaw); err != nil {
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
