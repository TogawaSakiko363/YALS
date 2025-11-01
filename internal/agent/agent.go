package agent

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
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
	lastConnected     time.Time // 最后连接时间
	firstSeen         time.Time // 首次连接时间
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
		log.Printf("Failed to read agent handshake: %v", err)
		return
	}

	if handshake.Type != "handshake" {
		log.Printf("Invalid handshake type: %s", handshake.Type)
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

	log.Printf("Agent registered: %s (Group: %s)", handshake.Name, handshake.Group)

	// Send acknowledgment
	ack := map[string]interface{}{
		"type":    "handshake_ack",
		"message": "Agent registered successfully",
	}
	if err := conn.WriteJSON(ack); err != nil {
		log.Printf("Failed to send handshake ack: %v", err)
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
		log.Printf("Agent disconnected: %s (keeping in memory)", agent.Name)

		// 触发一次清理检查（可选，在有配置的情况下）
		// 这里不直接清理，而是让定期清理来处理，避免在断开时立即删除
	}()

	for {
		var message map[string]interface{}
		if err := agent.conn.ReadJSON(&message); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Agent %s unexpected WebSocket close: %v", agent.Name, err)
			} else if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("Agent %s closed connection normally", agent.Name)
			} else {
				log.Printf("Agent %s connection error: %v", agent.Name, err)
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
			log.Printf("Unknown message type from agent %s: %s", agent.Name, msgType)
		}
	}
}

// handleCommandOutput processes command output from an agent
func (m *Manager) handleCommandOutput(msg map[string]interface{}) {
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
			// Channel is full, skip this output
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

	// Create a channel to receive command output
	outputChan := make(chan CommandOutput, 100)
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
			// Send stop command
			stopReq := map[string]interface{}{
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
func (m *Manager) buildAgentInfo(name string, agent *Agent) map[string]interface{} {
	// 将后端状态映射为前端期望的格式：0=离线，1=在线
	frontendStatus := 0 // 默认离线
	if agent.Status() == StatusConnected {
		frontendStatus = 1
	}

	// 获取命令列表
	agent.commandsLock.RLock()
	commands := make([]string, len(agent.availableCommands))
	for i, cmd := range agent.availableCommands {
		commands[i] = cmd.Name
	}
	agent.commandsLock.RUnlock()

	// 计算离线时长（如果离线的话）
	var offlineDuration string
	if agent.Status() != StatusConnected {
		duration := time.Since(agent.lastConnected)
		if duration < time.Minute {
			offlineDuration = "刚刚离线"
		} else if duration < time.Hour {
			offlineDuration = fmt.Sprintf("%d分钟前离线", int(duration.Minutes()))
		} else if duration < 24*time.Hour {
			offlineDuration = fmt.Sprintf("%d小时前离线", int(duration.Hours()))
		} else {
			offlineDuration = fmt.Sprintf("%d天前离线", int(duration.Hours()/24))
		}
	}

	result := map[string]interface{}{
		"name":     name,
		"status":   frontendStatus,
		"commands": commands,
		"details": map[string]interface{}{
			"location":    agent.Details.Location,
			"datacenter":  agent.Details.Datacenter,
			"test_ip":     agent.Details.TestIP,
			"description": agent.Details.Description,
		},
		"connection_info": map[string]interface{}{
			"first_seen":       agent.firstSeen.Format("2006-01-02 15:04:05"),
			"last_connected":   agent.lastConnected.Format("2006-01-02 15:04:05"),
			"offline_duration": offlineDuration,
		},
	}

	return result
}

// GetAgents returns a list of all agents with their status and details
func (m *Manager) GetAgents() []map[string]interface{} {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	// 先获取所有agent名称并排序
	names := make([]string, 0, len(m.agents))
	for name := range m.agents {
		names = append(names, name)
	}
	sort.Strings(names) // 按字母A-Z排序

	// 按排序后的顺序构建agent列表
	agents := make([]map[string]interface{}, 0, len(m.agents))
	for _, name := range names {
		if agent, exists := m.agents[name]; exists {
			agents = append(agents, m.buildAgentInfo(name, agent))
		}
	}

	return agents
}

// GetAgentGroups returns all agents organized by groups
func (m *Manager) GetAgentGroups() []map[string]interface{} {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	// Group agents by their group name
	groups := make(map[string][]map[string]interface{})

	// 先获取所有agent名称并排序
	names := make([]string, 0, len(m.agents))
	for name := range m.agents {
		names = append(names, name)
	}
	sort.Strings(names) // 按字母A-Z排序

	// 按排序后的顺序分组
	for _, name := range names {
		if agent, exists := m.agents[name]; exists {
			groupName := agent.Group
			if groupName == "" {
				groupName = "Default"
			}

			if groups[groupName] == nil {
				groups[groupName] = make([]map[string]interface{}, 0)
			}

			groups[groupName] = append(groups[groupName], m.buildAgentInfo(name, agent))
		}
	}

	// 对组名也进行排序
	groupNames := make([]string, 0, len(groups))
	for groupName := range groups {
		groupNames = append(groupNames, groupName)
	}
	sort.Strings(groupNames)

	// 按排序后的组名顺序构建结果
	result := make([]map[string]interface{}, 0, len(groups))
	for _, groupName := range groupNames {
		result = append(result, map[string]interface{}{
			"name":   groupName,
			"agents": groups[groupName],
		})
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
			Name:        cmd.Name,
			Description: cmd.Description,
		}
	}

	return commands
}

// GetAllAvailableCommands returns all unique commands from all connected agents
func (m *Manager) GetAllAvailableCommands() []validator.CommandDetail {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	commandMap := make(map[string]validator.CommandDetail)

	for _, agent := range m.agents {
		if agent.Status() == StatusConnected {
			agent.commandsLock.RLock()
			for _, cmd := range agent.availableCommands {
				commandMap[cmd.Name] = validator.CommandDetail{
					Name:        cmd.Name,
					Description: cmd.Description,
				}
			}
			agent.commandsLock.RUnlock()
		}
	}

	commands := make([]validator.CommandDetail, 0, len(commandMap))
	for _, cmd := range commandMap {
		commands = append(commands, cmd)
	}

	return commands
}

// CleanupOfflineAgents removes agents that have been offline for more than the specified duration
func (m *Manager) CleanupOfflineAgents(maxOfflineDuration time.Duration) int {
	m.agentsLock.Lock()
	defer m.agentsLock.Unlock()

	var toDelete []string
	now := time.Now()

	for name, agent := range m.agents {
		if agent.Status() == StatusDisconnected {
			if now.Sub(agent.lastConnected) > maxOfflineDuration {
				toDelete = append(toDelete, name)
			}
		}
	}

	for _, name := range toDelete {
		delete(m.agents, name)
		log.Printf("Cleaned up offline agent: %s", name)
	}

	return len(toDelete)
}

// GetAgentStats returns statistics about agents
func (m *Manager) GetAgentStats() map[string]interface{} {
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

	return map[string]interface{}{
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
func buildCommandRequest(commandName, target, commandID string) map[string]interface{} {
	return map[string]interface{}{
		"type":         "execute_command",
		"command_name": commandName,
		"target":       target,
		"command_id":   commandID,
	}
}
