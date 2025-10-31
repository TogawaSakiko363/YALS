package agent

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"YALS_SSH/internal/config"

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

// Agent represents a WebSocket agent
type Agent struct {
	Config     config.Agent
	conn       *websocket.Conn
	status     Status
	lastCheck  time.Time
	statusLock sync.RWMutex
	dialer     *websocket.Dialer
}

// CommandOutput represents command output from an agent
type CommandOutput struct {
	Output     string
	IsError    bool
	IsComplete bool
}

// Manager manages multiple WebSocket agents
type Manager struct {
	agents               map[string]*Agent
	config               *config.Config
	agentsLock           sync.RWMutex
	offlineCheckerTicker *time.Ticker
	offlineCheckerDone   chan bool
	outputHandlers       map[string]chan CommandOutput
	outputHandlersLock   sync.RWMutex
}

// NewManager creates a new agent manager
func NewManager(cfg *config.Config) *Manager {
	manager := &Manager{
		agents:             make(map[string]*Agent),
		config:             cfg,
		offlineCheckerDone: make(chan bool),
		outputHandlers:     make(map[string]chan CommandOutput),
	}

	// Initialize agents from config
	for _, agentCfg := range cfg.Agents {
		agent := &Agent{
			Config: agentCfg,
			status: StatusDisconnected,
			dialer: &websocket.Dialer{
				HandshakeTimeout: 10 * time.Second,
			},
		}
		manager.agents[agentCfg.Name] = agent
	}

	// 延迟启动离线检查器，让Connect()先执行，避免双重连接
	go func() {
		// 等待5秒让初始连接完成
		time.Sleep(5 * time.Second)
		manager.startOfflineAgentChecker()
	}()

	return manager
}

// Connect establishes WebSocket connections to all agents
func (m *Manager) Connect() {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	log.Println("Starting to connect to all agents...")
	for _, agent := range m.agents {
		log.Printf("Initiating connection to agent: %s (%s)", agent.Config.Name, agent.Config.Host)
		go m.connectAgent(agent)
	}
}

// startOfflineAgentChecker starts a background task that periodically checks offline agents
func (m *Manager) startOfflineAgentChecker() {
	// Check every 60 seconds by default
	checkInterval := 60
	if m.config.Connection.RetryInterval > 0 {
		// Use retry interval from configuration
		checkInterval = m.config.Connection.RetryInterval
	}

	m.offlineCheckerTicker = time.NewTicker(time.Duration(checkInterval) * time.Second)
	defer m.offlineCheckerTicker.Stop()

	log.Printf("Starting offline agent checker with interval %d seconds", checkInterval)

	// Check immediately on startup
	m.checkOfflineAgents()

	for {
		select {
		case <-m.offlineCheckerTicker.C:
			m.checkOfflineAgents()
		case <-m.offlineCheckerDone:
			log.Println("Offline agent checker stopped")
			return
		}
	}
}

// checkOfflineAgents checks all offline agents and tries to reconnect
func (m *Manager) checkOfflineAgents() {
	m.agentsLock.RLock()
	offlineAgents := make([]*Agent, 0)
	for _, agent := range m.agents {
		// 只检查真正离线的代理，不包括正在连接中的
		if agent.Status() == StatusDisconnected {
			offlineAgents = append(offlineAgents, agent)
		}
	}
	m.agentsLock.RUnlock()

	if len(offlineAgents) > 0 {
		log.Printf("Checking %d offline agents for reconnection", len(offlineAgents))
		for _, agent := range offlineAgents {
			go func(a *Agent) {
				// Try to connect to offline agent
				m.connectAgent(a)
				if a.Status() == StatusConnected {
					log.Printf("Successfully reconnected to offline agent: %s", a.Config.Name)
				}
			}(agent)
		}
	}
}

