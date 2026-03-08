// Package chat manages a stateful conversation session.
package chat

import (
	"context"
	"fmt"

	"llm-chat/internal/anthropic"
)

// Session keeps track of the full conversation history and drives the
// request/response cycle.
type Session struct {
	client       *anthropic.Client
	history      []anthropic.Message
	systemPrompt string
}

// NewSession creates a Session backed by the given client.
// systemPrompt may be empty.
func NewSession(client *anthropic.Client, systemPrompt string) *Session {
	return &Session{
		client:       client,
		systemPrompt: systemPrompt,
	}
}

// Stream appends a plain-text user message to history, streams the model
// response token-by-token via onToken, then appends the full assistant reply
// to history. Returns an error on failure.
func (s *Session) Stream(ctx context.Context, userInput string, onToken func(string)) error {
	msg := anthropic.NewTextMessage(anthropic.RoleUser, userInput)
	s.history = append(s.history, msg)

	reply, err := s.client.Stream(ctx, s.history, s.systemPrompt, onToken)
	if err != nil {
		// Pop the user message so the caller can retry if desired.
		s.history = s.history[:len(s.history)-1]
		return fmt.Errorf("send: %w", err)
	}

	s.history = append(s.history, anthropic.NewTextMessage(anthropic.RoleAssistant, reply))
	return nil
}

// StreamWithImage appends a user message that contains an image (and an
// optional caption) to history, then streams the model response.
func (s *Session) StreamWithImage(ctx context.Context, imagePath, caption string, onToken func(string)) error {
	msg, err := anthropic.NewImageMessage(anthropic.RoleUser, imagePath, caption)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}
	s.history = append(s.history, msg)

	reply, err := s.client.Stream(ctx, s.history, s.systemPrompt, onToken)
	if err != nil {
		s.history = s.history[:len(s.history)-1]
		return fmt.Errorf("send: %w", err)
	}

	s.history = append(s.history, anthropic.NewTextMessage(anthropic.RoleAssistant, reply))
	return nil
}

// Reset clears the conversation history (but keeps the system prompt).
func (s *Session) Reset() {
	s.history = nil
}

// History returns a read-only copy of the current conversation turns.
func (s *Session) History() []anthropic.Message {
	out := make([]anthropic.Message, len(s.history))
	copy(out, s.history)
	return out
}
