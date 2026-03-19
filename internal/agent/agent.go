package agent

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/proto"
	"YALS/internal/validator"
)

// Status represents the connection status of an agent
type Status int

const (
	// StatusDisconnected indicates the agent is disconnected
	StatusDisconnected Status = iota
	// StatusConnecting indicates the agent is currently connecting
	StatusConnecting
	// StatusConnected indicates the agent is connected
	StatusConnected
)

// Agent represents a connected agent
type Agent struct {
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
}

// CommandOutput represents command output from an agent
type CommandOutput struct {
	Output     string
	IsError    bool
	IsComplete bool
}

// Manager manages multiple WebSocket agents
type Manager struct {
	agents             map[string]*Agent
	agentsLock         sync.RWMutex
	outputHandlers     map[string]chan CommandOutput
	outputHandlersLock sync.RWMutex
}

// NewManager creates a new agent manager
func NewManager() *Manager {
	return &Manager{
		agents:         make(map[string]*Agent),
		outputHandlers: make(map[string]chan CommandOutput),
	}
}

// HandleAgentConnection handles a new agent gRPC stream connection
func (m *Manager) HandleAgentConnection(stream proto.AgentService_StreamCommandsServer) error {
	// The handshake is already done via the Handshake RPC call
	// This method now handles the bidirectional streaming

	// Get agent name from the first message or context
	// For now, we'll wait for messages and handle them
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				logger.Info("Agent stream closed")
			} else {
				logger.Errorf("Agent stream error: %v", err)
			}
			return err
		}

		// Handle command output messages
		if msg.Type == "command_output" {
			m.handleCommandOutputProto(msg)
		}
	}
}

// RegisterAgent registers a new agent from handshake
func (m *Manager) RegisterAgent(name, group string, details proto.AgentDetails, commands []proto.CommandInfo, stream proto.AgentService_StreamCommandsServer) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	// Convert proto details to config details
	agentDetails := config.AgentDetails{
		Location:    details.Location,
		Datacenter:  details.Datacenter,
		TestIP:      details.TestIP,
		Description: details.Description,
	}

	// Convert proto commands to config commands
	configCommands := make([]config.CommandInfo, len(commands))
	for i, cmd := range commands {
		configCommands[i] = config.CommandInfo{
			Name:         cmd.Name,
			IgnoreTarget: cmd.IgnoreTarget,
		}
	}

	agent, exists := m.agents[name]
	if exists {
		agent.Group = group
		agent.Details = agentDetails
		agent.stream = stream
		agent.status = StatusConnected
		agent.lastCheck = time.Now()
		agent.lastConnected = time.Now()
		agent.availableCommands = configCommands
	} else {
		now := time.Now()
		agent = &Agent{
			Name:              name,
			Group:             group,
			Details:           agentDetails,
			stream:            stream,
			status:            StatusConnected,
			lastCheck:         now,
			lastConnected:     now,
			firstSeen:         now,
			availableCommands: configCommands,
		}
		m.agents[name] = agent
	}

	logger.Infof("Agent registered: %s (Group: %s)", name, group)
}

// RegisterAgentStream registers an agent's stream connection
func (m *Manager) RegisterAgentStream(name, group string, stream proto.AgentService_StreamCommandsServer) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agents[name]
	if exists {
		agent.stream = stream
		agent.status = StatusConnected
		agent.lastConnected = time.Now()
		logger.Infof("Agent stream connected: %s", name)
	} else {
		logger.Warnf("Agent stream connected but agent not registered: %s", name)
	}
}

// UnregisterAgentStream marks an agent as disconnected
func (m *Manager) UnregisterAgentStream(name string) {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	agent, exists := m.agents[name]
	if exists {
		agent.statusLock.Lock()
		agent.status = StatusDisconnected
		agent.stream = nil
		agent.statusLock.Unlock()
		logger.Infof("Agent stream disconnected: %s", name)
	}
}