// connectAgent establishes a WebSocket connection to a single agent
func (m *Manager) connectAgent(agent *Agent) {
	// 检查是否已经在连接中或已连接，避免重复连接
	agent.statusLock.Lock()
	if agent.status == StatusConnecting || agent.status == StatusConnected {
		agent.statusLock.Unlock()
		if agent.status == StatusConnecting {
			log.Printf("Agent %s is already being connected, skipping duplicate connection attempt", agent.Config.Name)
		} else {
			log.Printf("Agent %s is already connected, skipping duplicate connection attempt", agent.Config.Name)
		}
		return
	}
	// 设置为连接中状态
	agent.status = StatusConnecting
	agent.statusLock.Unlock()

	log.Printf("Connecting to agent %s (%s)...", agent.Config.Name, agent.Config.Host)

	// Prepare WebSocket URL
	url := fmt.Sprintf("ws://%s/ws", agent.Config.Host)

	// Set up headers with password authentication
	headers := http.Header{}
	headers.Set("X-Agent-Password", agent.Config.Password)

	// Connect to the WebSocket server
	log.Printf("Dialing WebSocket server at %s for agent %s", url, agent.Config.Name)
	conn, _, err := agent.dialer.Dial(url, headers)
	if err != nil {
		log.Printf("Failed to connect to agent %s: %v", agent.Config.Name, err)
		agent.setStatus(StatusDisconnected)
		return
	}

	agent.conn = conn
	agent.setStatus(StatusConnected)
	log.Printf("Successfully connected to agent %s (%s)", agent.Config.Name, agent.Config.Host)

	// Start message handling goroutine
	go m.handleAgentMessages(agent)

	// Start keepalive goroutine
	go m.keepAlive(agent)
}

// handleAgentMessages handles incoming messages from an agent
func (m *Manager) handleAgentMessages(agent *Agent) {
	defer func() {
		if agent.conn != nil {
			agent.conn.Close()
		}
		agent.setStatus(StatusDisconnected)
		log.Printf("Agent %s message handler stopped", agent.Config.Name)
	}()

	for {
		var msg map[string]interface{}
		if err := agent.conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error for agent %s: %v", agent.Config.Name, err)
			}
			break
		}

		// Handle different message types
		if msgType, ok := msg["type"].(string); ok {
			switch msgType {
			case "auth_success":
				log.Printf("Agent %s authenticated successfully", agent.Config.Name)
			case "command_output":
				m.handleCommandOutput(msg)
			default:
				log.Printf("Unknown message type from agent %s: %s", agent.Config.Name, msgType)
			}
		}
	}
}

// handleCommandOutput processes command output from an agent
func (m *Manager) handleCommandOutput(msg map[string]interface{}) {
	commandID, ok := msg["command_id"].(string)
	if !ok {
		return
	}

	output := CommandOutput{}

	if outputStr, ok := msg["output"].(string); ok {
		output.Output = outputStr
	}

	if errorStr, ok := msg["error"].(string); ok && errorStr != "" {
		output.Output = errorStr
		output.IsError = true
	}

	if isError, ok := msg["is_error"].(bool); ok {
		output.IsError = isError
	}

	if isComplete, ok := msg["is_complete"].(bool); ok {
		output.IsComplete = isComplete
	}

	// Send to registered handler
	m.outputHandlersLock.RLock()
	if handler, exists := m.outputHandlers[commandID]; exists {
		select {
		case handler <- output:
		default:
			// Channel is full, skip this output
		}
	}
	m.outputHandlersLock.RUnlock()
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

// keepAlive sends periodic ping messages to the agent
func (m *Manager) keepAlive(agent *Agent) {
	interval := time.Duration(m.config.Connection.Keepalive) * time.Second
	log.Printf("Starting keepalive routine for agent %s with interval %v", agent.Config.Name, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if agent.conn == nil {
			log.Printf("Keepalive for agent %s stopped: connection is nil", agent.Config.Name)
			return
		}

		if err := agent.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(10*time.Second)); err != nil {
			log.Printf("Keepalive failed for agent %s: %v", agent.Config.Name, err)
			agent.setStatus(StatusDisconnected)
			go m.reconnect(agent)
			return
		}
	}
}

