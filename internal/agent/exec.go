package agent

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/plugin"
	"YALS/internal/proto"
	"YALS/internal/validator"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// convertCommandsToProto converts config commands to proto format
func (c *Client) convertCommandsToProto() []proto.CommandInfo {
	commands := c.config.GetAvailableCommands()
	protoCommands := make([]proto.CommandInfo, len(commands))
	for i, cmd := range commands {
		protoCommands[i] = proto.CommandInfo{
			Name:         cmd.Name,
			IgnoreTarget: cmd.IgnoreTarget,
			MaximumQueue: cmd.MaximumQueue,
		}
	}
	return protoCommands
}

// executeCommandGRPC executes a command and streams the output via gRPC
func (c *Client) executeCommandGRPC(stream proto.AgentService_StreamCommandsClient, msg *proto.CommandMessage) {
	req := CommandRequest{
		Type:        msg.Type,
		CommandName: msg.CommandName,
		Target:      msg.Target,
		CommandID:   msg.CommandID,
		IPVersion:   msg.IPVersion,
	}

	fullCommand, cmd, cmdConfig, err := c.prepareCommand(req)
	if err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
		return
	}

	if err := c.checkCommandQueueLimit(req.CommandName, cmdConfig); err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
		return
	}

	logger.Infof("Executing command: %s", req.CommandID)

	if strings.HasPrefix(fullCommand, "plugin:") {
		c.executePluginCommandGRPC(stream, req, fullCommand, cmdConfig)
		return
	}

	c.storeActiveCommand(req.CommandID, cmd, fullCommand, req.CommandName)
	defer c.removeActiveCommand(req.CommandID)

	if err := c.runCommandWithStreamingGRPC(stream, req.CommandID, cmd); err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
		return
	}

	c.sendCompletionGRPC(stream, req.CommandID)
}

// prepareCommand validates and prepares a command for execution
func (c *Client) prepareCommand(req CommandRequest) (string, *exec.Cmd, config.CommandTemplate, error) {
	if !c.config.IsCommandAllowed(req.CommandName) {
		logger.Warnf("SECURITY: Blocked unauthorized command '%s' from server", req.CommandName)
		return "", nil, config.CommandTemplate{}, fmt.Errorf("command '%s' is not allowed", req.CommandName)
	}

	cmdConfig, exists := c.config.GetCommandConfig(req.CommandName)
	if !exists {
		return "", nil, config.CommandTemplate{}, fmt.Errorf("command configuration not found: %s", req.CommandName)
	}

	resolvedTarget := req.Target
	if req.Target != "" && !cmdConfig.IgnoreTarget {
		resolvedTarget = c.resolveTargetIfNeeded(req.Target, req.IPVersion)
	}

	if cmdConfig.UsePlugin != "" {
		fullCommand, cmd, err := c.preparePluginCommand(cmdConfig, resolvedTarget)
		return fullCommand, cmd, cmdConfig, err
	}

	template := cmdConfig.Template
	if template == "" {
		return "", nil, config.CommandTemplate{}, fmt.Errorf("command template not found: %s", req.CommandName)
	}

	fullCommand := template
	if resolvedTarget != "" && !cmdConfig.IgnoreTarget {
		fullCommand = template + " " + resolvedTarget
	}

	cmd := c.createCommand(fullCommand)
	if cmd == nil {
		return "", nil, config.CommandTemplate{}, fmt.Errorf("empty command")
	}

	return fullCommand, cmd, cmdConfig, nil
}

func (c *Client) checkCommandQueueLimit(commandName string, cmdConfig config.CommandTemplate) error {
	maximumQueue := cmdConfig.MaximumQueue
	if cmdConfig.UsePlugin != "" {
		if hasOverride, overrideQueue := plugin.GetPluginMaximumQueue(cmdConfig.UsePlugin); hasOverride {
			maximumQueue = overrideQueue
		}
	}

	if maximumQueue <= 0 {
		return nil
	}

	c.commandsLock.RLock()
	defer c.commandsLock.RUnlock()
	count := 0
	for _, active := range c.activeCommands {
		if active != nil && active.CommandName == commandName {
			count++
		}
	}
	if count >= maximumQueue {
		return fmt.Errorf("execution limit reached for command '%s' (%d/%d)", commandName, count, maximumQueue)
	}
	return nil
}

