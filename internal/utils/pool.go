package utils

import (
	"bytes"
	"strings"
	"sync"
)

// StringBuilderPool provides a pool for reusing strings.Builder instances
var StringBuilderPool = sync.Pool{
	New: func() any {
		return &strings.Builder{}
	},
}

// BytesBufferPool provides a pool for reusing bytes.Buffer instances
var BytesBufferPool = sync.Pool{
	New: func() any {
		return &bytes.Buffer{}
	},
}

// StringSlicePool provides a pool for reusing string slices
var StringSlicePool = sync.Pool{
	New: func() any {
		return make([]string, 0, 32)
	},
}

// StringAnyMapPool provides a pool for reusing map[string]any instances
var StringAnyMapPool = sync.Pool{
	New: func() any {
		return make(map[string]any, 16)
	},
}

// GetStringBuilder retrieves a StringBuilder from the pool
func GetStringBuilder() *strings.Builder {
	return StringBuilderPool.Get().(*strings.Builder)
}

// PutStringBuilder returns a StringBuilder to the pool
func PutStringBuilder(sb *strings.Builder) {
	sb.Reset()
	StringBuilderPool.Put(sb)
}

// GetBytesBuffer retrieves a BytesBuffer from the pool
func GetBytesBuffer() *bytes.Buffer {
	return BytesBufferPool.Get().(*bytes.Buffer)
}

// PutBytesBuffer returns a BytesBuffer to the pool
func PutBytesBuffer(buf *bytes.Buffer) {
	buf.Reset()
	BytesBufferPool.Put(buf)
}

// GetStringSlice retrieves a StringSlice from the pool
func GetStringSlice() []string {
	return StringSlicePool.Get().([]string)
}

// PutStringSlice returns a StringSlice to the pool
func PutStringSlice(slice []string) {
	slice = slice[:0] // Clear the slice while keeping the capacity
	StringSlicePool.Put(&slice)
}

// GetStringAnyMap retrieves a map[string]any from the pool
func GetStringAnyMap() map[string]any {
	return StringAnyMapPool.Get().(map[string]any)
}

// PutStringAnyMap returns a map[string]any to the pool
func PutStringAnyMap(m map[string]any) {
	for k := range m {
		delete(m, k)
	}
	StringAnyMapPool.Put(m)
}
