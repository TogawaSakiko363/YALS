package agent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
	"YALS/internal/plugin"
	"YALS/internal/proto"
	"YALS/internal/validator"
)

// Status represents the connection status of an agent
type Status int

const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusConnected
)

// Agent represents a connected agent
type Agent struct {
	UUID              string
	Name              string
	Group             string
	Details           config.AgentDetails
	stream            proto.AgentService_StreamCommandsServer
	status            Status
	lastCheck         time.Time
	lastConnected     time.Time
	firstSeen         time.Time
	statusLock        sync.RWMutex
	availableCommands []config.CommandInfo
	commandsLock      sync.RWMutex
	runningCommands   map[string]int
	runningLock       sync.Mutex
	sendMu            sync.Mutex
}

// sendLocked serializes server→agent stream writes (command dispatch, reload,
// disconnect and probe-config push can be issued from different goroutines).
func (a *Agent) sendLocked(msg *proto.CommandMessage) error {
	a.sendMu.Lock()
	defer a.sendMu.Unlock()
	if a.stream == nil {
		return fmt.Errorf("agent stream unavailable")
	}
	return a.stream.Send(msg)
}

// AgentRegistration contains server-side metadata used when attaching a live stream.
type AgentRegistration struct {
	UUID     string
	Name     string
	Group    string
	Details  config.AgentDetails
	Commands []config.CommandInfo
}

// CommandOutput represents command output from an agent
type CommandOutput struct {
	Output     string
	IsError    bool
	IsComplete bool
}

// Manager manages multiple agents
type Manager struct {
	agents             map[string]*Agent
	agentsByUUID       map[string]*Agent
	agentsLock         sync.RWMutex
	outputHandlers     map[string]chan CommandOutput
	outputHandlersLock sync.RWMutex

	// Monitoring report sinks, wired by the HTTP handler to the store.
	metricsHandler func(uuid string, m proto.SystemMetrics)
	probeHandler   func(uuid string, batch proto.ProbeBatch)
}

// NewManager creates a new agent manager
func NewManager() *Manager {
	return &Manager{
		agents:         make(map[string]*Agent),
		agentsByUUID:   make(map[string]*Agent),
		outputHandlers: make(map[string]chan CommandOutput),
	}
}

// SetReportHandlers registers sinks for agent metrics and probe reports.
func (m *Manager) SetReportHandlers(metrics func(uuid string, m proto.SystemMetrics), probe func(uuid string, batch proto.ProbeBatch)) {
	m.metricsHandler = metrics
	m.probeHandler = probe
}

// HandleAgentConnection handles a new agent gRPC stream connection for uuid.
func (m *Manager) HandleAgentConnection(uuid string, stream proto.AgentService_StreamCommandsServer) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch msg.Type {
		case "command_output":
			m.handleCommandOutputProto(msg)
		case "metrics_report":
			if m.metricsHandler != nil && len(msg.Data) > 0 {
				var sm proto.SystemMetrics
				if err := json.Unmarshal(msg.Data, &sm); err == nil {
					m.metricsHandler(uuid, sm)
				}
			}
		case "probe_report":
			if m.probeHandler != nil && len(msg.Data) > 0 {
				var batch proto.ProbeBatch
				if err := json.Unmarshal(msg.Data, &batch); err == nil {
					m.probeHandler(uuid, batch)
				}
			}
		}
	}
}

// SendToAgent sends a message on a connected agent's stream.
func (m *Manager) SendToAgent(uuid string, msg *proto.CommandMessage) error {
	m.agentsLock.RLock()
	agent, exists := m.agentsByUUID[uuid]
	m.agentsLock.RUnlock()
	if !exists || agent == nil || agent.stream == nil {
		return fmt.Errorf("agent not connected: %s", uuid)
	}
	return agent.sendLocked(msg)
}

// OnlineAgentUUIDs returns the UUIDs of all currently connected agents.
func (m *Manager) OnlineAgentUUIDs() []string {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()
	uuids := make([]string, 0, len(m.agentsByUUID))
	for uuid, agent := range m.agentsByUUID {
		if agent.Status() == StatusConnected {
			uuids = append(uuids, uuid)
		}
	}
	return uuids
}