// resolveTargetIfNeeded resolves domain name to IP if target is a domain
func (c *Client) resolveTargetIfNeeded(target, ipVersion string) string {
	host := target
	port := ""

	if strings.Contains(target, ":") {
		parts := strings.Split(target, ":")
		if len(parts) == 2 {
			host = parts[0]
			port = parts[1]
		}
	}

	inputType := validator.ValidateInput(host)
	if inputType == validator.Domain {
		var dnsIPVersion validator.IPVersion
		switch ipVersion {
		case "ipv4":
			dnsIPVersion = validator.IPVersionIPv4
		case "ipv6":
			dnsIPVersion = validator.IPVersionIPv6
		default:
			dnsIPVersion = validator.IPVersionAuto
		}

		ips, err := validator.ResolveDomainWithVersion(host, dnsIPVersion)
		if err != nil {
			logger.Warnf("Failed to resolve domain %s with IP version %s: %v, using original target", host, ipVersion, err)
			return target
		}

		if len(ips) > 0 {
			resolvedIP := ips[0].String()

			parsedIP := ips[0]
			isIPv6 := parsedIP.To4() == nil

			if port != "" {
				if isIPv6 {
					return "[" + resolvedIP + "]:" + port
				}
				return resolvedIP + ":" + port
			}

			return resolvedIP
		}
	}

	return target
}

// preparePluginCommand prepares a plugin-based command for execution
func (c *Client) preparePluginCommand(cmdConfig config.CommandTemplate, resolvedTarget string) (string, *exec.Cmd, error) {
	fullCommand := fmt.Sprintf("plugin:%s %s", cmdConfig.UsePlugin, resolvedTarget)
	cmd := exec.Command("echo", "plugin_placeholder")
	return fullCommand, cmd, nil
}

// createCommand creates an exec.Cmd based on command complexity
func (c *Client) createCommand(fullCommand string) *exec.Cmd {
	for _, op := range shellOperators {
		if strings.Contains(fullCommand, op) {
			return exec.Command("/bin/bash", "-c", fullCommand)
		}
	}

	parts := strings.Fields(fullCommand)
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], parts[1:]...)
}

// storeActiveCommand stores a command for potential stopping
func (c *Client) storeActiveCommand(commandID string, cmd *exec.Cmd, fullCommand string, commandName string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()
	c.activeCommands[commandID] = &ActiveCommand{
		Cmd:         cmd,
		FullCommand: fullCommand,
		CommandName: commandName,
	}
}

// removeActiveCommand removes a command from active commands
func (c *Client) removeActiveCommand(commandID string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()
	delete(c.activeCommands, commandID)
}

