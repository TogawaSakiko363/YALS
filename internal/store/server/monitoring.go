package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// AgentMetrics is the latest system-metrics snapshot for one agent.
type AgentMetrics struct {
	AgentUUID    string    `json:"agent_uuid"`
	UpdatedAt    time.Time `json:"updated_at"`
	CPUPercent   float64   `json:"cpu_percent"`
	MemUsed      uint64    `json:"mem_used"`
	MemTotal     uint64    `json:"mem_total"`
	DiskUsed     uint64    `json:"disk_used"`
	DiskTotal    uint64    `json:"disk_total"`
	NetUpRate    float64   `json:"net_up_rate"`
	NetDownRate  float64   `json:"net_down_rate"`
	NetUpTotal   uint64    `json:"net_up_total"`
	NetDownTotal uint64    `json:"net_down_total"`
	UptimeSec    uint64    `json:"uptime_sec"`
}

// UpsertAgentMetrics stores the latest metrics snapshot for an agent.
func (s *Store) UpsertAgentMetrics(m AgentMetrics) error {
	_, err := s.dbW.Exec(`
INSERT INTO agent_metrics (agent_uuid, updated_at, cpu_percent, mem_used, mem_total, disk_used, disk_total, net_up_rate, net_down_rate, net_up_total, net_down_total, uptime_sec)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_uuid) DO UPDATE SET
    updated_at = excluded.updated_at,
    cpu_percent = excluded.cpu_percent,
    mem_used = excluded.mem_used,
    mem_total = excluded.mem_total,
    disk_used = excluded.disk_used,
    disk_total = excluded.disk_total,
    net_up_rate = excluded.net_up_rate,
    net_down_rate = excluded.net_down_rate,
    net_up_total = excluded.net_up_total,
    net_down_total = excluded.net_down_total,
    uptime_sec = excluded.uptime_sec
`, m.AgentUUID, time.Now().UTC().Format(time.RFC3339Nano), m.CPUPercent, m.MemUsed, m.MemTotal, m.DiskUsed, m.DiskTotal, m.NetUpRate, m.NetDownRate, m.NetUpTotal, m.NetDownTotal, m.UptimeSec)
	if err != nil {
		return fmt.Errorf("upsert agent metrics: %w", err)
	}
	return nil
}

// ListAgentMetrics returns the latest metrics snapshot per agent, keyed by UUID.
func (s *Store) ListAgentMetrics() (map[string]AgentMetrics, error) {
	rows, err := s.dbR.Query(`
SELECT agent_uuid, updated_at, cpu_percent, mem_used, mem_total, disk_used, disk_total, net_up_rate, net_down_rate, net_up_total, net_down_total, uptime_sec
FROM agent_metrics
`)
	if err != nil {
		return nil, fmt.Errorf("list agent metrics: %w", err)
	}
	defer rows.Close()

	result := make(map[string]AgentMetrics)
	for rows.Next() {
		var m AgentMetrics
		var updatedAt string
		if err := rows.Scan(&m.AgentUUID, &updatedAt, &m.CPUPercent, &m.MemUsed, &m.MemTotal, &m.DiskUsed, &m.DiskTotal, &m.NetUpRate, &m.NetDownRate, &m.NetUpTotal, &m.NetDownTotal, &m.UptimeSec); err != nil {
			return nil, err
		}
		if parsed, perr := time.Parse(time.RFC3339Nano, updatedAt); perr == nil {
			m.UpdatedAt = parsed
		}
		result[m.AgentUUID] = m
	}
	return result, rows.Err()
}

// DeleteAgentMetrics removes a deleted agent's metrics snapshot.
func (s *Store) DeleteAgentMetrics(uuid string) error {
	_, err := s.dbW.Exec(`DELETE FROM agent_metrics WHERE agent_uuid = ?`, strings.TrimSpace(uuid))
	return err
}

// ProbeResultRow is one (agent, target) probe-cycle result.
type ProbeResultRow struct {
	AgentUUID  string
	AgentName  string
	TargetName string
	TS         int64
	LatencyMs  float64
	Sent       int
	Recv       int
}

// InsertProbeResults appends a batch of probe results in one transaction.
func (s *Store) InsertProbeResults(rows []ProbeResultRow) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.dbW.Begin()
	if err != nil {
		return fmt.Errorf("begin probe insert: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO probe_results (agent_uuid, agent_name, target_name, ts, latency_ms, sent, recv) VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare probe insert: %w", err)
	}
	defer stmt.Close()
	for _, r := range rows {
		if _, err := stmt.Exec(r.AgentUUID, r.AgentName, r.TargetName, r.TS, r.LatencyMs, r.Sent, r.Recv); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert probe result: %w", err)
		}
	}
	return tx.Commit()
}

// PruneProbeResults deletes probe results older than the given unix timestamp.
func (s *Store) PruneProbeResults(beforeTS int64) error {
	_, err := s.dbW.Exec(`DELETE FROM probe_results WHERE ts < ?`, beforeTS)
	if err != nil {
		return fmt.Errorf("prune probe results: %w", err)
	}
	return nil
}

// PurgeProbeTargets deletes probe results for any target_name not in keepNames.
// Called after targets.yaml changes so renamed/removed targets drop their data.
func (s *Store) PurgeProbeTargets(keepNames map[string]bool) error {
	if len(keepNames) == 0 {
		_, err := s.dbW.Exec(`DELETE FROM probe_results`)
		return err
	}
	placeholders := make([]string, 0, len(keepNames))
	args := make([]any, 0, len(keepNames))
	for name := range keepNames {
		placeholders = append(placeholders, "?")
		args = append(args, name)
	}
	query := fmt.Sprintf(`DELETE FROM probe_results WHERE target_name NOT IN (%s)`, strings.Join(placeholders, ","))
	if _, err := s.dbW.Exec(query, args...); err != nil {
		return fmt.Errorf("purge probe targets: %w", err)
	}
	return nil
}

