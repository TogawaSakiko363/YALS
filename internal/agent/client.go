package agent

import (
	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/plugin"
	"YALS/internal/proto"
	"YALS/internal/validator"
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Shell operators that require bash execution
var shellOperators = []string{"|", "&&", "||", ">", "<", ";"}

// ActiveCommand represents an active command with its details
type ActiveCommand struct {
	Cmd         *exec.Cmd
	FullCommand string
}

// Client represents an agent client that connects to the server
type Client struct {
	config         *config.AgentConfig
	activeCommands map[string]*ActiveCommand
	commandsLock   sync.RWMutex
}

// CommandRequest represents a command request from the server
type CommandRequest struct {
	Type        string `json:"type"`
	CommandName string `json:"command_name"`
	Target      string `json:"target"`
	CommandID   string `json:"command_id"`
	IPVersion   string `json:"ip_version,omitempty"` // "auto", "ipv4", or "ipv6"
}

// CommandResponse represents a command response to the server
type CommandResponse struct {
	Type       string `json:"type"`
	CommandID  string `json:"command_id"`
	Output     string `json:"output"`
	Error      string `json:"error,omitempty"`
	IsComplete bool   `json:"is_complete"`
	IsError    bool   `json:"is_error"`
}

// NewClient creates a new agent client (deprecated, use NewClientWithConfig)
func NewClient(password string) *Client {
	agentConfig := &config.AgentConfig{}
	agentConfig.Server.Password = password
	return NewClientWithConfig(agentConfig)
}

// NewClientWithConfig creates a new agent client with configuration
func NewClientWithConfig(agentConfig *config.AgentConfig) *Client {
	// Set plugin manager configuration
	plugin.GetManager().SetConfig(agentConfig)

	return &Client{
		config:         agentConfig,
		activeCommands: make(map[string]*ActiveCommand),
	}
}

// ConnectToServer connects to the server and handles the gRPC connection
func (c *Client) ConnectToServer() error {
	// Set up gRPC dial options with TLS
	var opts []grpc.DialOption

	// Build server address
	serverAddr := fmt.Sprintf("%s:%d", c.config.Server.Host, c.config.Server.Port)

	// Extract hostname from server address for TLS ServerName
	// This ensures proper TLS handshake when using CDN or reverse proxy
	hostname := c.config.Server.Host
	// Remove port if present for ServerName
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	// Use TLS with insecure skip verify (for self-signed certificates)
	// ServerName is set to the actual hostname to match CDN/proxy certificate
	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         hostname,
	}
	creds := credentials.NewTLS(tlsConfig)
	opts = append(opts, grpc.WithTransportCredentials(creds))

	// Use JSON codec
	opts = append(opts, grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")))

	// Add keepalive options
	opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             3 * time.Second,
		PermitWithoutStream: true,
	}))

	logger.Infof("Connecting to server at %s", serverAddr)

	// Connect to server
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	logger.Infof("Connected to server successfully")

	// Create client
	client := proto.NewAgentServiceClient(conn)

	// Create context with token metadata
	ctx := metadata.AppendToOutgoingContext(context.Background(), "token", c.config.Server.Password)

	// Send handshake
	handshakeReq := &proto.HandshakeRequest{
		Name:  c.config.Agent.Name,
		Group: c.config.Agent.Group,
		Details: proto.AgentDetails{
			Location:    c.config.Agent.Details.Location,
			Datacenter:  c.config.Agent.Details.Datacenter,
			TestIP:      c.config.Agent.Details.TestIP,
			Description: c.config.Agent.Details.Description,
		},
		Commands: c.convertCommandsToProto(),
	}

	handshakeResp, err := client.Handshake(ctx, handshakeReq)
	if err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}

	if !handshakeResp.Success {
		return fmt.Errorf("handshake failed: %s", handshakeResp.Message)
	}

	logger.Infof("Handshake completed successfully")

	// Add agent info to metadata for stream
	streamCtx := metadata.AppendToOutgoingContext(ctx,
		"agent-name", c.config.Agent.Name,
		"agent-group", c.config.Agent.Group)

	// Start bidirectional streaming
	stream, err := client.StreamCommands(streamCtx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	// Handle incoming messages
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				logger.Info("Stream closed by server")
			} else {
				logger.Errorf("Stream error: %v", err)
			}
			break
		}

		switch msg.Type {
		case "execute_command":
			go c.executeCommandGRPC(stream, msg)
		case "stop_command":
			c.stopCommand(msg.CommandID)
		default:
			logger.Warnf("Unknown message type: %s", msg.Type)
		}
	}

	logger.Infof("Disconnected from server")
	return nil
}

