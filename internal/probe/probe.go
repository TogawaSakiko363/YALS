// Package probe defines the latency-probe target schema (targets.yaml), its
// loading/saving and a built-in default. targets.yaml is a top-level YAML list of
// entries, each with one or more IPs and a labels block:
//
//   - targets:
//   - 1.1.1.1
//     labels:
//     name: Cloudflare
//     location: Global
//     isp: ANYCAST
//     protocol: ICMP
//
// labels.name is the unique tracking key used to correlate stored probe results;
// renaming or removing a target makes its old data orphan (the server purges it).
package probe

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Target is one resolved latency-probe target. Name is the unique key.
type Target struct {
	IP       string `json:"ip"`
	Name     string `json:"name"`
	Location string `json:"location"`
	ISP      string `json:"isp"`
	Protocol string `json:"protocol"`
}

type entryLabels struct {
	Name     string `yaml:"name"`
	Location string `yaml:"location,omitempty"`
	ISP      string `yaml:"isp,omitempty"`
	Protocol string `yaml:"protocol,omitempty"`
}

type fileEntry struct {
	Targets []string    `yaml:"targets"`
	Labels  entryLabels `yaml:"labels"`
}

// Load parses targets.yaml into resolved targets. Each entry contributes one
// target (its first IP) keyed by labels.name. Entries with an empty name or IP
// are skipped; on a duplicate name the first wins.
func Load(path string) ([]Target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var entries []fileEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	targets := make([]Target, 0, len(entries))
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := strings.TrimSpace(e.Labels.Name)
		if name == "" || len(e.Targets) == 0 || seen[name] {
			continue
		}
		ip := strings.TrimSpace(e.Targets[0])
		if ip == "" {
			continue
		}
		seen[name] = true

		protocol := strings.TrimSpace(e.Labels.Protocol)
		if protocol == "" {
			protocol = "ICMP"
		}
		targets = append(targets, Target{
			IP:       ip,
			Name:     name,
			Location: strings.TrimSpace(e.Labels.Location),
			ISP:      strings.TrimSpace(e.Labels.ISP),
			Protocol: protocol,
		})
	}
	return targets, nil
}

// Save writes targets back to path in the canonical top-level-list format.
func Save(path string, targets []Target) error {
	entries := make([]fileEntry, 0, len(targets))
	for _, t := range targets {
		entries = append(entries, fileEntry{
			Targets: []string{strings.TrimSpace(t.IP)},
			Labels: entryLabels{
				Name:     strings.TrimSpace(t.Name),
				Location: strings.TrimSpace(t.Location),
				ISP:      strings.TrimSpace(t.ISP),
				Protocol: strings.TrimSpace(t.Protocol),
			},
		})
	}

	out, err := yaml.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal targets: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Default returns the minimal target set written when targets.yaml is absent.
func Default() []Target {
	return []Target{
		{IP: "1.1.1.1", Name: "Cloudflare", Location: "Global", ISP: "ANYCAST", Protocol: "ICMP"},
		{IP: "8.8.8.8", Name: "Google", Location: "Global", ISP: "ANYCAST", Protocol: "ICMP"},
	}
}

// EnsureFile writes a default targets.yaml when path does not exist, then loads it.
func EnsureFile(path string) ([]Target, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := Save(path, Default()); err != nil {
			return nil, err
		}
	}
	return Load(path)
}

// Names returns the set of target names (the tracking keys).
func Names(targets []Target) map[string]bool {
	names := make(map[string]bool, len(targets))
	for _, t := range targets {
		names[t.Name] = true
	}
	return names
}
