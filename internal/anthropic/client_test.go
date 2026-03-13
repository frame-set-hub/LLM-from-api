package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// mediaTypeFromExt
// ---------------------------------------------------------------------------

func TestMediaTypeFromExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".JPG", "image/jpeg"},
		{".png", "image/png"},
		{".gif", "image/gif"},
		{".webp", "image/webp"},
		{".bmp", "image/jpeg"}, // fallback
	}
	for _, tc := range tests {
		got := mediaTypeFromExt(tc.ext)
		if got != tc.want {
			t.Errorf("mediaTypeFromExt(%q) = %q, want %q", tc.ext, got, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ContentBlock helpers
// ---------------------------------------------------------------------------

func TestTextBlock(t *testing.T) {
	b := TextBlock("hello")
	if b.Type != "text" || b.Text != "hello" {
		t.Errorf("unexpected block: %+v", b)
	}
}

func TestImageBase64Block(t *testing.T) {
	b := ImageBase64Block([]byte("fake"), "image/png")
	if b.Type != "image" || b.Source == nil {
		t.Fatalf("unexpected block: %+v", b)
	}
	if b.Source.MediaType != "image/png" {
		t.Errorf("unexpected media type: %s", b.Source.MediaType)
	}
}

func TestMessage_ContentText(t *testing.T) {
	m := Message{
		Role: RoleUser,
		Content: []ContentBlock{
			TextBlock("hello "),
			{Type: "image", Source: &ImageSource{}},
			TextBlock("world"),
		},
	}
	got := m.ContentText()
	if got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Client.Stream — happy path with mock SSE server
// ---------------------------------------------------------------------------

func newTestSSEServer(events []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, ev := range events {
			fmt.Fprint(w, ev)
		}
	}))
}

func TestClientStream_HappyPath(t *testing.T) {
	events := []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" World\"}}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n",
	}

	srv := newTestSSEServer(events)
	defer srv.Close()

	client := New(Config{
		BaseURL:          srv.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		AnthropicVersion: "2023-06-01",
		MaxTokens:        100,
		Timeout:          5 * time.Second,
	})

	var tokens []string
	text, usage, err := client.Stream(
		context.Background(),
		[]Message{NewTextMessage(RoleUser, "hi")},
		"system",
		func(tok string) { tokens = append(tokens, tok) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", text)
	}
	if len(tokens) != 2 {
		t.Errorf("expected 2 token callbacks, got %d", len(tokens))
	}
	if usage.InputTokens != 10 {
		t.Errorf("expected 10 input tokens, got %d", usage.InputTokens)
	}
	if usage.OutputTokens != 5 {
		t.Errorf("expected 5 output tokens, got %d", usage.OutputTokens)
	}
}

func TestClientStream_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"type":"invalid_request","message":"bad input"}}`)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL:          srv.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		AnthropicVersion: "2023-06-01",
		MaxTokens:        100,
		Timeout:          5 * time.Second,
	})

	_, _, err := client.Stream(
		context.Background(),
		[]Message{NewTextMessage(RoleUser, "hi")},
		"system",
		nil,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad input") {
		t.Errorf("expected error message to contain 'bad input', got: %v", err)
	}
}

func TestClientStream_StreamError(t *testing.T) {
	events := []string{
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded\",\"message\":\"server busy\"}}\n\n",
	}
	srv := newTestSSEServer(events)
	defer srv.Close()

	client := New(Config{
		BaseURL:          srv.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		AnthropicVersion: "2023-06-01",
		MaxTokens:        100,
		Timeout:          5 * time.Second,
	})

	_, _, err := client.Stream(
		context.Background(),
		[]Message{NewTextMessage(RoleUser, "hi")},
		"system",
		nil,
	)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "server busy") {
		t.Errorf("expected 'server busy' in error, got: %v", err)
	}
}

func TestClientStream_ContextCancelled(t *testing.T) {
	// Server that blocks forever
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL:          srv.URL,
		APIKey:           "test-key",
		Model:            "test-model",
		AnthropicVersion: "2023-06-01",
		MaxTokens:        100,
		Timeout:          10 * time.Second,
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := client.Stream(ctx, []Message{NewTextMessage(RoleUser, "hi")}, "system", nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Request headers
// ---------------------------------------------------------------------------

func TestClientStream_SendsCorrectHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "my-key" {
			t.Errorf("expected x-api-key=my-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("unexpected anthropic-version: %s", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := New(Config{
		BaseURL:          srv.URL,
		APIKey:           "my-key",
		Model:            "m",
		AnthropicVersion: "2023-06-01",
		MaxTokens:        10,
		Timeout:          5 * time.Second,
	})

	client.Stream(context.Background(), []Message{NewTextMessage(RoleUser, "hi")}, "", nil)
}