// convertCommandsToProto converts config commands to proto format
func (c *Client) convertCommandsToProto() []proto.CommandInfo {
	commands := c.config.GetAvailableCommands()
	protoCommands := make([]proto.CommandInfo, len(commands))
	for i, cmd := range commands {
		protoCommands[i] = proto.CommandInfo{
			Name:         cmd.Name,
			IgnoreTarget: cmd.IgnoreTarget,
		}
	}
	return protoCommands
}

// executeCommandGRPC executes a command and streams the output via gRPC
func (c *Client) executeCommandGRPC(stream proto.AgentService_StreamCommandsClient, msg *proto.CommandMessage) {
	// Convert proto message to CommandRequest
	req := CommandRequest{
		Type:        msg.Type,
		CommandName: msg.CommandName,
		Target:      msg.Target,
		CommandID:   msg.CommandID,
		IPVersion:   msg.IPVersion,
	}

	// Validate and prepare command
	fullCommand, cmd, err := c.prepareCommand(req)
	if err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
		return
	}

	logger.Infof("Executing command: %s", req.CommandID)

	// Check if this is a plugin command
	if strings.HasPrefix(fullCommand, "plugin:") {
		c.executePluginCommandGRPC(stream, req, fullCommand)
		return
	}

	// Store and manage active command
	c.storeActiveCommand(req.CommandID, cmd, fullCommand)
	defer c.removeActiveCommand(req.CommandID)

	// Execute command with streaming output
	if err := c.runCommandWithStreamingGRPC(stream, req.CommandID, cmd); err != nil {
		c.sendErrorGRPC(stream, req.CommandID, err.Error())
		return
	}

	c.sendCompletionGRPC(stream, req.CommandID)
}

// prepareCommand validates and prepares a command for execution
func (c *Client) prepareCommand(req CommandRequest) (string, *exec.Cmd, error) {
	// Security check: Verify command is allowed
	if !c.config.IsCommandAllowed(req.CommandName) {
		logger.Warnf("SECURITY: Blocked unauthorized command '%s' from server", req.CommandName)
		return "", nil, fmt.Errorf("command '%s' is not allowed", req.CommandName)
	}

	// Get command configuration
	cmdConfig, exists := c.config.GetCommandConfig(req.CommandName)
	if !exists {
		return "", nil, fmt.Errorf("command configuration not found: %s", req.CommandName)
	}

	// Resolve domain name if target is a domain (for both plugin and traditional commands)
	resolvedTarget := req.Target
	if req.Target != "" && !cmdConfig.IgnoreTarget {
		resolvedTarget = c.resolveTargetIfNeeded(req.Target, req.IPVersion)
	}

	// Check if command uses a plugin
	if cmdConfig.UsePlugin != "" {
		return c.preparePluginCommand(cmdConfig, resolvedTarget)
	}

	// Get command template for traditional commands
	template := cmdConfig.Template
	if template == "" {
		return "", nil, fmt.Errorf("command template not found: %s", req.CommandName)
	}

	// Build full command with target parameter (only if not ignored)
	fullCommand := template
	if resolvedTarget != "" && !cmdConfig.IgnoreTarget {
		fullCommand = template + " " + resolvedTarget
	}

	// Create command based on complexity
	cmd := c.createCommand(fullCommand)
	if cmd == nil {
		return "", nil, fmt.Errorf("empty command")
	}

	return fullCommand, cmd, nil
}

