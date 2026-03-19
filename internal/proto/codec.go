package proto

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/encoding"
)

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return "json"
}

// CallOption to use JSON codec
func WithJSONCodec() interface{} {
	return struct{}{}
}

// Helper function to ensure message is JSON serializable
func EnsureJSONSerializable(v interface{}) error {
	_, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("message is not JSON serializable: %w", err)
	}
	return nil
}