// handleCommandOutputProto processes command output from an agent via proto
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

	if exists {
		select {
		case handler <- CommandOutput{
			Output:     output,
			IsError:    isError,
			IsComplete: isComplete,
		}:
		default:
			logger.Warnf("Output channel full for command %s, attempting to send with timeout", commandID)
			select {
			case handler <- CommandOutput{
				Output:     output,
				IsError:    isError,
				IsComplete: isComplete,
			}:
			case <-time.After(5 * time.Second):
				logger.Errorf("Failed to send output for command %s after timeout, output may be lost", commandID)
			}
		}
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

// Status returns the current status of the agent
func (a *Agent) Status() Status {
	a.statusLock.RLock()
	defer a.statusLock.RUnlock()
	return a.status
}

// StreamingOutputCallback is called for each chunk of output during command execution
type StreamingOutputCallback func(output string, isError bool, isComplete bool)

// StreamingOutputCallbackWithStop is called for each chunk of output during command execution with stop support
type StreamingOutputCallbackWithStop func(output string, isError bool, isComplete bool, isStopped bool)

// ExecuteCommand executes a command on an agent (deprecated)
func (m *Manager) ExecuteCommand(agentName, command string) (string, error) {
	return "Command execution via ExecuteCommand is deprecated, use ExecuteCommandStreaming", nil
}

// ExecuteCommandStreaming executes a command on an agent with streaming output
func (m *Manager) ExecuteCommandStreaming(agentName, command string, callback StreamingOutputCallback) error {
	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, "auto", nil, func(output string, isError bool, isComplete bool, isStopped bool) {
		callback(output, isError, isComplete)
	})
}

// ExecuteCommandStreamingWithStop executes a command on an agent with streaming output and stop support
func (m *Manager) ExecuteCommandStreamingWithStop(agentName, command string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, "auto", stopChan, callback)
}

// ExecuteCommandStreamingWithStopAndID executes a command on an agent with streaming output, stop support and custom command ID
func (m *Manager) ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, ipVersion string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent not connected: %s", agentName)
	}

	commandName, target, err := parseCommand(command)
	if err != nil {
		return err
	}

	cmdConfig, exists := m.getCommandConfig(agentName, commandName)
	if !exists {
		return fmt.Errorf("command not found: %s", commandName)
	}

	if cmdConfig.IgnoreTarget {
		target = ""
	}

	if ipVersion == "" {
		ipVersion = "auto"
	}

	outputChan := make(chan CommandOutput, 1000)
	defer close(outputChan)

	m.registerOutputHandler(commandID, outputChan)
	defer m.unregisterOutputHandler(commandID)

	// Send command request via gRPC
	req := &proto.CommandMessage{
		Type:        "execute_command",
		CommandName: commandName,
		Target:      target,
		CommandID:   commandID,
		IPVersion:   ipVersion,
	}

	if err := agent.stream.Send(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	for {
		select {
		case <-stopChan:
			stopReq := &proto.CommandMessage{
				Type:      "stop_command",
				CommandID: commandID,
			}
			agent.stream.Send(stopReq)
			callback("", false, false, true)
			return nil
		case output := <-outputChan:
			callback(output.Output, output.IsError, output.IsComplete, false)
			if output.IsComplete {
				return nil
			}
		}
	}
}

