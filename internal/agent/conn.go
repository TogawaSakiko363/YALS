package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"YALS/internal/config"
	"YALS/internal/logger"
	"YALS/internal/plugin"
	"YALS/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// buildTLSConfig constructs the TLS configuration used to dial the server. The
// agent verifies the server with standard CA validation (the host's system root
// store) against the connection hostname — exactly like a browser. For public
// deployments terminate TLS at a reverse proxy / CDN holding a CA-trusted
// certificate for the domain, and the agent will validate it.
func (c *Client) buildTLSConfig(hostname string) *tls.Config {
	return &tls.Config{
		ServerName: hostname,
		MinVersion: tls.VersionTLS12,
	}
}

// ConnectToServer connects to the server and handles the gRPC connection
func (c *Client) ConnectToServer() error {
	serverAddr := fmt.Sprintf("%s:%d", c.config.Server.Host, c.config.Server.Port)

	var opts []grpc.DialOption

	hostname := c.config.Server.Host
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	creds := credentials.NewTLS(c.buildTLSConfig(hostname))
	opts = append(opts, grpc.WithTransportCredentials(creds))
	opts = append(opts, grpc.WithDefaultCallOptions(grpc.CallContentSubtype("json")))
	opts = append(opts, grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                10 * time.Second,
		Timeout:             3 * time.Second,
		PermitWithoutStream: true,
	}))

	logger.Infof("Connecting to server at %s", serverAddr)
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer conn.Close()

	logger.Infof("Connected to server successfully")
	client := proto.NewAgentServiceClient(conn)

	handshakeCtx := metadata.AppendToOutgoingContext(context.Background(), "token", c.config.Server.Token)
	handshakeReq := &proto.HandshakeRequest{UUID: c.config.Server.UUID, Token: c.config.Server.Token}
	handshakeResp, err := client.Handshake(handshakeCtx, handshakeReq)
	if err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}
	if !handshakeResp.Success {
		return fmt.Errorf("handshake failed: %s", handshakeResp.Message)
	}
	if len(handshakeResp.Config) == 0 {
		return fmt.Errorf("handshake failed: missing runtime config")
	}

	var runtimeConfig config.AgentConfig
	if err := json.Unmarshal(handshakeResp.Config, &runtimeConfig); err != nil {
		return fmt.Errorf("failed to decode runtime config: %w", err)
	}
	runtimeConfig.Server.Token = c.config.Server.Token
	c.config = config.NormalizeAgentConfig(&runtimeConfig, nil)
	plugin.GetManager().SetConfig(c.config)

	logger.Infof("Handshake completed successfully for agent %s (%s)", c.config.Agent.Name, c.config.Server.UUID)
	logger.Infof("Loaded %d allowed commands from server", len(c.config.Commands))

	streamCtx := metadata.AppendToOutgoingContext(context.Background(), "agent-uuid", c.config.Server.UUID, "token", c.config.Server.Token)
	stream, err := client.StreamCommands(streamCtx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

	// Background reporters live for the lifetime of this connection; cancelling on
	// return stops them when the stream drops.
	monitorCtx, cancelMonitors := context.WithCancel(context.Background())
	defer cancelMonitors()
	go c.runMetricsReporter(monitorCtx, stream)
	go c.runProbeLoop(monitorCtx, stream)

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
		case "probe_config":
			var cfg proto.ProbeConfig
			if err := json.Unmarshal(msg.Data, &cfg); err != nil {
				logger.Warnf("Failed to decode probe config: %v", err)
			} else {
				logger.Infof("Received probe config: %d targets, interval %ds", len(cfg.Targets), cfg.IntervalSec)
				c.setProbeConfig(cfg)
			}
		case "disconnect":
			logger.Infof("Received disconnect request from server")
			return nil
		case "reload_config":
			logger.Infof("Received runtime config reload request from server")
			return nil
		default:
			logger.Warnf("Unknown message type: %s", msg.Type)
		}
	}

	logger.Infof("Disconnected from server")
	return nil
}
