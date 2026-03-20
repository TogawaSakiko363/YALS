package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"strings"
	"time"

	"YALS/internal/logger"
	"YALS/internal/proto"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// ConnectToServer connects to the server and handles the gRPC connection
func (c *Client) ConnectToServer() error {
	serverAddr := fmt.Sprintf("%s:%d", c.config.Server.Host, c.config.Server.Port)

	var opts []grpc.DialOption

	hostname := c.config.Server.Host
	if idx := strings.LastIndex(hostname, ":"); idx != -1 {
		hostname = hostname[:idx]
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         hostname,
	}
	creds := credentials.NewTLS(tlsConfig)
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

	ctx := metadata.AppendToOutgoingContext(context.Background(), "token", c.config.Server.Password)

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

	streamCtx := metadata.AppendToOutgoingContext(ctx,
		"agent-name", c.config.Agent.Name,
		"agent-group", c.config.Agent.Group)

	stream, err := client.StreamCommands(streamCtx)
	if err != nil {
		return fmt.Errorf("failed to create stream: %w", err)
	}

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
