// Package anthropic provides a lightweight HTTP client for the Anthropic
// Messages API (or any compatible gateway such as float16.cloud).
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Content block types (Anthropic multi-modal message format)
// ---------------------------------------------------------------------------

// ContentBlock is one element inside a message's content array.
// It is either a text block or an image block.
type ContentBlock struct {
	Type   string       `json:"type"`             // "text" or "image"
	Text   string       `json:"text,omitempty"`   // used when Type=="text"
	Source *ImageSource `json:"source,omitempty"` // used when Type=="image"
}

// ImageSource describes how the image is provided.
type ImageSource struct {
	Type      string `json:"type"`           // "base64" or "url"
	MediaType string `json:"media_type"`     // "image/jpeg", "image/png", etc.
	Data      string `json:"data,omitempty"` // base64-encoded bytes (type=="base64")
	URL       string `json:"url,omitempty"`  // image URL            (type=="url")
}

// TextBlock returns a ContentBlock carrying plain text.
func TextBlock(text string) ContentBlock {
	return ContentBlock{Type: "text", Text: text}
}

// ImageBase64Block returns a ContentBlock for a locally-read image file.
// mediaType should be e.g. "image/jpeg" or "image/png".
func ImageBase64Block(data []byte, mediaType string) ContentBlock {
	return ContentBlock{
		Type: "image",
		Source: &ImageSource{
			Type:      "base64",
			MediaType: mediaType,
			Data:      base64.StdEncoding.EncodeToString(data),
		},
	}
}

// ---------------------------------------------------------------------------
// Role / Message
// ---------------------------------------------------------------------------

// Role represents the author of a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is a single turn in the conversation.
// Content is a slice so it can hold multiple blocks (text + images).
type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

// NewTextMessage creates a simple text-only Message.
func NewTextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentBlock{TextBlock(text)}}
}

// NewImageMessage creates a Message with an image block and an optional text caption.
// imagePath is a path to a local file. caption may be empty.
func NewImageMessage(role Role, imagePath, caption string) (Message, error) {
	data, err := os.ReadFile(imagePath)
	if err != nil {
		return Message{}, fmt.Errorf("read image %q: %w", imagePath, err)
	}
	mediaType := mediaTypeFromExt(filepath.Ext(imagePath))
	blocks := []ContentBlock{ImageBase64Block(data, mediaType)}
	if caption != "" {
		blocks = append(blocks, TextBlock(caption))
	}
	return Message{Role: role, Content: blocks}, nil
}

// mediaTypeFromExt maps a file extension to an Anthropic-accepted media type.
func mediaTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg" // safe fallback
	}
}

// ContentText returns the concatenated text from all text blocks in a message.
func (m Message) ContentText() string {
	var sb strings.Builder
	for _, b := range m.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Request / Response payloads
// ---------------------------------------------------------------------------

type requestPayload struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Stream    bool      `json:"stream,omitempty"`
}

// SSE event payloads (minimal subset we parse)
type streamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta *struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta,omitempty"`
	Error *apiError `json:"error,omitempty"`
}

type apiError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *apiError) Error() string {
	return fmt.Sprintf("api error [%s]: %s", e.Type, e.Message)
}

// ---------------------------------------------------------------------------
// Config & Client
// ---------------------------------------------------------------------------

// Config holds the settings for the API client.
type Config struct {
	BaseURL          string
	APIKey           string
	Model            string
	AnthropicVersion string
	MaxTokens        int
	Timeout          time.Duration
}

// DefaultConfig returns a Config pre-filled with sensible defaults.
func DefaultConfig(baseURL, apiKey, model string) Config {
	return Config{
		BaseURL:          baseURL,
		APIKey:           apiKey,
		Model:            model,
		AnthropicVersion: "2023-06-01",
		MaxTokens:        8192,
		Timeout:          5 * time.Minute,
	}
}

// Client speaks to an Anthropic-compatible /v1/messages endpoint.
type Client struct {
	cfg  Config
	http *http.Client
}

// New creates a ready-to-use Client from the supplied Config.
func New(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
}

// ---------------------------------------------------------------------------
// Stream — SSE streaming request
// ---------------------------------------------------------------------------

// Stream sends the conversation to the model with stream:true and calls
// onToken for each text delta as it arrives. It returns the full accumulated
// reply text and any error.
func (c *Client) Stream(ctx context.Context, messages []Message, systemPrompt string, onToken func(string)) (string, error) {
	payload := requestPayload{
		Model:     c.cfg.Model,
		MaxTokens: c.cfg.MaxTokens,
		System:    systemPrompt,
		Messages:  messages,
		Stream:    true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.cfg.APIKey)
	req.Header.Set("anthropic-version", c.cfg.AnthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	// Non-2xx: read the body and return a clean error.
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var errResp struct {
			Error *apiError `json:"error"`
		}
		if json.Unmarshal(raw, &errResp) == nil && errResp.Error != nil {
			return "", errResp.Error
		}
		return "", fmt.Errorf("unexpected status %d: %s", resp.StatusCode, raw)
	}

	// Parse the SSE stream line by line.
	var accumulated strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE lines look like: "data: {...}" or "event: ..." or blank lines.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		jsonData := strings.TrimPrefix(line, "data: ")
		if jsonData == "[DONE]" {
			break
		}

		var ev streamEvent
		if err := json.Unmarshal([]byte(jsonData), &ev); err != nil {
			continue // skip malformed lines
		}

		if ev.Error != nil {
			return accumulated.String(), ev.Error
		}
		if ev.Type == "content_block_delta" && ev.Delta != nil && ev.Delta.Type == "text_delta" {
			delta := ev.Delta.Text
			accumulated.WriteString(delta)
			if onToken != nil {
				onToken(delta)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return accumulated.String(), fmt.Errorf("reading stream: %w", err)
	}

	return accumulated.String(), nil
}