// buildAgentInfo creates a standardized agent info map
func (m *Manager) buildAgentInfo(name string, agent *Agent) map[string]any {
	// Map backend status to frontend expected format: 0=offline, 1=online
	frontendStatus := 0
	if agent.Status() == StatusConnected {
		frontendStatus = 1
	}

	// Get command list with details
	agent.commandsLock.RLock()
	commands := make([]map[string]any, len(agent.availableCommands))
	for i, cmd := range agent.availableCommands {
		commands[i] = map[string]any{
			"name":          cmd.Name,
			"ignore_target": cmd.IgnoreTarget,
		}
	}
	agent.commandsLock.RUnlock()

	return map[string]any{
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

// calculateOfflineDuration calculates how long an agent has been offline
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

// GetAgents returns a list of all agents with their status and details
func (m *Manager) GetAgents() []map[string]any {
	names, agents := m.getSortedAgents()

	result := make([]map[string]any, len(names))
	for i := range names {
		result[i] = agents[i]
	}

	return result
}

// getSortedAgents returns sorted agent names and their corresponding info maps
func (m *Manager) getSortedAgents() ([]string, []map[string]any) {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	// Get sorted agent names
	names := make([]string, 0, len(m.agents))
	for name := range m.agents {
		names = append(names, name)
	}
	sort.Strings(names)

	// Build agent info in sorted order
	agents := make([]map[string]any, 0, len(names))
	for _, name := range names {
		if agent, exists := m.agents[name]; exists {
			agents = append(agents, m.buildAgentInfo(name, agent))
		}
	}

	return names, agents
}

// GetAgentGroups returns all agents organized by groups
func (m *Manager) GetAgentGroups() []map[string]any {
	names, agents := m.getSortedAgents()

	// Pre-calculate group count to avoid repeated map allocations
	groupCount := make(map[string]int, len(names))
	for i := range names {
		agentInfo := agents[i]
		groupName, ok := agentInfo["details"].(map[string]any)["group"]
		if !ok {
			groupName = "Default"
		}
		groupNameStr, _ := groupName.(string)
		if groupNameStr == "" {
			groupNameStr = "Default"
		}
		groupCount[groupNameStr] = groupCount[groupNameStr] + 1
	}

	// Group agents by their group name with pre-allocated slices
	groups := make(map[string][]map[string]any, len(groupCount))
	for i := range names {
		agentInfo := agents[i]
		groupName, ok := agentInfo["details"].(map[string]any)["group"]
		if !ok {
			groupName = "Default"
		}

		groupNameStr, _ := groupName.(string)
		if groupNameStr == "" {
			groupNameStr = "Default"
		}

		// Pre-allocate slice if not exists
		if groups[groupNameStr] == nil {
			groups[groupNameStr] = make([]map[string]any, 0, groupCount[groupNameStr])
		}
		groups[groupNameStr] = append(groups[groupNameStr], agentInfo)
	}

	// Build result with sorted group names
	groupNames := make([]string, 0, len(groups))
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	result := make([]map[string]any, len(groupNames))
	for i, groupName := range groupNames {
		result[i] = map[string]any{
			"name":   groupName,
			"agents": groups[groupName],
		}
	}

	return result
}

// GetAgentCommands returns the available commands for a specific agent
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

// GetAllAvailableCommands returns all unique commands from all connected agents
func (m *Manager) GetAllAvailableCommands() []validator.CommandDetail {
	commandMap := m.getAllConnectedAgentCommands()

	commands := make([]validator.CommandDetail, len(commandMap))
	i := 0
	for _, cmd := range commandMap {
		commands[i] = cmd
		i++
	}

	return commands
}

// getAllConnectedAgentCommands returns command map for all connected agents
func (m *Manager) getAllConnectedAgentCommands() map[string]validator.CommandDetail {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	commandMap := make(map[string]validator.CommandDetail)

	for _, agent := range m.agents {
		if agent.Status() == StatusConnected {
			agent.commandsLock.RLock()
			for _, cmd := range agent.availableCommands {
				commandMap[cmd.Name] = validator.CommandDetail{
					Name:         cmd.Name,
					IgnoreTarget: cmd.IgnoreTarget,
				}
			}
			agent.commandsLock.RUnlock()
		}
	}

	return commandMap
}

// CleanupOfflineAgents removes agents that have been offline for more than the specified duration
func (m *Manager) CleanupOfflineAgents(maxOfflineDuration time.Duration) int {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	// Pre-allocate slice with estimated size to avoid repeated allocations
	estimatedCount := len(m.agents) / 3 // Rough estimate that 1/3 might be offline
	toDelete := make([]string, 0, estimatedCount)
	actualCount := 0
	now := time.Now()

	for name, agent := range m.agents {
		if agent.Status() == StatusDisconnected && now.Sub(agent.lastConnected) > maxOfflineDuration {
			if actualCount >= cap(toDelete) {
				// Expand capacity if needed
				newSlice := make([]string, len(toDelete), cap(toDelete)*2)
				copy(newSlice, toDelete)
				toDelete = newSlice
			}
			toDelete = append(toDelete, name)
			actualCount++
		}
	}

	for _, name := range toDelete {
		delete(m.agents, name)
		logger.Infof("Cleaned up offline agent: %s", name)
	}

	return actualCount
}

// GetAgentStats returns statistics about agents
func (m *Manager) GetAgentStats() map[string]any {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	online := 0
	offline := 0
	total := len(m.agents)

	for _, agent := range m.agents {
		if agent.Status() == StatusConnected {
			online++
		} else {
			offline++
		}
	}

	return map[string]any{
		"total":   total,
		"online":  online,
		"offline": offline,
	}
}

// parseCommand parses a command string into command name and target
func parseCommand(command string) (string, string, error) {
	parts := strings.SplitN(strings.TrimSpace(command), " ", 2)
	if len(parts) == 0 {
		return "", "", fmt.Errorf("empty command")
	}

	commandName := parts[0]
	target := ""
	if len(parts) > 1 {
		target = parts[1]
	}

	return commandName, target, nil
}

// getCommandConfig returns the command configuration for a specific agent and command
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

// GetCommandConfigInternal returns the command configuration for a specific agent and command (public method for handler)
func (m *Manager) GetCommandConfigInternal(agentName, commandName string) (config.CommandInfo, bool) {
	return m.getCommandConfig(agentName, commandName)
}