// ProbeAggregate is one target's aggregated stats over a window, for one agent.
type ProbeAggregate struct {
	TargetName string  `json:"target_name"`
	LatestMs   float64 `json:"latest_ms"`
	LatestRecv int     `json:"latest_recv"`
	AvgMs      float64 `json:"avg_ms"`
	WorstMs    float64 `json:"worst_ms"`
	JitterMs   float64 `json:"jitter_ms"` // population stddev of latency over the window
	Sent       int     `json:"sent"`
	Recv       int     `json:"recv"`
}

// QueryProbeAggregates returns, for one agent and a time window (ts >= sinceTS),
// each target's latest latency, average and worst (max) latency over received
// cycles, and loss inputs (sent/recv totals). Uses window functions to do it in a
// single pass.
func (s *Store) QueryProbeAggregates(agentName string, sinceTS int64) ([]ProbeAggregate, error) {
	rows, err := s.dbR.Query(`
SELECT target_name, latest_ms, latest_recv, avg_ms, worst_ms, avg_sq, total_sent, total_recv FROM (
    SELECT
        target_name,
        FIRST_VALUE(latency_ms) OVER w AS latest_ms,
        FIRST_VALUE(recv) OVER w AS latest_recv,
        AVG(CASE WHEN recv > 0 THEN latency_ms END) OVER (PARTITION BY target_name) AS avg_ms,
        MAX(CASE WHEN recv > 0 THEN latency_ms END) OVER (PARTITION BY target_name) AS worst_ms,
        AVG(CASE WHEN recv > 0 THEN latency_ms * latency_ms END) OVER (PARTITION BY target_name) AS avg_sq,
        SUM(sent) OVER (PARTITION BY target_name) AS total_sent,
        SUM(recv) OVER (PARTITION BY target_name) AS total_recv,
        ROW_NUMBER() OVER w AS rn
    FROM probe_results
    WHERE agent_name = ? AND ts >= ?
    WINDOW w AS (PARTITION BY target_name ORDER BY ts DESC)
)
WHERE rn = 1
ORDER BY target_name
`, agentName, sinceTS)
	if err != nil {
		return nil, fmt.Errorf("query probe aggregates: %w", err)
	}
	defer rows.Close()

	var result []ProbeAggregate
	for rows.Next() {
		var a ProbeAggregate
		var avg, worst, avgSq sql.NullFloat64
		if err := rows.Scan(&a.TargetName, &a.LatestMs, &a.LatestRecv, &avg, &worst, &avgSq, &a.Sent, &a.Recv); err != nil {
			return nil, err
		}
		if avg.Valid {
			a.AvgMs = avg.Float64
		}
		if worst.Valid {
			a.WorstMs = worst.Float64
		}
		// Jitter = population stddev = sqrt(E[x^2] - E[x]^2) over received cycles.
		if avg.Valid && avgSq.Valid {
			if variance := avgSq.Float64 - avg.Float64*avg.Float64; variance > 0 {
				a.JitterMs = math.Sqrt(variance)
			}
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

// ProbeSeriesPoint is one probe cycle's latency for the row-expansion chart.
// Recv == 0 marks a fully-lost cycle (no latency), so the UI can break the line.
type ProbeSeriesPoint struct {
	TS        int64   `json:"ts"`
	LatencyMs float64 `json:"latency_ms"`
	Recv      int     `json:"recv"`
}

// QueryProbeSeries returns one target's per-cycle latency over a time window for
// one agent, oldest first, for the latency chart.
func (s *Store) QueryProbeSeries(agentName, targetName string, sinceTS int64) ([]ProbeSeriesPoint, error) {
	rows, err := s.dbR.Query(`
SELECT ts, latency_ms, recv FROM probe_results
WHERE agent_name = ? AND target_name = ? AND ts >= ?
ORDER BY ts ASC
`, agentName, targetName, sinceTS)
	if err != nil {
		return nil, fmt.Errorf("query probe series: %w", err)
	}
	defer rows.Close()

	var result []ProbeSeriesPoint
	for rows.Next() {
		var p ProbeSeriesPoint
		if err := rows.Scan(&p.TS, &p.LatencyMs, &p.Recv); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}

// ProbeSettings holds hot-reloadable probe configuration (the interval).
type ProbeSettings struct {
	IntervalSec int `json:"interval_sec"`
}

const probeSettingsKey = "probe_settings"

// GetProbeSettings returns the stored probe settings, or defaults on first use.
func (s *Store) GetProbeSettings() (ProbeSettings, error) {
	settings := ProbeSettings{IntervalSec: 60}
	row := s.dbR.QueryRow(`SELECT value_json FROM runtime_settings WHERE key = ?`, probeSettingsKey)
	var payload string
	if err := row.Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			return settings, nil
		}
		return settings, err
	}
	if err := json.Unmarshal([]byte(payload), &settings); err != nil {
		return settings, fmt.Errorf("unmarshal probe settings: %w", err)
	}
	if settings.IntervalSec <= 0 {
		settings.IntervalSec = 60
	}
	return settings, nil
}

// UpsertProbeSettings persists probe settings.
func (s *Store) UpsertProbeSettings(settings ProbeSettings) (ProbeSettings, error) {
	if settings.IntervalSec <= 0 {
		settings.IntervalSec = 60
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		return settings, fmt.Errorf("marshal probe settings: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.dbW.Exec(`
INSERT INTO runtime_settings (key, value_json, updated_at)
VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
`, probeSettingsKey, string(payload), now)
	if err != nil {
		return settings, fmt.Errorf("upsert probe settings: %w", err)
	}
	return settings, nil
}
