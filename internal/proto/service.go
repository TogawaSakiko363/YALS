// Package proto defines the gRPC service interface
package proto

import (
	"context"
	"encoding/json"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AgentServiceServer is the server API for AgentService
type AgentServiceServer interface {
	Handshake(context.Context, *HandshakeRequest) (*HandshakeResponse, error)
	StreamCommands(AgentService_StreamCommandsServer) error
}

// AgentServiceClient is the client API for AgentService
type AgentServiceClient interface {
	Handshake(ctx context.Context, in *HandshakeRequest, opts ...grpc.CallOption) (*HandshakeResponse, error)
	StreamCommands(ctx context.Context, opts ...grpc.CallOption) (AgentService_StreamCommandsClient, error)
}

// AgentService_StreamCommandsServer is the server stream for StreamCommands
type AgentService_StreamCommandsServer interface {
	Send(*CommandMessage) error
	Recv() (*CommandMessage, error)
	grpc.ServerStream
}

// AgentService_StreamCommandsClient is the client stream for StreamCommands
type AgentService_StreamCommandsClient interface {
	Send(*CommandMessage) error
	Recv() (*CommandMessage, error)
	grpc.ClientStream
}

type agentServiceClient struct {
	cc grpc.ClientConnInterface
}

// NewAgentServiceClient creates a new AgentServiceClient
func NewAgentServiceClient(cc grpc.ClientConnInterface) AgentServiceClient {
	return &agentServiceClient{cc}
}

func (c *agentServiceClient) Handshake(ctx context.Context, in *HandshakeRequest, opts ...grpc.CallOption) (*HandshakeResponse, error) {
	out := new(HandshakeResponse)
	err := c.cc.Invoke(ctx, "/proto.AgentService/Handshake", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *agentServiceClient) StreamCommands(ctx context.Context, opts ...grpc.CallOption) (AgentService_StreamCommandsClient, error) {
	stream, err := c.cc.NewStream(ctx, &_AgentService_serviceDesc.Streams[0], "/proto.AgentService/StreamCommands", opts...)
	if err != nil {
		return nil, err
	}
	x := &agentServiceStreamCommandsClient{stream}
	return x, nil
}

type agentServiceStreamCommandsClient struct {
	grpc.ClientStream
}

func (x *agentServiceStreamCommandsClient) Send(m *CommandMessage) error {
	return x.ClientStream.SendMsg(m)
}

func (x *agentServiceStreamCommandsClient) Recv() (*CommandMessage, error) {
	m := new(CommandMessage)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RegisterAgentServiceServer registers the service
func RegisterAgentServiceServer(s grpc.ServiceRegistrar, srv AgentServiceServer) {
	s.RegisterService(&_AgentService_serviceDesc, srv)
}

func _AgentService_Handshake_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HandshakeRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AgentServiceServer).Handshake(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/proto.AgentService/Handshake",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AgentServiceServer).Handshake(ctx, req.(*HandshakeRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _AgentService_StreamCommands_Handler(srv interface{}, stream grpc.ServerStream) error {
	return srv.(AgentServiceServer).StreamCommands(&agentServiceStreamCommandsServer{stream})
}

type agentServiceStreamCommandsServer struct {
	grpc.ServerStream
}

func (x *agentServiceStreamCommandsServer) Send(m *CommandMessage) error {
	return x.ServerStream.SendMsg(m)
}

func (x *agentServiceStreamCommandsServer) Recv() (*CommandMessage, error) {
	m := new(CommandMessage)
	if err := x.ServerStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

var _AgentService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "proto.AgentService",
	HandlerType: (*AgentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Handshake",
			Handler:    _AgentService_Handshake_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "StreamCommands",
			Handler:       _AgentService_StreamCommands_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
	Metadata: "agent.proto",
}

// JSONCodec is a Codec implementation with json
type JSONCodec struct{}

func (JSONCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (JSONCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (JSONCodec) Name() string {
	return "json"
}

func (JSONCodec) String() string {
	return "json"
}

// ValidateToken validates the token from metadata
func ValidateToken(ctx context.Context, expectedToken string) error {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return status.Errorf(codes.Unauthenticated, "missing metadata")
	}

	tokens := md.Get("token")
	if len(tokens) == 0 {
		return status.Errorf(codes.Unauthenticated, "missing token")
	}

	if tokens[0] != expectedToken {
		return status.Errorf(codes.Unauthenticated, "invalid token")
	}

	return nil
}

// RecvWithEOF receives a message and returns io.EOF on stream end
func RecvWithEOF(stream interface {
	RecvMsg(interface{}) error
}, m interface{}) error {
	err := stream.RecvMsg(m)
	if err != nil {
		if err == io.EOF {
			return io.EOF
		}
		return err
	}
	return nil
}
