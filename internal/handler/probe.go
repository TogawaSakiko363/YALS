package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"YALS/internal/logger"
	"YALS/internal/probe"
	"YALS/internal/proto"
	serverstore "YALS/internal/store/server"
)

const (
	probeResultRetention = 24 * time.Hour
	targetsPollInterval  = 10 * time.Second
	probePruneInterval   = 10 * time.Minute
)

// InitProbing loads targets.yaml + probe settings, wires agent report sinks to
// the store, and starts the hot-reload poller and the retention pruner.
func (h *Handler) InitProbing(targetsPath string) {
	h.probePath = targetsPath

	settings, err := h.store.GetProbeSettings()
	if err != nil {
		logger.Warnf("Failed to load probe settings: %v", err)
	}
	h.probeMu.Lock()
	h.probeInterval = settings.IntervalSec
	h.probeMu.Unlock()

	h.agentManager.SetReportHandlers(h.storeMetricsReport, h.storeProbeReport)

	h.reloadTargets(true)

	go h.watchTargetsFile()
	go h.runProbePruner()
}

func (h *Handler) storeMetricsReport(uuid string, m proto.SystemMetrics) {
	if err := h.store.UpsertAgentMetrics(serverstore.AgentMetrics{
		AgentUUID:    uuid,
		CPUPercent:   m.CPUPercent,
		MemUsed:      m.MemUsed,
		MemTotal:     m.MemTotal,
		DiskUsed:     m.DiskUsed,
		DiskTotal:    m.DiskTotal,
		NetUpRate:    m.NetUpRate,
		NetDownRate:  m.NetDownRate,
		NetUpTotal:   m.NetUpTotal,
		NetDownTotal: m.NetDownTotal,
		UptimeSec:    m.UptimeSec,
	}); err != nil {
		logger.Debugf("store metrics report: %v", err)
	}
}

func (h *Handler) storeProbeReport(uuid string, batch proto.ProbeBatch) {
	name := h.agentManager.NameByUUID(uuid)
	rows := make([]serverstore.ProbeResultRow, 0, len(batch.Results))
	for _, r := range batch.Results {
		rows = append(rows, serverstore.ProbeResultRow{
			AgentUUID:  uuid,
			AgentName:  name,
			TargetName: r.Name,
			TS:         batch.TS,
			LatencyMs:  r.LatencyMs,
			Sent:       r.Sent,
			Recv:       r.Recv,
		})
	}
	if err := h.store.InsertProbeResults(rows); err != nil {
		logger.Warnf("Failed to store probe results: %v", err)
	}
}

// reloadTargets (re)loads targets.yaml, purges orphaned probe data (renamed or
// removed targets) and pushes the new config to all online agents.
func (h *Handler) reloadTargets(initial bool) {
	targets, err := probe.EnsureFile(h.probePath)
	if err != nil {
		logger.Warnf("Failed to load %s: %v", h.probePath, err)
		return
	}

	h.probeMu.Lock()
	h.probeTargets = targets
	if info, statErr := os.Stat(h.probePath); statErr == nil {
		h.probeModTime = info.ModTime()
	}
	h.probeMu.Unlock()

	if err := h.store.PurgeProbeTargets(probe.Names(targets)); err != nil {
		logger.Warnf("Failed to purge stale probe data: %v", err)
	}
	if !initial {
		logger.Infof("Reloaded %d probe targets from %s", len(targets), h.probePath)
	}
	h.dispatchProbeConfig()
}

// watchTargetsFile polls the targets file mtime and reloads on external edits.
func (h *Handler) watchTargetsFile() {
	ticker := time.NewTicker(targetsPollInterval)
	defer ticker.Stop()
	for range ticker.C {
		info, err := os.Stat(h.probePath)
		if err != nil {
			continue
		}
		h.probeMu.RLock()
		last := h.probeModTime
		h.probeMu.RUnlock()
		if info.ModTime().After(last) {
			h.reloadTargets(false)
		}
	}
}