// NameByUUID returns an agent's display name for the given UUID.
func (m *Manager) NameByUUID(uuid string) string {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()
	if agent, ok := m.agentsByUUID[uuid]; ok {
		return agent.Name
	}
	return ""
}

// RegisterAgent registers or updates agent metadata from server persistence.
func (m *Manager) RegisterAgent(reg AgentRegistration, stream proto.AgentService_StreamCommandsServer) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agentsByUUID[reg.UUID]
	if exists {
		if agent.Name != reg.Name {
			delete(m.agents, agent.Name)
		}
		agent.UUID = reg.UUID
		agent.Name = reg.Name
		agent.Group = reg.Group
		agent.Details = reg.Details
		agent.stream = stream
		agent.lastCheck = time.Now()
		agent.availableCommands = cloneCommands(reg.Commands)
		if agent.runningCommands == nil {
			agent.runningCommands = make(map[string]int)
		}
		if stream != nil {
			agent.status = StatusConnected
			agent.lastConnected = time.Now()
		}
		m.agents[agent.Name] = agent
		return
	}

	now := time.Now()
	agent = &Agent{
		UUID:              reg.UUID,
		Name:              reg.Name,
		Group:             reg.Group,
		Details:           reg.Details,
		stream:            stream,
		status:            StatusDisconnected,
		lastCheck:         now,
		lastConnected:     now,
		firstSeen:         now,
		availableCommands: cloneCommands(reg.Commands),
		runningCommands:   make(map[string]int),
	}
	if stream != nil {
		agent.status = StatusConnected
	}

	m.agents[agent.Name] = agent
	m.agentsByUUID[agent.UUID] = agent
}

// ReloadAgent requests an online agent to reconnect and fetch fresh config.
func (m *Manager) ReloadAgent(uuid string) error {
	m.agentsLock.RLock()
	agent, exists := m.agentsByUUID[uuid]
	m.agentsLock.RUnlock()
	if !exists || agent == nil || agent.stream == nil {
		return nil
	}
	return agent.sendLocked(&proto.CommandMessage{Type: "reload_config"})
}

// DisconnectAgent forces an online agent to disconnect and removes it from memory.
func (m *Manager) DisconnectAgent(uuid string) error {
	m.agentsLock.Lock()
	agent, exists := m.agentsByUUID[uuid]
	if !exists {
		m.agentsLock.Unlock()
		return nil
	}

	name := agent.Name
	delete(m.agentsByUUID, uuid)
	delete(m.agents, name)
	m.agentsLock.Unlock()

	if agent.stream != nil {
		_ = agent.sendLocked(&proto.CommandMessage{Type: "disconnect"})
	}

	return nil
}

// RemoveAgent removes an agent from in-memory manager state.
func (m *Manager) RemoveAgent(uuid string) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agentsByUUID[uuid]
	if !exists {
		return
	}

	delete(m.agentsByUUID, uuid)
	delete(m.agents, agent.Name)
}

// RegisterAgentStream attaches an active stream for the specified UUID.
func (m *Manager) RegisterAgentStream(uuid string, stream proto.AgentService_StreamCommandsServer) (*Agent, error) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agentsByUUID[uuid]
	if !exists {
		return nil, fmt.Errorf("agent not registered: %s", uuid)
	}

	agent.stream = stream
	agent.statusLock.Lock()
	agent.status = StatusConnected
	agent.lastConnected = time.Now()
	agent.lastCheck = time.Now()
	agent.statusLock.Unlock()
	m.agents[agent.Name] = agent

	return agent, nil
}

// UnregisterAgentStream marks an agent as disconnected.
func (m *Manager) UnregisterAgentStream(uuid string) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agentsByUUID[uuid]
	if !exists {
		return
	}

	agent.statusLock.Lock()
	agent.status = StatusDisconnected
	agent.stream = nil
	agent.statusLock.Unlock()
}

// Status returns the current status of the agent
func (a *Agent) Status() Status {
	a.statusLock.RLock()
	defer a.statusLock.RUnlock()
	return a.status
}

