package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
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

// reportQueueSize bounds the in-flight agent reports awaiting persistence. At
// hundreds of agents this is far above the steady-state backlog; if it ever
// fills (a sustained DB stall) reports are dropped rather than growing memory.
const reportQueueSize = 4096

// reportJob is one queued agent report. Exactly one of metrics/probe is set.
type reportJob struct {
	uuid    string
	metrics *proto.SystemMetrics
	probe   *proto.ProbeBatch
}

// InitProbing loads targets.yaml + probe settings, wires agent report sinks to
// the store, starts the async report writer, and starts the hot-reload poller
// and the retention pruner.
func (h *Handler) InitProbing(targetsPath string) {
	h.probePath = targetsPath

	settings, err := h.store.GetProbeSettings()
	if err != nil {
		logger.Warnf("Failed to load probe settings: %v", err)
	}
	h.probeMu.Lock()
	h.probeInterval = settings.IntervalSec
	h.probeMu.Unlock()

	h.reportQueue = make(chan reportJob, reportQueueSize)
	go h.runReportWriter()

	h.agentManager.SetReportHandlers(h.storeMetricsReport, h.storeProbeReport)

	h.reloadTargets(true)

	go h.watchTargetsFile()
	go h.runProbePruner()
}

// runReportWriter is the single goroutine that persists queued agent reports, so
// the per-agent gRPC receive loops never block on the database.
func (h *Handler) runReportWriter() {
	for job := range h.reportQueue {
		switch {
		case job.metrics != nil:
			h.writeMetricsReport(job.uuid, *job.metrics)
		case job.probe != nil:
			h.writeProbeReport(job.uuid, *job.probe)
		}
	}
}

// storeMetricsReport / storeProbeReport are the report-handler callbacks invoked
// from each agent's receive loop. They only enqueue, so a DB stall cannot stall
// ingestion. enqueue drops (and counts) when the queue is full.
func (h *Handler) storeMetricsReport(uuid string, m proto.SystemMetrics) {
	mc := m
	h.enqueueReport(reportJob{uuid: uuid, metrics: &mc})
}

func (h *Handler) storeProbeReport(uuid string, batch proto.ProbeBatch) {
	bc := batch
	h.enqueueReport(reportJob{uuid: uuid, probe: &bc})
}

func (h *Handler) enqueueReport(job reportJob) {
	select {
	case h.reportQueue <- job:
	default:
		if d := atomic.AddUint64(&h.reportsDropped, 1); d%1000 == 1 {
			logger.Warnf("Report queue full; dropped %d agent reports so far (DB cannot keep up)", d)
		}
	}
}

func (h *Handler) writeMetricsReport(uuid string, m proto.SystemMetrics) {
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

func (h *Handler) writeProbeReport(uuid string, batch proto.ProbeBatch) {
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
		specs = append(specs, proto.ProbeTargetSpec{IP: t.IP, Name: t.Name, Location: t.Location, ISP: t.ISP, Protocol: t.Protocol, Port: t.Port})
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

	statuses := h.agentManager.GetAgentStatusList()

	// Order the cards by the operator-defined order (same as the control panel)
	// so the Status page is stable instead of following the agent map's random
	// iteration order. Unknown UUIDs sort last.
	if order, err := h.store.ListAgentOrder(); err == nil {
		idx := make(map[string]int, len(order))
		for i, uuid := range order {
			idx[uuid] = i
		}
		rank := func(uuid string) int {
			if i, ok := idx[uuid]; ok {
				return i
			}
			return len(order)
		}
		sort.SliceStable(statuses, func(i, j int) bool {
			return rank(statuses[i].UUID) < rank(statuses[j].UUID)
		})
	}

	items := make([]statusItem, 0, len(statuses))
	for _, a := range statuses {
		item := statusItem{UUID: a.UUID, Name: a.Name, Group: a.Group, Online: a.Online}
		if m, ok := metricsByUUID[a.UUID]; ok {
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
	Port      int     `json:"port"`
	HasData   bool    `json:"has_data"`
	LatestMs  float64 `json:"latest_ms"`
	HasLatest bool    `json:"has_latest"`
	AvgMs     float64 `json:"avg_ms"`
	HasAvg    bool    `json:"has_avg"`
	WorstMs   float64 `json:"worst_ms"`
	HasWorst  bool    `json:"has_worst"`
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
		row := probeRow{Name: t.Name, Location: t.Location, ISP: t.ISP, Protocol: t.Protocol, Port: t.Port}
		if agg, ok := aggByName[t.Name]; ok && agg.Sent > 0 {
			row.HasData = true
			row.HasLatest = agg.LatestRecv > 0
			row.LatestMs = agg.LatestMs
			row.HasAvg = agg.Recv > 0
			row.AvgMs = agg.AvgMs
			row.HasWorst = agg.Recv > 0
			row.WorstMs = agg.WorstMs
			row.LossPct = float64(agg.Sent-agg.Recv) / float64(agg.Sent) * 100
		}
		rows = append(rows, row)
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(map[string]any{"agent": agentName, "rows": rows})
}

// handleProbesSeries returns one target's per-cycle latency over a window, for
// the expandable per-row chart on the Probes page.
func (h *Handler) handleProbesSeries(w http.ResponseWriter, r *http.Request) {
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
	target := strings.TrimSpace(r.URL.Query().Get("target"))
	sinceTS := time.Now().Add(-time.Duration(windowSeconds(r.URL.Query().Get("window"))) * time.Second).Unix()

	points := []serverstore.ProbeSeriesPoint{}
	if agentName != "" && target != "" {
		got, err := h.store.QueryProbeSeries(agentName, target, sinceTS)
		if err != nil {
			logger.Errorf("Failed to query probe series: %v", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		points = got
	}

	w.Header().Set("Content-Type", "application/json")
	h.setNoCacheHeaders(w)
	_ = json.NewEncoder(w).Encode(map[string]any{"agent": agentName, "target": target, "points": points})
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
		if strings.EqualFold(strings.TrimSpace(t.Protocol), "TCP") && (t.Port < 1 || t.Port > 65535) {
			return fmt.Errorf("target %q: TCP requires a port between 1 and 65535", name)
		}
	}
	return nil
}