// resolveTargetIfNeeded resolves domain name to IP if target is a domain
func (c *Client) resolveTargetIfNeeded(target, ipVersion string) string {
	// Extract host from target (may include port)
	host := target
	port := ""

	// Handle host:port format
	if strings.Contains(target, ":") {
		parts := strings.Split(target, ":")
		if len(parts) == 2 {
			host = parts[0]
			port = parts[1]
		}
	}

	// Check if host is a domain name
	inputType := validator.ValidateInput(host)
	if inputType == validator.Domain {
		// Convert string to IPVersion type
		var dnsIPVersion validator.IPVersion
		switch ipVersion {
		case "ipv4":
			dnsIPVersion = validator.IPVersionIPv4
		case "ipv6":
			dnsIPVersion = validator.IPVersionIPv6
		default:
			dnsIPVersion = validator.IPVersionAuto
		}

		// Resolve domain to IP with version preference
		ips, err := validator.ResolveDomainWithVersion(host, dnsIPVersion)
		if err != nil {
			logger.Warnf("Failed to resolve domain %s with IP version %s: %v, using original target", host, ipVersion, err)
			return target
		}

		if len(ips) > 0 {
			resolvedIP := ips[0].String()

			// Check if it's IPv6 and format accordingly
			parsedIP := ips[0]
			isIPv6 := parsedIP.To4() == nil

			// Reconstruct target with resolved IP
			if port != "" {
				if isIPv6 {
					// IPv6 with port needs brackets: [ipv6]:port
					return "[" + resolvedIP + "]:" + port
				}
				// IPv4 with port: ipv4:port
				return resolvedIP + ":" + port
			}

			// No port specified - return IP as-is (no brackets)
			return resolvedIP
		}
	}

	return target
}

// preparePluginCommand prepares a plugin-based command for execution
func (c *Client) preparePluginCommand(cmdConfig config.CommandTemplate, resolvedTarget string) (string, *exec.Cmd, error) {
	// Use resolved target (already converted from domain to IP if needed)
	fullCommand := fmt.Sprintf("plugin:%s %s", cmdConfig.UsePlugin, resolvedTarget)
	cmd := exec.Command("echo", "plugin_placeholder")
	return fullCommand, cmd, nil
}

// createCommand creates an exec.Cmd based on command complexity
func (c *Client) createCommand(fullCommand string) *exec.Cmd {
	// Check if command contains shell operators
	for _, op := range shellOperators {
		if strings.Contains(fullCommand, op) {
			return exec.Command("/bin/bash", "-c", fullCommand)
		}
	}

	// Simple command - parse normally
	parts := strings.Fields(fullCommand)
	if len(parts) == 0 {
		return nil
	}
	return exec.Command(parts[0], parts[1:]...)
}

