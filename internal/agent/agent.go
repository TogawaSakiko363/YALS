package agent

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"YALS_SSH/internal/config"

	"golang.org/x/crypto/ssh"
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

// Agent represents an SSH agent
type Agent struct {
	Config     config.Agent
	client     *ssh.Client
	status     Status
	lastCheck  time.Time
	statusLock sync.RWMutex
}

// Manager manages multiple SSH agents
type Manager struct {
	agents     map[string]*Agent
	config     *config.Config
	agentsLock sync.RWMutex
	offlineCheckerTicker *time.Ticker
	offlineCheckerDone  chan bool
}

// NewManager creates a new agent manager
func NewManager(cfg *config.Config) *Manager {
	manager := &Manager{
		agents:              make(map[string]*Agent),
		config:              cfg,
		offlineCheckerDone:  make(chan bool),
	}

	// Initialize agents from config
	for _, agentCfg := range cfg.Agents {
		agent := &Agent{
			Config: agentCfg,
			status: StatusDisconnected,
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

// Connect establishes SSH connections to all agents
func (m *Manager) Connect() {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	log.Println("Starting to connect to all agents...")
	for _, agent := range m.agents {
		log.Printf("Initiating connection to agent: %s (%s:%d)", agent.Config.Name, agent.Config.Host, agent.Config.Port)
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

// connectAgent establishes an SSH connection to a single agent
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

	log.Printf("Connecting to agent %s (%s:%d)...", agent.Config.Name, agent.Config.Host, agent.Config.Port)
	var authMethods []ssh.AuthMethod

	// Use password if provided
	if agent.Config.Password != "" {
		log.Printf("Using password authentication for agent %s", agent.Config.Name)
		authMethods = append(authMethods, ssh.Password(agent.Config.Password))
	}

	// Use key file if provided
	if agent.Config.KeyFile != "" {
		log.Printf("Attempting to load private key from %s for agent %s", agent.Config.KeyFile, agent.Config.Name)
		key, err := loadPrivateKey(agent.Config.KeyFile)
		if err == nil {
			log.Printf("Successfully loaded private key for agent %s", agent.Config.Name)
			authMethods = append(authMethods, ssh.PublicKeys(key))
		} else {
			log.Printf("Failed to load private key for agent %s: %v", agent.Config.Name, err)
		}
	}

	if len(authMethods) == 0 {
		log.Printf("No authentication methods available for agent %s", agent.Config.Name)
		agent.setStatus(StatusDisconnected)
		return
	}

	config := &ssh.ClientConfig{
		User:            agent.Config.Username,
		Auth:            authMethods,
		Timeout:         time.Duration(m.config.Connection.Timeout) * time.Second,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // Note: In production, use proper host key verification
	}

	// Connect to the SSH server
	addr := fmt.Sprintf("%s:%d", agent.Config.Host, agent.Config.Port)
	log.Printf("Dialing SSH server at %s for agent %s", addr, agent.Config.Name)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		log.Printf("Failed to connect to agent %s: %v", agent.Config.Name, err)
		agent.setStatus(StatusDisconnected)
		return
	}

	agent.client = client
	agent.setStatus(StatusConnected)
	log.Printf("Successfully connected to agent %s (%s:%d)", agent.Config.Name, agent.Config.Host, agent.Config.Port)

	// Start keepalive goroutine
	go m.keepAlive(agent)
}

// loadPrivateKey loads a private key from a file
func loadPrivateKey(file string) (ssh.Signer, error) {
	privateKeyBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	// Try parsing the key as is first (unencrypted key)
	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		// If it fails, it might be an encrypted key
		if err.Error() == "ssh: this private key is passphrase protected" {
			return nil, fmt.Errorf("encrypted keys are not supported, please provide an unencrypted key")
		}
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return signer, nil
}

// keepAlive sends periodic keepalive messages to the agent
func (m *Manager) keepAlive(agent *Agent) {
	interval := time.Duration(m.config.Connection.Keepalive) * time.Second
	log.Printf("Starting keepalive routine for agent %s with interval %v", agent.Config.Name, interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		if agent.client == nil {
			log.Printf("Keepalive for agent %s stopped: client is nil", agent.Config.Name)
			return
		}

		_, _, err := agent.client.SendRequest("keepalive@yals", true, nil)
		if err != nil {
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

	session, err := agent.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	var stdout, stderr io.Reader
	stdout, err = session.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err = session.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	err = session.Start(command)
	if err != nil {
		return "", fmt.Errorf("failed to start command: %w", err)
	}

	stdoutBytes, err := io.ReadAll(stdout)
	if err != nil {
		return "", fmt.Errorf("failed to read stdout: %w", err)
	}

	stderrBytes, err := io.ReadAll(stderr)
	if err != nil {
		return "", fmt.Errorf("failed to read stderr: %w", err)
	}

	err = session.Wait()
	if err != nil {
		return "", fmt.Errorf("command execution failed: %w", err)
	}

	result := string(stdoutBytes)
	if len(stderrBytes) > 0 {
		result += "\n" + string(stderrBytes)
	}

	return result, nil
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

	// For mtr command, use non-streaming mode to avoid complexity
	if strings.Contains(command, "mtr") {
		output, err := m.ExecuteCommand(agentName, command)
		if err != nil {
			callback(fmt.Sprintf("Error: %v", err), true, false)
		} else {
			// Send the complete output at once
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					callback(line, false, false)
				}
			}
		}
		callback("", false, true) // Signal completion
		return nil
	}

	session, err := agent.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// For non-mtr commands, use standard streaming
	needsPTY := false

	if needsPTY {
		// Request a PTY for mtr interactive mode
		err = session.RequestPty("xterm-256color", 24, 80, ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		})
		if err != nil {
			return fmt.Errorf("failed to request PTY: %w", err)
		}

		// For PTY mode, use combined output
		output, outputErr := session.StdoutPipe()
		if outputErr != nil {
			return fmt.Errorf("failed to get output pipe: %w", outputErr)
		}

		startErr := session.Start(command)
		if startErr != nil {
			return fmt.Errorf("failed to start command: %w", startErr)
		}

		// Create channels for coordinating goroutines
		done := make(chan error, 1)
		outputDone := make(chan bool, 1)

		// Read PTY output with improved ANSI filtering for mtr
		go func() {
			defer func() { outputDone <- true }()
			reader := bufio.NewReader(output)
			var buffer []byte
			var lastSentLines []string

			for {
				char, readErr := reader.ReadByte()
				if readErr != nil {
					if readErr != io.EOF {
						callback(fmt.Sprintf("Error reading output: %v", readErr), true, false)
					}
					break
				}

				buffer = append(buffer, char)

				// Process buffer periodically or when we hit line endings
				if char == '\n' || char == '\r' || len(buffer) > 2048 {
					processed := m.processAnsiBuffer(buffer)
					if len(processed) > 0 {
						// Clean up the processed text
						processed = strings.ReplaceAll(processed, "\r\n", "\n")
						processed = strings.ReplaceAll(processed, "\r", "\n")

						// Split into lines
						lines := strings.Split(processed, "\n")
						var validLines []string

						for _, line := range lines {
							line = strings.TrimSpace(line)
							// Filter out empty lines and lines with only special characters
							if len(line) > 0 && !isOnlySpecialChars(line) {
								validLines = append(validLines, line)
							}
						}

						// Send new or updated lines
						for _, line := range validLines {
							// Check if this line is significantly different from what we sent before
							if !containsSimilarLine(lastSentLines, line) {
								callback(line, false, false)
								lastSentLines = append(lastSentLines, line)

								// Keep only recent lines to avoid memory growth
								if len(lastSentLines) > 50 {
									lastSentLines = lastSentLines[len(lastSentLines)-25:]
								}
							}
						}
					}
					buffer = buffer[:0] // Clear buffer
				}
			}

			// Process any remaining buffer content
			if len(buffer) > 0 {
				processed := m.processAnsiBuffer(buffer)
				if len(processed) > 0 {
					lines := strings.Split(processed, "\n")
					for _, line := range lines {
						line = strings.TrimSpace(line)
						if len(line) > 0 && !isOnlySpecialChars(line) {
							callback(line, false, false)
						}
					}
				}
			}
		}()

		// Wait for command to complete
		go func() {
			done <- session.Wait()
		}()

		// Wait for completion and all output to be read
		<-done
		<-outputDone

	} else {
		// Standard mode for other commands
		stdout, stdoutErr := session.StdoutPipe()
		if stdoutErr != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", stdoutErr)
		}

		stderr, stderrErr := session.StderrPipe()
		if stderrErr != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", stderrErr)
		}

		err = session.Start(command)
		if err != nil {
			return fmt.Errorf("failed to start command: %w", err)
		}

		// Create channels for coordinating goroutines
		done := make(chan error, 1)
		stdoutDone := make(chan bool, 1)
		stderrDone := make(chan bool, 1)

		// Start goroutines to read stdout and stderr
		go func() {
			defer func() { stdoutDone <- true }()
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				callback(scanner.Text(), false, false)
			}
			if scanErr := scanner.Err(); scanErr != nil {
				callback(fmt.Sprintf("Error reading stdout: %v", scanErr), true, false)
			}
		}()

		go func() {
			defer func() { stderrDone <- true }()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				callback(scanner.Text(), true, false)
			}
			if scanErr := scanner.Err(); scanErr != nil {
				callback(fmt.Sprintf("Error reading stderr: %v", scanErr), true, false)
			}
		}()

		// Wait for command to complete
		go func() {
			done <- session.Wait()
		}()

		// Wait for completion and all output to be read
		<-done
		<-stdoutDone
		<-stderrDone
	}

	callback("", false, true) // Signal completion

	if err != nil {
		return fmt.Errorf("command execution failed: %w", err)
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

	// For mtr command, use non-streaming mode to avoid complexity
	if strings.Contains(command, "mtr") {
		// 对于mtr命令，我们仍然使用原有的方式，但添加停止检查
		done := make(chan bool, 1)
		go func() {
			output, err := m.ExecuteCommand(agentName, command)
			if err != nil {
				callback(fmt.Sprintf("Error: %v", err), true, false, false)
			} else {
				// Send the complete output at once
				lines := strings.Split(output, "\n")
				for _, line := range lines {
					// 检查是否收到停止信号
					select {
					case <-stopChan:
						callback("", false, false, true) // 发送停止信号
						return
					default:
					}

					if strings.TrimSpace(line) != "" {
						callback(line, false, false, false)
					}
				}
			}
			done <- true
		}()

		// 等待完成或停止
		select {
		case <-done:
			callback("", false, true, false) // Signal completion
		case <-stopChan:
			callback("", false, false, true) // Signal stopped
		}
		return nil
	}

	session, err := agent.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	// For non-mtr commands, use standard streaming with stop support
	stdout, stdoutErr := session.StdoutPipe()
	if stdoutErr != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", stdoutErr)
	}

	stderr, stderrErr := session.StderrPipe()
	if stderrErr != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", stderrErr)
	}

	startErr := session.Start(command)
	if startErr != nil {
		return fmt.Errorf("failed to start command: %w", startErr)
	}

	// Create channels for coordinating goroutines
	done := make(chan error, 1)
	stdoutDone := make(chan bool, 1)
	stderrDone := make(chan bool, 1)
	stopped := make(chan bool, 1)

	// Start goroutines to read stdout and stderr
	go func() {
		defer func() { stdoutDone <- true }()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			select {
			case <-stopChan:
				stopped <- true
				return
			default:
				callback(scanner.Text(), false, false, false)
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			callback(fmt.Sprintf("Error reading stdout: %v", scanErr), true, false, false)
		}
	}()

	go func() {
		defer func() { stderrDone <- true }()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			select {
			case <-stopChan:
				stopped <- true
				return
			default:
				callback(scanner.Text(), true, false, false)
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			callback(fmt.Sprintf("Error reading stderr: %v", scanErr), true, false, false)
		}
	}()

	// Wait for command to complete
	go func() {
		done <- session.Wait()
	}()

	// Wait for completion, stop signal, or all output to be read
	select {
	case <-stopped:
		// 强制终止session
		session.Signal(ssh.SIGTERM)
		session.Close()
		callback("", false, false, true) // Signal stopped
		return nil
	case err = <-done:
		// 等待输出读取完成
		<-stdoutDone
		<-stderrDone
		callback("", false, true, false) // Signal completion
	}

	if err != nil {
		return fmt.Errorf("command execution failed: %w", err)
	}

	return nil
}