// runCommandWithStreamingGRPC executes a command and streams its output via gRPC
func (c *Client) runCommandWithStreamingGRPC(stream proto.AgentService_StreamCommandsClient, commandID string, cmd *exec.Cmd) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	var stdoutLines []string
	var stderrLines []string
	var stdoutMutex, stderrMutex sync.Mutex

	done := make(chan error, 1)
	outputDone := make(chan bool, 2)
	outputUpdate := make(chan bool, 100)

	go c.accumulateOutputWithNotify(stdout, &stdoutLines, &stdoutMutex, outputDone, outputUpdate)
	go c.accumulateOutputWithNotify(stderr, &stderrLines, &stderrMutex, outputDone, outputUpdate)

	go func() {
		for range outputUpdate {
			stdoutMutex.Lock()
			stderrMutex.Lock()

			var allLines []string
			allLines = append(allLines, stdoutLines...)
			allLines = append(allLines, stderrLines...)

			if len(allLines) > 0 {
				output := strings.Join(allLines, "\n")
				c.sendOutputGRPC(stream, commandID, output, false)
			}

			stderrMutex.Unlock()
			stdoutMutex.Unlock()
		}
	}()

	go func() {
		err := cmd.Wait()
		done <- err
		time.Sleep(200 * time.Millisecond)
		stdout.Close()
		stderr.Close()
	}()

	cmdErr := <-done
	<-outputDone
	<-outputDone
	close(outputUpdate)

	stdoutMutex.Lock()
	stderrMutex.Lock()
	var allLines []string
	allLines = append(allLines, stdoutLines...)
	allLines = append(allLines, stderrLines...)
	stderrMutex.Unlock()
	stdoutMutex.Unlock()

	if len(allLines) > 0 {
		finalOutput := strings.Join(allLines, "\n")
		if cmdErr != nil {
			finalOutput += fmt.Sprintf("\nCommand failed: %v", cmdErr)
		}
		c.sendOutputGRPC(stream, commandID, finalOutput, cmdErr != nil)
	} else if cmdErr != nil {
		c.sendOutputGRPC(stream, commandID, fmt.Sprintf("Command failed: %v", cmdErr), true)
	}

	time.Sleep(100 * time.Millisecond)

	return nil
}

// accumulateOutputWithNotify reads from a pipe, accumulates output lines, and notifies on updates
func (c *Client) accumulateOutputWithNotify(pipe interface{ Read([]byte) (int, error) }, lines *[]string, mutex *sync.Mutex, done chan<- bool, notify chan<- bool) {
	defer func() { done <- true }()

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		line := scanner.Text()
		line = convertToUTF8(line)
		mutex.Lock()
		*lines = append(*lines, line)
		mutex.Unlock()

		select {
		case notify <- true:
		default:
		}
	}

	if err := scanner.Err(); err != nil && !isClosedPipeError(err) {
		errorLine := fmt.Sprintf("Error reading output: %v", err)
		errorLine = convertToUTF8(errorLine)
		mutex.Lock()
		*lines = append(*lines, errorLine)
		mutex.Unlock()

		select {
		case notify <- true:
		default:
		}
	}
}

// isComplexCommand checks if a command needs shell execution
func (c *Client) isComplexCommand(fullCommand string) bool {
	complexOperators := shellOperators[:3]
	for _, op := range complexOperators {
		if strings.Contains(fullCommand, op) {
			return true
		}
	}
	return false
}

// sendOutputGRPC sends command output via gRPC stream
func (c *Client) sendOutputGRPC(stream proto.AgentService_StreamCommandsClient, commandID, output string, isError bool) {
	msg := &proto.CommandMessage{
		Type:      "command_output",
		CommandID: commandID,
		Output:    output,
		IsError:   isError,
	}
	if err := stream.Send(msg); err != nil {
		logger.Errorf("Failed to send output: %v", err)
	}
}

// sendErrorGRPC sends command error via gRPC stream
func (c *Client) sendErrorGRPC(stream proto.AgentService_StreamCommandsClient, commandID, errorMsg string) {
	msg := &proto.CommandMessage{
		Type:      "command_output",
		CommandID: commandID,
		Error:     errorMsg,
		IsError:   true,
	}
	if err := stream.Send(msg); err != nil {
		logger.Errorf("Failed to send error: %v", err)
	}
}

// sendCompletionGRPC sends command completion signal via gRPC stream
func (c *Client) sendCompletionGRPC(stream proto.AgentService_StreamCommandsClient, commandID string) {
	msg := &proto.CommandMessage{
		Type:       "command_output",
		CommandID:  commandID,
		IsComplete: true,
	}
	if err := stream.Send(msg); err != nil {
		logger.Errorf("Failed to send completion: %v", err)
	}
}

