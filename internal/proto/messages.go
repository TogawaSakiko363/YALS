package proto

import (
	"encoding/json"
)

// HandshakeRequest contains agent authentication and identity during connection.
type HandshakeRequest struct {
	UUID  string `json:"uuid"`
	Token string `json:"token"`
}

// Marshal implements custom marshaling for JSON codec.
func (m *HandshakeRequest) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal implements custom unmarshaling for JSON codec.
func (m *HandshakeRequest) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// HandshakeResponse acknowledges the handshake and delivers runtime config.
type HandshakeResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Config  []byte `json:"config,omitempty"`
}

// Marshal implements custom marshaling for JSON codec.
func (m *HandshakeResponse) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal implements custom unmarshaling for JSON codec.
func (m *HandshakeResponse) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}

// AgentDetails contains detailed information about the agent.
type AgentDetails struct {
	Location    string `json:"location"`
	Datacenter  string `json:"datacenter"`
	TestIP      string `json:"test_ip"`
	Description string `json:"description"`
}

// CommandInfo describes an available command.
type CommandInfo struct {
	Name         string `json:"name"`
	Template     string `json:"template,omitempty"`
	UsePlugin    string `json:"use_plugin,omitempty"`
	IgnoreTarget bool   `json:"ignore_target"`
	MaximumQueue int    `json:"maxmium_queue,omitempty"`
}

// CommandMessage is used for bidirectional streaming.
//
// Beyond command execution it also carries the monitoring messages, whose
// type-specific payload travels in Data:
//   - "metrics_report" (agent→server): Data is a SystemMetrics
//   - "probe_config"   (server→agent): Data is a ProbeConfig
//   - "probe_report"   (agent→server): Data is a ProbeBatch
type CommandMessage struct {
	Type        string          `json:"type"`
	CommandName string          `json:"command_name,omitempty"`
	Target      string          `json:"target,omitempty"`
	CommandID   string          `json:"command_id,omitempty"`
	IPVersion   string          `json:"ip_version,omitempty"`
	Output      string          `json:"output,omitempty"`
	Error       string          `json:"error,omitempty"`
	IsComplete  bool            `json:"is_complete,omitempty"`
	IsError     bool            `json:"is_error,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// SystemMetrics is one snapshot of an agent host's resource usage. Bandwidth
// fields are bytes/sec; total fields are cumulative bytes since the agent started.
type SystemMetrics struct {
	CPUPercent   float64 `json:"cpu_percent"`
	MemUsed      uint64  `json:"mem_used"`
	MemTotal     uint64  `json:"mem_total"`
	DiskUsed     uint64  `json:"disk_used"`
	DiskTotal    uint64  `json:"disk_total"`
	NetUpRate    float64 `json:"net_up_rate"`
	NetDownRate  float64 `json:"net_down_rate"`
	NetUpTotal   uint64  `json:"net_up_total"`
	NetDownTotal uint64  `json:"net_down_total"`
	UptimeSec    uint64  `json:"uptime_sec"`
}

// ProbeTargetSpec is one latency-probe target pushed to an agent.
type ProbeTargetSpec struct {
	IP       string `json:"ip"`
	Name     string `json:"name"`
	Location string `json:"location,omitempty"`
	ISP      string `json:"isp,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// ProbeConfig is the full latency-probe configuration pushed to an agent. An
// empty Targets slice tells the agent to stop probing.
type ProbeConfig struct {
	IntervalSec int               `json:"interval_sec"`
	Targets     []ProbeTargetSpec `json:"targets"`
}

// ProbeResult is one target's result for a single probe cycle.
type ProbeResult struct {
	Name      string  `json:"name"`
	LatencyMs float64 `json:"latency_ms"` // average RTT of the cycle; 0 when Recv == 0
	Sent      int     `json:"sent"`
	Recv      int     `json:"recv"`
}

// ProbeBatch is one probe cycle's results reported by an agent.
type ProbeBatch struct {
	TS      int64         `json:"ts"` // unix seconds
	Results []ProbeResult `json:"results"`
}

// Marshal implements custom marshaling for JSON codec.
func (m *CommandMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal implements custom unmarshaling for JSON codec.
func (m *CommandMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}
