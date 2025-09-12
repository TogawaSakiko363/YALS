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
}

// NewManager creates a new agent manager
func NewManager(cfg *config.Config) *Manager {
	manager := &Manager{
		agents: make(map[string]*Agent),
		config: cfg,
	}

	// Initialize agents from config
	for _, agentCfg := range cfg.Agents {
		agent := &Agent{
			Config: agentCfg,
			status: StatusDisconnected,
		}
		manager.agents[agentCfg.Name] = agent
	}

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

// connectAgent establishes an SSH connection to a single agent
func (m *Manager) connectAgent(agent *Agent) {
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
	retries := 0
	maxRetries := m.config.Connection.MaxRetries
	retryInterval := m.config.Connection.RetryInterval
	
	// If maxRetries is 0, retry indefinitely
	infiniteRetries := maxRetries == 0

	for infiniteRetries || retries < maxRetries {
		retries++
		if infiniteRetries {
			log.Printf("Reconnection attempt %d (infinite retries) for agent %s", retries, agent.Config.Name)
		} else {
			log.Printf("Reconnection attempt %d/%d for agent %s", retries, maxRetries, agent.Config.Name)
		}
		
		time.Sleep(time.Duration(retryInterval) * time.Second)
		m.connectAgent(agent)
		if agent.Status() == StatusConnected {
			log.Printf("Successfully reconnected to agent %s on attempt %d", agent.Config.Name, retries)
			return
		}
	}

	log.Printf("Failed to reconnect to agent %s after %d attempts", agent.Config.Name, maxRetries)
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
		output, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get output pipe: %w", err)
		}

		err = session.Start(command)
		if err != nil {
			return fmt.Errorf("failed to start command: %w", err)
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
				char, err := reader.ReadByte()
				if err != nil {
					if err != io.EOF {
						callback(fmt.Sprintf("Error reading output: %v", err), true, false)
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
		err = <-done
		<-outputDone
		
	} else {
		// Standard mode for other commands
		stdout, err := session.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to get stdout pipe: %w", err)
		}

		stderr, err := session.StderrPipe()
		if err != nil {
			return fmt.Errorf("failed to get stderr pipe: %w", err)
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
			if err := scanner.Err(); err != nil {
				callback(fmt.Sprintf("Error reading stdout: %v", err), true, false)
			}
		}()

		go func() {
			defer func() { stderrDone <- true }()
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				callback(scanner.Text(), true, false)
			}
			if err := scanner.Err(); err != nil {
				callback(fmt.Sprintf("Error reading stderr: %v", err), true, false)
			}
		}()

		// Wait for command to complete
		go func() {
			done <- session.Wait()
		}()

		// Wait for completion and all output to be read
		err = <-done
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
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	err = session.Start(command)
	if err != nil {
		return fmt.Errorf("failed to start command: %w", err)
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
		if err := scanner.Err(); err != nil {
			callback(fmt.Sprintf("Error reading stdout: %v", err), true, false, false)
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
		if err := scanner.Err(); err != nil {
			callback(fmt.Sprintf("Error reading stderr: %v", err), true, false, false)
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
					if (c >= '@' && c <= '~') {
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
			} else if char >= 160 && char <= 255 {
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
		result = append(result, map[string]interface{}{
			"name":     name,
			"status":   agent.Status(),
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
				agentInfo := map[string]interface{}{
					"name":     agentName,
					"status":   agent.Status(),
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
			agentInfo := map[string]interface{}{
				"name":     name,
				"status":   agent.Status(),
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