// stopCommand stops a running command
func (c *Client) stopCommand(commandID string) {
	c.commandsLock.RLock()
	activeCmd, exists := c.activeCommands[commandID]
	c.commandsLock.RUnlock()

	if plugin.StopPluginCommand(commandID) {
		logger.Infof("Stopping plugin command: %s", commandID)
		c.removeActiveCommand(commandID)
		return
	}

	if !exists || activeCmd == nil || activeCmd.Cmd == nil {
		logger.Warnf("No active command found to stop: %s", commandID)
		return
	}

	logger.Infof("Stopping command: %s", commandID)
	if activeCmd.Cmd.Process != nil {
		if err := activeCmd.Cmd.Process.Kill(); err != nil {
			logger.Warnf("Failed to kill command %s: %v", commandID, err)
		}
	}
	c.removeActiveCommand(commandID)
}

// isClosedPipeError checks if an error is related to closed pipe/file
func isClosedPipeError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "file already closed") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "use of closed file") ||
		err == os.ErrClosed
}

// convertToUTF8 converts the input string from any encoding to UTF-8
func convertToUTF8(input string) string {
	if input == "" {
		return input
	}

	if isUTF8([]byte(input)) {
		return input
	}

	encodings := []encoding.Encoding{
		simplifiedchinese.GBK,
		traditionalchinese.Big5,
		japanese.ShiftJIS,
		japanese.EUCJP,
		korean.EUCKR,
		charmap.Windows1252,
		charmap.ISO8859_1,
		unicode.UTF16(unicode.LittleEndian, unicode.UseBOM),
		unicode.UTF16(unicode.BigEndian, unicode.UseBOM),
	}

	for _, enc := range encodings {
		decoder := enc.NewDecoder()
		output, _, err := transform.String(decoder, input)
		if err == nil && isUTF8([]byte(output)) {
			return output
		}
	}

	logger.Debugf("Failed to convert encoding, using original string")
	return input
}

// isUTF8 checks if the given bytes are valid UTF-8
func isUTF8(data []byte) bool {
	for i := 0; i < len(data); {
		if data[i] < 0x80 {
			i++
			continue
		}
		if data[i] < 0xC2 {
			return false
		}
		if data[i] < 0xE0 {
			if i+1 >= len(data) {
				return false
			}
			if data[i+1] < 0x80 || data[i+1] >= 0xC0 {
				return false
			}
			i += 2
			continue
		}
		if data[i] < 0xF0 {
			if i+2 >= len(data) {
				return false
			}
			if data[i+1] < 0x80 || data[i+1] >= 0xC0 || data[i+2] < 0x80 || data[i+2] >= 0xC0 {
				return false
			}
			i += 3
			continue
		}
		if data[i] < 0xF8 {
			if i+3 >= len(data) {
				return false
			}
			if data[i+1] < 0x80 || data[i+1] >= 0xC0 || data[i+2] < 0x80 || data[i+2] >= 0xC0 || data[i+3] < 0x80 || data[i+3] >= 0xC0 {
				return false
			}
			i += 4
			continue
		}
		return false
	}
	return true
}

// executePluginCommandGRPC executes a plugin-based command via gRPC
func (c *Client) executePluginCommandGRPC(stream proto.AgentService_StreamCommandsClient, req CommandRequest, fullCommand string, cmdConfig config.CommandTemplate) {
	parts := strings.SplitN(fullCommand, " ", 2)
	if len(parts) < 2 {
		c.sendErrorGRPC(stream, req.CommandID, "invalid plugin command format")
		return
	}

	pluginName := strings.TrimPrefix(parts[0], "plugin:")
	resolvedTarget := parts[1]

	dummyCmd := exec.Command("echo", "plugin_execution")
	c.storeActiveCommand(req.CommandID, dummyCmd, fullCommand, req.CommandName)
	defer c.removeActiveCommand(req.CommandID)

	err := plugin.ExecutePluginCommand(pluginName, resolvedTarget, req.CommandID, func(output string, isError bool, isComplete bool) {
		if isError {
			c.sendErrorGRPC(stream, req.CommandID, output)
		} else if isComplete {
			c.sendOutputGRPC(stream, req.CommandID, output, false)
			c.sendCompletionGRPC(stream, req.CommandID)
		} else {
			c.sendOutputGRPC(stream, req.CommandID, output, false)
		}
	})

	if err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
	}
}