func (h *Handler) runProbePruner() {
	ticker := time.NewTicker(probePruneInterval)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-probeResultRetention).Unix()
		if err := h.store.PruneProbeResults(cutoff); err != nil {
			logger.Warnf("Failed to prune probe results: %v", err)
		}
	}
}

func (h *Handler) currentProbeConfig() proto.ProbeConfig {
	h.probeMu.RLock()
	defer h.probeMu.RUnlock()
	interval := h.probeInterval
	if interval <= 0 {
		interval = 60
	}
	specs := make([]proto.ProbeTargetSpec, 0, len(h.probeTargets))
	for _, t := range h.probeTargets {
		specs = append(specs, proto.ProbeTargetSpec{IP: t.IP, Name: t.Name, Location: t.Location, ISP: t.ISP, Protocol: t.Protocol})
	}
	return proto.ProbeConfig{IntervalSec: interval, Targets: specs}
}

func (h *Handler) probeConfigMessage() (*proto.CommandMessage, error) {
	data, err := json.Marshal(h.currentProbeConfig())
	if err != nil {
		return nil, err
	}
	return &proto.CommandMessage{Type: "probe_config", Data: data}, nil
}

func (h *Handler) dispatchProbeConfig() {
	msg, err := h.probeConfigMessage()
	if err != nil {
		return
	}
	for _, uuid := range h.agentManager.OnlineAgentUUIDs() {
		_ = h.agentManager.SendToAgent(uuid, msg)
	}
}

// pushProbeConfigToAgent pushes the current config to one agent (on connect).
func (h *Handler) pushProbeConfigToAgent(uuid string) {
	if msg, err := h.probeConfigMessage(); err == nil {
		_ = h.agentManager.SendToAgent(uuid, msg)
	}
}

// ---- HTTP endpoints ----

