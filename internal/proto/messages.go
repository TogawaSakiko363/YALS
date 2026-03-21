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
type CommandMessage struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name,omitempty"`
	Target      string `json:"target,omitempty"`
	CommandID   string `json:"command_id,omitempty"`
	IPVersion   string `json:"ip_version,omitempty"`
	Output      string `json:"output,omitempty"`
	Error       string `json:"error,omitempty"`
	IsComplete  bool   `json:"is_complete,omitempty"`
	IsError     bool   `json:"is_error,omitempty"`
}

// Marshal implements custom marshaling for JSON codec.
func (m *CommandMessage) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Unmarshal implements custom unmarshaling for JSON codec.
func (m *CommandMessage) Unmarshal(data []byte) error {
	return json.Unmarshal(data, m)
}