// reconnect attempts to reconnect to an agent
func (m *Manager) reconnect(agent *Agent) {
	log.Printf("Starting reconnection attempts for agent %s", agent.Config.Name)
	offlineAfterRetries := m.config.Connection.OfflineAfterRetries
	maxRetries := m.config.Connection.MaxRetries
	retries := 0
	retryInterval := m.config.Connection.RetryInterval

	// 尝试重连直到成功或达到最大重试次数（如果设置了）
	for {
		retries++

		// 如果设置了最大重试次数，并且已经达到，则停止重连
		if maxRetries > 0 && retries > maxRetries {
			log.Printf("Max retries (%d) reached for agent %s, stopping reconnection attempts", maxRetries, agent.Config.Name)
			break
		}

		log.Printf("Reconnection attempt %d for agent %s", retries, agent.Config.Name)

		time.Sleep(time.Duration(retryInterval) * time.Second)
		m.connectAgent(agent)
		if agent.Status() == StatusConnected {
			log.Printf("Successfully reconnected to agent %s on attempt %d", agent.Config.Name, retries)
			return
		}

		// 在指定次数失败后标记为离线，但继续尝试重连
		if offlineAfterRetries > 0 && retries >= offlineAfterRetries && agent.Status() != StatusDisconnected {
			log.Printf("Failed to reconnect to agent %s after %d attempts, marking as offline", agent.Config.Name, offlineAfterRetries)
			agent.setStatus(StatusDisconnected)
		}
	}

	// 如果循环退出（达到最大重试次数），标记为离线
	agent.setStatus(StatusDisconnected)
}

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

	// Send command request
	req := map[string]interface{}{
		"type":       "execute_command",
		"command":    command,
		"command_id": commandID,
	}

	if err := agent.conn.WriteJSON(req); err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	// For simple execution, we would need to collect the response
	// This is a simplified version - in practice, you'd want to implement
	// a proper response collection mechanism
	return "Command sent successfully", nil
}

// StreamingOutputCallback is called for each chunk of output during command execution
type StreamingOutputCallback func(output string, isError bool, isComplete bool)

// StreamingOutputCallbackWithStop is called for each chunk of output during command execution with stop support
type StreamingOutputCallbackWithStop func(output string, isError bool, isComplete bool, isStopped bool)

// ExecuteCommandStreaming executes a command on an agent with streaming output
func (m *Manager) ExecuteCommandStreaming(agentName, command string, callback StreamingOutputCallback) error {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent not connected: %s", agentName)
	}

	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())

	// Create a channel to receive command output
	outputChan := make(chan CommandOutput, 100)
	defer close(outputChan)

	// Register output handler
	m.registerOutputHandler(commandID, outputChan)
	defer m.unregisterOutputHandler(commandID)

	// Send command request
	req := map[string]interface{}{
		"type":       "execute_command",
		"command":    command,
		"command_id": commandID,
	}

	if err := agent.conn.WriteJSON(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	// Process output
	for output := range outputChan {
		if output.IsComplete {
			callback("", false, true) // Signal completion
			break
		}
		callback(output.Output, output.IsError, false)
	}

	return nil
}

// ExecuteCommandStreamingWithStop executes a command on an agent with streaming output and stop support
func (m *Manager) ExecuteCommandStreamingWithStop(agentName, command string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent not connected: %s", agentName)
	}

	// Generate command ID
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())

	// Create a channel to receive command output
	outputChan := make(chan CommandOutput, 100)
	defer close(outputChan)

	// Register output handler
	m.registerOutputHandler(commandID, outputChan)
	defer m.unregisterOutputHandler(commandID)

	// Send command request
	req := map[string]interface{}{
		"type":       "execute_command",
		"command":    command,
		"command_id": commandID,
	}

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
			if output.IsComplete {
				callback("", false, true, false) // Signal completion
				return nil
			}
			callback(output.Output, output.IsError, false, false)
		}
	}
}

