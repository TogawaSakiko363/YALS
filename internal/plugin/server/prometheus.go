// Package server hosts server-side observability for YALS. The Prometheus
// exporter here is intentionally NOT a command-style plugin (the Plugin
// Execute/ExecuteStreaming interface is for agent-side command execution and
// does not fit a scrape endpoint). Instead it renders the Prometheus text
// exposition format from a neutral snapshot supplied by the HTTP layer, keeping
// this package decoupled from the agent package.
package server

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// AgentSnapshot is the per-agent view used to render metrics. It is a neutral
// type (no dependency on the agent package) so this exporter stays self-contained.
type AgentSnapshot struct {
	UUID            string
	Name            string
	Group           string
	Location        string
	Datacenter      string
	Online          bool
	FirstSeen       time.Time
	LastConnected   time.Time
	CommandCount    int
	RunningCommands int
}

// Snapshot is the complete set of data rendered on a single scrape.
type Snapshot struct {
	AppName string
	Version string
	Total   int
	Online  int
	Offline int
	Agents  []AgentSnapshot
}

// WriteMetrics renders the snapshot in the Prometheus text exposition format
// (version 0.0.4). Each metric family is emitted with its HELP/TYPE header
// followed by all of its samples, as required by the format.
func WriteMetrics(w io.Writer, s Snapshot) {
	// Stable ordering keeps scrape output deterministic and diff-friendly.
	agents := make([]AgentSnapshot, len(s.Agents))
	copy(agents, s.Agents)
	sort.Slice(agents, func(i, j int) bool {
		if agents[i].Name != agents[j].Name {
			return agents[i].Name < agents[j].Name
		}
		return agents[i].UUID < agents[j].UUID
	})

	// Build info.
	writeHeader(w, "yals_build_info", "Build information of the YALS server.", "gauge")
	fmt.Fprintf(w, "yals_build_info{app=\"%s\",version=\"%s\"} 1\n",
		escapeLabelValue(s.AppName), escapeLabelValue(s.Version))

	// Aggregate agent counts.
	writeHeader(w, "yals_agents_total", "Total number of registered agents.", "gauge")
	fmt.Fprintf(w, "yals_agents_total %d\n", s.Total)
	writeHeader(w, "yals_agents_online", "Number of agents currently connected.", "gauge")
	fmt.Fprintf(w, "yals_agents_online %d\n", s.Online)
	writeHeader(w, "yals_agents_offline", "Number of agents currently disconnected.", "gauge")
	fmt.Fprintf(w, "yals_agents_offline %d\n", s.Offline)

	// Per-agent up/down.
	writeHeader(w, "yals_agent_up", "Whether the agent is currently connected (1) or not (0).", "gauge")
	for _, a := range agents {
		fmt.Fprintf(w, "yals_agent_up{%s} %d\n", identityLabels(a, true), boolToInt(a.Online))
	}

	// Per-agent first-seen timestamp.
	writeHeader(w, "yals_agent_first_seen_timestamp_seconds", "Unix timestamp when the agent was first registered.", "gauge")
	for _, a := range agents {
		fmt.Fprintf(w, "yals_agent_first_seen_timestamp_seconds{%s} %d\n", identityLabels(a, false), unixOrZero(a.FirstSeen))
	}

	// Per-agent last-connected timestamp.
	writeHeader(w, "yals_agent_last_connected_timestamp_seconds", "Unix timestamp of the agent's most recent successful connection.", "gauge")
	for _, a := range agents {
		fmt.Fprintf(w, "yals_agent_last_connected_timestamp_seconds{%s} %d\n", identityLabels(a, false), unixOrZero(a.LastConnected))
	}

	// Per-agent available command count.
	writeHeader(w, "yals_agent_commands", "Number of commands available on the agent.", "gauge")
	for _, a := range agents {
		fmt.Fprintf(w, "yals_agent_commands{%s} %d\n", identityLabels(a, false), a.CommandCount)
	}

	// Per-agent running command count.
	writeHeader(w, "yals_agent_running_commands", "Number of commands currently running on the agent.", "gauge")
	for _, a := range agents {
		fmt.Fprintf(w, "yals_agent_running_commands{%s} %d\n", identityLabels(a, false), a.RunningCommands)
	}
}

// writeHeader emits the HELP and TYPE lines for a metric family.
func writeHeader(w io.Writer, name, help, metricType string) {
	fmt.Fprintf(w, "# HELP %s %s\n", name, escapeHelp(help))
	fmt.Fprintf(w, "# TYPE %s %s\n", name, metricType)
}

// identityLabels builds the label set identifying an agent. When withDetails is
// true the location/datacenter labels are included (used on yals_agent_up so the
// inventory dimensions are available without an extra info metric).
func identityLabels(a AgentSnapshot, withDetails bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "uuid=\"%s\",name=\"%s\",group=\"%s\"",
		escapeLabelValue(a.UUID), escapeLabelValue(a.Name), escapeLabelValue(a.Group))
	if withDetails {
		fmt.Fprintf(&b, ",location=\"%s\",datacenter=\"%s\"",
			escapeLabelValue(a.Location), escapeLabelValue(a.Datacenter))
	}
	return b.String()
}

// escapeLabelValue escapes a label value per the exposition format: backslash,
// double-quote and newline must be escaped.
func escapeLabelValue(v string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return replacer.Replace(v)
}

// escapeHelp escapes a HELP string: only backslash and newline are escaped.
func escapeHelp(v string) string {
	replacer := strings.NewReplacer(`\`, `\\`, "\n", `\n`)
	return replacer.Replace(v)
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