type statusItem struct {
	UUID    string                    `json:"uuid"`
	Name    string                    `json:"name"`
	Group   string                    `json:"group"`
	Online  bool                      `json:"online"`
	Metrics *serverstore.AgentMetrics `json:"metrics,omitempty"`
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.validateSessionID(r.URL.Query().Get("session_id")) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	metricsByUUID, err := h.store.ListAgentMetrics()
	if err != nil {
		logger.Errorf("Failed to list agent metrics: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	items := make([]statusItem, 0)
	for _, a := range h.agentManager.GetAgents() {
		uuid, _ := a["uuid"].(string)
		name, _ := a["name"].(string)
		statusVal, _ := a["status"].(int)
		group := ""
		if details, ok := a["details"].(map[string]any); ok {
			group, _ = details["group"].(string)
		}
		item := statusItem{UUID: uuid, Name: name, Group: group, Online: statusVal == 1}
		if m, ok := metricsByUUID[uuid]; ok {
			snapshot := m
			item.Metrics = &snapshot
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(items)
}

type probeRow struct {
	Name      string  `json:"name"`
	Location  string  `json:"location"`
	ISP       string  `json:"isp"`
	Protocol  string  `json:"protocol"`
	HasData   bool    `json:"has_data"`
	LatestMs  float64 `json:"latest_ms"`
	HasLatest bool    `json:"has_latest"`
	AvgMs     float64 `json:"avg_ms"`
	HasAvg    bool    `json:"has_avg"`
	LossPct   float64 `json:"loss_pct"`
}

func windowSeconds(window string) int64 {
	switch window {
	case "6h":
		return 6 * 3600
	case "12h":
		return 12 * 3600
	case "24h":
		return 24 * 3600
	default:
		return 3600 // 1h
	}
}

func (h *Handler) handleProbes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.validateSessionID(r.URL.Query().Get("session_id")) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	agentName := strings.TrimSpace(r.URL.Query().Get("agent"))
	if agentName == "" {
		agentName = h.firstAgentName()
	}
	sinceTS := time.Now().Add(-time.Duration(windowSeconds(r.URL.Query().Get("window"))) * time.Second).Unix()

	aggByName := map[string]serverstore.ProbeAggregate{}
	if agentName != "" {
		aggs, err := h.store.QueryProbeAggregates(agentName, sinceTS)
		if err != nil {
			logger.Errorf("Failed to query probe aggregates: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		for _, a := range aggs {
			aggByName[a.TargetName] = a
		}
	}

	h.probeMu.RLock()
	targets := append([]probe.Target(nil), h.probeTargets...)
	h.probeMu.RUnlock()

	rows := make([]probeRow, 0, len(targets))
	for _, t := range targets {
		row := probeRow{Name: t.Name, Location: t.Location, ISP: t.ISP, Protocol: t.Protocol}
		if agg, ok := aggByName[t.Name]; ok && agg.Sent > 0 {
			row.HasData = true
			row.HasLatest = agg.LatestRecv > 0
			row.LatestMs = agg.LatestMs
			row.HasAvg = agg.Recv > 0
			row.AvgMs = agg.AvgMs
			row.LossPct = float64(agg.Sent-agg.Recv) / float64(agg.Sent) * 100
		}
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(map[string]any{"agent": agentName, "rows": rows})
}

func (h *Handler) handleProbesMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.validateSessionID(r.URL.Query().Get("session_id")) {
		http.Error(w, "Invalid or missing session_id", http.StatusUnauthorized)
		return
	}

	names := h.agentNames()
	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(map[string]any{"agents": names})
}

func (h *Handler) agentNames() []string {
	agents := h.agentManager.GetAgents()
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		if name, ok := a["name"].(string); ok && name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func (h *Handler) firstAgentName() string {
	names := h.agentNames()
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// ---- Control: probe targets + interval ----

type probeConfigPayload struct {
	IntervalSec int            `json:"interval_sec"`
	Targets     []probe.Target `json:"targets"`
}

func (h *Handler) handleControlTargets(w http.ResponseWriter, r *http.Request) {
	if !h.requireControlAuth(w, r) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.probeMu.RLock()
		payload := probeConfigPayload{IntervalSec: h.probeInterval, Targets: append([]probe.Target(nil), h.probeTargets...)}
		h.probeMu.RUnlock()
		if payload.IntervalSec <= 0 {
			payload.IntervalSec = 60
		}
		w.Header().Set("Content-Type", "application/json")
		h.setNoCacheHeaders(w)
		_ = json.NewEncoder(w).Encode(payload)

	case http.MethodPut:
		var payload probeConfigPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}
		if err := validateProbeTargets(payload.Targets); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if payload.IntervalSec <= 0 {
			payload.IntervalSec = 60
		}

		if err := probe.Save(h.probePath, payload.Targets); err != nil {
			http.Error(w, "Failed to write targets file", http.StatusInternalServerError)
			return
		}
		if _, err := h.store.UpsertProbeSettings(serverstore.ProbeSettings{IntervalSec: payload.IntervalSec}); err != nil {
			http.Error(w, "Failed to save probe settings", http.StatusInternalServerError)
			return
		}
		h.probeMu.Lock()
		h.probeInterval = payload.IntervalSec
		h.probeMu.Unlock()
		h.reloadTargets(false)

		w.Header().Set("Content-Type", "application/json")
		h.setNoCacheHeaders(w)
		_ = json.NewEncoder(w).Encode(payload)

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func validateProbeTargets(targets []probe.Target) error {
	seen := make(map[string]bool, len(targets))
	for i, t := range targets {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			return fmt.Errorf("target #%d: name is required", i+1)
		}
		if seen[name] {
			return fmt.Errorf("duplicate target name: %q", name)
		}
		seen[name] = true
		if strings.TrimSpace(t.IP) == "" {
			return fmt.Errorf("target %q: IP is required", name)
		}
	}
	return nil
}
