package agent

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"

	"YALS_SSH/internal/config"

	"golang.org/x/crypto/ssh"
)

type Status int

const (
	StatusDisconnected Status = iota
	StatusConnected
)

type Agent struct {
	Config     config.Agent
	client     *ssh.Client
	status     Status
	lastCheck  time.Time
	statusLock sync.RWMutex
}

type Manager struct {
	agents     map[string]*Agent
	config     *config.Config
	agentsLock sync.RWMutex
}

func NewManager(cfg *config.Config) *Manager {
	manager := &Manager{
		agents: make(map[string]*Agent),
		config: cfg,
	}

	for _, agentCfg := range cfg.Agents {
		agent := &Agent{
			Config: agentCfg,
			status: StatusDisconnected,
		}
		manager.agents[agentCfg.Name] = agent
	}

	return manager
}

func (m *Manager) Connect() {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	log.Println("Starting to connect to all agents...")
	for _, agent := range m.agents {
		log.Printf("Initiating connection to agent: %s (%s:%d)", agent.Config.Name, agent.Config.Host, agent.Config.Port)
		go m.connectAgent(agent)
	}
}

func (m *Manager) connectAgent(agent *Agent) {
	log.Printf("Connecting to agent %s (%s:%d)...", agent.Config.Name, agent.Config.Host, agent.Config.Port)
	var authMethods []ssh.AuthMethod

	if agent.Config.Password != "" {
		log.Printf("Using password authentication for agent %s", agent.Config.Name)
		authMethods = append(authMethods, ssh.Password(agent.Config.Password))
	}

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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
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
	go m.keepAlive(agent)
}

func loadPrivateKey(file string) (ssh.Signer, error) {
	privateKeyBytes, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key file: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKeyBytes)
	if err != nil {
		if err.Error() == "ssh: this private key is passphrase protected" {
			return nil, fmt.Errorf("encrypted keys are not supported, please provide an unencrypted key")
		}
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	return signer, nil
}

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

func (m *Manager) reconnect(agent *Agent) {
	log.Printf("Starting reconnection attempts for agent %s", agent.Config.Name)
	retries := 0
	maxRetries := m.config.Connection.MaxRetries
	retryInterval := m.config.Connection.RetryInterval

	for retries < maxRetries {
		retries++
		log.Printf("Reconnection attempt %d/%d for agent %s", retries, maxRetries, agent.Config.Name)
		time.Sleep(time.Duration(retryInterval) * time.Second)
		m.connectAgent(agent)
		if agent.Status() == StatusConnected {
			log.Printf("Successfully reconnected to agent %s on attempt %d", agent.Config.Name, retries)
			return
		}
	}

	log.Printf("Failed to reconnect to agent %s after %d attempts", agent.Config.Name, maxRetries)
}

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

func (m *Manager) GetAgentGroups() []map[string]interface{} {
	m.agentsLock.RLock()
	defer m.agentsLock.RUnlock()

	groupMap := make(map[string][]map[string]interface{})
	
	for _, group := range m.config.Groups {
		groupMap[group.Name] = make([]map[string]interface{}, 0)
	}
	
	ungrouped := make([]map[string]interface{}, 0)
	
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
	
	result := make([]map[string]interface{}, 0)
	
	for _, group := range m.config.Groups {
		if len(groupMap[group.Name]) > 0 {
			result = append(result, map[string]interface{}{
				"name":   group.Name,
				"agents": groupMap[group.Name],
			})
		}
	}
	
	if len(ungrouped) > 0 {
		result = append(result, map[string]interface{}{
			"name":   "Ungrouped",
			"agents": ungrouped,
		})
	}
	
	return result
}

func (a *Agent) setStatus(status Status) {
	a.statusLock.Lock()
	defer a.statusLock.Unlock()
	a.status = status
	a.lastCheck = time.Now()
}

func (a *Agent) Status() Status {
	a.statusLock.RLock()
	defer a.statusLock.RUnlock()
	return a.status
}
