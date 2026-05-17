package codebuddy

import (
	"context"
	"encoding/json"
	"sync"
)

// mockTransport implements Transport for unit testing without a real CLI process.
type mockTransport struct {
	mu       sync.Mutex
	msgCh    chan RawMessage
	written  []string // records everything Write() received
	closed   bool
	sdkNames []string
}

func newMockTransport(bufSize int) *mockTransport {
	return &mockTransport{
		msgCh: make(chan RawMessage, bufSize),
	}
}

// injectRaw pushes a raw JSON map into the message channel.
func (m *mockTransport) injectRaw(data map[string]any) {
	raw, _ := json.Marshal(data)
	m.msgCh <- RawMessage{Data: data, Raw: raw}
}

// injectErr pushes an error into the message channel.
func (m *mockTransport) injectErr(err error) {
	m.msgCh <- RawMessage{Err: err}
}

// closeMessages closes the message channel (simulates CLI exit).
func (m *mockTransport) closeMessages() {
	close(m.msgCh)
}

func (m *mockTransport) Connect(_ context.Context) error { return nil }
func (m *mockTransport) ReadMessages() <-chan RawMessage  { return m.msgCh }
func (m *mockTransport) Write(_ context.Context, data string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.written = append(m.written, data)
	return nil
}
func (m *mockTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}
func (m *mockTransport) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}
func (m *mockTransport) SDKMCPServerNames() []string { return m.sdkNames }
func (m *mockTransport) HandleMCPMessageRequest(_ context.Context, _ string, _ map[string]any) {}
func (m *mockTransport) IsReady() bool                               { return true }
func (m *mockTransport) OnNotification(_ SubscriptionChannel, _ NotificationHandler)  {}
func (m *mockTransport) OffNotification(_ SubscriptionChannel, _ NotificationHandler) {}
func (m *mockTransport) SendControlRequestNoWait(_ context.Context, _ map[string]any) error {
	return nil
}

// writtenJSON returns the i-th Write() call parsed as a JSON map.
func (m *mockTransport) writtenJSON(i int) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i >= len(m.written) {
		return nil
	}
	var out map[string]any
	json.Unmarshal([]byte(m.written[i]), &out)
	return out
}