// GetAgents returns a list of all agents with their status and details
func (m *Manager) GetAgents() []map[string]interface{} {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	result := make([]map[string]interface{}, 0, len(m.agents))
	for name, agent := range m.agents {
		// 将后端状态映射为前端期望的格式：1表示在线，其他表示离线
		frontendStatus := 0 // 默认离线
		if agent.Status() == StatusConnected {
			frontendStatus = 1 // 在线
		}

		result = append(result, map[string]interface{}{
			"name":     name,
			"status":   frontendStatus,
			"host":     agent.Config.Host,
			"commands": agent.Config.Commands,
			"details": map[string]interface{}{
				"location":    agent.Config.Details.Location,
				"datacenter":  agent.Config.Details.Datacenter,
				"test_ip":     agent.Config.Details.TestIP,
				"description": agent.Config.Details.Description,
			},
			"group": m.getAgentGroup(name),
		})
	}

	return result
}

// getAgentGroup returns the group name for an agent
func (m *Manager) getAgentGroup(agentName string) string {
	for _, group := range m.config.Groups {
		for _, agent := range group.Agents {
			if agent == agentName {
				return group.Name
			}
		}
	}
	return ""
}

// GetAgentGroups returns all agents organized by groups with defined order
func (m *Manager) GetAgentGroups() []map[string]interface{} {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	// Create a map for quick lookup
	groupMap := make(map[string][]map[string]interface{})

	// Initialize groups in config order
	for _, group := range m.config.Groups {
		groupMap[group.Name] = make([]map[string]interface{}, 0)
	}

	// Handle ungrouped agents
	ungrouped := make([]map[string]interface{}, 0)

	// Organize agents by group, maintaining config order
	for _, group := range m.config.Groups {
		for _, agentName := range group.Agents {
			if agent, exists := m.agents[agentName]; exists {
				// 将后端状态映射为前端期望的格式：1表示在线，其他表示离线
				frontendStatus := 0 // 默认离线
				if agent.Status() == StatusConnected {
					frontendStatus = 1 // 在线
				}

				agentInfo := map[string]interface{}{
					"name":     agentName,
					"status":   frontendStatus,
					"host":     agent.Config.Host,
					"commands": agent.Config.Commands,
					"details": map[string]interface{}{
						"location":    agent.Config.Details.Location,
						"datacenter":  agent.Config.Details.Datacenter,
						"test_ip":     agent.Config.Details.TestIP,
						"description": agent.Config.Details.Description,
					},
				}
				groupMap[group.Name] = append(groupMap[group.Name], agentInfo)
			}
		}
	}

	// Handle ungrouped agents
	for name, agent := range m.agents {
		groupName := m.getAgentGroup(name)
		if groupName == "" {
			// 将后端状态映射为前端期望的格式：1表示在线，其他表示离线
			frontendStatus := 0 // 默认离线
			if agent.Status() == StatusConnected {
				frontendStatus = 1 // 在线
			}

			agentInfo := map[string]interface{}{
				"name":     name,
				"status":   frontendStatus,
				"host":     agent.Config.Host,
				"commands": agent.Config.Commands,
				"details": map[string]interface{}{
					"location":    agent.Config.Details.Location,
					"datacenter":  agent.Config.Details.Datacenter,
					"test_ip":     agent.Config.Details.TestIP,
					"description": agent.Config.Details.Description,
				},
			}
			ungrouped = append(ungrouped, agentInfo)
		}
	}

	// Build ordered result
	result := make([]map[string]interface{}, 0)

	// Add groups in config order
	for _, group := range m.config.Groups {
		if len(groupMap[group.Name]) > 0 {
			result = append(result, map[string]interface{}{
				"name":   group.Name,
				"agents": groupMap[group.Name],
			})
		}
	}

	// Add ungrouped agents at the end
	if len(ungrouped) > 0 {
		result = append(result, map[string]interface{}{
			"name":   "Ungrouped",
			"agents": ungrouped,
		})
	}

	return result
}

// setStatus sets the agent status with thread safety
func (a *Agent) setStatus(status Status) {
	a.statusLock.Lock()
	defer a.statusLock.Unlock()
	a.status = status
	a.lastCheck = time.Now()
}

// Status returns the agent status with thread safety
func (a *Agent) Status() Status {
	a.statusLock.RLock()
	defer a.statusLock.RUnlock()
	return a.status
}