// storeActiveCommand stores a command for potential stopping
func (c *Client) storeActiveCommand(commandID string, cmd *exec.Cmd, fullCommand string) {
	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()
	c.activeCommands[commandID] = &ActiveCommand{
		Cmd:         cmd,
		FullCommand: fullCommand,
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
	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Accumulate output with immediate updates
	var stdoutLines []string
	var stderrLines []string
	var stdoutMutex, stderrMutex sync.Mutex

	done := make(chan error, 1)
	outputDone := make(chan bool, 2)
	outputUpdate := make(chan bool, 100)

	// Read stdout and stderr concurrently with accumulation
	go c.accumulateOutputWithNotify(stdout, &stdoutLines, &stdoutMutex, outputDone, outputUpdate)
	go c.accumulateOutputWithNotify(stderr, &stderrLines, &stderrMutex, outputDone, outputUpdate)

	// Send immediate updates when output changes
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

	// Wait for command completion
	go func() {
		err := cmd.Wait()
		done <- err
		time.Sleep(200 * time.Millisecond)
		stdout.Close()
		stderr.Close()
	}()

	// Wait for completion and output processing
	cmdErr := <-done
	<-outputDone
	<-outputDone
	close(outputUpdate)

	// Send final output
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
		// Convert GBK to UTF-8 on Windows to fix encoding issues
		line = convertToUTF8(line)
		mutex.Lock()
		*lines = append(*lines, line)
		mutex.Unlock()

		// Notify immediately when new output is available
		select {
		case notify <- true:
		default:
			// Channel full, skip notification (update will happen soon anyway)
		}
	}

	if err := scanner.Err(); err != nil && !isClosedPipeError(err) {
		errorLine := fmt.Sprintf("Error reading output: %v", err)
		// Convert error message as well
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

// stopCommand stops a running command
func (c *Client) stopCommand(commandID string) {
	if plugin.StopPluginCommand(commandID) {
		logger.Infof("Stopping command: %s", commandID)
		return
	}

	c.commandsLock.Lock()
	defer c.commandsLock.Unlock()

	activeCmd, exists := c.activeCommands[commandID]
	if !exists || activeCmd.Cmd.Process == nil {
		logger.Warnf("No active command found to stop: %s", commandID)
		return
	}

	logger.Infof("Stopping command: %s", commandID)

	// Determine timeout based on command complexity
	timeout := 1 * time.Second
	if c.isComplexCommand(activeCmd.FullCommand) {
		timeout = 500 * time.Millisecond
	}

	// Try graceful termination first
	if err := activeCmd.Cmd.Process.Signal(os.Interrupt); err != nil {
		activeCmd.Cmd.Process.Kill()
		return
	}

	// Force kill after timeout
	go func() {
		time.Sleep(timeout)
		activeCmd.Cmd.Process.Kill()
	}()
}

// sendCommandResponseGRPC sends a command response to the server via gRPC
func (c *Client) sendCommandResponseGRPC(stream proto.AgentService_StreamCommandsClient, commandID, output, errorMsg string, isComplete, isError bool) {
	resp := &proto.CommandMessage{
		Type:       "command_output",
		CommandID:  commandID,
		Output:     output,
		Error:      errorMsg,
		IsComplete: isComplete,
		IsError:    isError,
	}

	if err := stream.Send(resp); err != nil {
		logger.Errorf("Failed to send command response: %v", err)
	}
}

// sendOutputGRPC sends command output to the server via gRPC
func (c *Client) sendOutputGRPC(stream proto.AgentService_StreamCommandsClient, commandID, output string, isError bool) {
	c.sendCommandResponseGRPC(stream, commandID, output, "", false, isError)
}

// sendErrorGRPC sends an error message to the server via gRPC
func (c *Client) sendErrorGRPC(stream proto.AgentService_StreamCommandsClient, commandID, errorMsg string) {
	c.sendCommandResponseGRPC(stream, commandID, errorMsg, errorMsg, true, true)
}

// sendCompletionGRPC sends a completion message to the server via gRPC
func (c *Client) sendCompletionGRPC(stream proto.AgentService_StreamCommandsClient, commandID string) {
	c.sendCommandResponseGRPC(stream, commandID, "", "", true, false)
}

// isClosedPipeError checks if the error is a closed pipe error
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
// It automatically detects and handles various encodings including GBK, Big5, Shift-JIS, EUC-KR, etc.
func convertToUTF8(input string) string {
	if input == "" {
		return input
	}

	// Try UTF-8 first (most common case)
	if isUTF8([]byte(input)) {
		return input
	}

	// List of encodings to try (common ones)
	encodings := []encoding.Encoding{
		// Chinese encodings
		simplifiedchinese.GBK,
		traditionalchinese.Big5,
		// Japanese encodings
		japanese.ShiftJIS,
		japanese.EUCJP,
		// Korean encodings
		korean.EUCKR,
		// Western European encodings
		charmap.Windows1252,
		charmap.ISO8859_1,
		// Unicode BOM variants
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

	// If all conversions fail, return original string
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
func (c *Client) executePluginCommandGRPC(stream proto.AgentService_StreamCommandsClient, req CommandRequest, fullCommand string) {
	parts := strings.SplitN(fullCommand, " ", 2)
	if len(parts) < 2 {
		c.sendErrorGRPC(stream, req.CommandID, "invalid plugin command format")
		return
	}

	pluginName := strings.TrimPrefix(parts[0], "plugin:")
	resolvedTarget := parts[1]

	dummyCmd := exec.Command("echo", "plugin_execution")

	c.storeActiveCommand(req.CommandID, dummyCmd, fullCommand)
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
