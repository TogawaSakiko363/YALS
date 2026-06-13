package agent

import (
	"fmt"
	"time"

	"YALS/internal/proto"
)

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
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, "auto", nil, func(output string, isError bool, isComplete bool, isStopped bool) {
		callback(output, isError, isComplete)
	})
}

// ExecuteCommandStreamingWithStop executes a command on an agent with streaming output and stop support
func (m *Manager) ExecuteCommandStreamingWithStop(agentName, command string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	commandID := fmt.Sprintf("%s-%d", agentName, time.Now().UnixNano())
	return m.ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, "auto", stopChan, callback)
}

// ExecuteCommandStreamingWithStopAndID executes a command on an agent with streaming output, stop support and custom command ID
func (m *Manager) ExecuteCommandStreamingWithStopAndID(agentName, command, commandID, ipVersion string, stopChan <-chan bool, callback StreamingOutputCallbackWithStop) error {
	m.agentsLock.RLock()
	agent, exists := m.agents[agentName]
	m.agentsLock.RUnlock()

	if !exists || agent == nil {
		return fmt.Errorf("agent not found: %s", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent not connected: %s", agentName)
	}

	if agent.stream == nil {
		return fmt.Errorf("agent stream unavailable: %s", agentName)
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

	if err := m.reserveCommandSlot(agentName, commandName, cmdConfig.MaximumQueue, cmdConfig.UsePlugin); err != nil {
		return err
	}
	defer m.releaseCommandSlot(agentName, commandName)

	req := &proto.CommandMessage{
		Type:        "execute_command",
		CommandName: commandName,
		Target:      target,
		CommandID:   commandID,
		IPVersion:   ipVersion,
	}

	if err := agent.sendLocked(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	for {
		select {
		case <-stopChan:
			stopReq := &proto.CommandMessage{
				Type:      "stop_command",
				CommandID: commandID,
			}
			_ = agent.sendLocked(stopReq)
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

// ExecuteCommandWithID executes a command with a specific command ID
func (m *Manager) ExecuteCommandWithID(agentName, command, commandID, ipVersion string) error {
	agent := m.getAgent(agentName)
	if agent == nil {
		return fmt.Errorf("agent '%s' not found", agentName)
	}

	if agent.Status() != StatusConnected {
		return fmt.Errorf("agent '%s' is not connected", agentName)
	}

	if agent.stream == nil {
		return fmt.Errorf("agent '%s' has no active stream", agentName)
	}

	cmdName, target, err := parseCommand(command)
	if err != nil {
		return err
	}

	req := &proto.CommandMessage{
		Type:        "execute_command",
		CommandName: cmdName,
		Target:      target,
		CommandID:   commandID,
		IPVersion:   ipVersion,
	}

	if err := agent.sendLocked(req); err != nil {
		return fmt.Errorf("failed to send command: %w", err)
	}

	return nil
}

// StopCommand stops a running command on the specified agent
func (m *Manager) StopCommand(agentName, commandID string) error {
	agent := m.getAgent(agentName)
	if agent == nil {
		return fmt.Errorf("agent '%s' not found", agentName)
	}

	if agent.stream == nil {
		return fmt.Errorf("agent '%s' has no active stream", agentName)
	}

	req := &proto.CommandMessage{
		Type:      "stop_command",
		CommandID: commandID,
	}

	if err := agent.sendLocked(req); err != nil {
		return fmt.Errorf("failed to send stop command: %w", err)
	}

	return nil
}
