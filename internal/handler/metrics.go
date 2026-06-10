package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"YALS/internal/agent"
	"YALS/internal/config"
	prom "YALS/internal/plugin/server"
	"YALS/internal/utils"
)

// handleMetrics serves the Prometheus /metrics endpoint, aggregating the status
// of every agent the server knows about. It is gated by server config:
//   - disabled by default (returns 404 so the endpoint's existence isn't leaked);
//   - optionally protected by a bearer token (constant-time compared).
func (h *Handler) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cfg := config.GetConfig()
	if cfg == nil || !cfg.Server.MetricsEnabled {
		http.NotFound(w, r)
		return
	}

	if token := strings.TrimSpace(cfg.Server.MetricsToken); token != "" {
		if subtle.ConstantTimeCompare([]byte(bearerToken(r)), []byte(token)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	stats := h.agentManager.GetAgentStats()
	snapshot := prom.Snapshot{
		AppName: utils.GetAppName(),
		Version: utils.GetAppVersion(),
		Total:   intStat(stats, "total"),
		Online:  intStat(stats, "online"),
		Offline: intStat(stats, "offline"),
		Agents:  toAgentSnapshots(h.agentManager.GetAgentMetrics()),
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.setNoCacheHeaders(w)
	prom.WriteMetrics(w, snapshot)
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		return strings.TrimSpace(authHeader[7:])
	}
	return ""
}

// intStat safely reads an int value from the stats map.
func intStat(stats map[string]any, key string) int {
	if v, ok := stats[key].(int); ok {
		return v
	}
	return 0
}

// toAgentSnapshots maps the agent package's metric type onto the exporter's
// neutral snapshot type, keeping the exporter decoupled from the agent package.
func toAgentSnapshots(metrics []agent.AgentMetric) []prom.AgentSnapshot {
	snapshots := make([]prom.AgentSnapshot, len(metrics))
	for i, m := range metrics {
		snapshots[i] = prom.AgentSnapshot{
			UUID:            m.UUID,
			Name:            m.Name,
			Group:           m.Group,
			Location:        m.Location,
			Datacenter:      m.Datacenter,
			Online:          m.Online,
			FirstSeen:       m.FirstSeen,
			LastConnected:   m.LastConnected,
			CommandCount:    m.CommandCount,
			RunningCommands: m.RunningCommands,
		}
	}
	return snapshots
}