// processAnsiBuffer processes a buffer and removes ANSI escape sequences and control characters
func (m *Manager) processAnsiBuffer(buffer []byte) string {
	var result strings.Builder
	i := 0

	for i < len(buffer) {
		char := buffer[i]

		// Handle ANSI escape sequences
		if char == '\x1b' && i+1 < len(buffer) {
			i++ // Skip ESC
			next := buffer[i]

			if next == '[' {
				// CSI (Control Sequence Introducer) sequences: ESC[...
				i++ // Skip [
				// Skip parameters and intermediate characters
				for i < len(buffer) {
					c := buffer[i]
					// Parameters: 0-9, ;, :
					// Intermediate: space to /
					// Final: @ to ~
					if c >= '@' && c <= '~' {
						i++ // Skip the final character
						break
					}
					i++
				}
			} else if next == '(' || next == ')' {
				// Charset selection: ESC(B, ESC)0, etc.
				i++ // Skip ( or )
				if i < len(buffer) {
					i++ // Skip the charset character
				}
			} else if next == '=' || next == '>' {
				// Application keypad mode
				i++
			} else if next == 'D' || next == 'E' || next == 'H' || next == 'M' {
				// Single character sequences
				i++
			} else {
				// Unknown escape sequence, skip one character
				i++
			}
			continue
		}

		// Handle other control characters
		switch char {
		case '\r':
			// Carriage return - add newline for line updates
			result.WriteByte('\n')
		case '\n':
			result.WriteByte('\n')
		case '\t':
			result.WriteString("    ") // Convert tab to 4 spaces
		case '\b':
			// Backspace - ignore
		case '\x07':
			// Bell - ignore
		case '\x0c':
			// Form feed - ignore
		case '\x00':
			// Null - ignore
		default:
			// Only include printable ASCII characters and extended ASCII
			if char >= 32 && char <= 126 {
				result.WriteByte(char)
			} else if char >= 160 {
				// Extended ASCII characters
				result.WriteByte(char)
			}
			// Skip other control characters (1-31, 127-159)
		}
		i++
	}

	return result.String()
}

// isOnlySpecialChars checks if a line contains only special characters or whitespace
func isOnlySpecialChars(line string) bool {
	if len(line) == 0 {
		return true
	}

	for _, char := range line {
		// If we find any alphanumeric character or common punctuation, it's valid
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '.' || char == '-' ||
			char == ':' || char == '%' || char == '(' || char == ')' ||
			char == '[' || char == ']' || char == ' ' || char == '|' {
			return false
		}
	}
	return true
}

// containsSimilarLine checks if a similar line already exists in the slice
func containsSimilarLine(lines []string, newLine string) bool {
	for _, existingLine := range lines {
		// Check for exact match
		if existingLine == newLine {
			return true
		}

		// Check for similar content (for mtr updates)
		if len(existingLine) > 10 && len(newLine) > 10 {
			// Extract the host part (before the first space or tab)
			existingHost := strings.Fields(existingLine)
			newHost := strings.Fields(newLine)

			if len(existingHost) > 0 && len(newHost) > 0 {
				// If it's the same host, consider it similar
				if existingHost[0] == newHost[0] {
					return true
				}
			}
		}
	}
	return false
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
