package agent

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/validator"

	"github.com/gorilla/websocket"
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
	conn              *websocket.Conn
	status            Status
	lastCheck         time.Time
	lastConnected     time.Time // Last connection time
	firstSeen         time.Time // First connection time
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

// HandleAgentConnection handles a new agent connection
func (m *Manager) HandleAgentConnection(conn *websocket.Conn) {
	defer conn.Close()

	// Wait for handshake message from agent
	var handshake struct {
		Type     string               `json:"type"`
		Name     string               `json:"name"`
		Group    string               `json:"group"`
		Details  config.AgentDetails  `json:"details"`
		Commands []config.CommandInfo `json:"commands"`
	}

	if err := conn.ReadJSON(&handshake); err != nil {
		logger.Errorf("Failed to read agent handshake: %v", err)
		return
	}

	if handshake.Type != "handshake" {
		logger.Warnf("Invalid handshake type: %s", handshake.Type)
		return
	}

	// Create or update agent
	m.agentsLock.Lock()
	agent, exists := m.agents[handshake.Name]
	if exists {
		// Update existing agent
		agent.Group = handshake.Group
		agent.Details = handshake.Details
		agent.conn = conn
		agent.status = StatusConnected
		agent.lastCheck = time.Now()
		agent.lastConnected = time.Now()
		agent.availableCommands = handshake.Commands
	} else {
		// Create new agent
		now := time.Now()
		agent = &Agent{
			Name:              handshake.Name,
			Group:             handshake.Group,
			Details:           handshake.Details,
			conn:              conn,
			status:            StatusConnected,
			lastCheck:         now,
			lastConnected:     now,
			firstSeen:         now,
			availableCommands: handshake.Commands,
		}
		m.agents[handshake.Name] = agent
	}
	m.agentsLock.Unlock()

	logger.Infof("Agent registered: %s (Group: %s)", handshake.Name, handshake.Group)

	// Send acknowledgment
	ack := map[string]any{
		"type":    "handshake_ack",
		"message": "Agent registered successfully",
	}
	if err := conn.WriteJSON(ack); err != nil {
		logger.Errorf("Failed to send handshake ack: %v", err)
		return
	}

	// Handle agent messages
	m.handleAgentMessages(agent)
}

// handleAgentMessages handles incoming messages from an agent
func (m *Manager) handleAgentMessages(agent *Agent) {
	defer func() {
		// Mark agent as disconnected but keep it in memory
		agent.statusLock.Lock()
		agent.status = StatusDisconnected
		agent.conn = nil
		agent.statusLock.Unlock()
		logger.Infof("Agent disconnected: %s (keeping in memory)", agent.Name)

		// Trigger cleanup check (optional, when configured)
		// Don't clean immediately, let periodic cleanup handle it to avoid instant deletion on disconnect
	}()

	for {
		var message map[string]any
		if err := agent.conn.ReadJSON(&message); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Errorf("Agent %s unexpected WebSocket close: %v", agent.Name, err)
			} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.Infof("Agent %s closed connection normally", agent.Name)
			} else {
				logger.Errorf("Agent %s connection error: %v", agent.Name, err)
			}
			break
		}

		msgType, ok := message["type"].(string)
		if !ok {
			continue
		}

		switch msgType {
		case "command_output":
			m.handleCommandOutput(message)
		default:
			logger.Warnf("Unknown message type from agent %s: %s", agent.Name, msgType)
		}
	}
}

// handleCommandOutput processes command output from an agent
func (m *Manager) handleCommandOutput(msg map[string]any) {
	commandID, ok := msg["command_id"].(string)
	if !ok {
		return
	}

	output, _ := msg["output"].(string)
	errorMsg, _ := msg["error"].(string)
	isComplete, _ := msg["is_complete"].(bool)
	isError, _ := msg["is_error"].(bool)

	// If there's an error message, use it as output and mark as error
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
			// Channel is full, log warning but try to send anyway with timeout
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

// ExecuteCommand executes a command on an agent
func (m *Manager) ExecuteCommand(agentName, command string) (string, error) {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists {
		return "", fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return "", fmt.Errorf("agent not connected: %s", agentName)
	}

	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())

	// Parse command and target from the command string
	commandName, target, err := parseCommand(command)
	if err != nil {
		return "", err
	}

	// Send command request
	req := buildCommandRequest(commandName, target, commandID)

	if err := agent.conn.WriteJSON(req); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// For simple execution, we would need to collect the response
	// This is a simplified version - in practice, you'd want to implement
	// a proper response collection mechanism
	return "Command sent successfully", nil
}

// ExecuteCommandStreaming executes a command on an agent with streaming output
func (m *Manager) ExecuteCommandStreaming(agentName, command string, callback StreamingOutputCallback) error {
	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, nil, func(output string, isError bool, isComplete bool, isStopped bool) {
		callback(output, isError, isComplete)
	})
}

// ExecuteCommandStreamingWithStop executes a command on an agent with streaming output and stop support
func (m *Manager) ExecuteCommandStreamingWithStop(agentName, command string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, stopChan, callback)
}

// ExecuteCommandStreamingWithStopAndID executes a command on an agent with streaming output, stop support and custom command ID
func (m *Manager) ExecuteCommandStreamingWithStopAndID(agentName, command, commandID string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent not connected: %s", agentName)
	}

	// Parse command and target from the command string
	commandName, target, err := parseCommand(command)
	if err != nil {
		return err
	}

	// Get command configuration
	cmdConfig, exists := m.getCommandConfig(agentName, commandName)
	if !exists {
		return fmt.Errorf("command not found: %s", commandName)
	}

	// Handle ignore_target: if ignore_target is true, don't send target parameter
	if cmdConfig.IgnoreTarget {
		target = ""
	}

	// Create a channel to receive command output with larger buffer to prevent output loss
	outputChan := make(chan CommandOutput, 1000)
	defer close(outputChan)

	// Register output handler
	m.registerOutputHandler(commandID, outputChan)
	defer m.unregisterOutputHandler(commandID)

	// Send command request
	req := buildCommandRequest(commandName, target, commandID)

	if err := agent.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	// Process output with stop support
	for {
		select {
		case <-stopChan:
			// Send stop command to agent
			stopReq := map[string]any{
				"type":       "stop_command",
				"command_id": commandID,
			}
			agent.conn.WriteJSON(stopReq)
			callback("", false, false, true) // Signal stopped
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

	// Get command list
	agent.commandsLock.RLock()
	commands := make([]string, len(agent.availableCommands))
	for i, cmd := range agent.availableCommands {
		commands[i] = cmd.Name
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
			Description:  cmd.Description,
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
					Description:  cmd.Description,
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

// buildCommandRequest builds a command request
func buildCommandRequest(commandName, target, commandID string) map[string]any {
	return map[string]any{
		"type":         "execute_command",
		"command_name": commandName,
		"target":       target,
		"command_id":   commandID,
	}
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