func (m *Manager) reserveCommandSlot(agentName, commandName string, maximumQueue int, pluginName string) error {
	if maximumQueue <= 0 && pluginName != "" {
		if hasOverride, overrideQueue := plugin.GetPluginMaximumQueue(pluginName); hasOverride {
			maximumQueue = overrideQueue
		}
	}
	if maximumQueue <= 0 {
		return nil
	}

	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()
	if !exists || agent == nil {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	agent.runningLock.Lock()
	defer agent.runningLock.Unlock()
	if agent.runningCommands == nil {
		agent.runningCommands = make(map[string]int)
	}
	current := agent.runningCommands[commandName]
	if current >= maximumQueue {
		return fmt.Errorf("execution limit reached for command '%s' (%d/%d)", commandName, current, maximumQueue)
	}
	agent.runningCommands[commandName] = current + 1
	return nil
}

func (m *Manager) releaseCommandSlot(agentName, commandName string) {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()
	if !exists || agent == nil {
		return
	}

	agent.runningLock.Lock()
	defer agent.runningLock.Unlock()
	if agent.runningCommands == nil {
		return
	}
	current := agent.runningCommands[commandName]
	if current <= 1 {
		delete(agent.runningCommands, commandName)
		return
	}
	agent.runningCommands[commandName] = current - 1
}

// GetAgents returns a list of all agents with their status and details.
func (m *Manager) GetAgents() []map[string]any {
	names, agents := m.getSortedAgents()
	result := make([]map[string]any, len(names))
	for i := range names {
		result[i] = agents[i]
	}
	return result
}

// GetAgentGroups returns all agents organized by groups.
func (m *Manager) GetAgentGroups() []map[string]any {
	names, agents := m.getSortedAgents()
	groups := make(map[string][]map[string]any)
	for i := range names {
		agentInfo := agents[i]
		groupName, _ := agentInfo["details"].(map[string]any)["group"].(string)
		if groupName == "" {
			groupName = "Default"
		}
		groups[groupName] = append(groups[groupName], agentInfo)
	}

	groupNames := make([]string, 0, len(groups))
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	result := make([]map[string]any, 0, len(groupNames))
	for _, groupName := range groupNames {
		result = append(result, map[string]any{
			"name":   groupName,
			"agents": groups[groupName],
		})
	}
	return result
}

// GetAgentCommands returns the available commands for a specific agent.
func (m *Manager) GetAgentCommands(agentName string) []validator.CommandDetail {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()
	if !exists {
		return []validator.CommandDetail{}
	}

	agent.commandsLock.RLock()
	defer agent.commandsLock.RUnlock()

	commands := make([]validator.CommandDetail, len(agent.availableCommands))
	for i, cmd := range agent.availableCommands {
		commands[i] = validator.CommandDetail{
			Name:         cmd.Name,
			IgnoreTarget: cmd.IgnoreTarget,
		}
	}
	return commands
}

// GetAgentStats returns statistics about agents.
func (m *Manager) GetAgentStats() map[string]any {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	online := 0
	offline := 0
	for _, agent := range m.agents {
		if agent.Status() == StatusConnected {
			online++
		} else {
			offline++
		}
	}

	return map[string]any{
		"total":   len(m.agents),
		"online":  online,
		"offline": offline,
	}
}

// CleanupOfflineAgents removes agents that have been offline for more than the specified duration.
func (m *Manager) CleanupOfflineAgents(maxOfflineDuration time.Duration) int {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	cleaned := 0
	now := time.Now()
	for name, agent := range m.agents {
		if agent.Status() == StatusDisconnected && now.Sub(agent.lastConnected) > maxOfflineDuration {
			delete(m.agentsByUUID, agent.UUID)
			delete(m.agents, name)
			cleaned++
		}
	}
	return cleaned
}

// GetCommandConfigInternal returns the command configuration for a specific agent and command.
func (m *Manager) GetCommandConfigInternal(agentName, commandName string) (config.CommandInfo, bool) {
	return m.getCommandConfig(agentName, commandName)
}

func (m *Manager) buildAgentInfo(name string, agent *Agent) map[string]any {
	frontendStatus := 0
	if agent.Status() == StatusConnected {
		frontendStatus = 1
	}

	agent.commandsLock.RLock()
	commands := make([]map[string]any, len(agent.availableCommands))
	for i, cmd := range agent.availableCommands {
		commands[i] = map[string]any{
			"name":          cmd.Name,
			"template":      cmd.Template,
			"use_plugin":    cmd.UsePlugin,
			"ignore_target": cmd.IgnoreTarget,
			"maxmium_queue": cmd.MaximumQueue,
		}
	}
	agent.commandsLock.RUnlock()

	return map[string]any{
		"uuid":     agent.UUID,
		"name":     name,
		"status":   frontendStatus,
		"commands": commands,
		"details": map[string]any{
			"location":    agent.Details.Location,
			"datacenter":  agent.Details.Datacenter,
			"test_ip":     agent.Details.TestIP,
			"description": agent.Details.Description,
			"group":       agent.Group,
		},
		"connection_info": map[string]any{
			"first_seen":       agent.firstSeen.Format("2006-01-02 15:04:05"),
			"last_connected":   agent.lastConnected.Format("2006-01-02 15:04:05"),
			"offline_duration": m.calculateOfflineDuration(agent),
		},
	}
}

func (m *Manager) calculateOfflineDuration(agent *Agent) string {
	if agent.Status() == StatusConnected {
		return ""
	}

	duration := time.Since(agent.lastConnected)
	switch {
	case duration < time.Minute:
		return "Just offline"
	case duration < time.Hour:
		return fmt.Sprintf("Offline %d minutes ago", int(duration.Minutes()))
	case duration < 24*time.Hour:
		return fmt.Sprintf("Offline %d hours ago", int(duration.Hours()))
	default:
		return fmt.Sprintf("Offline %d days ago", int(duration.Hours()/24))
	}
}

func (m *Manager) getSortedAgents() ([]string, []map[string]any) {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	names := make([]string, 0, len(m.agents))
	for name := range m.agents {
		names = append(names, name)
	}
	sort.Strings(names)

	agents := make([]map[string]any, 0, len(names))
	for _, name := range names {
		if agent, exists := m.agents[name]; exists {
			agents = append(agents, m.buildAgentInfo(name, agent))
		}
	}

	return names, agents
}

func (m *Manager) handleCommandOutputProto(msg *proto.CommandMessage) {
	commandID := msg.CommandID
	if commandID == "" {
		return
	}

	output := msg.Output
	errorMsg := msg.Error
	isComplete := msg.IsComplete
	isError := msg.IsError
	if errorMsg != "" {
		output = errorMsg
		isError = true
	}

	m.outputHandlersLock.RLock()
	handler, exists := m.outputHandlers[commandID]
	m.outputHandlersLock.RUnlock()
	if !exists {
		return
	}

	select {
	case handler <- CommandOutput{Output: output, IsError: isError, IsComplete: isComplete}:
	case <-time.After(5 * time.Second):
	}
}

// registerOutputHandler registers a handler for command output
func (m *Manager) registerOutputHandler(commandID string, handler chan CommandOutput) {
	m.outputHandlersLock.Lock()
	m.outputHandlers[commandID] = handler
	m.outputHandlersLock.Unlock()
}

// unregisterOutputHandler removes a handler for command output
func (m *Manager) unregisterOutputHandler(commandID string) {
	m.outputHandlersLock.Lock()
	delete(m.outputHandlers, commandID)
	m.outputHandlersLock.Unlock()
}

func (m *Manager) getCommandConfig(agentName, commandName string) (config.CommandInfo, bool) {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()
	if !exists {
		return config.CommandInfo{}, false
	}

	agent.commandsLock.RLock()
	defer agent.commandsLock.RUnlock()
	for _, cmd := range agent.availableCommands {
		if cmd.Name == commandName {
			return cmd, true
		}
	}
	return config.CommandInfo{}, false
}

func (m *Manager) getAgent(name string) *Agent {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()
	return m.agents[name]
}

func parseCommand(command string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(command), " ", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", "", fmt.Errorf("empty command")
	}

	commandName := parts[0]
	target := ""
	if len(parts) > 1 {
		target = parts[1]
	}
	return commandName, target, nil
}

func cloneCommands(commands []config.CommandInfo) []config.CommandInfo {
	result := make([]config.CommandInfo, len(commands))
	copy(result, commands)
	return result
}
